#!/usr/bin/env python3
"""Prints the Samsung TV shell id for scripts/build-tpk.sh.

An in-app update replaces the Dart code and Go core but keeps the installed
package (the "shell"): flutter-tizen's engine, the C# runner and native
plugin libraries. The id hashes everything an update depends on, so a TV
only accepts updates its shell can run.

    tizen-shell-id.py <flutter-tizen revision> <api-version>
"""
import hashlib
import json
import pathlib
import sys
from urllib.parse import urlparse
from urllib.request import url2pathname

app = pathlib.Path(__file__).resolve().parent.parent


def native_plugins():
    config = json.loads((app / '.dart_tool' / 'package_config.json').read_text())
    for package in config['packages']:
        uri = package['rootUri']
        if uri.startswith('file:'):
            root = pathlib.Path(url2pathname(urlparse(uri).path))
        else:
            root = (app / '.dart_tool' / uri).resolve()
        if (root / 'tizen').is_dir():
            yield package['name'], root


def main():
    revision, api = sys.argv[1], sys.argv[2]
    digest = hashlib.sha256()
    digest.update(f'flutter-tizen {revision}\napi {api}\n'.encode())
    for path in sorted((app / 'tizen').glob('*.cs')) + [app / 'tizen' / 'Runner.csproj']:
        digest.update(f'{path.name}\n'.encode() + path.read_bytes())
    for name, root in sorted(native_plugins()):
        digest.update(f'plugin {name}\n'.encode())
        for path in sorted((root / 'tizen').rglob('*')):
            if path.is_file():
                digest.update(str(path.relative_to(root)).encode() + b'\n' + path.read_bytes())
    print(digest.hexdigest()[:32])


if __name__ == '__main__':
    main()
