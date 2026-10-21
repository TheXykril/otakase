package cast

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// An ASS file sizes its text for a screen the viewer sits close to. Burned in
// as written and watched on a TV across the room, dialogue reads small. It
// cannot be scaled as a whole, though: a release's signs and typesetting are
// placed and sized to sit on the picture, and growing them puts them over
// whatever they were translating.
//
// So only the dialogue styles grow. Which those are is told two ways, because
// neither is enough alone: fansub groups name them much alike (Default, Main,
// Italics, Flashback, Top...), and whatever a group calls it, the style most
// lines use without being placed by hand is the dialogue.

// dialogueStyleName matches the names fansub groups give dialogue styles.
var dialogueStyleName = regexp.MustCompile(`(?i)default|main|dialog|italic|flashback|top|alt|overlap|narrat|thought|internal`)

// ScaleDialogueStyles grows the text of the dialogue styles in an ASS file by
// percent, in place: its font size, outline and shadow, so the line keeps its
// look at the larger size. A file that is not ASS is left alone.
func ScaleDialogueStyles(path string, percent int) error {
	if percent <= 0 || percent == 100 {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cast: could not read the subtitles to resize: %w", err)
	}
	if !isASS(string(raw)) {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")

	dialogue := dialogueStyles(lines)
	factor := float64(percent) / 100

	section := ""
	var nameField, sizeField, outlineField, shadowField, styleFields = -1, -1, -1, -1, 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			section = strings.ToLower(trimmed)
			continue
		}
		if section != "[v4+ styles]" && section != "[v4 styles]" {
			continue
		}
		key, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Format":
			names := strings.Split(rest, ",")
			styleFields = len(names)
			for j, name := range names {
				switch strings.ToLower(strings.TrimSpace(name)) {
				case "name":
					nameField = j
				case "fontsize":
					sizeField = j
				case "outline":
					outlineField = j
				case "shadow":
					shadowField = j
				}
			}
		case "Style":
			fields := strings.Split(rest, ",")
			if styleFields == 0 || len(fields) != styleFields || nameField < 0 || !dialogue[strings.TrimSpace(fields[nameField])] {
				continue
			}
			for _, field := range []int{sizeField, outlineField, shadowField} {
				if field < 0 {
					continue
				}
				value, err := strconv.ParseFloat(strings.TrimSpace(fields[field]), 64)
				if err != nil {
					continue
				}
				fields[field] = keepLeadingSpace(fields[field], strconv.FormatFloat(math.Round(value*factor*100)/100, 'f', -1, 64))
			}
			lines[i] = key + ":" + strings.Join(fields, ",")
		}
	}

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return fmt.Errorf("cast: could not write the resized subtitles: %w", err)
	}
	return nil
}

// dialogueStyles names the styles that carry dialogue: those named like it,
// and the one most lines use without a position of their own.
func dialogueStyles(lines []string) map[string]bool {
	styles := map[string]bool{}
	counts := map[string]int{}
	section := ""
	styleField, fieldCount := 3, 10
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			section = strings.ToLower(trimmed)
			continue
		}
		key, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch {
		case (section == "[v4+ styles]" || section == "[v4 styles]") && strings.TrimSpace(key) == "Style":
			name := strings.TrimSpace(strings.SplitN(rest, ",", 2)[0])
			if dialogueStyleName.MatchString(name) {
				styles[name] = true
			}
		case section == "[events]" && strings.TrimSpace(key) == "Format":
			names := strings.Split(rest, ",")
			fieldCount = len(names)
			for j, name := range names {
				if strings.EqualFold(strings.TrimSpace(name), "style") {
					styleField = j
				}
			}
		case section == "[events]" && strings.TrimSpace(key) == "Dialogue":
			fields := strings.SplitN(rest, ",", fieldCount)
			if styleField >= len(fields) || len(fields) < fieldCount {
				continue
			}
			text := fields[fieldCount-1]
			if strings.Contains(text, `\pos`) || strings.Contains(text, `\move`) {
				continue
			}
			counts[strings.TrimSpace(fields[styleField])]++
		}
	}

	most, mostCount := "", 0
	for name, count := range counts {
		if count > mostCount || (count == mostCount && name < most) {
			most, mostCount = name, count
		}
	}
	if most != "" {
		styles[most] = true
	}
	return styles
}
