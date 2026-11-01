#!/usr/bin/env bash
# Installs what scripts/build-tpk.sh needs on 64-bit Linux (Ubuntu 22.04+):
# Tizen Studio CLI with the TV native rootstraps, and flutter-tizen.
# The .NET 8 SDK and Go must already be installed. Run as a normal user.
#
#   scripts/setup-tizen-sdk.sh [install-dir]   (default: ~/tizen-studio)
#
# Prints the PATH and TIZEN_SDK lines to use afterwards.
set -euo pipefail
tizen_sdk=${1:-$HOME/tizen-studio}
flutter_tizen=${FLUTTER_TIZEN_DIR:-$HOME/flutter-tizen}
# flutter-tizen pins its own Flutter (3.47.5 at this commit).
flutter_tizen_rev=22bcec9058fbe1e0943e6c6fda21792f24868e91
studio=6.1

# The Tizen package manager checks for desktop packages the CLI never uses,
# and the bundled clang still links libtinfo.so.5.
missing=()
for pkg in python3.8 libpython3.8 libkf5itemmodels5 libkf5kiowidgets5 libkchart2; do
  dpkg -s "$pkg" > /dev/null 2>&1 || missing+=("$pkg")
done
if ((${#missing[@]})) || [[ ! -e /usr/lib/x86_64-linux-gnu/libtinfo.so.5 ]]; then
  work=$(mktemp -d)
  for pkg in "${missing[@]}"; do
    mkdir -p "$work/$pkg/DEBIAN"
    printf 'Package: %s\nVersion: 0\nArchitecture: all\nMaintainer: none <none@localhost>\nDescription: placeholder for the Tizen package manager\n' \
      "$pkg" > "$work/$pkg/DEBIAN/control"
    dpkg-deb -b "$work/$pkg" "$work/$pkg.deb" > /dev/null
    sudo dpkg -i "$work/$pkg.deb" > /dev/null
  done
  if [[ ! -e /usr/lib/x86_64-linux-gnu/libtinfo.so.5 ]]; then
    curl -fsSL -o "$work/libtinfo5.deb" \
      http://archive.ubuntu.com/ubuntu/pool/universe/n/ncurses/libtinfo5_6.3-2ubuntu0.3_amd64.deb
    sudo dpkg -i "$work/libtinfo5.deb" > /dev/null
  fi
  rm -rf "$work"
fi

if [[ ! -x $tizen_sdk/package-manager/package-manager-cli.bin ]]; then
  installer=$(mktemp)
  curl -fsSL -o "$installer" \
    "https://download.tizen.org/sdk/Installer/tizen-studio_$studio/web-cli_Tizen_Studio_${studio}_ubuntu-64.bin"
  chmod +x "$installer"
  "$installer" --accept-license "$tizen_sdk"
  rm -f "$installer"
fi
# The package manager reports a failure for unreachable extension mirrors
# even when every package installed, so check the results instead.
"$tizen_sdk/package-manager/package-manager-cli.bin" install --accept-license \
  NativeCLI Certificate-Manager NativeToolchain-Gcc-9.2 \
  IOT-Headed-6.0-NativeAppDevelopment-CLI IOT-Headed-6.5-NativeAppDevelopment-CLI \
  TIZEN-10.0-NativeAppDevelopment-CLI || true
for path in \
  tools/tizen-core/tz \
  tools/arm-linux-gnueabi-gcc-9.2/bin/arm-linux-gnueabi-gcc \
  platforms/tizen-6.0/iot-headed/rootstraps/iot-headed-6.0-device.core \
  platforms/tizen-6.5/iot-headed/rootstraps/iot-headed-6.5-device.core \
  platforms/tizen-10.0/tizen/rootstraps; do
  if [[ ! -e $tizen_sdk/$path ]]; then
    echo "Tizen SDK install incomplete: $tizen_sdk/$path is missing." >&2
    exit 1
  fi
done

if [[ ! -d $flutter_tizen/.git ]]; then
  git clone https://github.com/flutter-tizen/flutter-tizen.git "$flutter_tizen"
fi
git -C "$flutter_tizen" fetch --quiet origin "$flutter_tizen_rev"
git -C "$flutter_tizen" checkout --quiet "$flutter_tizen_rev"
"$flutter_tizen/bin/flutter-tizen" --version > /dev/null

echo "export TIZEN_SDK=$tizen_sdk"
echo "export PATH=$flutter_tizen/bin:$tizen_sdk/tools:$tizen_sdk/tools/ide/bin:\$PATH"
