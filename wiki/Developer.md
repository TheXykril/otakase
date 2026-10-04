# Developer

Pages for people working on otakase itself. Nothing here is needed to install
or use it; that is all in the [README](https://github.com/TheXykril/otakase#readme).

## Pages

- **[Providers](Developer-Providers)** — how stream sources are built, registered and turned off, and how to add one
- **[Casting Verification](Developer-Casting-Verification)** — checking casting against a real Chromecast, DLNA TV or Kodi
- **[Versions and releases](Developer-Releases)** — the `YY.DROP.HOTFIX` numbering and how a release is cut
- **[Cast Window Prompts](Developer-Cast-Window-Prompts)** — what prompts do in a terminal opened by a rofi cast, where nobody is watching
- **[Player Look](Developer-Player-Look)** — how otakase's own mpv controls are shipped, themed and loaded
- **[Theming](Developer-Theming)** — where the menu colours come from and how each desktop's theme is read

## Building and testing

```bash
git clone https://github.com/TheXykril/otakase.git
cd otakase
go build ./cmd/otakase
go vet ./...
go test -short -race ./...
```

CI runs the same `go vet` and tests plus a Linux build on every pull request.
Dependencies are vendored (`vendor/`).

## Debugging

Everything otakase does that can fail is logged to
`<StoragePath>/otakase-debug.log` (default `~/.local/share/otakase/`),
including ffmpeg's own output during casts and downloads.
`otakase -provider-status` probes each provider and reports which respond.

## Menu icons

Menu rows can start with a Nerd Font icon (`internal/icons`). Each icon is a
constant there. Give an option one with `SelectionOption{..., Icon: icons.X}`:
both menus draw it in front of the label, and leave it out when `Icons`
resolves to off. It is not part of `Label`, so matching a rofi selection,
filtering and sorting work on the label alone. Back, Quit and Add new get
theirs from their keys. Text that is not a menu row can use
`icons.Label(icons.X, "Text")`.

Icons go on actions and categories, not on rows that are values (an anime
title, a quality, a tracker name): a column of the same icon says nothing.
The terminal's category tabs and rofi's Lists menu share each list's icon
(`categoryIcon`), and rofi's toolbar buttons use the action's. A toggle shows
its state in its icon and its words, not a checkbox: the cast button uses
`CastOff` while casting is off.

rofi's toolbar (`internal/rofi_toolbar.go`) is a row of rofi `button-*`
widgets added to the theme's mainbox with `-theme-str`. Each button's action
is a `kb-custom-N` binding, so a click and its Ctrl key both end the menu with
exit code `9+N`. The rofi defaults on the same keys are unbound for that menu,
or rofi refuses to start. In rasi, keep one quoted string per line: rofi's
lexer reads a string to the last quote on its line.

otakase embeds `internal/icons/font/OtakaseSymbols.ttf`, Symbols Nerd Font Mono
cut down to only the glyphs in use, and installs it to the user's font
directory so rofi and terminals find it through fontconfig. After adding an
icon, regenerate the font with `Build/icon-font.py` (instructions at the top of
the script); `TestBundledFontCoversEveryIcon` fails until you do.

## Pull requests and releases

Open pull requests against `main` with a conventional title and fill in the
template. Version numbering and how a release is cut are on
[Versions and releases](Developer-Releases).

## Editing this wiki

The wiki is kept in the [`wiki/`](https://github.com/TheXykril/otakase/tree/main/wiki)
folder of the main repository. A workflow copies it here on every push to
`main`, so edit the files there; edits made on the wiki directly are
overwritten by the next sync. Pages for users go at the top level; developer
pages are named `Developer-*.md` and listed in the sidebar's Developer section.
