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

## Window

There is no system title bar on any OS (`window_manager`, `TitleBarStyle.hidden`; on Linux the GTK runner turns decorations off). The app draws its own top bar: the empty part of a page header moves the window and a double click maximises it (`WindowDragArea`), minimise, maximise and close sit at the top right (`WindowButtons`), and a 10 px strip along the top edge moves the window on every page. macOS keeps its traffic-light buttons over the top-left corner. Esc goes back one page. Code: `app/lib/desktop/window_frame.dart`.

The desktop window uses the wide TV layout, drawn on a 1920×1080 canvas and scaled to the window (never below 0.7), with a Schedule tab next to Home, Search and More. The mouse works everywhere; arrow keys and Enter still work too.

## Launcher

`otakase --launcher` opens a small always-on-top window that works like the CLI's rofi menus: type to filter, arrows or PageUp/PageDown to move, Enter to pick, Esc to clear the text, go back, then close.

- Main menu: Continue, the Watching list, history, Search anime, Open full app, Quit. Typing anything adds a "Search for" row.
- Show menu: Play next, Choose episode… (type a number), Replay, Open in full app.
- Ctrl+W or Open full app turns the same window into the full app.

Playing from the launcher grows the window to 1280×760 for the player. Code: `app/lib/desktop/launcher.dart`.

## Sign-in

AniList and MAL sign-in opens the default browser (`xdg-open`, `open` or `rundll32`, http/https links only). The app listens on a random port on `127.0.0.1` and puts it in the sign-in state; the site's callback page (`site/app-login/callback.js`) sends the browser back to `http://127.0.0.1:<port>/login`. The callback page change only works once the site is deployed from main.

## Player

The desktop player looks like the CLI's mpv skin (see [Player Look](Developer-Player-Look)): a title line with the window buttons at the top and one floating bar at the bottom with times, a seek rail (buffered part, opening and ending stripes) and small icon buttons for back/forward 10 s, play, next episode, mute, audio and subtitles, quality and full screen. Moving the mouse shows the controls; they and the pointer hide after 5 seconds. Keys: Space play/pause, Left/Right 10 s, F full screen, M mute. Code: `app/lib/desktop/player_bar.dart`.

## Not done yet

External mpv, cast, torrents, Discord-style device features and packaging (installers, signing) are still to do. Windows and macOS builds have not been tried on real machines.
