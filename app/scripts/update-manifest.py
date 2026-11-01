#!/usr/bin/env python3
"""Write update metadata for an app release file.

The universal Android APK (--apk) or a desktop download (--file with
--asset, the name it has on the release). The app reads this file from the
release to learn the version, size and checksum of what it downloads.
"""

import argparse
import hashlib
import json
from pathlib import Path


def manifest(path: Path, name: str, code: int, asset: str = "otakase-android.apk", key: str = "apk") -> dict:
    if not name or len(name) > 80 or not 1 <= code <= 2_100_000_000:
        raise ValueError("Invalid app version")
    size = path.stat().st_size
    if not 0 < size <= 512 * 1024 * 1024:
        raise ValueError("Invalid release file size")
    with path.open("rb") as source:
        checksum = hashlib.file_digest(source, "sha256").hexdigest()
    return {
        "schema": 1,
        "package_name": "com.xyverion.otakase",
        "version_name": name,
        "version_code": code,
        key: asset,
        "size": size,
        "sha256": checksum,
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--apk", type=Path)
    source.add_argument("--file", type=Path)
    parser.add_argument("--asset", help="release file name, with --file")
    parser.add_argument("--name", required=True)
    parser.add_argument("--code", type=int, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if args.file and not args.asset:
        parser.error("--file needs --asset")
    if args.apk:
        data = manifest(args.apk, args.name, args.code)
    else:
        data = manifest(args.file, args.name, args.code, args.asset, "file")
    args.output.write_text(json.dumps(data, indent=2) + "\n")
