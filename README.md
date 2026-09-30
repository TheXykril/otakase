# MPV Binaries Branch

This branch contains prebuilt MPV binaries for Windows used by the Curd anime player.

## Current build
mpv v0.41.0 (x86_64, shinchiro build from https://sourceforge.net/projects/mpv-player-windows/files/release/)

## Files
- `Build/mpv.exe.gz` - Compressed Windows MPV binary
- `Build/mpv.zip` - Alternative compressed Windows MPV binary

## Usage
This branch is used by the GitHub Actions CI/CD pipeline to bundle MPV with Windows releases.
Users cloning the main repository will not download these binaries, reducing clone size by ~86 MB.
