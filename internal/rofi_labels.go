package internal

import (
	"fmt"
	"regexp"
	"strings"
)

// Rofi rows carry real structure that a single flat string throws away:
//
//	One Piece · 1171 eps [anipub]
//	Attack on Titan — TV • 25 Episodes [anineko]
//
// The title is what you scan for; the episode count and provider are there to
// confirm a choice you have already made. Setting them at the same weight makes
// every row an undifferentiated wall of text, so the metadata is dimmed and the
// titles are left to carry the list.
//
// Curd already passes -markup-rows, which also means labels are interpreted as
// pango markup: an unescaped "&" in a title breaks the row. Escaping happens
// here, and parseRofiSelection unescapes on the way back.

var (
	// rofiTrailingProvider matches the " [provider]" suffix qualifySelectionOption
	// appends when several providers are searched at once.
	rofiTrailingProvider = regexp.MustCompile(`\s+\[[^\[\]]+\]\s*$`)
	// rofiMetaSeparator marks where a provider's own metadata starts.
	rofiMetaSeparator = regexp.MustCompile(`\s+[·—•]\s`)
	// pangoEntity matches the escapes pango recognises.
	pangoEntity = regexp.MustCompile(`&(amp|lt|gt|quot|#39);`)
)

// escapePango lives in update.go, which already needed it for release notes.

// unescapePango reverses escapePango so a selection can be matched back to the
// option it came from.
func unescapePango(text string) string {
	return pangoEntity.ReplaceAllStringFunc(text, func(entity string) string {
		switch entity {
		case "&lt;":
			return "<"
		case "&gt;":
			return ">"
		case "&quot;":
			return `"`
		case "&#39;":
			return "'"
		default:
			return "&"
		}
	})
}

// splitRofiLabel separates a row's title from its trailing metadata.
func splitRofiLabel(label string) (title, meta string) {
	rest := strings.TrimSpace(label)
	suffix := ""

	if match := rofiTrailingProvider.FindStringIndex(rest); match != nil {
		suffix = strings.TrimSpace(rest[match[0]:])
		rest = rest[:match[0]]
	}

	if match := rofiMetaSeparator.FindStringIndex(rest); match != nil {
		meta = strings.TrimSpace(rest[match[0]:])
		rest = rest[:match[0]]
	}

	if suffix != "" {
		if meta == "" {
			meta = suffix
		} else {
			meta = meta + " " + suffix
		}
	}

	return strings.TrimSpace(rest), meta
}

// rofiRowMarkup renders one row: title at full strength, metadata dimmed.
func rofiRowMarkup(label string) string {
	title, meta := splitRofiLabel(label)
	if title == "" {
		// Nothing recognisable to split; escape and leave it alone.
		return escapePango(strings.TrimSpace(label))
	}
	if meta == "" {
		return escapePango(title)
	}
	return fmt.Sprintf("%s <span foreground=\"%s\">%s</span>",
		escapePango(title), rofiMetaColor, escapePango(meta))
}

// gridLabelGap separates a truncated title from its counts. splitRofiLabel
// already returns the "·" as part of the metadata, so only a space is needed
// here -- adding another separator produced "Title · · 4/24".
const gridLabelGap = " "

// GridRowMarkup renders one poster-grid label, budgeting the title so the
// episode counts always survive.
//
// The grid clips a label at the column width, and a long anime title used the
// whole line, so the counts -- the one thing a cover cannot tell you -- were the
// part that disappeared. The title absorbs the truncation instead, and the
// counts stay pinned at the end.
//
// The budget is in characters, which is only meaningful because the menus are
// set in a monospace face; see theme.MonospaceFont.
func GridRowMarkup(label string, capacity int) string {
	title, meta := splitRofiLabel(label)
	if title == "" {
		return escapePango(strings.TrimSpace(label))
	}
	if meta == "" {
		return escapePango(truncateRunes(title, capacity))
	}

	budget := capacity - len([]rune(meta)) - len([]rune(gridLabelGap))
	if budget < gridLabelMinTitle {
		// The counts are so long that truncating the title further would leave
		// nothing recognisable; let the row clip rather than render a stub.
		budget = gridLabelMinTitle
	}

	return fmt.Sprintf("%s<span foreground=\"%s\">%s%s</span>",
		escapePango(truncateRunes(title, budget)),
		rofiMetaColor,
		escapePango(gridLabelGap),
		escapePango(meta))
}

// gridLabelMinTitle is the fewest title characters worth showing.
const gridLabelMinTitle = 8

// truncateRunes shortens text to at most limit characters, ending with an
// ellipsis. It counts runes, not bytes, so a multi-byte title is not cut mid
// character.
func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if limit <= 0 {
		return ""
	}
	if len(runes) <= limit {
		return text
	}
	if limit == 1 {
		return "…"
	}
	return strings.TrimRight(string(runes[:limit-1]), " ") + "…"
}
