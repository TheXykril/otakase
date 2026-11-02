package internal

// A desktop opens otakase:// links through otakase-link.desktop
// (internal/appicon): `otakase -rofi %u` with no terminal, so a Play button
// does not leave an empty terminal window behind. LinkMenus picks where the
// link's menus go: "rofi" (the default) or "terminal". With "terminal", or
// without rofi installed, otakase reopens itself in a terminal.

// LinkMenusTerminal is the LinkMenus value that opens links in a terminal.
const LinkMenusTerminal = "terminal"
