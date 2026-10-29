#!/usr/bin/env python3
"""Write update metadata for the universal Android APK."""

import argparse
import hashlib
import json
from pathlib import Path


def manifest(apk: Path, name: str, code: int) -> dict:
    if not name or len(name) > 80 or not 1 <= code <= 2_100_000_000:
        raise ValueError("Invalid Android app version")
    size = apk.stat().st_size
    if not 0 < size <= 512 * 1024 * 1024:
        raise ValueError("Invalid Android APK size")
    with apk.open("rb") as source:
        checksum = hashlib.file_digest(source, "sha256").hexdigest()
    return {
        "schema": 1,
        "package_name": "com.xyverion.otakase",
        "version_name": name,
        "version_code": code,
        "apk": "otakase-android.apk",
        "size": size,
        "sha256": checksum,
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apk", type=Path, required=True)
    parser.add_argument("--name", required=True)
    parser.add_argument("--code", type=int, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    args.output.write_text(json.dumps(manifest(args.apk, args.name, args.code), indent=2) + "\n")
