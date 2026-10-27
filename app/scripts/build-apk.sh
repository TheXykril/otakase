#!/usr/bin/env bash
set -euo pipefail
app_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
"$app_dir/scripts/build-core.sh"
cd "$app_dir"
flutter pub get
flutter analyze
flutter test
# One phone + TV APK, with both ARM runtimes.
flutter build apk --release --target-platform android-arm,android-arm64
# Optional smaller downloads, built from exactly the same app.
flutter build apk --release --target-platform android-arm,android-arm64 --split-per-abi
python3 - <<'PY'
from pathlib import Path
for path in sorted(Path('build/app/outputs/flutter-apk').glob('*release.apk')):
    print(f'{path.name}: {path.stat().st_size} bytes ({path.stat().st_size/1024/1024:.2f} MiB)')
PY
