package mobilecore

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// subtitleExtension names a subtitle file's format from its text, for ffmpeg,
// which picks the reader by extension when the text alone is ambiguous.
func subtitleExtension(body []byte) string {
	text := strings.TrimSpace(strings.TrimPrefix(string(body), "\uFEFF"))
	switch {
	case strings.HasPrefix(text, "WEBVTT"):
		return ".vtt"
	case strings.Contains(text, "[Script Info]") || strings.Contains(text, "[Events]"):
		return ".ass"
	default:
		return ".srt"
	}
}

// subtitlesToVTT turns WebVTT, SRT or ASS into WebVTT, which browsers and
// Kodi both read. ASS styling and positions are dropped: a browser has no
// place for them.
func subtitlesToVTT(body []byte) ([]byte, error) {
	if !utf8.Valid(body) {
		body = bytes.ToValidUTF8(body, []byte("�"))
	}
	text := strings.ReplaceAll(strings.TrimPrefix(string(body), "\uFEFF"), "\r\n", "\n")
	switch subtitleExtension(body) {
	case ".vtt":
		return []byte(text), nil
	case ".ass":
		return assToVTT(text)
	default:
		return srtToVTT(text)
	}
}

var srtTiming = regexp.MustCompile(`(\d+:\d{2}:\d{2}),(\d{3})`)

func srtToVTT(text string) ([]byte, error) {
	if !strings.Contains(text, "-->") {
		return nil, errors.New("no subtitle cues found")
	}
	var out strings.Builder
	out.WriteString("WEBVTT\n\n")
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "-->") {
			line = srtTiming.ReplaceAllString(line, "$1.$2")
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return []byte(out.String()), nil
}

var assOverride = regexp.MustCompile(`\{[^}]*\}`)

func assToVTT(text string) ([]byte, error) {
	format := []string{}
	var cues strings.Builder
	inEvents := false
	count := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inEvents = strings.EqualFold(line, "[Events]")
			continue
		}
		if !inEvents {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Format":
			format = format[:0]
			for _, f := range strings.Split(value, ",") {
				format = append(format, strings.ToLower(strings.TrimSpace(f)))
			}
		case "Dialogue":
			if len(format) == 0 {
				format = []string{"layer", "start", "end", "style", "name", "marginl", "marginr", "marginv", "effect", "text"}
			}
			fields := strings.SplitN(strings.TrimSpace(value), ",", len(format))
			if len(fields) < len(format) {
				continue
			}
			get := func(name string) string {
				for i, f := range format {
					if f == name {
						return strings.TrimSpace(fields[i])
					}
				}
				return ""
			}
			start, err1 := assSeconds(get("start"))
			end, err2 := assSeconds(get("end"))
			if err1 != nil || err2 != nil || end <= start {
				continue
			}
			body := fields[len(format)-1]
			body = assOverride.ReplaceAllString(body, "")
			body = strings.NewReplacer(`\N`, "\n", `\n`, "\n", `\h`, " ").Replace(body)
			body = strings.TrimSpace(body)
			if body == "" {
				continue
			}
			count++
			fmt.Fprintf(&cues, "%s --> %s\n%s\n\n", vttTime(start), vttTime(end), escapeVTT(body))
		}
	}
	if count == 0 {
		return nil, errors.New("no subtitle cues found")
	}
	return []byte("WEBVTT\n\n" + cues.String()), nil
}

func escapeVTT(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

func assSeconds(value string) (float64, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, errors.New("bad time")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	sec, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, err
	}
	return float64(h*3600+m*60) + sec, nil
}

func vttTime(seconds float64) string {
	ms := int64(seconds*1000 + 0.5)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}
