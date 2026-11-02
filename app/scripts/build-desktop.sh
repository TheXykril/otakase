#!/usr/bin/env bash
# Builds the desktop app for the host OS (linux, windows or macos) and puts
# the otakase-core helper where the app looks for it.
#
#   scripts/build-desktop.sh [release|debug]
#
# Flutter cannot cross-compile desktop apps, so run this on the OS you build
# for. Linux needs clang, cmake, ninja, GTK 3, libsecret and libmpv headers.
set -euo pipefail
cd "$(dirname "$0")/.."

mode=${1:-release}
case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=macos ;;
  MINGW* | MSYS* | CYGWIN*) os=windows ;;
  *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

# Same version and build number as the Android app (see build-apk.sh); the
# in-app updater compares them with the release's.
version=${OTAKASE_VERSION:-$(tr -d '[:space:]' < ../VERSION.txt)}
IFS=. read -r year drop hotfix <<< "${version%%-*}"
code=$((10#$year * 1000000 + 10#$drop * 1000 + 10#${hotfix:-0}))

flutter pub get
flutter build "$os" "--$mode" --build-name "$version" --build-number "$code" \
  --dart-define "OTAKASE_VERSION=$version"

core=otakase-core
[[ $os == windows ]] && core=otakase-core.exe
# Capitalised folder name (Release, Debug); macOS ships bash 3.2, which has
# no ${mode^}.
mode_dir=$(printf '%s' "${mode:0:1}" | tr '[:lower:]' '[:upper:]')${mode:1}

case $os in
  linux)
    arch=$(uname -m)
    [[ $arch == x86_64 ]] && arch=x64
    [[ $arch == aarch64 ]] && arch=arm64
    dest=build/linux/$arch/$mode/bundle
    ;;
  windows)
    dest=build/windows/x64/runner/$mode_dir
    ;;
  macos)
    dest=build/macos/Build/Products/$mode_dir/Otakase.app/Contents/Resources
    ;;
esac

build_core() {
  (cd .. && CGO_ENABLED=0 GOARCH=${1:-} go build -trimpath -ldflags="-s -w" \
    -o "app/$2" ./cmd/otakase-core)
}

if [[ $os == macos ]]; then
  # The app is universal, so the helper is too.
  build_core arm64 "build/otakase-core-arm64"
  build_core amd64 "build/otakase-core-amd64"
  lipo -create -output "$dest/$core" build/otakase-core-arm64 build/otakase-core-amd64
  rm build/otakase-core-arm64 build/otakase-core-amd64
else
  build_core "" "$dest/$core"
fi

echo "built Otakase $version ($code) in $dest with $core"
