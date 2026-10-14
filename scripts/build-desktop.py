#!/usr/bin/env python3
"""Build and package the current desktop source without changing build outputs."""
import argparse
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def run(command, cwd=None, env=None, capture=False):
    return subprocess.run(command, cwd=cwd, env=env, check=True, text=True,
                          stdout=subprocess.PIPE if capture else None).stdout


def require(command):
    path = shutil.which(command)
    if not path:
        raise RuntimeError(f'{command} is required')
    return path


def host_ready(target):
    if not shutil.which('go'):
        return False
    if target == 'windows':
        return True
    if target == 'macos':
        return shutil.which('xcrun') is not None and subprocess.run(
            ['xcrun', '--find', 'clang'], stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL).returncode == 0
    return all(shutil.which(c) for c in ('gcc', 'pkg-config')) and subprocess.run(
        ['pkg-config', '--exists', 'gtk4', 'webkitgtk-6.0'],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0


def copy_source(destination):
    # Copy build inputs from disk, including edits and new source files. Runtime
    # data, credentials, dependencies and previous outputs never enter the tree.
    for name in ('go.mod', 'go.sum', 'main.go', 'LICENSE', 'README.md',
                 'Dockerfile.desktop-build'):
        shutil.copy2(ROOT / name, destination / name)
    for name in ('internal', 'frontend', 'scripts'):
        shutil.copytree(ROOT / name, destination / name, ignore=shutil.ignore_patterns(
            'node_modules', 'dist', '__pycache__', '.env', '.env.*', '*.log',
            '*.db', '*.sqlite', '*.sqlite3', '*.state', '*.pyc'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--platform', choices=('linux', 'macos', 'windows'), required=True)
    parser.add_argument('--mode', choices=('auto', 'host', 'container'), default='auto')
    parser.add_argument('--arch', choices=('amd64', 'arm64'))
    parser.add_argument('--version', default='0.0.0', help='package version (default: 0.0.0)')
    parser.add_argument('--output', type=Path, help='new package directory; archive is placed beside it')
    args = parser.parse_args()
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?', args.version):
        parser.error('--version must be a version such as 0.1.0 or 0.1.0-dev')
    system = platform.system()
    if args.platform == 'linux' and system != 'Linux':
        parser.error('Linux builds require a Linux host')
    if args.platform == 'macos' and system != 'Darwin':
        parser.error('macOS builds require macOS and Xcode command line tools')
    architecture = args.arch or {'x86_64': 'amd64', 'AMD64': 'amd64',
                                'aarch64': 'arm64', 'arm64': 'arm64'}.get(platform.machine())
    if not architecture:
        parser.error('unknown host architecture; supply --arch')
    if args.mode == 'container' and args.platform != 'linux':
        parser.error('container mode is available only for Linux')
    ready = host_ready(args.platform)
    mode = ('host' if ready else 'container') if args.mode == 'auto' and args.platform == 'linux' else args.mode
    if mode == 'auto':
        mode = 'host'
    if mode == 'host' and not ready:
        raise RuntimeError('Host build needs Go; Linux also needs gcc, pkg-config, GTK4 and WebKitGTK 6.0; macOS needs Xcode command line tools')
    node = require('node')
    npm = require('npm')
    run([node, '-e', 'const [a,b]=process.versions.node.split(".").map(Number);process.exit(a>22||a===22&&b>=12?0:1)'])
    if mode == 'container':
        require('docker')
        run(['docker', 'info'], capture=True)
    if args.platform == 'macos':
        require('codesign')
        require('ditto')
    name = f'netsidian-{args.version}-{args.platform}-{architecture}'
    output = (args.output or ROOT / 'dist' / name).absolute()
    suffix = '.tar.gz' if args.platform == 'linux' else '.zip'
    archive_output = Path(str(output) + suffix)
    # Never replace directories or archives supplied by the caller.
    if output.exists() or output.is_symlink() or archive_output.exists() or archive_output.is_symlink():
        raise RuntimeError(f'Output already exists: {output} or {archive_output}; choose a new --output')
    print(f'Building desktop: {args.platform}/{architecture} ({mode})', flush=True)
    with tempfile.TemporaryDirectory(prefix='netsidian-desktop-') as temporary:
        build = Path(temporary)
        copy_source(build)
        frontend = build / 'frontend'
        run([npm, 'ci'], cwd=frontend)
        run([npm, 'run', 'build'], cwd=frontend)
        if not (frontend / 'dist' / 'index.html').is_file():
            raise RuntimeError('Frontend build did not produce index.html')
        # Native modules are not needed by the compiler or packager.
        shutil.rmtree(frontend / 'node_modules')
        (build / 'bin').mkdir()
        binary = 'netsidian.exe' if args.platform == 'windows' else 'netsidian'
        command = ['go', 'build', '-trimpath', '-tags', 'production',
                   '-ldflags=-s -w', '-o', f'bin/{binary}', '.']
        env = dict(os.environ, GOOS={'macos': 'darwin'}.get(args.platform, args.platform),
                   GOARCH=architecture, CGO_ENABLED='0' if args.platform == 'windows' else '1')
        if mode == 'host':
            command[0] = require('go')
            run(command, cwd=build, env=env)
        else:
            image = f'netsidian-desktop-builder:{architecture}'
            run(['docker', 'build', '--platform', f'linux/{architecture}',
                 '-f', str(build / 'Dockerfile.desktop-build'), '-t', image, str(build)])
            label = ''
            if shutil.which('getenforce'):
                if run(['getenforce'], capture=True).strip().lower() in ('enforcing', 'permissive'):
                    label = ':z'
            run(['docker', 'run', '--rm', '--platform', f'linux/{architecture}',
                 '--user', f'{os.getuid()}:{os.getgid()}', '-e', 'HOME=/tmp',
                 '-e', 'GOCACHE=/tmp/go-build', '-e', 'GOMODCACHE=/tmp/go-mod',
                 '-e', 'CGO_ENABLED=1', '-e', 'GOOS=linux',
                 '-e', f'GOARCH={architecture}', '-v', f'{build}:/build{label}',
                 '-w', '/build', image, *command])
        run([sys.executable, str(build / 'scripts' / 'package-release.py'),
             '--version', args.version, '--platform', args.platform, '--arch', architecture], cwd=build)
        output.parent.mkdir(parents=True, exist_ok=True)
        shutil.copytree(build / 'release-stage' / name, output)
        shutil.copy2(build / 'release-assets' / (name + suffix), archive_output)
    print(f'Package: {output}\nArchive: {archive_output}')


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, subprocess.CalledProcessError, OSError) as error:
        print(f'build-desktop: {error}', file=sys.stderr)
        sys.exit(1)
