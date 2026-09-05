package theme

import (
	"os/exec"
	"strings"
	"sync"
)

// The menus are laid out on a character grid: the grid labels budget a title
// against a fixed column width, which only holds if every glyph is the same
// width. Omarchy's own menus are monospace too, so asking fontconfig for the
// system monospace family matches the desktop and keeps that budget honest.
//
// fontconfig is the source of truth Omarchy itself uses (`omarchy font current`
// is a wrapper around fc-match), so this follows `omarchy font set` for free and
// still works on machines with no Omarchy at all.

var (
	monoOnce sync.Once
	monoName string
)

// MonospaceFont returns the system monospace family, or "monospace" when
// fontconfig cannot be queried.
func MonospaceFont() string {
	monoOnce.Do(func() {
		monoName = "monospace"

		path, err := exec.LookPath("fc-match")
		if err != nil {
			return
		}
		out, err := exec.Command(path, "monospace", "-f", "%{family}").Output()
		if err != nil {
			return
		}

		// fc-match returns a comma-separated alias list, e.g.
		// "JetBrainsMono Nerd Font,JetBrainsMono NF"; the first entry is the name
		// pango expects.
		family := strings.TrimSpace(string(out))
		if family == "" {
			return
		}
		if idx := strings.Index(family, ","); idx > 0 {
			family = family[:idx]
		}
		if family = strings.TrimSpace(family); family != "" {
			monoName = family
		}
	})
	return monoName
}

// resetMonospaceForTest clears the cached lookup.
func resetMonospaceForTest() {
	monoOnce = sync.Once{}
	monoName = ""
}
