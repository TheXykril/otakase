#!/bin/sh
# Builds Otakase.app around a macOS otakase binary, with the app icon, so it
# can sit in Applications and the Dock. Not part of a release yet: it waits
# until someone can test it on a Mac and the app can be signed.
#
#   Build/macos-app/make-app.sh path/to/otakase-macos-universal [version]
#
# The app is written to Build/Output/Otakase.app.
set -eu

binary="$1"
version="${2:-$(cat "$(dirname "$0")/../../VERSION.txt")}"
here="$(cd "$(dirname "$0")" && pwd)"
app="$here/../Output/Otakase.app"

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
sed "s/__VERSION__/$version/g" "$here/Info.plist" > "$app/Contents/Info.plist"
cp "$here/launcher" "$app/Contents/MacOS/Otakase"
cp "$binary" "$app/Contents/MacOS/otakase"
chmod +x "$app/Contents/MacOS/Otakase" "$app/Contents/MacOS/otakase"
cp "$here/../app-icon/otakase.icns" "$app/Contents/Resources/otakase.icns"
echo "Wrote $app"
