# Samsung TV app

The Android app (`app/`) also builds for Samsung smart TVs, which run Tizen.
It is the same Dart code with three platform differences:

| Part | Android | Samsung TV |
|---|---|---|
| Go core (`mobilecore`) | gomobile AAR, `otakase/core` method channel | `mobilecore/clib` built as `libotakasecore.so`, loaded with dart:ffi (`lib/playback/tizen_core.dart`) |
| Player | media_kit (libmpv + libass) | the TV's AVPlay through `video_player_avplay` (`lib/playback/tizen_engine.dart`) |
| Subtitles | libass, full ASS styling | plain text drawn by the app (`lib/playback/subtitle_cues.dart`) |

`lib/playback/engine.dart` is the player interface both player pages use
(`player.dart`, `ui/watch_rooms_page.dart`). `PlaybackEngine.create()` picks
the engine from the `OTAKASE_TIZEN` dart-define, which only `build-tpk.sh`
sets. Android builds never touch the Tizen code paths.

Things the TV build leaves out: the in-app updater (a Tizen app cannot
install packages; users rerun the installer), the HLS/ASS probe (mpv only),
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

## Installing on a TV

Retail Samsung TVs only accept apps signed with a Samsung distributor
certificate that lists the TV's DUID, so the release TPKs (Tizen public
distributor) usually need re-signing. `scripts/samsung-tv/install.sh` and
`install.bat` do that when `SAMSUNG_PROFILE` names a Tizen Studio profile
holding the user's Samsung certificate, pick the TPK from the TV's
`platform_version` (`sdb capability`), and install over Wi-Fi with
`tizen install` (or `sdb install`). The user steps are in
`scripts/samsung-tv/README.txt`, which ships in the zip.

For development: `flutter-tizen run -d <TV-IP>:26101 --dart-define OTAKASE_TIZEN=true`
after `sdb connect <TV-IP>`, with a Samsung certificate profile active.

## Not verified yet

The TPKs build and the Dart side is covered by tests, but nothing has run on
a real Samsung TV yet: AVPlay with provider streams and headers, the video
rectangle inside the scaled TV canvas, the ffi core load path, and the .NET
runner (`tizen80`) on Tizen 6.0 TVs all need a device check.
