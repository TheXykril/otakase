// Package icons draws the small line icons in front of menu rows.
//
// The icons are Nerd Font glyphs: one colour, the size of a letter, so they
// sit in a row like text instead of the coloured pictures an emoji is. They
// live in the private-use area, which means a font has to supply them. Most
// desktops that would use them already have a Nerd Font (Omarchy ships one);
// for the rest otakase carries a tiny font with only these glyphs and installs
// it where fontconfig finds it (see font.go). Where neither can be relied on,
// the icons stay off and every label reads as plain text.
package icons

import (
	"sync"
	"sync/atomic"
	"time"
)

// Icon is one glyph, named by what it means rather than what it shows.
type Icon rune

// Codepoints are the Material Design range of Nerd Fonts (nf-md-*). Adding one
// here means regenerating the bundled font: run Build/icon-font.py.
const (
	Download  Icon = 0xF01DA // nf-md-download
	Changelog Icon = 0xF09ED // nf-md-text_box_outline
	OpenLink  Icon = 0xF03CC // nf-md-open_in_new
	Clock     Icon = 0xF0150 // nf-md-clock_outline
	Skip      Icon = 0xF04AD // nf-md-skip_next
	BellOff   Icon = 0xF0A91 // nf-md-bell_off_outline
	Play      Icon = 0xF040A // nf-md-play
	Tag       Icon = 0xF04FC // nf-md-tag_outline
)

// All is every icon in use. The bundled font is cut down to exactly these, and
// a test checks it still covers them.
var All = []Icon{Download, Changelog, OpenLink, Clock, Skip, BellOff, Play, Tag}

// gap separates an icon from its label. The glyphs are one cell wide, and a
// single space after them reads as cramped next to a letter.
const gap = "  "

var (
	enabled atomic.Bool
	// ready is closed once Resolve has decided; until then Enabled waits on it
	// briefly, so the first menu does not draw without icons only because the
	// font check was still running.
	ready     = make(chan struct{})
	readyOnce sync.Once
)

// readyWait bounds how long a label waits for the font check. It is normally
// done long before the first menu; a slow fontconfig only costs this much once.
const readyWait = 300 * time.Millisecond

// Resolve records whether icons are drawn. It is called once at startup.
func Resolve(on bool) {
	enabled.Store(on)
	readyOnce.Do(func() { close(ready) })
}

// Enabled reports whether labels get icons.
func Enabled() bool {
	select {
	case <-ready:
	case <-time.After(readyWait):
		// Undecided: draw plain. Settle it so later labels do not wait again.
		readyOnce.Do(func() { close(ready) })
	}
	return enabled.Load()
}

// String is the glyph followed by its gap, or nothing with icons off.
func (i Icon) String() string {
	if !Enabled() {
		return ""
	}
	return string(rune(i)) + gap
}

// Label puts the icon in front of text, or returns text alone with icons off.
func Label(i Icon, text string) string {
	return i.String() + text
}
