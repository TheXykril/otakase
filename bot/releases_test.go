package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"26.5.0", "26.7.0", -1},
		{"v26.7.0", "26.7.0", 0},
		{"26.10.0", "26.9.3", 1},
		{"26.7", "26.7.0", 0},
		{"2.2.2", "26.0.0", -1},
		{"junk", "1.0.0", -1},
		{"1.13.0", "1.12.3", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	for _, bad := range []string{"", "v", "1.2.3.4", "1.x.0", "-1.0.0"} {
		if _, ok := parseVersion(bad); ok {
			t.Errorf("parseVersion(%q) ok", bad)
		}
	}
}

func TestEmbedMarkdown(t *testing.T) {
	in := "## 📝 Changelog\r\n\r\n- No direct commits.\r\n\r\n\r\n### Fixes ###\r\n---\r\n" +
		"* fix by @x in https://github.com/TheXykril/otakase/pull/63\r\n"
	want := "**📝 Changelog**\n\n- No direct commits.\n\n**Fixes**\n" +
		"* fix by @x in [#63](https://github.com/TheXykril/otakase/pull/63)"
	if got := embedMarkdown(in); got != want {
		t.Errorf("embedMarkdown:\n%q\nwant\n%q", got, want)
	}
	if got := embedMarkdown("# Title\n#123 is not a heading"); got != "**Title**\n#123 is not a heading" {
		t.Errorf("embedMarkdown kept %q", got)
	}
}

func TestFitText(t *testing.T) {
	if got := fitText("short", 100, "more"); got != "short" {
		t.Errorf("fitText changed short text: %q", got)
	}
	long := strings.Repeat("line of notes\n", 50)
	got := fitText(long, 200, "[More](x)")
	if n := len([]rune(got)); n > 200 {
		t.Errorf("fitText gave %d runes", n)
	}
	if !strings.HasSuffix(got, "\n\n[More](x)") || !strings.Contains(got, "line of notes\n\n") {
		t.Errorf("fitText = %q", got)
	}
}

func testReleases() []ghRelease {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	list := []ghRelease{
		{TagName: "v26.5.0", Body: "## Fixes\n- five", HTMLURL: "u5", PublishedAt: day},
		{TagName: "v26.7.0", Body: "## New\n- seven", HTMLURL: "u7", PublishedAt: day.AddDate(0, 0, 20)},
		{TagName: "v26.6.0", Body: "- six", HTMLURL: "u6", PublishedAt: day.AddDate(0, 0, 10)},
	}
	sortReleases(list)
	return list
}

func TestFindAndNewerReleases(t *testing.T) {
	list := testReleases()
	if r, ok := findRelease(list, ""); !ok || r.version() != "26.7.0" {
		t.Errorf("latest = %v %v", r.TagName, ok)
	}
	if r, ok := findRelease(list, "v26.6.0"); !ok || r.HTMLURL != "u6" {
		t.Errorf("find 26.6.0 = %v %v", r.TagName, ok)
	}
	if _, ok := findRelease(list, "26.1.0"); ok {
		t.Error("found a release that isn't there")
	}
	newer := newerReleases(list, "26.5.0")
	if len(newer) != 2 || newer[0].version() != "26.7.0" || newer[1].version() != "26.6.0" {
		t.Errorf("newer = %v", newer)
	}
	if len(newerReleases(list, "26.7.0")) != 0 {
		t.Error("newer than latest should be empty")
	}
}

func TestSinceEmbed(t *testing.T) {
	list := testReleases()
	e := sinceEmbed(newerReleases(list, "26.5.0"), "v26.5.0")
	if e.Title != "What's new since 26.5.0" {
		t.Errorf("title %q", e.Title)
	}
	if i, j := strings.Index(e.Description, "26.7.0"), strings.Index(e.Description, "26.6.0"); i < 0 || j < i {
		t.Errorf("not newest first: %q", e.Description)
	}
	if !strings.Contains(e.Description, "**New**") || strings.Contains(e.Description, "More on GitHub") {
		t.Errorf("description %q", e.Description)
	}

	// Too much to fit: whole releases are kept, then a link to the rest.
	var big []ghRelease
	for k := 30; k > 0; k-- {
		big = append(big, ghRelease{TagName: fmt.Sprintf("v26.%d.0", k), Body: strings.Repeat("- a change\n", 40)})
	}
	e = sinceEmbed(big, "26.0.0")
	if n := len([]rune(e.Description)); n > embedTextLimit {
		t.Errorf("description is %d runes", n)
	}
	if !strings.HasSuffix(e.Description, "/releases)") || !strings.Contains(e.Description, "Otakase 26.30.0") {
		t.Errorf("trimmed description ends %q", e.Description[len(e.Description)-60:])
	}

	// One release bigger than the limit is cut inside.
	huge := []ghRelease{{TagName: "v27.0.0", Body: strings.Repeat("- a long change line\n", 400)}}
	e = sinceEmbed(huge, "26.0.0")
	if n := len([]rune(e.Description)); n > embedTextLimit || !strings.Contains(e.Description, "More on GitHub") {
		t.Errorf("huge release: %d runes", n)
	}
}

func TestReleaseChoices(t *testing.T) {
	list := testReleases()
	all := releaseChoices(list, "")
	if len(all) != 3 || all[0].Value != "26.7.0" || !strings.Contains(all[0].Name, "(latest)") {
		t.Errorf("choices = %+v", all[0])
	}
	if got := releaseChoices(list, "v26.6"); len(got) != 1 || got[0].Value != "26.6.0" {
		t.Errorf("filtered = %v", got)
	}
}

func TestFetchReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]ghRelease{
			{TagName: "v26.6.0"}, {TagName: "v26.8.0"}, {TagName: "v26.9.0", Draft: true},
		})
	}))
	defer srv.Close()
	old := releasesURL
	releasesURL = srv.URL
	releaseCache.list = nil
	defer func() { releasesURL = old; releaseCache.list = nil }()
	list, err := fetchReleases("")
	if err != nil || len(list) != 2 || list[0].TagName != "v26.8.0" {
		t.Fatalf("fetchReleases = %v, %v", list, err)
	}
	e, comps := changelogAnswer("", "", "26.6.0")
	if !strings.Contains(e.Description, "26.8.0") || len(comps) != 1 {
		t.Errorf("since answer = %q", e.Description)
	}
	if e, _ := changelogAnswer("", "", "26.8.0"); !strings.Contains(e.Description, "up to date") {
		t.Errorf("up to date answer = %q", e.Description)
	}
	if e, _ := changelogAnswer("", "", "banana"); !strings.Contains(e.Description, "doesn't look like a version") {
		t.Errorf("bad since answer = %q", e.Description)
	}
	if e, _ := changelogAnswer("", "1.0.0", ""); !strings.Contains(e.Description, "no release") {
		t.Errorf("missing release answer = %q", e.Description)
	}
	if e, _ := changelogAnswer("", "", ""); e.Title != "Otakase 26.8.0" || e.Description != "No notes for this release." {
		t.Errorf("latest answer = %q %q", e.Title, e.Description)
	}
}

const testChangelog = `# Changelog

## 1.13.0

- Welcome DM card.
- Episode DM cards.

## 1.12.3

- More space.

## 1.12.2

- Board comes back.

## 1.12.0

- Live board.
`

func TestChangelogSections(t *testing.T) {
	secs := changelogSections(testChangelog)
	if len(secs) != 4 || secs[0].Version != "1.13.0" || secs[0].Lines != "- Welcome DM card.\n- Episode DM cards." {
		t.Fatalf("sections = %+v", secs)
	}
	between := sectionsBetween(testChangelog, "1.12.2", "1.13.0")
	if len(between) != 2 || between[0].Version != "1.13.0" || between[1].Version != "1.12.3" {
		t.Errorf("between = %+v", between)
	}
}

func TestUpdateEmbed(t *testing.T) {
	e := updateEmbed(testChangelog, "1.12.3", "1.13.0")
	if e.Title != "Bot updated to 1.13.0" || e.Description != "- Welcome DM card.\n- Episode DM cards." || e.Footer.Text != "Was 1.12.3" {
		t.Errorf("one version: %q %q", e.Title, e.Description)
	}
	e = updateEmbed(testChangelog, "1.12.0", "1.13.0")
	want := "**1.13.0**\n- Welcome DM card.\n- Episode DM cards.\n\n**1.12.3**\n- More space.\n\n**1.12.2**\n- Board comes back."
	if e.Description != want {
		t.Errorf("skipped versions:\n%q\nwant\n%q", e.Description, want)
	}
	if e := updateEmbed(testChangelog, "1.13.0", "1.12.0"); e.Title != "Bot changed back to 1.12.0" {
		t.Errorf("downgrade title %q", e.Title)
	}
	if e := updateEmbed(testChangelog, "1.13.0", "1.14.0"); e.Description != "No changelog notes for this version." {
		t.Errorf("no notes: %q", e.Description)
	}
}

func TestNoteUpdate(t *testing.T) {
	b := &bot{store: openStore(t.TempDir() + "/s.json")}
	b.noteUpdate() // first run: only remembers, no session needed
	var got string
	b.store.view(func(d *storeData) { got = d.BotVersion })
	if got != botVersion() {
		t.Errorf("stored %q, want %q", got, botVersion())
	}
	b.noteUpdate() // same version: nothing to post
}
