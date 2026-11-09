#!/usr/bin/env bash
# Builds the website's live demo: the app for the web with the shows in
# test/fixtures/site, plus a generated clip the demo player plays.
# Usage: app/demo/build.sh [output folder]   (default: site/demo)
set -euo pipefail
cd "$(dirname "$0")/.."
out=$(realpath -m "${1:-../site/demo}")

flutter build web -t demo/main.dart --release --base-href /demo/
rm -rf "$out"
mkdir -p "$(dirname "$out")"
cp -r build/web "$out"
# Only CanvasKit is used; the other renderers and debug symbols are 20 MB.
rm -rf "$out"/canvaskit/{skwasm*,wimp*,webparagraph,*.symbols} \
  "$out"/canvaskit/*/*.symbols "$out/flutter_service_worker.js"
mkdir -p "$out/art"
cp test/fixtures/site/*.jpg test/fixtures/site/shows.json "$out/art/"

# 24 minutes of a slow red gradient that says DEMO VIDEO, about 1 MB.
font=assets/fonts/ArchivoBlack-Regular.ttf
ffmpeg -loglevel error -y \
  -f lavfi -i "gradients=s=640x360:c0=0x0e0d0c:c1=0x4a1d14:c2=0x0e0d0c:x0=0:y0=0:x1=640:y1=360:speed=0.002:r=4,format=yuv420p" \
  -f lavfi -i "anullsrc=r=22050:cl=mono" -t 1440 \
  -vf "drawtext=fontfile=$font:text='DEMO VIDEO':fontcolor=0xfff8ec:fontsize=44:x=(w-tw)/2:y=(h-th)/2-14,drawtext=fontfile=$font:text='%{pts\:hms}':fontcolor=0xece4d6@0.7:fontsize=18:x=(w-tw)/2:y=(h/2)+30" \
  -c:v libx264 -preset veryslow -crf 36 -g 240 -tune stillimage \
  -c:a aac -b:a 8k -shortest -movflags +faststart "$out/demo.mp4"
du -sh "$out"
