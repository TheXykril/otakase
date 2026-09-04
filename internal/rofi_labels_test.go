package internal

import (
	"strings"
	"testing"
)

func TestSplitRofiLabel(t *testing.T) {
	cases := []struct {
		label, title, meta string
	}{
		// The shapes the providers actually produce.
		{"One Piece · 1171 eps [anipub]", "One Piece", "· 1171 eps [anipub]"},
		{"Attack on Titan — TV • 25 Episodes [anineko]", "Attack on Titan", "— TV • 25 Episodes [anineko]"},
		{"Frieren: Beyond Journey's End [anipub]", "Frieren: Beyond Journey's End", "[anipub]"},
		{"One Piece Film Red", "One Piece Film Red", ""},
		{"One Piece · 1171 eps", "One Piece", "· 1171 eps"},
		// A colon is part of a title, not a metadata separator.
		{"Re:Zero kara Hajimeru Isekai Seikatsu", "Re:Zero kara Hajimeru Isekai Seikatsu", ""},
		{"", "", ""},
	}

	for _, tc := range cases {
		title, meta := splitRofiLabel(tc.label)
		if title != tc.title || meta != tc.meta {
			t.Fatalf("splitRofiLabel(%q) = (%q, %q), want (%q, %q)", tc.label, title, meta, tc.title, tc.meta)
		}
	}
}

func TestRofiRowMarkupDimsMetadata(t *testing.T) {
	rofiMetaColor = "#4b4e55"

	row := rofiRowMarkup("One Piece · 1171 eps [anipub]")
	if !strings.HasPrefix(row, "One Piece <span") {
		t.Fatalf("expected the title outside the span, got %q", row)
	}
	if !strings.Contains(row, `foreground="#4b4e55"`) {
		t.Fatalf("expected the metadata dimmed, got %q", row)
	}
	if !strings.Contains(row, "· 1171 eps [anipub]</span>") {
		t.Fatalf("expected the metadata inside the span, got %q", row)
	}

	// Nothing to dim: no markup at all rather than an empty span.
	if got := rofiRowMarkup("One Piece Film Red"); got != "One Piece Film Red" {
		t.Fatalf("got %q", got)
	}
}

// -markup-rows means labels are parsed as pango, so an unescaped ampersand
// breaks the row. Anime titles contain them.
func TestRofiRowMarkupEscapesPango(t *testing.T) {
	rofiMetaColor = "#4b4e55"

	row := rofiRowMarkup("Tom & Jerry <Special> [anipub]")
	if strings.Contains(row, "Tom & Jerry") {
		t.Fatalf("ampersand was not escaped: %q", row)
	}
	if !strings.Contains(row, "Tom &amp; Jerry") {
		t.Fatalf("expected an escaped ampersand, got %q", row)
	}
	if strings.Contains(row, "<Special>") {
		t.Fatalf("angle brackets were not escaped: %q", row)
	}
	// The span we added ourselves must survive.
	if !strings.Contains(row, "<span foreground=") {
		t.Fatalf("expected our own markup intact: %q", row)
	}
}

func TestUnescapePango(t *testing.T) {
	cases := map[string]string{
		"Tom &amp; Jerry":     "Tom & Jerry",
		"&lt;Special&gt;":     "<Special>",
		"a &quot;b&quot; c":   `a "b" c`,
		"it&#39;s":            "it's",
		"nothing to unescape": "nothing to unescape",
		// A literal escaped ampersand round-trips without eating the next entity.
		"&amp;lt;": "&lt;",
	}
	for input, want := range cases {
		if got := unescapePango(input); got != want {
			t.Fatalf("unescapePango(%q) = %q, want %q", input, got, want)
		}
	}
}

// The whole point: a row is rendered with markup, comes back from rofi, and must
// still match the option it came from. A break here makes selections silently
// fail to resolve.
func TestRofiRowMarkupRoundTripsToOriginalLabel(t *testing.T) {
	rofiMetaColor = "#4b4e55"

	labels := []string{
		"One Piece · 1171 eps [anipub]",
		"Attack on Titan — TV • 25 Episodes [anineko]",
		"Frieren: Beyond Journey's End [anipub]",
		"Tom & Jerry <Special> [anipub]",
		"Re:Zero kara Hajimeru Isekai Seikatsu",
		"One Piece Film Red",
	}

	for _, label := range labels {
		rendered := rofiRowMarkup(label)
		// What parseRofiSelection does to what rofi hands back.
		recovered := unescapePango(strings.TrimSpace(pangoStrip.ReplaceAllString(rendered, "")))
		if recovered != label {
			t.Fatalf("round trip failed:\n  label     %q\n  rendered  %q\n  recovered %q", label, rendered, recovered)
		}
	}
}
