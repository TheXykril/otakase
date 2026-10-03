package internal

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/theme"
)

const sampleChangelog = `# Changelog

## 26.4.0 — 2026-10-02

### Added

- **otakase now shows when it is working.** Slow steps show a spinner
  with the step and the seconds spent.

### Changed

- **Notifications update in place.** A message that repeats changes one
  notification instead of stacking new ones.

## 26.3.1 — 2026-10-02

### Fixed

- **DLNA casts no longer stop after a couple of minutes** on TVs that keep
  reporting the same position.

## 26.3.0 — 2026-10-02

### Added

- **Rescan** in the cast device list.

## 26.1.0 — 2026-10-01

Versions now follow the year.

### Added

- ` + "`Icons`" + ` setting, see [the wiki](https://example.com).
`

func TestParseChangelogReadsReleasesSectionsAndWrappedItems(t *testing.T) {
	releases := parseChangelog(sampleChangelog)
	if len(releases) != 4 {
		t.Fatalf("got %d releases: %#v", len(releases), releases)
	}
	first := releases[0]
	if first.Version != "26.4.0" || first.Date != "2026-10-02" {
		t.Fatalf("first release = %#v", first)
	}
	if len(first.Sections) != 2 || first.Sections[0].Title != "Added" || first.Sections[1].Title != "Changed" {
		t.Fatalf("sections = %#v", first.Sections)
	}
	item := first.Sections[0].Items[0]
	if item.Lead != "otakase now shows when it is working." {
		t.Fatalf("lead = %q", item.Lead)
	}
	if item.Text != "Slow steps show a spinner with the step and the seconds spent." {
		t.Fatalf("wrapped text not joined: %q", item.Text)
	}

	// An introduction before the first heading, and markdown flattened.
	last := releases[3]
	if last.Sections[0].Title != "" || last.Sections[0].Items[0].Text != "Versions now follow the year." {
		t.Fatalf("intro = %#v", last.Sections[0])
	}
	if got := last.Sections[1].Items[0].Text; got != "Icons setting, see the wiki." {
		t.Fatalf("markdown not flattened: %q", got)
	}
}

// Someone three releases behind sees all three, newest first, and nothing
// they already have.
func TestChangelogRangeIsEverythingSinceTheRunningVersion(t *testing.T) {
	releases := parseChangelog(sampleChangelog)
	picked := changelogRange(releases, "26.3.0", "26.4.0")
	var versions []string
	for _, release := range picked {
		versions = append(versions, release.Version)
	}
	if strings.Join(versions, ",") != "26.4.0,26.3.1" {
		t.Fatalf("got %v", versions)
	}
	if got := changelogRange(releases, "26.4.0", "26.4.0"); len(got) != 0 {
		t.Fatalf("nothing is newer, got %#v", got)
	}
}

func TestChangelogRangeOffersUnreleasedForADevBuild(t *testing.T) {
	releases := parseChangelog("## Unreleased\n\n### Added\n\n- **Icons.**\n\n" + strings.TrimPrefix(sampleChangelog, "# Changelog\n"))
	picked := changelogRange(releases, "26.3.1", "26.5.0-dev.2")
	if len(picked) != 2 || picked[0].Version != "Unreleased" || picked[1].Version != "26.4.0" {
		t.Fatalf("got %#v", picked)
	}
	if got := changelogRange(releases, "26.3.1", "26.4.0"); len(got) != 1 || got[0].Version != "26.4.0" {
		t.Fatalf("a release update must not show unreleased work: %#v", got)
	}
}

// The GitHub release body is a generated commit list. What reaches the
// prompt is the changes, without the authors, links, emoji headings,
// maintenance commits or empty placeholder sections.
func TestParseReleaseBodyKeepsOnlyTheChanges(t *testing.T) {
	body := "## 📝 Changelog\n\n- No direct commits in this release range.\n\n## 🤝 Contributors\n- None\n\n---\n" +
		"_Diff range: [v26.3.1..v26.4.0](https://example.com)_\n\n## What's Changed\n" +
		"* feat: show a spinner while otakase is busy by @TheXykril in https://github.com/x/y/pull/57\n" +
		"* fix(cast): keep a DLNA cast going by @TheXykril in https://github.com/x/y/pull/56\n" +
		"* docs: keep the wiki in the repo by @TheXykril in https://github.com/x/y/pull/58\n" +
		"* chore(deps): bump actions/setup-go from 5 to 7 by @dependabot[bot] in https://github.com/x/y/pull/53\n\n" +
		"**Full Changelog**: https://github.com/x/y/compare/v26.3.1...v26.4.0\n"
	release := parseReleaseBody("26.4.0", body)
	var got []string
	for _, section := range release.Sections {
		for _, item := range section.Items {
			got = append(got, section.Title+"|"+item.Text)
		}
	}
	want := []string{"Added|Show a spinner while otakase is busy", "Fixed|Cast: Keep a DLNA cast going"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	empty := parseReleaseBody("26.2.1", "## What's Changed\n* chore: tidy by @x in https://x\n")
	if len(empty.Sections) != 1 || empty.Sections[0].Items[0].Text == "" {
		t.Fatalf("a maintenance-only release still says so: %#v", empty)
	}
}

func TestUpdateChangelogPrefersTheChangelogFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/TheXykril/otakase/v26.4.0/CHANGELOG.md" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(sampleChangelog))
	}))
	t.Cleanup(server.Close)
	saved := githubRawBase
	githubRawBase = server.URL
	t.Cleanup(func() { githubRawBase = saved })

	release := githubReleaseAPI{TagName: "v26.4.0", Body: "## What's Changed\n* feat: from the body by @x in https://x\n"}
	got := updateChangelog(DefaultUpdateRepo, release, "26.4.0", "26.3.0")
	if len(got) != 2 || got[0].Version != "26.4.0" {
		t.Fatalf("expected the changelog file's entries, got %#v", got)
	}

	release.TagName = "v9.9.9"
	got = updateChangelog(DefaultUpdateRepo, release, "9.9.9", "26.3.0")
	if len(got) != 1 || got[0].Sections[0].Items[0].Text != "From the body" {
		t.Fatalf("expected the release body fallback, got %#v", got)
	}
}

func TestChangelogSummaryLeadsWithNewFeatures(t *testing.T) {
	releases := changelogRange(parseChangelog(sampleChangelog), "2.0.0", "26.4.0")
	headlines, more := changelogSummary(releases, 3)
	want := []string{"otakase now shows when it is working.", "Rescan", "Icons setting, see the wiki."}
	if strings.Join(headlines, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", headlines)
	}
	if more != 3 {
		t.Fatalf("more = %d, want 3", more)
	}
}

func TestUpdateActionOptionsHaveNoEmoji(t *testing.T) {
	icons.Resolve(false)
	emoji := regexp.MustCompile("[\U0001F300-\U0001FAFF☀-➿⏩-⏿]")
	for _, option := range updateActionOptions(updatePendingState{LatestVersion: "26.5.0", HTMLURL: "https://x"}) {
		if emoji.MatchString(option.Label) {
			t.Errorf("emoji in %q", option.Label)
		}
	}
}

// Every colour in the rofi summary and changelog comes from the palette, so
// a light theme stays readable, and every row's markup is balanced: pango
// rejects a whole row over one unclosed tag.
func TestUpdateRofiMarkupUsesThePaletteAndBalances(t *testing.T) {
	releases := changelogRange(parseChangelog(sampleChangelog), "2.0.0", "26.4.0")
	state := updatePendingState{LatestVersion: "26.4.0"}
	text := updateRofiMessage("26.1.0", state, releases) + "\n" + strings.Join(changelogRofiRows(releases), "\n")

	p := theme.Active()
	allowed := map[string]bool{}
	for _, c := range []string{p.Foreground, p.Muted, p.Accent} {
		allowed[strings.ToLower(c)] = true
	}
	for _, m := range regexp.MustCompile(`foreground="([^"]+)"`).FindAllStringSubmatch(text, -1) {
		if !allowed[strings.ToLower(m[1])] {
			t.Errorf("colour %s is not from the active palette", m[1])
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Count(line, "<span") != strings.Count(line, "</span>") || strings.Count(line, "<b>") != strings.Count(line, "</b>") {
			t.Errorf("unbalanced markup: %q", line)
		}
	}
}

func TestChangelogRofiRowsWrapLongEntries(t *testing.T) {
	icons.Resolve(false)
	item := changelogItem{Lead: "A lead.", Text: strings.Repeat("word ", 60)}
	rows := changelogItemRofiRows(item, theme.Active())
	if len(rows) < 3 {
		t.Fatalf("expected wrapping, got %d rows", len(rows))
	}
	tags := regexp.MustCompile(`<[^>]*>`)
	for _, row := range rows {
		if n := len([]rune(tags.ReplaceAllString(row, ""))); n > rofiChangelogWidth {
			t.Errorf("row is %d characters: %q", n, row)
		}
	}
	if !strings.Contains(rows[0], "<b>A lead.</b>") {
		t.Errorf("lead not bold: %q", rows[0])
	}
}
