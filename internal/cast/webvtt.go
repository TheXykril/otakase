package cast

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Seeking a cast episode restarts the remux at an offset, so the stream the
// device plays begins at the seek target rather than at zero. Burned subtitles
// do not follow: the subtitles filter reads a file whose cues are stamped
// against the original episode, and a video that now starts at 0 would show a
// line meant for 12:00 twelve minutes early.
//
// The cues are therefore shifted to match. Shifting the file is done rather than
// asking ffmpeg to preserve timestamps (-copyts), because preserved timestamps
// leave the HLS output starting at the offset, which the receiver reads as a
// stream that begins twelve minutes in -- trading a subtitle bug for a position
// bug.

// ShiftWebVTT writes src to dst with every cue moved earlier by seconds.
//
// A cue that ends before the new zero is dropped: it belongs to a part of the
// episode this stream no longer contains. A cue that straddles zero is kept with
// its start clamped, because a line already on screen when the viewer seeks
// should stay on screen.
func ShiftWebVTT(src, dst string, seconds float64) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("cast: could not read the subtitles: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))

	// A cue is a timing line plus the lines under it, so dropping a cue means
	// dropping what follows until the blank line that ends it.
	skipping := false
	for _, line := range lines {
		if skipping {
			if strings.TrimSpace(line) == "" {
				skipping = false
				continue
			}
			continue
		}

		if !strings.Contains(line, "-->") {
			out = append(out, line)
			continue
		}

		shifted, keep, err := shiftCueTiming(line, seconds)
		if err != nil {
			// A timing line this cannot read is left exactly as it was: a
			// subtitle file that is stranger than expected should lose its
			// synchronisation, not its contents.
			out = append(out, line)
			continue
		}
		if !keep {
			skipping = true
			// A cue's identifier sits above its timing line and has already been
			// written out by the time the timing is read. Left behind it is a
			// bare number where a cue should be, which is a parse error for the
			// filter, so it comes back out -- along with the blank line that
			// separated it, so the file does not fill with gaps.
			for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = out[:len(out)-1]
			}
			for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
				out = out[:len(out)-1]
			}
			if len(out) > 0 {
				out = append(out, "")
			}
			continue
		}
		out = append(out, shifted)
	}

	if err := os.WriteFile(dst, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return fmt.Errorf("cast: could not write the shifted subtitles: %w", err)
	}
	return nil
}

// shiftCueTiming moves one "start --> end" line earlier, reporting whether the
// cue still belongs in the file at all.
func shiftCueTiming(line string, seconds float64) (string, bool, error) {
	before, after, found := strings.Cut(line, "-->")
	if !found {
		return "", false, fmt.Errorf("not a timing line")
	}

	start, err := parseWebVTTTime(strings.TrimSpace(before))
	if err != nil {
		return "", false, err
	}

	// Anything after the end time is cue settings (line:, align:, position:),
	// which are kept exactly as they were.
	endField := strings.TrimSpace(after)
	endText, settings, _ := strings.Cut(endField, " ")
	end, err := parseWebVTTTime(strings.TrimSpace(endText))
	if err != nil {
		return "", false, err
	}

	start -= seconds
	end -= seconds
	if end <= 0 {
		return "", false, nil
	}
	if start < 0 {
		start = 0
	}

	shifted := formatWebVTTTime(start) + " --> " + formatWebVTTTime(end)
	if settings = strings.TrimSpace(settings); settings != "" {
		shifted += " " + settings
	}
	return shifted, true, nil
}

// parseWebVTTTime reads HH:MM:SS.mmm or MM:SS.mmm, both of which are valid.
func parseWebVTTTime(value string) (float64, error) {
	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("cast: %q is not a WebVTT timestamp", value)
	}

	var total float64
	for _, part := range parts[:len(parts)-1] {
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return 0, fmt.Errorf("cast: %q is not a WebVTT timestamp", value)
		}
		total = total*60 + float64(number)
	}
	total *= 60

	// The seconds field carries the fraction, and WebVTT writes it with a dot
	// while some files in the wild use a comma, as SubRip does.
	secondsField := strings.Replace(strings.TrimSpace(parts[len(parts)-1]), ",", ".", 1)
	seconds, err := strconv.ParseFloat(secondsField, 64)
	if err != nil {
		return 0, fmt.Errorf("cast: %q is not a WebVTT timestamp", value)
	}
	return total + seconds, nil
}

// formatWebVTTTime writes HH:MM:SS.mmm, the long form, which every parser reads.
func formatWebVTTTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	// Rounded to the millisecond the format carries, before the split, so a
	// value like 59.9996 does not become 59:60.000.
	milliseconds := int64(seconds*1000 + 0.5)
	hours := milliseconds / 3600000
	milliseconds -= hours * 3600000
	minutes := milliseconds / 60000
	milliseconds -= minutes * 60000
	secs := milliseconds / 1000
	milliseconds -= secs * 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, milliseconds)
}
