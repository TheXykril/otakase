#!/usr/bin/env bash
# Builds the Samsung TV (Tizen) app: one TPK per TizenOS group, because the
# TV video plugin ships different native code for each group.
#
#   6.0  -> 2021 TVs        6.5 -> 2022-2025 TVs (Tizen 6.5-9.0)
#   10.0 -> 2026 TVs
#
# Needs flutter-tizen on PATH, Tizen Studio CLI (TIZEN_SDK, default
# ~/tizen-studio) with the IOT-Headed-6.0 native rootstrap and the Gcc 9.2
# toolchain, the .NET SDK, and Go.
#
# TIZEN_EMULATOR=1 builds one x86_64 TPK (Tizen 10.0) for the Samsung TV
# emulator from Tizen Studio's TV Extensions, into build/samsung-tv-emulator.
#
# Signing uses the active Tizen Studio security profile. Without one, set
# TIZEN_AUTHOR_P12 + TIZEN_AUTHOR_PASSWORD to an author certificate; the
# public Tizen distributor certificate is added. On machines without a
# desktop keyring (CI), the passwords are passed to the .NET build directly.
#
# In-app updates: next to each TPK goes otakase-samsung-tv-brain-tizen<api>.zip
# (the Dart code and Go core for that shell) and one otakase-samsung-tv.json
# manifest. With TV_UPDATE_KEY set to the ed25519 private key (PEM file) the
# manifest is signed (.json.sig); TVs only install signed updates.
set -euo pipefail
app_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$app_dir"
tizen_sdk=${TIZEN_SDK:-$HOME/tizen-studio}
version=${OTAKASE_VERSION:-$(tr -d '[:space:]' < "$app_dir/../VERSION.txt")}
tpk_version=${version%%-*}
apis=${TIZEN_API_VERSIONS:-6.0 6.5 10.0}
out="$app_dir/build/samsung-tv"

if [[ -n ${TIZEN_EMULATOR:-} ]]; then
  apis=10.0
  out="$app_dir/build/samsung-tv-emulator"
  arch_flags=(--target-arch x64)
  goarch=(GOARCH=amd64)
  rootstrap="$tizen_sdk/platforms/tizen-10.0/tizen/rootstraps/tizen-10.0-emulator64.core"
  # Tizen's gcc 9.2 linker can't read the 10.0 rootstrap's packed relocations;
  # the host's x86_64 gcc links against the same sysroot instead.
  gcc=${TIZEN_EMULATOR_CC:-/usr/bin/gcc}
  host=x86_64-linux-gnu
else
  # Go core for 32-bit ARM, the userland of every Samsung TV. Linking against
  # the oldest rootstrap keeps the library loadable on all TizenOS versions.
  arch_flags=()
  goarch=(GOARCH=arm GOARM=7)
  rootstrap="$tizen_sdk/platforms/tizen-6.0/iot-headed/rootstraps/iot-headed-6.0-device.core"
  gcc="$tizen_sdk/tools/arm-linux-gnueabi-gcc-9.2/bin/arm-linux-gnueabi-gcc"
  host=arm-linux-gnueabi
fi
if [[ ! -d $rootstrap || ! -x $gcc ]]; then
  echo "Missing $rootstrap or $gcc; run scripts/setup-tizen-sdk.sh." >&2
  exit 1
fi
mkdir -p tizen/lib
(cd .. && env CGO_ENABLED=1 GOOS=linux "${goarch[@]}" \
  CC="$gcc --sysroot=$rootstrap" \
  go build -trimpath -ldflags='-s -w' -buildmode=c-shared \
  -o app/tizen/lib/libotakasecore.so ./mobilecore/clib)
rm -f tizen/lib/libotakasecore.h
# libass for ASS subtitles (lib/playback/ass_overlay.dart).
scripts/build-libass.sh "$gcc" "$rootstrap" "$host" tizen/lib/libotakase_ass.so

manifest=tizen/tizen-manifest.xml
original=$(cat "$manifest")
restore() { printf '%s\n' "$original" > "$manifest"; }
cleanup() {
  restore
  if [[ -n ${lock_original:-} ]]; then
    rm -f pubspec_overrides.yaml
    printf '%s\n' "$lock_original" > pubspec.lock
    rm -rf "$stub"
    flutter-tizen pub get > /dev/null 2>&1 || true
  fi
  if [[ -n ${tz_config:-} ]]; then
    printf '%s\n' "$tz_config_original" > "$tz_config"
  fi
  if [[ -n ${shim:-} ]]; then
    rm -rf "$shim"
  fi
}
trap cleanup EXIT

if [[ -n ${TIZEN_AUTHOR_P12:-} ]]; then
  : "${TIZEN_AUTHOR_PASSWORD:?Set TIZEN_AUTHOR_PASSWORD for TIZEN_AUTHOR_P12}"
  dist="$tizen_sdk/tools/certificate-generator/certificates/distributor"
  tz="$tizen_sdk/tools/tizen-core/tz"
  "$tz" security-profiles remove otakase-build > /dev/null 2>&1 || true
  "$tz" security-profiles add -n otakase-build -A \
    -a "$TIZEN_AUTHOR_P12" -p "$TIZEN_AUTHOR_PASSWORD" \
    -d "$dist/tizen-distributor-signer.p12" -P tizenpkcs12passfordsigner \
    -C "$dist/tizen-distributor-ca.cer" > /dev/null
  export TIZEN_DISTRIBUTOR_PASSWORD=${TIZEN_DISTRIBUTOR_PASSWORD:-tizenpkcs12passfordsigner}
fi
if [[ -n ${TIZEN_AUTHOR_PASSWORD:-} ]]; then
  # tz reads profile passwords from the desktop keyring and hands the .NET
  # build "ERROR" when there is none. Swap in the real passwords.
  real_root=${DOTNET_ROOT:-$(dirname -- "$(readlink -f -- "$(command -v dotnet)")")}
  shim=$(mktemp -d)
  for entry in "$real_root"/*; do ln -s "$entry" "$shim/"; done
  rm "$shim/dotnet"
  cat > "$shim/dotnet" <<SHIM
#!/usr/bin/env bash
args=()
for a in "\$@"; do
  case "\$a" in
    /p:AuthorPass=*) args+=("/p:AuthorPass=\$TIZEN_AUTHOR_PASSWORD") ;;
    /p:DistributorPass=*) args+=("/p:DistributorPass=\$TIZEN_DISTRIBUTOR_PASSWORD") ;;
    *) args+=("\$a") ;;
  esac
done
exec "$real_root/dotnet" "\${args[@]}"
SHIM
  chmod +x "$shim/dotnet"
  # tz runs the dotnet named in its own config, not the one on PATH.
  tz_config="$tizen_sdk/tools/tizen-core/config.yaml"
  touch "$tz_config"
  tz_config_original=$(cat "$tz_config")
  grep -v '^dotnet_cli_path:' <<< "$tz_config_original" > "$tz_config" || true
  echo "dotnet_cli_path: $shim/dotnet" >> "$tz_config"
fi

if [[ -n ${TIZEN_EMULATOR:-} ]]; then
  # AVPlay has no emulator libraries. Swap in its Dart side only, so the app
  # starts and everything but playback can be tried on the emulator.
  flutter-tizen pub get > /dev/null
  avplay=$(python3 -c 'import json,sys
for p in json.load(open(".dart_tool/package_config.json"))["packages"]:
    if p["name"] == "video_player_avplay": print(p["rootUri"].removeprefix("file://"))')
  lock_original=$(cat pubspec.lock)
  stub=$(mktemp -d)/video_player_avplay
  cp -r "$avplay" "$stub"
  rm -rf "$stub/tizen"
  python3 - "$stub/pubspec.yaml" <<'PY'
import re, sys
path = sys.argv[1]
text = open(path).read()
text = re.sub(r"\n  plugin:\n(    .*\n)+", "\n", text)
open(path, "w").write(text)
PY
  printf 'dependency_overrides:\n  video_player_avplay:\n    path: %s\n' "$stub" > pubspec_overrides.yaml
fi
flutter-tizen pub get
rm -rf "$out"
mkdir -p "$out"
IFS=. read -r year drop hotfix <<< "$tpk_version"
code=$((10#$year * 1000000 + 10#$drop * 1000 + 10#${hotfix:-0}))
flutter_tizen_rev=$(git -C "$(dirname -- "$(readlink -f -- "$(command -v flutter-tizen)")")/.." rev-parse HEAD)
echo "Otakase Samsung TV app $version (code $code)"
for api in $apis; do
  restore
  sed -i.bak -E "s/api-version=\"[^\"]+\"/api-version=\"$api\"/; s/(<manifest [^>]*)[[:space:]]version=\"[^\"]+\"/\1 version=\"$tpk_version\"/" "$manifest"
  rm -f "$manifest.bak"
  # An update runs only in the shell it was built with: the same
  # flutter-tizen, TV runner, native plugins and TizenOS group.
  python3 scripts/tizen-shell-id.py "$flutter_tizen_rev" "$api" > tizen/shell-id
  flutter-tizen build tpk --release --device-profile tv "${arch_flags[@]}" \
    --dart-define OTAKASE_TIZEN=true --dart-define "OTAKASE_TIZEN_API=$api" \
    --dart-define "OTAKASE_VERSION=$version"
  cp build/tizen/tpk/com.xyverion.otakase-*.tpk "$out/otakase-samsung-tv-tizen$api.tpk"
  root=build/tizen/tpk/tpkroot
  brain=$(mktemp -d)
  cp -r "$root/res/flutter_assets" "$brain/"
  cp "$root/lib/libapp.so" "$root/lib/libotakasecore.so" \
    "$root/lib/libotakase_ass.so" tizen/shell-id "$brain/"
  (cd "$brain" && zip -q -r -X "$out/otakase-samsung-tv-brain-tizen$api.zip" .)
  rm -rf "$brain"
done
if [[ -n ${TIZEN_EMULATOR:-} ]]; then
  rm -f tizen/shell-id
  ls -l "$out"
  exit 0
fi
rm -f "$app_dir/build/otakase-samsung-tv.zip"
python3 scripts/tizen-update-manifest.py --out "$out" --version "$version" \
  --code "$code" --apis $apis > "$out/otakase-samsung-tv.json"
if [[ -n ${TV_UPDATE_KEY:-} ]]; then
  openssl pkeyutl -sign -rawin -inkey "$TV_UPDATE_KEY" -in "$out/otakase-samsung-tv.json" |
    base64 -w0 > "$out/otakase-samsung-tv.json.sig"
else
  echo "TV_UPDATE_KEY is not set; the update manifest is unsigned and TVs will ignore it." >&2
fi
rm -f tizen/shell-id
(cd "$out" && zip -q ../otakase-samsung-tv.zip otakase-samsung-tv-tizen*.tpk \
  -j "$app_dir/scripts/samsung-tv/install.sh" "$app_dir/scripts/samsung-tv/install.bat" \
  "$app_dir/scripts/samsung-tv/README.txt")
ls -l "$out" "$app_dir/build/otakase-samsung-tv.zip"
