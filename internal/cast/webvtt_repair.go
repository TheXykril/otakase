package cast

import (
	"regexp"
	"strings"
)

var vttBlockSeparator = regexp.MustCompile(`\n[ \t]*\n`)
var vttCueTiming = regexp.MustCompile(`^(?:\d{2,}:)?\d{2}:\d{2}\.\d{3}[ \t]+-->[ \t]+(?:\d{2,}:)?\d{2}:\d{2}\.\d{3}(?:[ \t].*)?$`)

// RepairWebVTT drops orphan text blocks that otherwise stop native demuxers
// before later cues. Valid files and other subtitle formats retain their bytes.
func RepairWebVTT(body []byte) []byte {
	text := strings.TrimPrefix(string(body), "\ufeff")
	if !strings.HasPrefix(text, "WEBVTT") {
		return body
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	kept := []string{}
	dropped := false
	for _, raw := range vttBlockSeparator.Split(text, -1) {
		block := strings.Trim(raw, "\n")
		if strings.TrimSpace(block) == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		first := lines[0]
		header := len(kept) == 0 && strings.HasPrefix(first, "WEBVTT")
		metadata := first == "NOTE" || strings.HasPrefix(first, "NOTE ") || strings.HasPrefix(first, "NOTE\t") || first == "STYLE" || first == "REGION"
		cue := vttCueTiming.MatchString(first) || (len(lines) > 1 && vttCueTiming.MatchString(lines[1]))
		if header || metadata || cue {
			kept = append(kept, block)
		} else {
			dropped = true
		}
	}
	if !dropped {
		return body
	}
	return []byte(strings.Join(kept, "\n\n") + "\n")
}
