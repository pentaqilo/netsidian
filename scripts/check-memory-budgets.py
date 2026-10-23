#!/usr/bin/env python3
"""Measure rendered instructions offline after tokenizer/cache setup.
Install scripts/memory-tokenizer-requirements.txt once; TIKTOKEN_CACHE_DIR selects
an existing vocabulary cache. Without tiktoken, explicit byte checks still run.
"""
import argparse
import json
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--require-tokenizer', action='store_true')
parser.add_argument('--output')
parser.add_argument('--catalog', help='JSON from TestMemoryCatalogBudget via NETSIDIAN_TEST_CATALOG_OUTPUT')
parser.add_argument('--workflow', help='JSON from an isolated transport workflow fixture')
args = parser.parse_args()
try:
    import tiktoken
    if tiktoken.__version__ != '0.12.0':
        raise SystemExit('Expected tiktoken 0.12.0')
    encoding = tiktoken.get_encoding('cl100k_base')
except ImportError:
    if args.require_tokenizer:
        raise SystemExit('Install scripts/memory-tokenizer-requirements.txt')
    encoding = None
assets = ROOT / 'skills/agent-memory'
surfaces = {
    'core': ((assets / 'SKILL.md').read_text(), 700, 4300),
    'server-guidance': ((assets / 'references/server-guidance.md').read_text(), 200, 1200),
}
for provider in ['local', 'remote', 'netsidian']:
    surfaces['adapter-' + provider] = ((assets / ('references/' + provider + '.md')).read_text(), 600, 3200)
rendered = json.loads(subprocess.check_output(['node', 'scripts/render-memory-policy.mjs'], cwd=ROOT))
for key, value in rendered.items():
    surfaces['single-' + key] = (value, 1200, 7000)
report = {'tokenizer': 'tiktoken==0.12.0/cl100k_base' if encoding else 'unavailable; byte checks only', 'surfaces': {}}
errors = []
for name, (text, ceiling, byte_ceiling) in surfaces.items():
    tokens = len(encoding.encode(text)) if encoding else None
    size = len(text.encode())
    report['surfaces'][name] = {'utf8Bytes': size, 'tokens': tokens, 'tokenCeiling': ceiling, 'byteCeiling': byte_ceiling}
    if size > byte_ceiling or (tokens is not None and tokens > ceiling):
        errors.append(name + ' exceeds instruction budget')
# Full examples/schema/helper are intentionally absent from copied skills.
for name, text in rendered.items():
    if 'def operate(' in text or '# Memory examples' in text or '](' + 'references/' in text:
        errors.append(name + ' preloads exceptional references or has unresolved local links')
if args.catalog:
    catalog = json.loads(Path(args.catalog).read_text())
    baseline = json.loads((ROOT / 'scripts/memory-context-baseline.json').read_text())
    report['catalog'] = {}
    for name, text in [('tools', json.dumps(catalog['tools'], ensure_ascii=False, separators=(',', ':'))), ('instructions', catalog['instructions'])]:
        size = len(text.encode())
        count = len(encoding.encode(text)) if encoding else None
        report['catalog'][name] = {'utf8Bytes': size, 'tokens': count, 'baseline': baseline[name]}
        if name == 'tools' and size > baseline['catalogRegressionByteCeiling']:
            errors.append('full tool catalog exceeds regression ceiling')
if args.workflow:
    records = json.loads(Path(args.workflow).read_text())
    serialized = [json.dumps(row['result'], ensure_ascii=False, separators=(',', ':')) for row in records]
    report['workflow'] = {'calls': len(records), 'serializedResultBytes': sum(len(text.encode()) for text in serialized), 'serializedResultTokens': sum(len(encoding.encode(text)) for text in serialized) if encoding else None, 'elapsedMicrosSingleObservation': sum(row.get('elapsedMicros', 0) for row in records)}
if args.output:
    Path(args.output).write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps(report, indent=2))
if errors:
    raise SystemExit('\n'.join(errors))
