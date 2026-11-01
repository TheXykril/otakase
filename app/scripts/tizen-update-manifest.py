#!/usr/bin/env python3
"""Writes otakase-samsung-tv.json, the in-app update manifest for Samsung
TVs (see mobilecore/tvupdate). Run by scripts/build-tpk.sh."""
import argparse
import hashlib
import json
import pathlib
import zipfile

REPO = 'https://github.com/TheXykril/otakase/releases/download'


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--out', required=True, type=pathlib.Path)
    p.add_argument('--version', required=True)
    p.add_argument('--code', required=True, type=int)
    p.add_argument('--apis', required=True, nargs='+')
    a = p.parse_args()
    bundles = {}
    for api in a.apis:
        name = f'otakase-samsung-tv-brain-tizen{api}.zip'
        path = a.out / name
        with zipfile.ZipFile(path) as z:
            shell = z.read('shell-id').decode().strip()
        bundles[api] = {
            'url': f'{REPO}/v{a.version}/{name}',
            'sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
            'size': path.stat().st_size,
            'shell': shell,
        }
    print(json.dumps({'version': a.version, 'code': a.code, 'bundles': bundles},
                     indent=2, sort_keys=True))


if __name__ == '__main__':
    main()
