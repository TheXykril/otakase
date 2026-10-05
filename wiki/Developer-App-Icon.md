# App Icon

The icon is 任 with a dark red shadow over お任せ and OTAKASE, drawn in
`site/img/icon.svg`. `site/img/favicon.svg` is the small version with 任 alone,
used wherever the full icon would be under 64 px. Both are the source for
every other copy; never edit a generated file by hand.

## Regenerating

After changing either SVG:

```bash
cd Build/app-icon && go run .
```

`Build/app-icon` is its own Go module, so its dependencies stay out of
otakase's `vendor/`. It writes:

| File | Used by |
|---|---|
| `cmd/otakase/rsrc_windows_*.syso` | `otakase.exe`'s icon. `go build` links a `.syso` beside `main.go` by its `_windows_<arch>` suffix, so other platforms never see it |
| `Build/app-icon/otakase.ico` | The Windows installer (`SetupIconFile`) |
| `Build/app-icon/png/otakase-<size>.png` | The PKGBUILD's hicolor icons and the release icon pack |
| `Build/app-icon/otakase.icns` | The macOS app bundle (below) |
| `internal/appicon/otakase.png`, `otakase-small.png`, `otakase.svg` | Embedded in the binary |
| `internal/mpvskin/assets/logo/otakase.ass` | The player's loading logo |

The social preview card and the release banner, `site/img/social-preview.png`
and `site/img/release-banner.png`, are renders like the site's other PNGs.

## Where it shows

- **Windows**: `otakase.exe`, its Start menu and desktop shortcuts, the
  installer and *Apps & features*. winget takes the icon from the installer
  itself; its manifests have no icon field to fill.
- **Linux app menu**: `internal/appicon` installs `otakase.desktop` and the
  icon under `~/.local/share` on the first run, unless a package already put
  them in `/usr/share` (the PKGBUILD does). A file is only rewritten when its
  contents changed.
- **Notifications**: every notification without a cover image of its own
  carries the icon. On Linux it is the installed icon's path; elsewhere a copy
  in the user cache directory.
- **The player window**: on Linux mpv is started with
  `--wayland-app-id=otakase` and `--x11-name=otakase`, each only when that mpv
  has the option, so the desktop matches the window to `otakase.desktop`
  (`StartupWMClass=otakase`) and shows its icon. A window rule written for
  mpv's own app id no longer matches these windows; MpvArgs can set either
  flag back. Windows and macOS take a window's icon from the program file,
  which is mpv's.
- **The player's loading screen**: until the first frame, the mpv skin draws
  the logo in the middle of the window, in the theme's accent and text
  colours.
- **rofi**: the small icon sits in front of the search box. It is written
  beside the rofi themes as `otakase-icon.png`.
- **README, releases and link previews**: the README header, a banner at the
  top of each release's notes, `otakase-icons.zip` among the release files,
  and the site's `og:image`. The repository's own social preview is set by
  hand: Settings → General → Social preview, upload
  `site/img/social-preview.png`.

## macOS app bundle

Not shipped yet. `Build/macos-app/make-app.sh <binary>` wraps a macOS build in
`Otakase.app` with the icon, opening otakase in Terminal when launched. It
waits for someone to test it on a Mac, and for signing, before it joins a
release.
