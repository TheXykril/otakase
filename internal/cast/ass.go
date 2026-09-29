package cast

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ShiftSubtitles writes src to dst with every line moved earlier by seconds,
// in whichever format src is written in.
//
// The format is read from the content rather than the name: the file is saved
// under the extension its URL named, and a URL is not always honest about what
// it serves. ASS is the one format that needs its own handling -- its timings
// sit in Dialogue lines, not "-->" lines -- and before this it went through the
// WebVTT shift untouched, so every line was burned the seek distance late.
// SubRip shares WebVTT's timing lines and is handled by ShiftWebVTT.
func ShiftSubtitles(src, dst string, seconds float64) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("cast: could not read the subtitles: %w", err)
	}
	if isASS(string(raw)) {
		return shiftASS(string(raw), dst, seconds)
	}
	return ShiftWebVTT(src, dst, seconds)
}

// isASS reports whether a subtitle file is ASS/SSA, by its section headers.
func isASS(body string) bool {
	body = strings.TrimPrefix(body, "\ufeff")
	return strings.Contains(body, "[Script Info]") || strings.Contains(body, "[Events]")
}

// shiftASS moves every Dialogue and Comment line earlier by seconds, with the
// same rules ShiftWebVTT applies to cues: a line that ends before the new zero
// is dropped, and one that straddles it starts at zero.
func shiftASS(body, dst string, seconds float64) error {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))

	// Start and End are the second and third fields unless the [Events] Format
	// line says otherwise.
	startField, endField, fieldCount := 1, 2, 10
	section := ""
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "[") {
			section = strings.ToLower(trimmed)
		}
		key, rest, found := strings.Cut(line, ":")
		if !found {
			out = append(out, line)
			continue
		}
		switch strings.TrimSpace(key) {
		case "Format":
			// The styles section has a Format line of its own, describing
			// styles; only the one under [Events] places Start and End.
			if section != "[events]" {
				out = append(out, line)
				continue
			}
			names := strings.Split(rest, ",")
			for i, name := range names {
				switch strings.ToLower(strings.TrimSpace(name)) {
				case "start":
					startField = i
				case "end":
					endField = i
				}
			}
			fieldCount = len(names)
			out = append(out, line)
		case "Dialogue", "Comment":
			shifted, keep := shiftASSEvent(rest, seconds, startField, endField, fieldCount)
			if keep {
				out = append(out, key+":"+shifted)
			}
		default:
			out = append(out, line)
		}
	}

	if err := os.WriteFile(dst, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return fmt.Errorf("cast: could not write the shifted subtitles: %w", err)
	}
	return nil
}

// shiftASSEvent moves the timing fields of one event, reporting whether it
// still belongs in the file. An event this cannot read is kept as it was, for
// the reason ShiftWebVTT keeps an unreadable cue.
func shiftASSEvent(rest string, seconds float64, startField, endField, fieldCount int) (string, bool) {
	// Split only as far as the field count: Text is last and may hold commas.
	fields := strings.SplitN(rest, ",", fieldCount)
	if startField >= len(fields) || endField >= len(fields) {
		return rest, true
	}
	start, startErr := parseASSTime(fields[startField])
	end, endErr := parseASSTime(fields[endField])
	if startErr != nil || endErr != nil {
		return rest, true
	}

	start -= seconds
	end -= seconds
	if end <= 0 {
		return "", false
	}
	if start < 0 {
		start = 0
	}
	// The leading space after "Dialogue:" belongs to the first field.
	fields[startField] = keepLeadingSpace(fields[startField], formatASSTime(start))
	fields[endField] = keepLeadingSpace(fields[endField], formatASSTime(end))
	return strings.Join(fields, ","), true
}

func keepLeadingSpace(original, value string) string {
	return original[:len(original)-len(strings.TrimLeft(original, " "))] + value
}

// parseASSTime reads H:MM:SS.cc.
func parseASSTime(value string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("cast: %q is not an ASS timestamp", value)
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("cast: %q is not an ASS timestamp", value)
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("cast: %q is not an ASS timestamp", value)
	}
	secs, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, fmt.Errorf("cast: %q is not an ASS timestamp", value)
	}
	return float64(hours*3600+minutes*60) + secs, nil
}

// formatASSTime writes H:MM:SS.cc, rounded to the centisecond ASS carries.
func formatASSTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	centis := int64(seconds*100 + 0.5)
	hours := centis / 360000
	centis -= hours * 360000
	minutes := centis / 6000
	centis -= minutes * 6000
	secs := centis / 100
	centis -= secs * 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", hours, minutes, secs, centis)
}
