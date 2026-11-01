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

Linux needs clang, cmake, ninja, `libgtk-3-dev`, `libsecret-1-dev` and `libmpv-dev`. media_kit downloads mimalloc from GitHub during the first CMake run.

To run from source:

```
go build -o /tmp/otakase-core ./cmd/otakase-core
cd app && OTAKASE_CORE=/tmp/otakase-core flutter run -d linux
```

## Tests

`flutter test` runs on a desktop host but exercises the Android paths: `isDesktop` is false under `flutter test` unless a test sets `debugDesktopOverride`. `test/desktop_core_test.dart` checks the app side against a fake helper; `go test ./mobilecore/stdio` checks the server, including one request through the real core.

## Not done yet

The device features the Android host provides (`otakase/device` channel: login redirect, opening links, status) have no desktop implementation yet, and the layout is still the phone layout. The plan covers the desktop window layout, a launcher mode like the CLI's rofi menus, external mpv, cast, torrents and packaging.
