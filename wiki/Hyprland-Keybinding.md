# Hyprland keybinding

One command binds **Super+Shift+A** to open the rofi menu with poster previews —
no terminal, pick a show, watch it:

```bash
otakase -install-keybind
```

Launched this way there is no terminal, so install `rofi` (the menus),
`libnotify` (progress and errors have nowhere else to go) and `xdg-utils` (the
sign-in browser, if you have not signed in yet).

It writes to `~/.config/hypr/bindings.lua` on Omarchy, or `hyprland.conf` on a
stock Hyprland, backs the file up first, and is safe to run twice. If something
already owns that key it tells you what and changes nothing; `-force-keybind`
takes it anyway and comments out the old line rather than deleting it.
`-remove-keybind` undoes the whole thing.

**Installing the package does this for you**, and uninstalling removes the
binding again. Set `OTAKASE_NO_KEYBIND=1` before installing to skip it. An
upgrade only refreshes a binding that is already there; it will not re-add one
you removed.
