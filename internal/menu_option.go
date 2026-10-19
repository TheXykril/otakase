package internal

import "github.com/thexykril/otakase/internal/icons"

// The rows the menus are built from.

// SelectionOption is one row of a menu: what is shown and the key it answers
// with.
type SelectionOption struct {
	Title string
	Label string
	// Icon is drawn before the label when icons are on (see internal/icons).
	// It is not part of the label, so matching, filtering and sorting by
	// label are the same with icons on or off.
	Icon icons.Icon
	// Key is what the caller gets back. The menus answer "-1" for quit and
	// "-2" for back.
	Key       string
	Thumbnail string
	// ExtraData is whatever the source of the row wants back with it, such
	// as a provider's search hit.
	ExtraData      any
	HasNewEpisodes bool
}

// RofiSelectPreview is one tile of the rofi cover grid.
type RofiSelectPreview struct {
	Title      string `json:"title"`
	CoverImage string `json:"coverImage"`
	// HasNewEpisodes marks a show with aired episodes the viewer has not
	// seen (ShowNewEpisodes).
	HasNewEpisodes bool `json:"-"`
	// Rank is the display position this entry should hold. The grid is built
	// from a map, which has no order, so the ordering has to travel with the
	// value rather than being recovered by sorting titles at render time.
	Rank int `json:"-"`
}
