#!/usr/bin/env bash
# Installs Otakase on a Samsung TV over Wi-Fi. See README.txt first.
#   ./install.sh [TV-IP]
# Set SAMSUNG_PROFILE to a Tizen Studio security profile that holds your
# Samsung certificate to re-sign the app before installing (needed by most TVs).
set -euo pipefail
dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
tools=${TIZEN_SDK:-$HOME/tizen-studio}/tools
sdb=$(command -v sdb || true)
[[ -z $sdb && -x $tools/sdb ]] && sdb=$tools/sdb
tizen=$(command -v tizen || true)
[[ -z $tizen && -x $tools/ide/bin/tizen ]] && tizen=$tools/ide/bin/tizen
if [[ -z $sdb ]]; then
  echo "sdb was not found. Install Tizen Studio (see README.txt) or set TIZEN_SDK."
  exit 1
fi

ip=${1:-}
[[ -z $ip ]] && read -r -p "TV IP address (TV Settings > General > Network > Network Status): " ip
[[ -z $ip ]] && { echo "No IP address given."; exit 1; }
"$sdb" connect "$ip" > /dev/null
serial="$ip:26101"
if ! "$sdb" -s "$serial" get-state 2>/dev/null | grep -q device; then
  echo "Could not reach the TV at $ip."
  echo "Turn on Developer Mode with this computer's IP address and restart the TV (README.txt)."
  exit 1
fi

version=$("$sdb" -s "$serial" capability | sed -n 's/^platform_version:\([0-9.]*\).*/\1/p')
major=${version%%.*}
minor=${version#*.}; minor=${minor%%.*}
if [[ -z $major ]] || (( major < 6 )); then
  echo "This TV runs Tizen ${version:-unknown}. Otakase needs a 2021 or newer Samsung TV (Tizen 6.0+)."
  exit 1
elif (( major >= 10 )); then group=10.0
elif (( major == 6 && minor == 0 )); then group=6.0
else group=6.5
fi
tpk="$dir/otakase-samsung-tv-tizen$group.tpk"
echo "TV runs Tizen $version, installing $(basename "$tpk")"

if [[ -n ${SAMSUNG_PROFILE:-} ]]; then
  [[ -z $tizen ]] && { echo "Re-signing needs the tizen command from Tizen Studio."; exit 1; }
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  cp "$tpk" "$work/"
  tpk="$work/$(basename "$tpk")"
  "$tizen" package -t tpk -s "$SAMSUNG_PROFILE" -- "$tpk"
fi

if [[ -n $tizen ]]; then
  output=$("$tizen" install -n "$tpk" -s "$serial" 2>&1 || true)
else
  output=$("$sdb" -s "$serial" install "$tpk" 2>&1 || true)
fi
echo "$output"
if grep -qiE 'fail|error' <<< "$output"; then
  echo
  echo "Install failed. If the message mentions a certificate or signature,"
  echo "create a Samsung certificate for this TV and run again with"
  echo "SAMSUNG_PROFILE=<profile name> ./install.sh $ip (README.txt, step 3)."
  exit 1
fi
echo "Done. Open Otakase from Apps on the TV."
