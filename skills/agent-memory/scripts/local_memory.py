#!/usr/bin/env python3
"""POSIX single-file Markdown CAS. All writers must honor the root-wide lock."""
import argparse
import contextlib
import fcntl
import hashlib
import json
import os
import secrets
import stat
from pathlib import Path


def revision(data):
    return hashlib.sha256(data).hexdigest()


@contextlib.contextmanager
def destination(root, relative):
    parts = relative.split('/')
    if not parts or any(p in ('', '.', '..') or '\\' in p or '\0' in p for p in parts):
        raise ValueError('path must be relative and confined to the configured root')
    if parts[0].startswith('.agent-memory-'):
        raise ValueError('reserved operational path')
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    root_fd = os.open('/', flags)
    try:
        for component in Path(os.path.abspath(root)).parts[1:]:
            child = os.open(component, flags, dir_fd=root_fd)
            os.close(root_fd)
            root_fd = child
    except BaseException:
        os.close(root_fd)
        raise
    lock_fd = None
    directory_fd = None
    try:
        lock_fd = os.open('.agent-memory-lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600, dir_fd=root_fd)
        info = os.fstat(lock_fd)
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.getuid():
            raise ValueError('unsafe lock file')
        fcntl.flock(lock_fd, fcntl.LOCK_EX)
        directory_fd = os.dup(root_fd)
        for part in parts[:-1]:
            child = os.open(part, flags, dir_fd=directory_fd)
            os.close(directory_fd)
            directory_fd = child
        yield directory_fd, parts[-1]
    finally:
        if directory_fd is not None:
            os.close(directory_fd)
        if lock_fd is not None:
            os.close(lock_fd)
        os.close(root_fd)


def read_bytes(directory_fd, name):
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory_fd)
    except FileNotFoundError:
        return None
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise ValueError('target must be a regular, non-hardlinked file')
        if info.st_size > 1024 * 1024:
            raise ValueError('local memory file exceeds 1 MiB')
        data = stream.read(1024 * 1024 + 1)
        if len(data) > 1024 * 1024:
            raise ValueError('local memory file exceeds 1 MiB')
        data.decode('utf-8')
        return data


def operate(root, path, expected=None, data=None):
    if data is not None:
        data.decode('utf-8')
        if len(data) > 1024 * 1024:
            raise ValueError('local memory file exceeds 1 MiB')
    with destination(root, path) as (directory_fd, name):
        current = read_bytes(directory_fd, name)
        actual = revision(current) if current is not None else 'missing'
        if data is None:
            return {'revision': actual, 'markdown': current.decode('utf-8') if current is not None else None}
        if expected != actual:
            raise ValueError('revision conflict; read current content and merge')
        if data == current:
            return {'revision': actual, 'status': 'unchanged', 'verified': True}
        temporary = '.agent-memory-' + secrets.token_hex(16)
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=directory_fd)
        try:
            with os.fdopen(fd, 'wb') as stream:
                stream.write(data)
                stream.flush()
                os.fsync(stream.fileno())
            # Re-check immediately before replacement as well as under the lock.
            latest = read_bytes(directory_fd, name)
            if (revision(latest) if latest is not None else 'missing') != expected:
                raise ValueError('revision conflict before replacement')
            os.replace(temporary, name, src_dir_fd=directory_fd, dst_dir_fd=directory_fd)
            try:
                os.fsync(directory_fd)
            except OSError as error:
                raise ValueError('completion unknown after replacement; inspect target before retrying') from error
        finally:
            try:
                os.unlink(temporary, dir_fd=directory_fd)
            except FileNotFoundError:
                pass
        verified = read_bytes(directory_fd, name)
        if verified != data:
            raise ValueError('completion unknown: replacement could not be verified; inspect target')
        return {'revision': revision(data), 'status': 'created' if current is None else 'updated', 'verified': True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=['read', 'write'])
    parser.add_argument('--root', required=True)
    parser.add_argument('--path', required=True)
    parser.add_argument('--revision')
    parser.add_argument('--input', type=Path)
    args = parser.parse_args()
    try:
        if args.operation == 'write' and (not args.revision or args.input is None):
            raise ValueError('write requires --revision and --input')
        data = args.input.read_bytes() if args.operation == 'write' else None
        print(json.dumps(operate(args.root, args.path, args.revision, data), ensure_ascii=False))
    except (OSError, ValueError) as error:
        parser.exit(1, json.dumps({'error': str(error)}, ensure_ascii=False) + '\n')


if __name__ == '__main__':
    main()
