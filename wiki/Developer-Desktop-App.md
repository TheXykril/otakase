# Desktop App

The Flutter app in `app/` also builds for Windows, macOS and Linux. This page covers how the desktop build is put together. The desktop app is in early development and is not released yet; the command-line otakase is unchanged.

## Go core: the otakase-core helper

On Android the Go core is linked into the app (gomobile `.aar` from `mobilecore`). On desktop the app starts a helper process instead, `otakase-core` (`cmd/otakase-core`), and talks to it over stdin and stdout. Reasons:

- It builds with `CGO_ENABLED=0` for every OS, the same way as the CLI. No C toolchain is needed for the core.
- A provider that crashes or hangs takes down the helper, not the window. The app starts it again on the next request.
- `mobilecore` owns the `providerhost` hooks, so it must not share a process with the CLI code that later moves into the desktop core.

The app looks for the helper next to its executable (Windows, Linux) or in `Otakase.app/Contents/Resources` (macOS). Set `OTAKASE_CORE=/path/to/otakase-core` to use another build while developing.

### Protocol

One JSON object per line. Requests carry the same JSON the Android binding passes to `mobilecore.Core.Start`.

```
app  -> core  {"id":7,"op":"request","input":{"version":1,"operation":"search","provider":"anikoto","query":"frieren","mode":"sub"}}
app  -> core  {"id":7,"op":"cancel"}
app  -> core  {"op":"stop_playback"}
core -> app   {"event":"ready","version":1}
core -> app   {"id":7,"result":{"version":1,"data":{...}}}
core -> app   {"event":"error","result":{"version":1,"error":{...}}}
```

- Ids are positive and chosen by the app. Results come back in completion order.
- Every request gets exactly one result line, also after `cancel`.
- Errors that belong to no request (a line that is not JSON, a missing id) come back as `"event":"error"`.
- Lines are limited to 64 KiB. Closing stdin cancels all work and the helper exits, so it never outlives the app.

The server is `mobilecore/stdio`; the app side is `app/lib/desktop/desktop_core.dart`. If the helper exits, running requests fail with `core_exited` (retryable).

## Building

Flutter cannot cross-compile desktop apps. Build on the OS you build for:

```
cd app
scripts/build-desktop.sh            # release
scripts/build-desktop.sh debug
```

The script runs `flutter build <os>` and builds `otakase-core` into the bundle.

Linux needs clang, cmake, ninja (Arch: `ninja`), `libgtk-3-dev`, `libsecret-1-dev` and `libmpv-dev`. media_kit downloads mimalloc from GitHub during the first CMake run.

To run from source:

```
go build -o /tmp/otakase-core ./cmd/otakase-core
cd app && OTAKASE_CORE=/tmp/otakase-core flutter run -d linux
```

## Tests

`flutter test` runs on a desktop host but exercises the Android paths: `isDesktop` is false under `flutter test` unless a test sets `debugDesktopOverride`. `test/desktop_core_test.dart` checks the app side against a fake helper; `go test ./mobilecore/stdio` checks the server, including one request through the real core.

To look at the desktop pages without a desktop, `flutter test test/desktop_screens_test.dart --update-goldens --dart-define=DESKTOP_SCREENSHOTS=<out dir> --dart-define=SCREENSHOT_ART=<art dir>` writes PNGs of each page. The art folder needs `cover.jpg` and `banner.jpg`; for fuller pages add `gallery.json` (a list of `{i, t, g, studio, score, f, y, ep, banner}`) with `c<i>.jpg` covers and `b<i>.jpg` banners. The images stay out of Git.

## Window

There is no system title bar on any OS (`window_manager`, `TitleBarStyle.hidden`; on Linux the GTK runner turns decorations off). The app draws its own top bar: the empty part of a page header moves the window and a double click maximises it (`WindowDragArea`), minimise, maximise and close sit at the top right (`WindowButtons`; tiling window managers such as Hyprland, sway, i3 and niri, spotted from their environment variables, only get close because they have no minimised or maximised state), and a 10 px strip along the top edge moves the window on every page. macOS keeps its traffic-light buttons over the top-left corner. Esc goes back one page. Code: `app/lib/desktop/window_frame.dart`.

The top bar has the tabs in the middle on every system. Windows and Linux put the logo on the left and the window buttons on the right; macOS keeps its own buttons on the left, so the logo sits on the right there. The window buttons are drawn like the system's own (`WindowButtonStyle`): square full-height buttons on Windows, round grey ones on GNOME and other Linux desktops, Breeze arrows on KDE, only close on tiling window managers. A future account button goes beside the window buttons (on macOS beside the coloured dots), with a thin line between.

There is no full-width bar behind the top bar. Each group sits on its own soft dark island instead (`HeaderIsland` in `app/lib/desktop/header_island.dart`): the logo and the window buttons grow out of the top corners, and the tabs hang from the middle of the top edge. The islands are sized to what they hold, curve into the top edge, and have blurred edges so they fade into the art with no hard line.

The desktop window uses the wide TV layout, drawn on a 1920×1080 canvas and scaled to the window (never below 0.7), with a Schedule tab next to Home, Search and More. The mouse works everywhere; arrow keys and Enter still work too.

## Pages

The desktop pages share the phone and TV data but have their own layout, picked with `isDesktop`:

- **Home** (`desktop/desktop_home.dart`): a big hero that starts with the shows you are watching and then trending picks, changing every 9 seconds unless the pointer rests on it; wide Continue watching cards (up to four across, fewer shows stretch to fill the row); shelves whose cards are sized so the row always ends at the window edge (`fittedCardWidth` in `ui/components.dart`).
- **Show page**: cover and details over the banner, Sub/Dub beside the play button, an episode thumbnail grid in blocks of 50 (up to four range buttons; longer shows get one range menu with arrows), and an info sidebar with studio, format, status, length, other titles and related shows. With a mouse, pages never scroll by themselves to the focused control; only arrow keys and Tab do (`DesktopInput`). Pages pushed over the tabs use the same top bar (`DesktopTopBar` in `desktop/top_bar.dart`) with a back button by the logo.
- **Search**: no page heading; Filters opens a menu under the button that applies each choice at once, and chosen filters show as chips with a × under the search field.
- **Schedule**: the whole week in seven columns, shows on your list or in your history outlined, My list only / Everything, and arrows for other weeks.
- **More**: settings sections in a sidebar with the chosen section beside it as grouped cards (`SettingsPage(embedded: true)`); Discord, Watch rooms and the rest still open on their own.

## Launcher

`otakase --launcher` opens a small always-on-top window that works like the CLI's rofi menus: type to filter, arrows or PageUp/PageDown to move, Enter to pick, Esc to clear the text, go back, then close.

- Main menu: Continue, the Watching list, history, Search anime, Open full app, Quit. Typing anything adds a "Search for" row.
- Show menu: Play next, Choose episode… (type a number), Replay, Open in full app.
- Ctrl+W or Open full app turns the same window into the full app.

Playing from the launcher grows the window to 1280×760 for the player. Code: `app/lib/desktop/launcher.dart`.

## Sign-in

AniList and MAL sign-in opens the default browser (`xdg-open`, `open` or `rundll32`, http/https links only). The app listens on `127.0.0.1:47613` (`AuthService.desktopLoginPort`) and marks the sign-in state `desktop: true`; the site's callback page (`site/app-login/callback.js`) sends the browser back to that fixed address. The state never carries a desktop host or port, so a crafted sign-in link cannot send the login anywhere else. TV sign-ins show a four-letter check code beside the QR code and the phone page shows the same code (`check: true` in the state), so the viewer can see the page belongs to their own TV. The callback page change only works once the site is deployed from main.

## Player

The desktop player looks like the CLI's mpv skin (see [Player Look](Developer-Player-Look)): a title line with the window buttons at the top and one floating bar at the bottom with times, a seek rail (buffered part, opening and ending stripes) and small icon buttons for back/forward 10 s, play, next episode, mute, audio and subtitles, quality and full screen. Moving the mouse shows the controls; they and the pointer hide after 5 seconds. Keys: Space play/pause, Left/Right 10 s, F full screen, M mute. Code: `app/lib/desktop/player_bar.dart`.

## Packaging and updates

`app/scripts/package-desktop.sh` turns a release build from `build-desktop.sh` into the file the release offers, plus a metadata file with its version, size and SHA-256 (`scripts/update-manifest.py`, the same format as the Android `otakase-android.json` but with `file` instead of `apk`). Output goes to `app/build/desktop-release/`.

| OS | Release file | How the app updates |
| --- | --- | --- |
| Windows | `otakase-app-windows-setup.exe` (Inno Setup, per user, `app/packaging/windows/otakase-app.iss`) | Runs the new installer with `/VERYSILENT /WAITPID=<pid>` and quits. Setup waits for the app to exit, installs over the same folder and opens it again. |
| macOS | `otakase-app-macos.zip` (universal, ad hoc signed) | Unpacks next to `Otakase.app`, swaps the two folders, opens the new one once the old process has exited. |
| Linux | `otakase-app-linux-x86_64.AppImage` (`app/packaging/linux/`) | Downloads next to the running AppImage (`$APPIMAGE`), renames it over the old file and starts it once the old process has exited. |

The version is VERSION.txt (or `OTAKASE_VERSION`), passed in as `--dart-define OTAKASE_VERSION` and compared as the same number Android uses (26.7.0 -> 26007000, `versionCode` in `app/lib/data/app_updates.dart`). A build without it counts as older than every release.

The check, download and checksum are shared with Android (`AppUpdates`); only finding the files and installing differ (`DesktopUpdatePlatform` in `app/lib/desktop/desktop_updates.dart`). A copy that cannot replace itself gets an **Open download page** button instead: a Windows folder not made by the installer (no `unins000.exe`), an app in a folder the user cannot write to, a Linux install that is not an AppImage (a package manager updates those). Settings > About & updates shows it, and the check runs at startup when "Check for app updates at startup" is on.

Every desktop download carries mpv, so nobody installs it separately. Windows and macOS get libmpv from media_kit's bundled libraries. The AppImage copies the build machine's `libmpv.so.2` and the libraries it needs into `usr/lib/otakase/lib` (`bundle_libmpv` in `package-desktop.sh`), points them at each other with `patchelf --add-rpath '$ORIGIN'`, and `AppRun` sets `LIBMPV_LIBRARY_PATH` so media_kit loads that copy. Libraries that must match the running system (glibc, the C++ runtime, GL/Vulkan drivers, X11/Wayland, sound servers, GTK, fontconfig) come from the system. Ubuntu's libmpv pulls in a lot, so the AppImage is around 90 MB. macOS turns the app sandbox off: the app runs mpv's library, a helper process and replaces itself, none of which a sandboxed app outside the App Store may do.

CI: the `desktop` job in `ci-cd.yml` builds and packs on one runner per OS. It runs for every release, or for a normal run with **desktop** ticked (macOS runners use more Actions minutes). The release job attaches all six files.

## Not done yet

External mpv, cast, torrents, deb/rpm packages and signing (Windows, Apple) are still to do. Windows and macOS builds have not been tried on real machines.
