#!/usr/bin/env bash
set -euo pipefail
repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_dir"
targets=android/arm64,android/arm
if [[ ${1:-} == --emulator && $# == 1 ]]; then
  targets+=,android/amd64
elif (( $# != 0 )); then
  echo "Usage: build-core.sh [--emulator]" >&2
  exit 2
fi
: "${ANDROID_HOME:?Set ANDROID_HOME to the Android SDK directory}"
: "${JAVA_HOME:?Set JAVA_HOME to a JDK 17 directory}"
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/28.2.13676358"
export PATH="$JAVA_HOME/bin:$PATH"
# Go 1.26.0-1.26.2 probes a syscall blocked on older 32-bit Android.
# Require the runtime fix backported in golang/go#77931.
go_version=$(go env GOVERSION)
if [[ ! $go_version =~ ^go([0-9]+)\.([0-9]+)\.([0-9]+)([^0-9].*)?$ ]] ||
  (( BASH_REMATCH[1] < 1 ||
     BASH_REMATCH[1] == 1 && (BASH_REMATCH[2] < 26 ||
     BASH_REMATCH[2] == 26 && BASH_REMATCH[3] < 3) )); then
  echo "Go 1.26.3 or newer is required for Android builds (found $go_version)." >&2
  exit 1
fi
mobile_version=v0.0.0-20260821190718-4776eadac327
# Keep generator binaries out of the source tree and pin both to the same commit.
tools_dir="$(go env GOPATH)/bin"
GOBIN="$tools_dir" go install "golang.org/x/mobile/cmd/gomobile@$mobile_version"
GOBIN="$tools_dir" go install "golang.org/x/mobile/cmd/gobind@$mobile_version"
export PATH="$tools_dir:$PATH"
mkdir -p app/android/app/libs
gomobile bind -target="$targets" -androidapi 23 \
  -javapkg com.xyverion.otakase -o app/android/app/libs/mobilecore.aar ./mobilecore
