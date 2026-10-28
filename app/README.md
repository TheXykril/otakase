# Otakase Android app

Developer notes for the Android phone and TV app. The read-only catalog uses AniList directly from Dart; public lists use the usernames in Settings. Playback uses the six HTTP sources in mobilecore. Login, tracker writes, downloads, torrents, casting and background playback are outside this change. Android 7.0 (API 24) or newer is required. The CLI version and CLI/bot behavior are unchanged.

## Pinned toolchain

- Flutter 3.47.8, framework 737ba8f885dc774b53b442759999a312c9461e4e.
- Dart 3.13.7, bundled with that Flutter release.
- Go 1.26.0.
- gomobile and gobind: golang.org/x/mobile v0.0.0-20260821190718-4776eadac327.
- JDK 17, built with Eclipse Temurin 17.0.21+9.
- Android command-line tools 19.0 (archive commandlinetools-linux-13114758_latest.zip).
- Android platforms 35 (plugin compile SDK) and 36; build tools 36.0.0.
- Android NDK 28.2.13676358 and CMake 3.22.1.
- Gradle 9.3.1, Android Gradle Plugin 9.1.0, Kotlin plugin 2.4.0.
- media_kit 1.2.6, media_kit_video 2.0.1, media_kit_libs_video 1.0.7.
  Transitive media_kit_libs_android_video 1.3.8 is locked in pubspec.lock.

## Build from a fresh checkout

Install Git, Python 3, JDK 17, Go 1.26.0, Flutter 3.47.8 and the Android SDK
command-line tools. Keep the Flutter checkout outside this repository.

```sh
git clone --branch 3.47.8 --depth 1 https://github.com/flutter/flutter.git "$HOME/flutter"
export PATH="$HOME/flutter/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
export CI=true # Avoid optional cloud-environment probes in build environments.
export JAVA_HOME=/path/to/jdk-17
export ANDROID_HOME="$HOME/Android/Sdk"
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/28.2.13676358"
export PATH="$JAVA_HOME/bin:$ANDROID_HOME/platform-tools:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
sdkmanager --licenses
sdkmanager 'platform-tools' 'platforms;android-35' 'platforms;android-36' 'build-tools;36.0.0' 'ndk;28.2.13676358' 'cmake;3.22.1'
flutter config --android-sdk "$ANDROID_HOME"
flutter doctor -v

# From the otakase repository root:
./app/scripts/build-apk.sh
```

The script installs the two pinned Go generators, binds mobilecore into
app/android/app/libs/mobilecore.aar, runs Flutter checks, and builds both the
universal APK and the optional ABI splits. The AAR and build outputs are ignored
by Git. The Go core stays in the existing module, not a separate app module.

Equivalent binding command, run from the repository root:

```sh
gomobile bind -target=android/arm64,android/arm -androidapi 23 \
  -javapkg com.xyverion.otakase \
  -o app/android/app/libs/mobilecore.aar ./mobilecore
cd app
flutter pub get
flutter analyze
flutter test
flutter build apk --release --target-platform android-arm,android-arm64
flutter build apk --release --target-platform android-arm,android-arm64 --split-per-abi
```

Outputs live under app/build/app/outputs/flutter-apk/:
app-release.apk (both ARM ABIs), app-arm64-v8a-release.apk and
app-armeabi-v7a-release.apk. Without android/key.properties, release-mode builds use the local debug key.
This is a developer APK signing fallback. Never commit a key or key.properties. Use the same test key across
updates, or uninstall the previous test app before installing a different key.

## Release signing

Release APKs are signed with the Otakase release key when
`app/android/key.properties` exists. It is ignored by Git and looks like:

```properties
storeFile=/absolute/path/to/otakase-release.jks
storePassword=...
keyAlias=otakase
keyPassword=...
```

Without it the build falls back to the machine's debug key. A debug-signed APK
cannot update one signed with the release key (or one built on another
machine), so every APK given to anyone must use the release key. Keep the
keystore and its passwords backed up offline. Losing them means installed apps
can only be replaced by uninstalling first.

## Core boundary

mobilecore/ is public because gomobile builds a generated wrapper module which
cannot import an internal package. The core itself belongs to the existing
module and imports internal/providers and internal/providerhost directly.
The six sources are anikoto, megavid, animex, kickassanime, anizone and reanime.
Client injection remains isolated from registry factories.
The registry factories and CLI/bot paths are unchanged. Root go.mod adds the
pinned gobind tool without upgrading existing runtime dependencies.

Kotlin owns one Core per Flutter engine. Start(JSON) returns a handle. Result
waits on a worker thread. Cancel(handle) stops the operation and in-flight HTTP.
Every result, including cancellation, gets collected once. Closing the engine
cancels all work and closes active playback relays. The stop operation releases
ReAnime loopback relays when playback stops or switches source. Each operation has an isolated provider/client and a 90-second
limit, at most 16 outstanding handles. A provider's DNS check can unwind for up
to five seconds after Result has already returned cancellation.

All requests use version 1 and mode sub or dub:

```json
{"version":1,"operation":"search","provider":"megavid","query":"Frieren","mode":"sub"}
{"version":1,"operation":"episodes","provider":"megavid","show_id":"52991","mode":"sub"}
{"version":1,"operation":"resolve","provider":"megavid","show_id":"52991","episode":1,"mode":"sub"}
```

Anikoto show_id is the slug returned by search. Responses contain version and
either data (shows, episodes or streams) or error (code, message, retryable).
Streams retain url, headers, referrer, hls, subtitle and all subtitle tracks.
Error codes include invalid_request, unsupported_provider, invalid_handle,
cancelled, timeout, rate_limited, selection_required, no_stream and provider_error.
Upstream error bodies and signed URLs never enter logs or error messages.
providerhost hooks are set by NewCore, quiet output, soft subtitles and an
explicit selection_required error for prompts. No CLI init/config is imported.

The providers and their dependency tree build with CGO_ENABLED=0. gomobile
itself uses Android JNI/cgo glue, with no third-party cgo or torrent packages.

## Phone installation

Enable Developer options and USB debugging, connect the phone, then:

```sh
adb devices
adb -s PHONE_SERIAL shell getprop ro.product.cpu.abilist
adb -s PHONE_SERIAL install -r app/build/app/outputs/flutter-apk/app-release.apk
adb -s PHONE_SERIAL shell am start -n com.xyverion.otakase/.MainActivity
```

## Sony Android TV installation

Enable Developer options and network/ADB debugging on the TV. Keep the computer
on the same network. Use the address shown in the TV settings. Older Bravia
builds use port 5555, newer wireless debugging uses pairing and connection ports.

```sh
# Older Android TV, approve the prompt on the TV:
adb connect TV_IP:5555
# Newer wireless debugging, use the two ports shown on screen:
adb pair TV_IP:PAIRING_PORT
adb connect TV_IP:DEBUG_PORT
adb devices
adb -s TV_IP:DEBUG_PORT shell getprop ro.product.cpu.abilist
adb -s TV_IP:DEBUG_PORT install -r app/build/app/outputs/flutter-apk/app-release.apk
adb -s TV_IP:DEBUG_PORT shell am start -n com.xyverion.otakase/.MainActivity
```

For older TV commands substitute TV_IP:5555 for TV_IP:DEBUG_PORT. The universal
APK includes armeabi-v7a even if the TV chip is 64-bit. Use the system ABI list,
not the chip model, when choosing a split. The launcher includes a 320x180 banner
made from Build/app-icon/png/otakase-1024.png. leanback and touchscreen features
both declare required=false, with phone and LEANBACK_LAUNCHER entries.

## Device acceptance, Xykril's phone and Sony TV

1. Use D-pad only on TV. Visible Shu focus rings, OK on every action, Android
   Back up one page. Home Back asks to exit. Search opens the Android keyboard.
2. Run HLS headers + ASS probe. This is a bundled 20-second synthetic H.264/AAC
   HLS clip, with master, variant and segments served on device loopback. Every
   request must include X-Otakase-Probe, Referer and User-Agent or it gets 403.
   Confirm accepted resources include both playlists and segments, refused=0,
   moving video, sound, red ASS heading, the rotated moving sign for 15s and the Japanese sign at top right (no empty boxes).
3. Probe uses PlayerConfiguration(libass: true), a bundled Noto Sans JP font (Latin, kana and kanji), raw ASS
   content and native libass. No SRT/WebVTT conversion or Flutter subtitle
   overlay. Subtitles > Off and Load ASS probe allow comparing the same scene.
4. Home: check the banner carousel, Watch Now, metadata and all rows. Navigate
   MyList, Search filters, Schedule, Show details, Drawer and Settings. TV Up
   returns to the top bar; Left there opens the drawer. Check card focus,
   automatic row scrolling and the right settings sheet with only the D-pad.
5. In Settings, enter public AniList and MAL usernames. Check each account's
   status chips, pagination, private/invalid usernames and adult filtering.
   AdultContent defaults off and filters catalog, airing rows, lists and history.
6. Play sub and dub on all six sources. Sources run automatically in core order, with a working choice remembered
   per show and silent fallback; AutoAudioFallback controls whether
   the opposite audio mode may be tried. Verify provider headers and ASS,
   embedded/provider track selection, preferred language, quality, OP/ED/recap
   marks and auto-skip, next countdown, remote media keys and resume after
   process restart. Quality selects the matching HLS master bandwidth when
   resolution metadata is available; otherwise the original default remains.
   Check weak-TV scrolling, image cache, 1080p hardware decoding and memory.
   Native playback, libass appearance and device performance require real
   devices. A successful APK build does not prove those results.
7. Measure idle and playing memory with the same APK on each device:

```sh
adb -s DEVICE shell dumpsys meminfo com.xyverion.otakase
adb -s DEVICE shell input keyevent KEYCODE_DPAD_DOWN
adb -s DEVICE shell input keyevent KEYCODE_DPAD_CENTER
adb -s DEVICE shell input keyevent KEYCODE_BACK
```

Stop and report any gomobile provider binding failure, HLS header failure or
Android ASS rendering failure. Do not replace providers or convert subtitles to
make a failed probe appear successful. PR #105 stays draft until device acceptance and memory measurements are recorded.

## Go checks

```sh
gofmt -l .
go vet ./...
go test -race ./...
(cd bot && gofmt -l . && go vet ./... && go test -race ./...)
CGO_ENABLED=0 go test ./mobilecore
go list -deps ./mobilecore
```

mobilecore/testdata documents recording and sanitisation. Tests replay both
providers, preserve playback hints, check validation, typed failures, HTTP
cancellation, timeout, handle limits and non-interactive host hooks. Flutter tests cover Home, phone navigation, show navigation, TV arrows/OK/Back
and visible focus, adult filtering, public list parsing, cache, local persistence
and source fallback.
The on-device HLS/ASS probe is the playback acceptance test.
## Data and local state

AniList supplies trending, seasonal popularity, recent airingSchedules, the
weekly schedule, search, show metadata, relations and recommendations. Queries
are cached for ten minutes. Schedule times use the device timezone. Posters use
cached_network_image with sized memory/disk thumbnails and lazy horizontal rows.
MAL public lists use the public MyAnimeListClientID from config_defaults.go.
No access tokens or tracker writes are used. shared_preferences stores versioned
settings and local resume/watched records. Removing app data removes that history.

Provider skip ranges take precedence; AniSkip can fill missing OP/ED/recap ranges
when its episode length matches. Jikan filler flags apply only during automatic
next-episode selection. All are optional and fail without blocking playback.
The player keeps original ASS text and the bundled Noto Sans JP font. Android's
voice search activity is used when installed, with keyboard search as fallback.
Diagnostics retains the independent bundled HLS/ASS acceptance probe.

## Fixture regeneration

The HLS clip contains generated test bars and a tone, with no external media:

```sh
ffmpeg -f lavfi -i testsrc2=size=640x360:rate=24 \
  -f lavfi -i sine=frequency=440:sample_rate=44100 -t 20 \
  -c:v libx264 -preset fast -pix_fmt yuv420p -g 48 -sc_threshold 0 \
  -c:a aac -b:a 64k -f hls -hls_time 2 -hls_list_size 0 \
  -hls_segment_filename app/assets/probe/segment%02d.ts app/assets/probe/variant.m3u8
```

master.m3u8 names variant.m3u8. Synthetic media is about 2 MiB of the APK.
Noto Sans JP (notofonts/noto-cjk, SubsetOTF) is bundled under the SIL OFL in assets/fonts, so Japanese signs render instead of empty boxes.

## UI assets and visual checks

The app uses the Shu tokens from site/style.css and internal/theme/palette.go.
Sources are automatic. Source names and health appear only in Settings > Diagnostics.
Phone settings are in the drawer; TV retains its gear and Left-to-drawer shortcut.
Focus outlines follow FocusManager highlight mode, so touch does not leave a ring.

The v2 direction in otakase-context/docs/mobile-design.md takes precedence over
its earlier editorial styling. Inter is used for UI text at weights 400/500/600/700;
Archivo Black is limited to the wordmark and hero, and JetBrains Mono to tiny
badges/time codes. Inter 4.000 is copied from the repository's existing mpv skin
and bot font assets, with its SIL OFL in assets/fonts/OFL-Inter.txt. Cormorant
Garamond is removed. Secondary text is #b3aa9c; UI text is at least 15 sp on phone
and 20 sp on TV.

Archivo Black, JetBrains Mono and Noto Serif JP are bundled from google/fonts
commit ddf386ba1b9fbc6b2c818f1ddcd9148f54ced94b with their OFL licenses. Noto Serif
JP is subset to the app's display kanji. Japanese title fallback and original
ASS subtitles retain full Noto Sans JP. Cards use 12 px corners, controls use
pills, and the phone's floating bar uses a clipped blur. Focus handling, automatic
source selection and the player/subtitle lifecycle are unchanged.
Lucide 4.0.1 supplies the line icons. Native and Flutter splash screens use the
existing 任 mark from site/img/mark-256.png.

Phone renders are generated from the real widgets with fixture catalog data,
bundled fonts and reduced motion. PNG outputs stay outside the source tree;
they are for local inspection only. Do not upload, attach or commit them, including in PR descriptions. Device playback and
weak-TV performance still require the phone and Sony TV.

To regenerate the phone renders with local fixture art (a landscape banner and
2:3 poster named banner.jpg and cover.jpg), use an absolute output directory:

```sh
cd app
flutter test --update-goldens \
  --dart-define=PHONE_SCREENSHOTS=/absolute/output/directory \
  --dart-define=SCREENSHOT_ART=/absolute/fixture/art \
  test/phone_screens_test.dart
```

The capture test pre-decodes art and loads every bundled font, and asserts that
images are ready before recording Home, MyList, Search and Settings. Without
those opt-in paths the four capture cases are skipped; regression tests still run.

To exercise all four screens with bundled fonts/artwork without writing images:

```sh
flutter test --dart-define=VERIFY_PHONE_SCREENS=true test/phone_screens_test.dart
```

This mode verifies decoded images and layout; the existing opt-in golden command
above still renders with your local banner/poster fixtures. No screenshots are
uploaded, attached or committed.
