package internal

import (
	"regexp"
	"strings"
)

const (
	// DefaultUpdateRepo is the fork every update path pulls from: the
	// launch check, the update prompt, and `otakase -u` alike.
	DefaultUpdateRepo     = "TheXykril/otakase"
	updatePendingFileName = "update_pending.json"
)

var ()

// ansiStrip and pangoStrip remove terminal colour codes and pango tags, for
// matching a rofi selection back to its option.
var ansiStrip = regexp.MustCompile(`\x1b\[[0-9;]*m`)

var pangoStrip = regexp.MustCompile(`<[^>]*>`)

var pangoEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func escapePango(s string) string { return pangoEscaper.Replace(s) }
