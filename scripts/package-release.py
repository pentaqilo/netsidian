"""Package an already built native executable for a tagged GitHub release."""
import argparse
import pathlib
import plistlib
import shutil
import subprocess
import tarfile
import zipfile

parser = argparse.ArgumentParser()
parser.add_argument('--version', required=True)
parser.add_argument('--platform', choices=['linux', 'windows', 'macos'], required=True)
parser.add_argument('--arch', choices=['amd64', 'arm64'], required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
name = f'netsidian-{args.version}-{args.platform}-{args.arch}'
stage = root / 'release-stage' / name
stage.mkdir(parents=True, exist_ok=True)
out = root / 'release-assets'
out.mkdir(exist_ok=True)
ext = '.exe' if args.platform == 'windows' else ''
binary = root / 'bin' / f'netsidian{ext}'
if args.platform == 'macos':
    contents = stage / 'Netsidian.app' / 'Contents'
    (contents / 'MacOS').mkdir(parents=True)
    (contents / 'Resources').mkdir()
    destination = contents / 'MacOS' / 'netsidian'
    shutil.copy2(binary, destination)
    destination.chmod(0o755)
    with (contents / 'Info.plist').open('wb') as file:
        plistlib.dump({
            'CFBundleName': 'Netsidian', 'CFBundleDisplayName': 'Netsidian',
            'CFBundleIdentifier': 'io.github.pentaqilo.netsidian',
            'CFBundleExecutable': 'netsidian', 'CFBundlePackageType': 'APPL',
            'CFBundleShortVersionString': args.version.lstrip('v'),
            'CFBundleVersion': args.version.lstrip('v'),
            'NSHighResolutionCapable': True, 'LSMinimumSystemVersion': '11.0',
        }, file)
    subprocess.run(['codesign', '--force', '--deep', '--sign', '-', str(stage / 'Netsidian.app')], check=True)
else:
    shutil.copy2(binary, stage / f'netsidian{ext}')
    if args.platform == 'linux':
        (stage / 'netsidian').chmod(0o755)
for filename in ('LICENSE', 'README.md'):
    shutil.copy2(root / filename, stage / filename)
(stage / 'INSTALL.txt').write_text(
    'Netsidian desktop release ' + args.version + '\n\n' + {
        'linux': 'Install GTK4 and WebKitGTK 6.0 runtime libraries (Ubuntu: sudo apt install libgtk-4-1 libwebkitgtk-6.0-4), then run ./netsidian.\n',
        'windows': 'Extract the archive, then run netsidian.exe. Microsoft Edge WebView2 Runtime is required.\n',
        'macos': 'Move Netsidian.app to Applications. This release is ad-hoc signed, not Apple notarized; macOS may require approval in System Settings > Privacy & Security.\n',
    }[args.platform] + '\nMCP: Settings > MCP & agents. User data stays outside the application bundle.\n',
    encoding='utf-8')
if args.platform == 'linux':
    with tarfile.open(out / f'{name}.tar.gz', 'w:gz') as archive:
        archive.add(stage, arcname=name)
elif args.platform == 'macos':
    subprocess.run(['ditto', '-c', '-k', '--sequesterRsrc', '--keepParent', str(stage), str(out / f'{name}.zip')], check=True)
else:
    with zipfile.ZipFile(out / f'{name}.zip', 'w', zipfile.ZIP_DEFLATED) as archive:
        for path in stage.rglob('*'):
            if path.is_file():
                archive.write(path, path.relative_to(stage.parent))
