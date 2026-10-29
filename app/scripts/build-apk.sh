#!/usr/bin/env bash
set -euo pipefail
app_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
"$app_dir/scripts/build-core.sh"
cd "$app_dir"
flutter pub get
flutter analyze
flutter test
python3 -m unittest discover -s scripts -p '*_test.py'
app_version=$(sed -n 's/^version: //p' pubspec.yaml)
android_name=${app_version%%+*}
android_code=${OTAKASE_ANDROID_BUILD_NUMBER:-${app_version##*+}}
# One phone + TV APK, with both ARM runtimes.
flutter build apk --release --target-platform android-arm,android-arm64 \
  --build-name="$android_name" --build-number="$android_code"
python3 scripts/update-manifest.py \
  --apk build/app/outputs/flutter-apk/app-release.apk \
  --name "$android_name" --code "$android_code" \
  --output build/app/outputs/flutter-apk/otakase-android.json
# Optional smaller downloads, built from exactly the same app.
flutter build apk --release --target-platform android-arm,android-arm64 --split-per-abi \
  --build-name="$android_name" --build-number="$android_code"
python3 - <<'PY'
from pathlib import Path
for path in sorted(Path('build/app/outputs/flutter-apk').glob('*release.apk')):
    print(f'{path.name}: {path.stat().st_size} bytes ({path.stat().st_size/1024/1024:.2f} MiB)')
PY
