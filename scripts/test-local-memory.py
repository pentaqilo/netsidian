#!/usr/bin/env python3
import importlib.util
import json
import subprocess
import sys
sys.dont_write_bytecode = True
import tempfile
import unittest
from pathlib import Path

HELPER = Path(__file__).resolve().parents[1] / 'skills/agent-memory/scripts/local_memory.py'
spec = importlib.util.spec_from_file_location('local_memory', HELPER)
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)


class LocalMemoryTest(unittest.TestCase):
    def test_two_process_cas_and_verbatim(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = ('Najjaśniejszy Panie: "dokładnie" `tak`\n' * 40).encode()
            first = helper.operate(root, 'index.md', 'missing', text)
            self.assertTrue(first['verified'])
            self.assertEqual(helper.operate(root, 'index.md')['markdown'].encode(), text)
            paths = []
            for i in range(2):
                file = root / f'input-{i}.md'
                file.write_bytes(text + str(i).encode())
                paths.append(file)
            processes = [subprocess.Popen([sys.executable, str(HELPER), 'write', '--root', str(root), '--path', 'index.md', '--revision', first['revision'], '--input', str(file)], stdout=subprocess.PIPE, stderr=subprocess.PIPE) for file in paths]
            outputs = [p.communicate() for p in processes]
            self.assertEqual(sorted(p.returncode for p in processes), [0, 1])
            self.assertTrue(any(b'revision conflict' in err for _, err in outputs))
            current = helper.operate(root, 'index.md')
            self.assertIn(current['markdown'].encode(), [p.read_bytes() for p in paths])
            self.assertEqual(helper.operate(root, 'index.md', current['revision'], current['markdown'].encode())['status'], 'unchanged')

    def test_root_confinement(self):
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as other:
            root = Path(tmp)
            outside = Path(other)
            (outside / 'private.md').write_text('secret')
            (root / 'escape').symlink_to(outside, target_is_directory=True)
            (root / 'link.md').symlink_to(outside / 'private.md')
            for path in ['../private.md', '/private.md', 'escape/private.md', 'link.md', '.agent-memory-lock', 'a//b.md']:
                with self.subTest(path=path), self.assertRaises((OSError, ValueError)):
                    helper.operate(root, path, 'missing', b'overwrite')
            self.assertEqual((outside / 'private.md').read_text(), 'secret')
            with self.assertRaises(OSError):
                helper.operate(root / 'escape', 'private.md')
            (outside / 'nested').mkdir()
            with self.assertRaises(OSError):
                helper.operate(root / 'escape/nested', 'private.md')

    def test_special_targets_and_invalid_encoding(self):
        import os
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            os.mkfifo(root / 'pipe.md')
            with self.assertRaises(ValueError):
                helper.operate(root, 'pipe.md')
            with self.assertRaises(UnicodeDecodeError):
                helper.operate(root, 'index.md', 'missing', b'\xff')


if __name__ == '__main__':
    unittest.main()
