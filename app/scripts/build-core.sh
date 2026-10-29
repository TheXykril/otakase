#!/usr/bin/env bash
set -euo pipefail
repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_dir"
: "${ANDROID_HOME:?Set ANDROID_HOME to the Android SDK directory}"
: "${JAVA_HOME:?Set JAVA_HOME to a JDK 17 directory}"
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/28.2.13676358"
export PATH="$JAVA_HOME/bin:$PATH"
mobile_version=v0.0.0-20260821190718-4776eadac327
# Keep generator binaries out of the source tree and pin both to the same commit.
tools_dir="$(go env GOPATH)/bin"
GOBIN="$tools_dir" go install "golang.org/x/mobile/cmd/gomobile@$mobile_version"
GOBIN="$tools_dir" go install "golang.org/x/mobile/cmd/gobind@$mobile_version"
export PATH="$tools_dir:$PATH"
mkdir -p app/android/app/libs
gomobile bind -target=android/arm64,android/arm -androidapi 23 \
  -javapkg com.xyverion.otakase -o app/android/app/libs/mobilecore.aar ./mobilecore
