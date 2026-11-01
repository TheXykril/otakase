# Samsung TV app

The Android app (`app/`) also builds for Samsung smart TVs, which run Tizen.
It is the same Dart code with three platform differences:

| Part | Android | Samsung TV |
|---|---|---|
| Go core (`mobilecore`) | gomobile AAR, `otakase/core` method channel | `mobilecore/clib` built as `libotakasecore.so`, loaded with dart:ffi (`lib/playback/tizen_core.dart`) |
| Player | media_kit (libmpv + libass) | the TV's AVPlay through `video_player_avplay` (`lib/playback/tizen_engine.dart`) |
| Subtitles | libass, full ASS styling | ASS files: libass in `libotakase_ass.so` (`native/otakase_ass.c`, `lib/playback/ass_overlay.dart`); SRT/VTT and AVPlay's embedded tracks: plain text (`lib/playback/subtitle_cues.dart`) |

`lib/playback/engine.dart` is the player interface both player pages use
(`player.dart`, `ui/watch_rooms_page.dart`). `PlaybackEngine.create()` picks
the engine from the `OTAKASE_TIZEN` dart-define, which only `build-tpk.sh`
sets. Android builds never touch the Tizen code paths.

Things the TV build leaves out: the Android APK updater (the TV has its own,
see below), the HLS/ASS probe (mpv only),
voice search and Android device status. The phone remote works: the TV side
listens the same way as on Android TV.

## Toolchain

- flutter-tizen at commit `22bcec9058fbe1e0943e6c6fda21792f24868e91`
  (Flutter 3.47.5, Dart 3.13.4). The app's Dart constraint is `^3.13.4` for it.
- Tizen Studio 6.1 CLI with NativeCLI, Gcc 9.2, Certificate Manager and the
  IOT-Headed-6.0, IOT-Headed-6.5 and Tizen 10.0 native CLI rootstraps
  (flutter-tizen builds TV apps against the public IoT rootstraps).
- .NET 8 SDK (the app runner is C#; flutter-tizen's C++ runner does not run
  on TVs).
- Go 1.26.

`app/scripts/build-libass.sh` (run by `build-tpk.sh`) downloads libass and
FriBidi at pinned versions (sha256 checked), builds them statically with the
same compiler and rootstrap as the Go core, and links `libotakase_ass.so`
against the TV's own FreeType and HarfBuzz. The overlay renders on a worker
isolate: libass draws each changed frame, `otakase_ass.c` blends it into one
RGBA image covering only the subtitle area, and Flutter draws it over the
video. Fonts: Noto Sans JP from the app assets (the fallback for every
family) plus fonts embedded in the subtitle file. Timing interpolates
between AVPlay's position updates.

`app/scripts/setup-tizen-sdk.sh` installs all of it on Ubuntu, including two
quirks: the Tizen package manager wants some desktop packages that are never
used (placeholders are installed), and the bundled clang needs `libtinfo5`.

## Build

```sh
app/scripts/setup-tizen-sdk.sh          # once; prints PATH/TIZEN_SDK lines
TIZEN_AUTHOR_P12=... TIZEN_AUTHOR_PASSWORD=... app/scripts/build-tpk.sh
```

Output: `app/build/samsung-tv/` and `app/build/otakase-samsung-tv.zip`, with
three TPKs and the install scripts. One TPK per TizenOS group is required
because `video_player_avplay` ships different native libraries per
`api-version`:

| TPK | api-version | TVs |
|---|---|---|
| `otakase-samsung-tv-tizen6.0.tpk` | 6.0 | 2021 |
| `otakase-samsung-tv-tizen6.5.tpk` | 6.5 | 2022-2025 (Tizen 6.5-9.0) |
| `otakase-samsung-tv-tizen10.0.tpk` | 10.0 | 2026 |

The script builds the Go core for 32-bit ARM against the Tizen 6.0 rootstrap
(so it only needs glibc 2.4 symbols and loads on every group), rewrites
`api-version` and `version` in `tizen/tizen-manifest.xml` for each build and
restores the file afterwards.

Signing: without `TIZEN_AUTHOR_P12` the active Tizen Studio security profile
is used. `tz` keeps profile passwords in the desktop keyring; without one
(CI, containers) it passes `ERROR` as the password, so when
`TIZEN_AUTHOR_PASSWORD` is set the script points `tz` at a small `dotnet`
wrapper that substitutes the real passwords, then restores `tz`'s config.

The CI job `samsung-tv` in `ci-cd.yml` runs both scripts and attaches
`otakase-samsung-tv.zip` to releases. It signs with the
`TIZEN_AUTHOR_P12_BASE64` / `TIZEN_AUTHOR_PASSWORD` secrets on main and a
throwaway author certificate otherwise.

## Updates without reinstalling

A Tizen app cannot install packages, so the TV app updates its "brain"
instead of the TPK. The installed TPK is the shell: the C# runner, the
Flutter engine and the native plugins. The brain is everything that changes
between most releases: `flutter_assets`, the compiled Dart (`libapp.so`), the
Go core (`libotakasecore.so`) and libass (`libotakase_ass.so`).

- `tizen/OtakaseApplication.cs` replaces flutter-tizen's `FlutterApplication`.
  Before starting the engine, `tizen/Brain.cs` reads `<data>/brain/active`
  and, when that brain matches the shell, starts the engine with its assets
  and AOT library (absolute paths; the embedder resolves relative ones
  against the package) and passes `--brain=<dir>` to Dart.
- Crash guard: the shell writes `<data>/brain/booting` before starting a
  brain, and the app deletes it 15 s after a good start. If the marker is
  still there next launch, that brain is marked `failed`, `active` is
  removed and the TPK's own brain runs.
- `lib/playback/tizen_updates.dart` checks every 6 hours and calls
  `mobilecore/tvupdate` through the Go core. It downloads
  `otakase-samsung-tv.json` from the latest release, checks its ed25519
  signature (`.sig`, public key in `tvupdate.go`), picks the bundle for this
  shell, checks size and sha256, unpacks into `<data>/brain/<code>/` and
  switches `active` atomically. The new brain runs the next time the app is
  closed and reopened (the app exits when sent to the background with an
  update ready).
- Shell id: `scripts/tizen-shell-id.py` hashes the flutter-tizen commit, the
  TizenOS group, the C# runner and the native plugin sources. A brain only
  runs in a shell with the same id, so a release that changes any of those
  makes old TVs say once that the app needs installing again, instead of
  loading a brain that cannot work.
- Version code: `YY*1000000 + DROP*1000 + HOTFIX`, the same as Android.

`build-tpk.sh` writes `otakase-samsung-tv-brain-tizen<api>.zip` per group and
`otakase-samsung-tv.json`, and signs it when `TV_UPDATE_KEY` points at the
ed25519 private key (PEM). In CI that is the `TV_UPDATE_KEY_PEM` secret on
main; the brain zips, the manifest and `.sig` are release assets. Without
the key the manifest is unsigned and TVs ignore it. Losing the key means
every TV needs one reinstall with a TPK carrying a new public key.

## Emulator

Tizen Studio's TV Extensions include a Samsung TV emulator (x86_64, needs
KVM, runs on Ubuntu). `TIZEN_EMULATOR=1 app/scripts/build-tpk.sh` builds one
Tizen 10.0 x64 TPK plus its brain into `app/build/samsung-tv-emulator/`.
AVPlay has no emulator libraries, so that build swaps in the plugin's Dart
side only (`pubspec_overrides.yaml`, removed afterwards): menus, D-pad,
sign-in, sources and the updater work, video playback does not. The emulator
build uses Tizen's gcc 14.2 (installed with the 10.0 rootstrap) because gcc 9.2
can't read the 10.0 emulator rootstrap.

## Installing on a TV

Retail Samsung TVs only accept apps signed with a Samsung distributor
certificate that lists the TV's DUID, so the release TPKs (Tizen public
distributor) usually need re-signing. `scripts/samsung-tv/install.sh` and
`install.bat` do that when `SAMSUNG_PROFILE` names a Tizen Studio profile
holding the user's Samsung certificate, pick the TPK from the TV's
`platform_version` (`sdb capability`), and install over Wi-Fi with
`tizen install` (or `sdb install`). The user steps are in
`scripts/samsung-tv/README.txt`, which ships in the zip. For family TVs the
easiest path is one Samsung certificate that lists every TV's DUID: install
once per TV, and later releases arrive through the in-app update.

For development: `flutter-tizen run -d <TV-IP>:26101 --dart-define OTAKASE_TIZEN=true`
after `sdb connect <TV-IP>`, with a Samsung certificate profile active.

## Not verified yet

The TPKs build and the Dart side is covered by tests, but nothing has run on
a real Samsung TV yet: the brain switch in the custom runner, AVPlay with provider streams and headers, the video
rectangle inside the scaled TV canvas, the ffi core load path, and the .NET
runner (`tizen80`) on Tizen 6.0 TVs all need a device check.
