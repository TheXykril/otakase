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

flutter pub get
flutter build "$os" "--$mode"

core=otakase-core
[[ $os == windows ]] && core=otakase-core.exe
mode_dir=${mode^}

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

(cd .. && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
  -o "app/$dest/$core" ./cmd/otakase-core)

echo "built $dest with $core"
