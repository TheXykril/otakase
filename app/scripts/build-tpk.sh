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
# Signing uses the active Tizen Studio security profile. Without one, set
# TIZEN_AUTHOR_P12 + TIZEN_AUTHOR_PASSWORD to an author certificate; the
# public Tizen distributor certificate is added. On machines without a
# desktop keyring (CI), the passwords are passed to the .NET build directly.
set -euo pipefail
app_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$app_dir"
tizen_sdk=${TIZEN_SDK:-$HOME/tizen-studio}
version=${OTAKASE_VERSION:-$(tr -d '[:space:]' < "$app_dir/../VERSION.txt")}
tpk_version=${version%%-*}
apis=${TIZEN_API_VERSIONS:-6.0 6.5 10.0}
out="$app_dir/build/samsung-tv"

# Go core for 32-bit ARM, the userland of every Samsung TV. Linking against
# the oldest rootstrap keeps the library loadable on all TizenOS versions.
rootstrap="$tizen_sdk/platforms/tizen-6.0/iot-headed/rootstraps/iot-headed-6.0-device.core"
gcc="$tizen_sdk/tools/arm-linux-gnueabi-gcc-9.2/bin/arm-linux-gnueabi-gcc"
if [[ ! -d $rootstrap || ! -x $gcc ]]; then
  echo "Install the Tizen IOT-Headed-6.0 native CLI rootstrap and Gcc 9.2 toolchain." >&2
  exit 1
fi
mkdir -p tizen/lib
(cd .. && CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 \
  CC="$gcc --sysroot=$rootstrap" \
  go build -trimpath -ldflags='-s -w' -buildmode=c-shared \
  -o app/tizen/lib/libotakasecore.so ./mobilecore/clib)
rm -f tizen/lib/libotakasecore.h

manifest=tizen/tizen-manifest.xml
original=$(cat "$manifest")
restore() { printf '%s\n' "$original" > "$manifest"; }
cleanup() {
  restore
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

flutter-tizen pub get
rm -rf "$out"
mkdir -p "$out"
echo "Otakase Samsung TV app $version"
for api in $apis; do
  restore
  sed -i.bak -E "s/api-version=\"[^\"]+\"/api-version=\"$api\"/; s/(<manifest [^>]*)[[:space:]]version=\"[^\"]+\"/\1 version=\"$tpk_version\"/" "$manifest"
  rm -f "$manifest.bak"
  flutter-tizen build tpk --release --device-profile tv \
    --dart-define OTAKASE_TIZEN=true --dart-define "OTAKASE_VERSION=$version"
  cp build/tizen/tpk/com.xyverion.otakase-*.tpk "$out/otakase-samsung-tv-tizen$api.tpk"
done
cp scripts/samsung-tv/install.sh scripts/samsung-tv/install.bat scripts/samsung-tv/README.txt "$out/"
(cd "$out" && zip -q -r ../otakase-samsung-tv.zip .)
ls -l "$out" "$app_dir/build/otakase-samsung-tv.zip"
