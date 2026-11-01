#!/usr/bin/env bash
# Packs a release build from build-desktop.sh into the file the release
# offers for this OS, with the metadata the in-app updater reads:
#
#   Windows  otakase-app-windows-setup.exe      (per-user installer)
#   macOS    otakase-app-macos.zip              (universal Otakase.app)
#   Linux    otakase-app-linux-x86_64.AppImage
#
# plus otakase-app-<os>.json beside each, all in build/desktop-release/.
# Run build-desktop.sh release first, on the same OS.
#
# Windows needs Inno Setup 6 (ISCC). Linux downloads appimagetool unless
# APPIMAGETOOL points at one.
set -euo pipefail
cd "$(dirname "$0")/.."

version=${OTAKASE_VERSION:-$(tr -d '[:space:]' < ../VERSION.txt)}
IFS=. read -r year drop hotfix <<< "${version%%-*}"
code=$((10#$year * 1000000 + 10#$drop * 1000 + 10#${hotfix:-0}))
out=build/desktop-release
rm -rf "$out"
mkdir -p "$out"

case "$(uname -s)" in
  Linux)
    asset=otakase-app-linux-x86_64.AppImage
    manifest=otakase-app-linux-x86_64.json
    appdir=build/Otakase.AppDir
    rm -rf "$appdir"
    mkdir -p "$appdir/usr/lib/otakase"
    cp -a build/linux/x64/release/bundle/. "$appdir/usr/lib/otakase/"
    cp packaging/linux/AppRun "$appdir/AppRun"
    chmod 755 "$appdir/AppRun"
    cp packaging/linux/otakase.desktop "$appdir/otakase.desktop"
    cp ../Build/app-icon/png/otakase-256.png "$appdir/otakase.png"
    tool=${APPIMAGETOOL:-}
    if [[ -z $tool ]]; then
      tool=build/appimagetool
      curl -fsSL -o "$tool" \
        https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage
      chmod 755 "$tool"
    fi
    # No FUSE on CI machines, so the tool unpacks itself instead of mounting.
    ARCH=x86_64 APPIMAGE_EXTRACT_AND_RUN=1 "$tool" --no-appstream "$appdir" "$out/$asset"
    ;;
  Darwin)
    asset=otakase-app-macos.zip
    manifest=otakase-app-macos.json
    app=build/macos/Build/Products/Release/Otakase.app
    # The helper was added after Flutter signed the app; sign it all again
    # (ad hoc, there is no Apple developer account) so macOS accepts it.
    codesign --force --deep --sign - "$app"
    ditto -c -k --keepParent "$app" "$out/$asset"
    ;;
  MINGW* | MSYS* | CYGWIN*)
    asset=otakase-app-windows-setup.exe
    manifest=otakase-app-windows.json
    iscc=${ISCC:-"/c/Program Files (x86)/Inno Setup 6/ISCC.exe"}
    # Git Bash would turn /DAppVersion into a path without the exclusion.
    MSYS2_ARG_CONV_EXCL='*' "$iscc" "/DAppVersion=$version" packaging/windows/otakase-app.iss
    ;;
  *)
    echo "unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

py=$(command -v python3 || command -v python)
"$py" scripts/update-manifest.py --file "$out/$asset" --asset "$asset" \
  --name "$version" --code "$code" --output "$out/$manifest"
echo "packed $out/$asset ($version, $code)"
