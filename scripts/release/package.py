#!/usr/bin/env python3
"""Package a native executable; deterministic public asset names."""
import argparse
import hashlib
from pathlib import Path
import tarfile
import zipfile

parser = argparse.ArgumentParser()
parser.add_argument('--os', required=True, choices=['linux', 'darwin', 'windows'])
parser.add_argument('--arch', required=True, choices=['amd64', 'arm64'])
parser.add_argument('--binary', required=True, type=Path)
parser.add_argument('--out', default='release', type=Path)
parser.add_argument('--version', required=True)
args = parser.parse_args()
args.out.mkdir(parents=True, exist_ok=True)
name = 'excalibur.exe' if args.os == 'windows' else 'excalibur'
archive = args.out / f'excalibur_{args.os}_{args.arch}'
if args.os == 'windows':
    archive = archive.with_suffix('.zip')
    with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as writer:
        writer.write(args.binary, name)
else:
    archive = archive.with_suffix('.tar.gz')
    with tarfile.open(archive, 'w:gz') as writer:
        writer.add(args.binary, name)
(args.out / 'version.txt').write_text(args.version + '\n', encoding='utf-8', newline='\n')
with (args.out / 'checksums.txt').open('w', encoding='utf-8', newline='\n') as checksums:
    for path in sorted([archive, args.out / 'version.txt']):
        checksums.write(f'{hashlib.file_digest(path.open("rb"), "sha256").hexdigest()}  {path.name}\n')
