# Theming

How the menu palette is resolved (`internal/theme`). For the user side, see
[Configuration → Theming](Configuration#theming).

## One set of colour names

Every source is translated into the colour names of an Omarchy `colors.toml`
(`background`, `foreground`, `accent`, `selection`, `muted`, `red`, ...) and
passed to `paletteFromValues`, which is the one place a palette role is
chosen. Anything a source leaves out falls back to `Builtin()` or
`BuiltinLight()`, picked by the `mode` key if the source gives one, otherwise
by the background's luminance.

Desktops that expose only dark/light and an accent go through `withAccent`
instead: the builtin palette for that brightness with `Accent` and `Selection`
swapped for the desktop's accent.

## Sources

| Mode | Reads | Mapping |
|---|---|---|
| `omarchy` | `~/.local/state/omarchy/current/theme/colors.toml` | names as they are |
| `wal` | `$XDG_CACHE_HOME/wal/colors.json` | `special.background/foreground`, `color4` accent and blue, `color8` muted, `color1`-`color6` red..cyan, `color15` bright foreground |
| `base16` | tinty: `$XDG_DATA_HOME/tinted-theming/tinty/current_scheme` → `repos/schemes/<system>/<slug>.yaml`; else stylix `~/.config/stylix/palette.json`, `/etc/stylix/palette.json` | `base00` background, `01` lighter background, `02` selection, `03` muted, `04` dark foreground, `05` foreground, `07` bright foreground, `08` red, `0A` yellow, `0B` green, `0C` cyan, `0D` blue and accent, `0E` magenta |
| `kde` | `$XDG_CONFIG_HOME/kdeglobals` | `[Colors:View]` background/foreground/inactive; negative, positive, neutral, link, visited text as red, green, yellow, blue, magenta; `[Colors:Selection]` selection; `[General] AccentColor` accent, else selection |
| `gnome` | `gsettings` `color-scheme`, `gtk-theme`, `accent-color` | accent only, libadwaita's named accent colours |
| `macos` | `defaults read -g AppleInterfaceStyle`, `AppleAccentColor` | accent only, Apple's system colours for that mode |
| `windows` | registry `Themes\Personalize\AppsUseLightTheme`, `DWM\AccentColor` (0xAABBGGRR) | accent only |

`ThemeFile` (`LoadFile`) accepts a pywal `colors.json`, a base16 scheme in
YAML or JSON, or a `colors.toml`, told apart by content.

## Resolution order

`Resolve(mode, file)`:

1. `builtin` returns the builtin palette and ignores everything else.
2. `auto` with a `ThemeFile` reads the file.
3. A named mode loads that source only; failure falls back to the builtin with
   the error logged.
4. `auto` walks `autoOrder`: Omarchy, wal, base16, KDE, GNOME, macOS, Windows.
   Each source's `available()` is a cheap check (a file exists, the desktop
   matches `XDG_CURRENT_DESKTOP`, the OS matches). A source that is available
   but fails to read is logged and the walk continues.

KDE and GNOME are only auto-detected on their own desktop, since
`kdeglobals` and the GNOME settings stay around after switching desktops.
Generated palettes (wal, base16) come before desktop schemes because they are
chosen deliberately, often on top of a desktop scheme nobody changed.

`ThemeOverrides` is applied after all of this, in `internal/theming.go`.

## Adding a source

Write a loader that turns the source into the colour names above, add it to
`sources` and `autoOrder` in `sources.go`, give it a `Mode` and `ParseMode`
aliases, and test the parser with a fixture. Commands run through `runCommand`
(two-second timeout) and paths through `homeDir`/`getenv`, so tests can fake
both.

There is no "use the terminal's colours" mode: palettes must be real hex
colours, because rofi themes are written from them and several roles are
blended (`Mix`) or contrast-checked.
