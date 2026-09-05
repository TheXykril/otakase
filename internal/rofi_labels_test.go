package internal

import (
	"strings"
	"testing"
	"unicode/utf8"
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

// The grid clips at the column width, and a long title used to consume the whole
// line so the episode counts -- the one thing a cover cannot tell you -- were
// what disappeared. The title must absorb the truncation instead.
func TestGridRowMarkupKeepsCountsVisible(t *testing.T) {
	rofiMetaColor = "#4b4e55"
	const capacity = 37

	cases := []string{
		"That Time I Got Reincarnated as a Slime Season 4 · 0/24 (21 aired)",
		"Rich Girl Caretaker: I'm Secretly the Caregiver of the Most Popular Girl · 9/12 (9 aired)",
		"From Old Country Bumpkin to Master Swordsman · 4/12",
		"Uzaki-chan Wants to Hang Out! · 3/12",
	}

	for _, label := range cases {
		row := GridRowMarkup(label, capacity)
		plain := unescapePango(pangoStrip.ReplaceAllString(row, ""))

		if got := len([]rune(plain)); got > capacity {
			t.Fatalf("row exceeds the budget (%d > %d): %q", got, capacity, plain)
		}

		_, meta := splitRofiLabel(label)
		// The counts must survive intact -- never truncated, never dropped.
		if !strings.HasSuffix(plain, meta) {
			t.Fatalf("counts did not survive:\n  in  %q\n  out %q", label, plain)
		}
	}
}

func TestGridRowMarkupDimsOnlyTheCounts(t *testing.T) {
	rofiMetaColor = "#4b4e55"

	row := GridRowMarkup("Uzaki-chan Wants to Hang Out! · 3/12", 37)
	if strings.Count(row, "<span") != 1 {
		t.Fatalf("expected one dimmed span, got %q", row)
	}
	if !strings.HasPrefix(row, "Uzaki-chan Wants to Hang Out!<span") {
		t.Fatalf("expected the title outside the span, got %q", row)
	}
}

// A title short enough to fit is left exactly as it is.
func TestGridRowMarkupLeavesShortTitlesIntact(t *testing.T) {
	rofiMetaColor = "#4b4e55"

	row := GridRowMarkup("Monster · 4/24", 37)
	plain := unescapePango(pangoStrip.ReplaceAllString(row, ""))
	if plain != "Monster · 4/24" {
		t.Fatalf("got %q", plain)
	}
}

// Counts so long that the title would vanish: keep a recognisable stub rather
// than rendering a single ellipsis.
func TestGridRowMarkupKeepsAMinimumTitle(t *testing.T) {
	rofiMetaColor = "#4b4e55"

	row := GridRowMarkup("Some Extremely Long Anime Title Here · 1100/1177 aired", 20)
	plain := unescapePango(pangoStrip.ReplaceAllString(row, ""))
	title, _ := splitRofiLabel(plain)
	if len([]rune(title)) < gridLabelMinTitle {
		t.Fatalf("title collapsed to %q", title)
	}
}

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		in    string
		limit int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly-ten", 11, "exactly-ten"},
		{"truncate me please", 10, "truncate…"},
		{"trailing space  x", 15, "trailing space…"},
		{"日本語のタイトルです", 5, "日本語の…"},
		{"anything", 1, "…"},
		{"anything", 0, ""},
	}
	for _, tc := range cases {
		if got := truncateRunes(tc.in, tc.limit); got != tc.want {
			t.Fatalf("truncateRunes(%q, %d) = %q, want %q", tc.in, tc.limit, got, tc.want)
		}
	}
}

// A multi-byte title must not be cut mid-character.
func TestGridRowMarkupHandlesMultibyteTitles(t *testing.T) {
	rofiMetaColor = "#4b4e55"

	row := GridRowMarkup("転生したらスライムだった件 第4期 とても長いタイトル · 0/24", 24)
	plain := unescapePango(pangoStrip.ReplaceAllString(row, ""))
	if !strings.HasSuffix(plain, "0/24") {
		t.Fatalf("counts lost: %q", plain)
	}
	if !utf8.ValidString(plain) {
		t.Fatalf("produced invalid utf-8: %q", plain)
	}
}
