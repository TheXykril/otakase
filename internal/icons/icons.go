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
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Icon is one glyph, named by what it means rather than what it shows.
type Icon rune

// Codepoints are the Material Design range of Nerd Fonts (nf-md-*). Adding one
// here means regenerating the bundled font: run Build/icon-font.py.
const (
	// The update prompt.
	Download  Icon = 0xF01DA // nf-md-download
	Changelog Icon = 0xF09ED // nf-md-text_box_outline
	OpenLink  Icon = 0xF03CC // nf-md-open_in_new
	Clock     Icon = 0xF0150 // nf-md-clock_outline
	Skip      Icon = 0xF04AD // nf-md-skip_next
	BellOff   Icon = 0xF0A91 // nf-md-bell_off_outline
	Play      Icon = 0xF040A // nf-md-play
	Tag       Icon = 0xF04FC // nf-md-tag_outline

	// Rows every menu has.
	Back Icon = 0xF004D // nf-md-arrow_left
	Quit Icon = 0xF0425 // nf-md-power
	Add  Icon = 0xF0415 // nf-md-plus

	// The main menu and the list categories.
	Watching   Icon = 0xF040D // nf-md-play_circle_outline
	AllShows   Icon = 0xF0279 // nf-md-format_list_bulleted
	Search     Icon = 0xF0349 // nf-md-magnify
	Edit       Icon = 0xF0CB6 // nf-md-pencil_outline
	Remap      Icon = 0xF04E1 // nf-md-swap_horizontal
	History    Icon = 0xF02DA // nf-md-history
	Surprise   Icon = 0xF1156 // nf-md-dice_multiple_outline
	Planning   Icon = 0xF00C3 // nf-md-bookmark_outline
	Completed  Icon = 0xF05E1 // nf-md-check_circle_outline
	Paused     Icon = 0xF03E6 // nf-md-pause_circle_outline
	Dropped    Icon = 0xF015A // nf-md-close_circle_outline
	Rewatching Icon = 0xF0456 // nf-md-repeat
	Tracker    Icon = 0xF04E6 // nf-md-sync
	Provider   Icon = 0xF048B // nf-md-server
	Cast       Icon = 0xF0118 // nf-md-cast
	CastOff    Icon = 0xF078A // nf-md-cast_off
	Stats      Icon = 0xF0128 // nf-md-chart_bar

	// Actions inside menus.
	Folder   Icon = 0xF0256 // nf-md-folder_outline
	Counter  Icon = 0xF0199 // nf-md-counter
	Star     Icon = 0xF04D2 // nf-md-star_outline
	Quality  Icon = 0xF0878 // nf-md-high_definition_box
	Info     Icon = 0xF02FD // nf-md-information_outline
	Yes      Icon = 0xF012C // nf-md-check
	No       Icon = 0xF0156 // nf-md-close
	Next     Icon = 0xF0054 // nf-md-arrow_right
	Refresh  Icon = 0xF0450 // nf-md-refresh
	Firewall Icon = 0xF1A11 // nf-md-wall_fire
	Number   Icon = 0xF03A0 // nf-md-numeric
	Audio    Icon = 0xF05CA // nf-md-translate
	Pin      Icon = 0xF0931 // nf-md-pin_outline
	Undo     Icon = 0xF054C // nf-md-undo
	TV       Icon = 0xF0502 // nf-md-television
	Merge    Icon = 0xF0F5C // nf-md-merge
	Import   Icon = 0xF02FA // nf-md-import
	Calendar Icon = 0xF00F0 // nf-md-calendar_clock
)

// All is every icon in use. The bundled font is cut down to exactly these, and
// a test checks it still covers them.
var All = []Icon{
	Download, Changelog, OpenLink, Clock, Skip, BellOff, Play, Tag,
	Back, Quit, Add,
	Watching, AllShows, Search, Edit, Remap, History, Surprise, Planning,
	Completed, Paused, Dropped, Rewatching, Tracker, Provider, Cast, CastOff, Stats,
	Folder, Counter, Star, Quality, Info, Yes, No, Next, Refresh, Firewall,
	Number, Audio, Pin, Undo, TV, Merge, Import, Calendar,
}

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
	if i == 0 || !Enabled() {
		return ""
	}
	return string(rune(i)) + gap
}

// Label puts the icon in front of text, or returns text alone with icons off.
// A zero Icon is no icon.
func Label(i Icon, text string) string {
	if i == 0 {
		return text
	}
	return i.String() + text
}

// Strip removes a leading icon and its gap, so a row rofi hands back can be
// matched to the label it was drawn from.
func Strip(text string) string {
	runes := []rune(text)
	if len(runes) == 0 || !privateUse(runes[0]) {
		return text
	}
	return strings.TrimLeft(string(runes[1:]), " ")
}

// privateUse reports whether r is in a Unicode private-use area, where every
// Nerd Font glyph lives.
func privateUse(r rune) bool {
	return (r >= 0xE000 && r <= 0xF8FF) || (r >= 0xF0000 && r <= 0xFFFFD) || (r >= 0x100000 && r <= 0x10FFFD)
}
