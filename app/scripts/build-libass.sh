#!/usr/bin/env bash
# Builds libotakase_ass.so (native/otakase_ass.c + libass + FriBidi) for the
# Samsung TV app. libass and FriBidi are linked in statically; FreeType and
# HarfBuzz come from the TV's system libraries.
#
# Usage: build-libass.sh <cc> <sysroot> <host triplet> <out.so>
# build-tpk.sh calls it with the same compiler and rootstrap as the Go core.
set -euo pipefail

cc=$1 sysroot=$2 host=$3 out=$4
app_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)

libass_version=0.17.4
libass_sha256=78f1179b838d025e9c26e8fef33f8092f65611444ffa1bfc0cfac6a33511a05a
fribidi_version=1.0.16
fribidi_sha256=1b1cde5b235d40479e91be2f0e88a309e3214c8ab470ec8a2744d82a5a9ea05c

cache=${OTAKASE_NATIVE_CACHE:-$app_dir/build/native-src}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$cache"

fetch() { # name url sha256
  local file=$cache/$1
  if [[ ! -f $file ]] || ! echo "$3  $file" | sha256sum -c --quiet - 2> /dev/null; then
    curl -sSfL --retry 3 -o "$file.part" "$2"
    echo "$3  $file.part" | sha256sum -c --quiet -
    mv "$file.part" "$file"
  fi
  tar -xf "$file" -C "$work"
}
fetch "libass-$libass_version.tar.xz" \
  "https://github.com/libass/libass/releases/download/$libass_version/libass-$libass_version.tar.xz" \
  "$libass_sha256"
fetch "fribidi-$fribidi_version.tar.xz" \
  "https://github.com/fribidi/fribidi/releases/download/v$fribidi_version/fribidi-$fribidi_version.tar.xz" \
  "$fribidi_sha256"

prefix=$work/prefix
export CC="$cc --sysroot=$sysroot" CFLAGS="-O2 -fPIC" PKG_CONFIG_LIBDIR=/nonexistent

build() { # dir configure-args...
  local dir=$1
  shift
  (cd "$work/$dir" && ./configure --host="$host" --prefix="$prefix" \
    --enable-static --disable-shared "$@" > "$work/$dir.log" 2>&1 \
    && make -j"$(nproc)" install >> "$work/$dir.log" 2>&1) || {
    tail -40 "$work/$dir.log" >&2
    exit 1
  }
}
build "fribidi-$fribidi_version" --disable-docs
export FREETYPE_CFLAGS="-I$sysroot/usr/include/freetype2" FREETYPE_LIBS="-lfreetype" \
  HARFBUZZ_CFLAGS="-I$sysroot/usr/include/harfbuzz" HARFBUZZ_LIBS="-lharfbuzz" \
  FRIBIDI_CFLAGS="-I$prefix/include/fribidi" FRIBIDI_LIBS="$prefix/lib/libfribidi.a"
build "libass-$libass_version" --disable-fontconfig --disable-require-system-font-provider \
  --disable-asm --disable-libunibreak

$cc --sysroot="$sysroot" -O2 -fPIC -shared -s -Wall -Wl,-z,defs -Wl,--exclude-libs,ALL \
  -I"$prefix/include" -o "$out" "$app_dir/native/otakase_ass.c" \
  "$prefix/lib/libass.a" "$prefix/lib/libfribidi.a" -lfreetype -lharfbuzz -lm
echo "Built $out"
