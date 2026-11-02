package internal

import (
	"strings"
)

// The update prompt shows what changed between the version you have and the
// one on offer. That text comes from CHANGELOG.md at the new release's tag,
// which is written for people using otakase. The GitHub release body is the
// fallback only: it is a commit and pull request list ("feat(cast): ... by
// @someone in https://..."), written for whoever maintains the code.

var ()

// wrapWords breaks text into lines of at most width characters, at spaces.
func wrapWords(text string, width int) []string {
	if width < 10 {
		width = 10
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
