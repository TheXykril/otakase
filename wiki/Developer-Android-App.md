# Android app development

Developer notes for the Android phone and TV app. Kage uses personal Home, Search and More on touch devices and Android TV. AniList and MAL login support real lists, status, progress, score and synced resume. Public usernames remain read only. Playback chooses sources automatically through mobilecore. Downloads, torrents, casting and background playback are outside this change. Android 7.0 (API 24) or newer is required. The CLI version and CLI/bot behavior are unchanged.

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

1. Use only D-pad, OK and Back on TV. Reach Home, Search and More, account categories,
   every episode, Accounts, settings and Schedule. Check focus restoration after a Show,
   menu or sheet, and safe initial focus for confirmation dialogs.
2. Check the phone's cutout, gesture inset, keyboard and enlarged text. The app hides the
   separate status bar, draws live clock/battery information, and hides the root bar while
   typing. Test system notification gestures and transient bar recovery.
3. Sign in to AniList and MAL separately and together. Verify private list reads, status,
   score and episode updates. Test MAL refresh, individual logout and expired AniList login.
   Complete TV QR pairing from a phone on the same LAN using the deployed callback page.
   With both signed in, verify one Home list and category picker, including MAL-only titles.
   Make one tracker unavailable and use its retry link to restore the combined list.
4. Stop mid-episode, reopen on another device, finish an episode, and finish the final
   episode. Check notes/comments preservation and verify stale polls never replace a done tag.
   Confirm the episode index thread ends at the real resume episode, without a node for
   unstarted shows. Check the Shu played timeline and D-pad movement in both episode views.
5. Verify 18+ filtering for no login, MAL only and both AniList preference values. Remove
   network access at startup and confirm the same account retains its cached preference.
6. Play real sub/dub HLS, check required request headers, hardware decoding and raw ASS
   signs, then select real audio/subtitle tracks. Subtitles must wait for a valid duration,
   and no track menu should open automatically. Test OP/ED markers, skip controls, quality,
   cancelling the next countdown and Back while a player panel is open.
7. Check idle and playing memory and scrolling on Sony TV. Preserve launcher adaptive,
   monochrome and banner assets, and test both launcher surfaces from one APK.

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

AniList supplies the anime catalog, metadata and airing calendar. Public usernames read lists
without tracker write access. Authenticated requests use the account's token and include
private lists. MAL list entries resolve to AniList metadata through their MAL identifier.
With both accounts signed in, Home merges AniList `idMal` with MAL IDs. Progress is the
most recently updated entry's value, together with its status, score and dates. An update
time beats a missing one. Equal times use the CLI freshness score: progress, status, score,
repeat count and cover presence. Missing metadata is filled from the other tracker, including
the AniList airing schedule and either side's adult flag. Unmatched titles remain visible. MAL statuses use AniList
category names in the combined list, including the MAL rewatch flag. Each list loads
independently so one failure leaves the other available with a retry link. Writes still
target both accounts, while public usernames and single-account lists retain their own views.

The optional background list sync defaults on in Accounts. It starts after a successful
combined list load, at most once per 30 minutes or on explicit refresh, without holding up
Home. It copies one-sided entries and updates differing states, preserving notes/comments.
AniList-private titles never queue a MAL write. Visibility rules also gate sync. Writes are
500 ms apart, capped at 100 requests per run, with a stop on 429. Completed MAL rewatches
use the CLI's completion-then-count sequence. Pending count writes survive a throttled run.

Show artwork uses the AniList banner. Without one, the cover becomes a blurred, darkened
backdrop beside a sharp 2:3 poster at the hero's full height. Episode titles and thumbnails
prefer AniList streaming episodes, with missing fields filled from `api.ani.zip/mappings`
by AniList ID. The anime episode map key takes precedence over provider episode numbering.
Successful mappings stay in memory for six hours per show. Failed refreshes retain prior
episode metadata and wait a minute before another lookup. Unknown images retain the treated
cover fallback. Mapping data never changes episode availability.

Detail reads retain the null-variable filtering, retry once after 600 ms for 429/5xx, and
log HTTP status or GraphQL error messages under `otakase.catalog`. Last good details remain
in memory per show after the ten-minute query cache expires, including when both retry
attempts fail. Clearing the catalog cache clears these details and episode mappings.
The recorded JSON tests mock all HTTP calls and download no artwork.

Settings and local history use versioned JSON in shared preferences. OAuth sessions and
pending PKCE state use secure storage. More owns Accounts, Schedule, Playback, Subtitles &
languages, Appearance, About & updates and Diagnostics. There is no drawer or movie mode.
Provider names and health appear only in Diagnostics. Normal playback calls the core match
operation and silently reuses a working source or falls back.

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

The Kage section of `otakase-context/docs/mobile-design.md` is the implementation authority.
The app uses exact warm Shu tokens, Inter UI text, Archivo Black display titles, JetBrains
Mono times/counts and supplied Noto Sans JP native titles. All fonts and licences are bundled.
The contained in-app seal uses the existing favicon vector path geometry with no background
tile. Launcher icons and TV banners are separate existing assets.

Phone Home is personal: local resume and one independently filtered shelf per configured
account. The floating Home/Search/More bar has readable labels, a selected marker and bottom
inset clearance. TV has separate compositions, 20sp minimum text, exterior split focus frames
and D-pad controls. Schedule is inside More. Show status editing owns progress and score.

`flutter test` includes production phone screens with bundled fonts and decoded fixture art,
without saving images. Optional local captures use the existing `PHONE_SCREENSHOTS` and
`SCREENSHOT_ART` defines. Keep all captures, renders and fixture artwork outside Git.

## Account callbacks

The public OAuth identifiers and shared redirect live in `app/lib/data/oauth_config.dart`.
Publish `site/app-login/` at `https://otakase.xyverion.com/app-login/` before testing login.
Both OAuth registrations must use that exact HTTPS redirect, including its trailing slash.
AniList uses the implicit token response. MAL uses a random 64-character plain PKCE challenge,
keeps the verifier in secure storage, exchanges with the client ID and no secret, and refreshes
expired access tokens. Tokens and pending state stay in `flutter_secure_storage`. Android
backup is disabled, and callback intents are cleared after delivery.

On a phone, the page removes callback parameters from browser history and opens
`otakase://login`. Android handles cold starts and new intents. TV shows an authorization QR
and listens for ten minutes on an ephemeral LAN port. State contains a nonce, service,
private IPv4 address and port. The page requires an explicit button press before navigating
top-level to the TV callback. No cross-origin fetch is involved. The app validates the full
state, expiry and response, verifies the tracker identity, and closes the listener on success,
cancellation or expiry. The phone and TV must share a network without client isolation.

18+ visibility follows AniList Viewer options. The account caches `displayAdultContent`,
refreshes at login and startup, and retains its own cached value on a failed lookup. No
AniList login, MAL-only login and a disabled preference all filter search, lists and local
history. More shows read-only information. There is no local override.

Resume compatibility follows `internal/synced_resume.go`: one notes/comments tag, original
text preserved, furthest valid position wins, matching episode only, four-second reads,
60-second pacing, ten-second minimum and five-second minimum change. Stop flushes the final
position. Finish advances to the next episode at zero or adds the final done line. Ordered
writes discard stale polls after completion. Both signed-in trackers receive edits.

## Focused checks

```sh
cd app
flutter analyze
flutter test
cd ..
node --test site/app-login/callback.test.cjs
```

Before acceptance, test real AniList and MAL approval and token expiry, the deployed callback
on phone and TV, Sony remote traversal and focus restoration, cutouts, keyboard and enlarged
text, system time/battery updates, HLS/ASS playback, track discovery and subtitle clearance.
These require actual accounts and Android devices.
