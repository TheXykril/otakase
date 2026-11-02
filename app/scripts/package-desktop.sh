#!/usr/bin/env bash
# Packs a release build from build-desktop.sh into the file the release
# offers for this OS, in build/desktop-release/:
#
#   Windows  otakase-app-windows-setup.exe      (per-user installer)
#   macOS    otakase-app-macos.zip              (universal Otakase.app)
#   Linux    otakase-app-linux-x86_64.AppImage  (--install adds it to the
#            app menu and the otakase command)
#
# The in-app updater reads the version from the release tag and the size
# and checksum GitHub records for the file.
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

# Copies the system libmpv and every library it needs into dir, so the
# AppImage plays video without mpv installed. Libraries every desktop has
# and that must match the running system (glibc, the C++ runtime, graphics
# drivers, X11/Wayland, sound servers, GTK and friends) stay out. Each copy
# and the app's own plugins look in their own folder first ($ORIGIN).
bundle_libmpv() {
  local dir=$1 lib name
  lib=$(ldconfig -p | awk '/libmpv\.so\.2 .*x86-64/ {print $NF; exit}')
  [[ -n $lib ]] || { echo "libmpv.so.2 not found; install libmpv-dev" >&2; exit 1; }
  local keep='^(ld-linux|libc\.|libm\.|libdl\.|libpthread\.|librt\.|libresolv\.|libutil\.|libstdc\+\+|libgcc_s|libGL|libEGL|libOpenGL|libdrm|libgbm|libvulkan|libX|libxcb|libwayland|libasound|libpulse|libpipewire|libjack|libfontconfig|libfreetype|libharfbuzz|libglib-|libgobject-|libgio-|libgmodule-|libgtk|libgdk|libcairo|libpango|libz\.|libexpat|libdbus-|libsystemd|libudev|libselinux)'
  cp -L "$lib" "$dir/libmpv.so.2"
  ldd "$lib" | awk '/=> \// {print $3}' | while read -r dep; do
    name=$(basename "$dep")
    [[ $name =~ $keep ]] && continue
    [[ -e $dir/$name ]] || cp -L "$dep" "$dir/$name"
  done
  for lib in "$dir"/*.so*; do
    patchelf --add-rpath '$ORIGIN' "$lib"
  done
  echo "bundled libmpv with $(ls "$dir" | wc -l) libraries in $dir"
}

case "$(uname -s)" in
  Linux)
    asset=otakase-app-linux-x86_64.AppImage
    appdir=build/Otakase.AppDir
    rm -rf "$appdir"
    mkdir -p "$appdir/usr/lib/otakase"
    cp -a build/linux/x64/release/bundle/. "$appdir/usr/lib/otakase/"
    bundle_libmpv "$appdir/usr/lib/otakase/lib"
    cp packaging/linux/AppRun "$appdir/AppRun"
    chmod 755 "$appdir/AppRun"
    cp packaging/linux/otakase.desktop "$appdir/otakase.desktop"
    cp ../Build/app-icon/png/otakase-256.png "$appdir/otakase.png"
    # --install (packaging/linux/install.sh) copies these into the user's
    # app menu and icon theme.
    install -Dm755 packaging/linux/install.sh "$appdir/usr/share/otakase/install.sh"
    for size in 128 256 512; do
      install -Dm644 "../Build/app-icon/png/otakase-$size.png" \
        "$appdir/usr/share/icons/hicolor/${size}x${size}/apps/otakase.png"
    done
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
    app=build/macos/Build/Products/Release/Otakase.app
    # The helper was added after Flutter signed the app; sign it all again
    # (ad hoc, there is no Apple developer account) so macOS accepts it.
    codesign --force --deep --sign - "$app"
    ditto -c -k --keepParent "$app" "$out/$asset"
    ;;
  MINGW* | MSYS* | CYGWIN*)
    asset=otakase-app-windows-setup.exe
    iscc=${ISCC:-"/c/Program Files (x86)/Inno Setup 6/ISCC.exe"}
    # Git Bash would turn /DAppVersion into a path without the exclusion.
    MSYS2_ARG_CONV_EXCL='*' "$iscc" "/DAppVersion=$version" packaging/windows/otakase-app.iss
    ;;
  *)
    echo "unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

echo "packed $out/$asset ($version, $code)"
