#!/usr/bin/env bash
set -euo pipefail
app_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
"$app_dir/scripts/build-core.sh"
cd "$app_dir"
flutter pub get
flutter analyze
flutter test
# The app carries the CLI's version (VERSION.txt, or OTAKASE_VERSION when set).
# Android only installs an update with an equal or higher versionCode, so the
# code is the version as one number: 26.7.0 -> 26007000.
version=${OTAKASE_VERSION:-$(tr -d '[:space:]' < "$app_dir/../VERSION.txt")}
IFS=. read -r year drop hotfix <<< "${version%%-*}"
code=$((10#$year * 1000000 + 10#$drop * 1000 + 10#${hotfix:-0}))
build_args=(--release --target-platform android-arm,android-arm64
  --build-name "$version" --build-number "$code" --dart-define "OTAKASE_VERSION=$version")
echo "Otakase app $version (versionCode $code)"
# One phone + TV APK, with both ARM runtimes.
flutter build apk "${build_args[@]}"
# Optional smaller downloads, built from exactly the same app.
flutter build apk "${build_args[@]}" --split-per-abi
python3 - <<'PY'
from pathlib import Path
for path in sorted(Path('build/app/outputs/flutter-apk').glob('*release.apk')):
    print(f'{path.name}: {path.stat().st_size} bytes ({path.stat().st_size/1024/1024:.2f} MiB)')
PY
