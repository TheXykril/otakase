//go:build toolkit

// Package internal pins the TUI libraries that are chosen but not yet used.
//
// go mod tidy removes any dependency nothing imports, which is how bubbles was
// lost the first time it was added. These blank imports keep the versions
// recorded so the decision survives, and the build tag keeps the packages out
// of the binary until real code imports them.
//
// bubbles is Charm's component library. textinput is in real use by the
// prompt; list and viewport are still pinned for later. The
// menu currently hand-rolls all of those -- the filter is manual string
// handling and scrolling is manual offset arithmetic, which has already been
// the source of one bug where the tab bar scrolled off the top.
//
// rasterm draws images through a terminal's own protocol. It was used for
// covers in the detail pane and removed when that did not work; the remaining
// obstacle is recorded in the history of internal/poster_source.go.
package internal

import (
	_ "github.com/BourgeoisBear/rasterm"
	_ "github.com/charmbracelet/bubbles/list"
	_ "github.com/charmbracelet/bubbles/viewport"
)
