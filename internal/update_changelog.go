package internal

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// The update prompt shows what changed between the version you have and the
// one on offer. That text comes from CHANGELOG.md at the new release's tag,
// which is written for people using otakase. The GitHub release body is the
// fallback only: it is a commit and pull request list ("feat(cast): ... by
// @someone in https://..."), written for whoever maintains the code.

// changelogRelease is one version's entry.
type changelogRelease struct {
	Version  string             `json:"version"`
	Date     string             `json:"date,omitempty"`
	Sections []changelogSection `json:"sections"`
}

// changelogSection is one heading within a release: Added, Fixed, and so on.
// An entry's opening paragraph, before any heading, has an empty Title.
type changelogSection struct {
	Title string          `json:"title,omitempty"`
	Items []changelogItem `json:"items"`
}

// changelogItem is one change. Lead is the bold opening sentence the
// changelog starts most entries with, and is what the prompt's summary shows.
type changelogItem struct {
	Lead string `json:"lead,omitempty"`
	Text string `json:"text,omitempty"`
}

// maxChangelogReleases bounds how far back the prompt reaches for someone who
// skipped many updates. Older entries are a click away on GitHub.
const maxChangelogReleases = 12

// githubRawBase serves repository files; tests point it at a local server.
var githubRawBase = "https://raw.githubusercontent.com"

var (
	changelogVersionRe = regexp.MustCompile(`^##\s+v?([0-9][^\s]*|Unreleased)\s*(?:[—–-]\s*(.+))?$`)
	changelogLeadRe    = regexp.MustCompile(`^\*\*(.+?)\*\*\s*(.*)$`)
	mdLinkRe           = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	mdEmphasisRe       = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)
	// A GitHub-generated "What's Changed" line ends in who opened the pull
	// request and its URL; a commit line in its short hash and author.
	releaseByPRRe     = regexp.MustCompile(`\s+by\s+@\S+\s+in\s+\S+\s*$`)
	releaseCommitRe   = regexp.MustCompile(`\s*\(\[[0-9a-f]{7,40}\]\([^)]*\)\)(\s+by\s+.*)?$`)
	conventionalRe    = regexp.MustCompile(`^([a-zA-Z]+)(?:\(([^)]*)\))?!?:\s*(.+)$`)
	headingDecorRe    = regexp.MustCompile(`^[^\p{L}\p{N}]+`)
	releaseNoiseLines = []string{"no direct commits", "full changelog", "diff range", "new contributors"}
)

// fetchChangelogFile reads CHANGELOG.md at a tag.
func fetchChangelogFile(repo, ref string) (string, error) {
	if strings.TrimSpace(repo) == "" {
		repo = DefaultUpdateRepo
	}
	url := fmt.Sprintf("%s/%s/%s/CHANGELOG.md", githubRawBase, repo, ref)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", AppName+"-update-check")
	client := sharedHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("CHANGELOG.md at %s: status %d", ref, resp.StatusCode)
	}
	// The whole history is a few hundred KB at most; the cap only guards
	// against something that is not the changelog at all.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// updateChangelog is what the prompt shows for an update from current to the
// release on offer: CHANGELOG.md's entries in that range, or the release's own
// notes when the changelog has none for it.
func updateChangelog(repo string, release githubReleaseAPI, latest, current string) []changelogRelease {
	if file, err := fetchChangelogFile(repo, release.TagName); err != nil {
		Log(fmt.Sprintf("Update changelog: %v; using the release notes", err))
	} else if picked := changelogRange(parseChangelog(file), current, latest); len(picked) > 0 {
		return picked
	}
	return []changelogRelease{parseReleaseBody(latest, release.Body)}
}

// changelogRange keeps the entries newer than current, up to latest, newest
// first. A dev build on offer is the changelog's Unreleased entry.
func changelogRange(releases []changelogRelease, current, latest string) []changelogRelease {
	_, pre := splitPrerelease(normalizeReleaseVersion(latest))
	var picked []changelogRelease
	for _, release := range releases {
		if release.Version == "Unreleased" {
			if pre != "" && len(picked) == 0 {
				picked = append(picked, release)
			}
			continue
		}
		if compareUpdateVersions(release.Version, current) <= 0 {
			continue
		}
		if compareUpdateVersions(release.Version, latest) > 0 {
			continue
		}
		picked = append(picked, release)
		if len(picked) == maxChangelogReleases {
			break
		}
	}
	return picked
}

// parseChangelog reads CHANGELOG.md: "## 26.4.0 — 2026-10-02" opens a
// release, "### Added" a section, and "- " an item, which may wrap onto
// indented lines.
func parseChangelog(md string) []changelogRelease {
	var releases []changelogRelease
	var release *changelogRelease
	var section *changelogSection
	var item *strings.Builder

	flushItem := func() {
		if item == nil || section == nil {
			item = nil
			return
		}
		if parsed, ok := newChangelogItem(item.String()); ok {
			section.Items = append(section.Items, parsed)
		}
		item = nil
	}
	flushSection := func() {
		flushItem()
		if section != nil && release != nil && len(section.Items) > 0 {
			release.Sections = append(release.Sections, *section)
		}
		section = nil
	}
	flushRelease := func() {
		flushSection()
		if release != nil && len(release.Sections) > 0 {
			releases = append(releases, *release)
		}
		release = nil
	}

	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "## "):
			flushRelease()
			if match := changelogVersionRe.FindStringSubmatch(trimmed); match != nil {
				release = &changelogRelease{Version: match[1], Date: strings.TrimSpace(match[2])}
				section = &changelogSection{}
			}
		case release == nil:
			// The file's title and anything before the first release.
		case strings.HasPrefix(trimmed, "### "):
			flushSection()
			section = &changelogSection{Title: cleanHeading(strings.TrimPrefix(trimmed, "### "))}
		case trimmed == "":
			flushItem()
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			flushItem()
			item = &strings.Builder{}
			item.WriteString(trimmed[2:])
		case item != nil:
			// A wrapped line, or a nested point, belongs to the item above.
			item.WriteString(" ")
			item.WriteString(strings.TrimLeft(trimmed, "-* "))
		default:
			// A paragraph, such as a release's introduction.
			item = &strings.Builder{}
			item.WriteString(trimmed)
		}
	}
	flushRelease()
	return releases
}

// parseReleaseBody turns a GitHub release body into one entry, keeping the
// changes and dropping what only matters on GitHub: contributor lists, compare
// links, and maintenance commits nobody using otakase would notice.
func parseReleaseBody(version, body string) changelogRelease {
	release := changelogRelease{Version: version}
	byTitle := map[string]int{}
	add := func(title string, item changelogItem) {
		index, ok := byTitle[title]
		if !ok {
			index = len(release.Sections)
			byTitle[title] = index
			release.Sections = append(release.Sections, changelogSection{Title: title})
		}
		for _, existing := range release.Sections[index].Items {
			if existing == item {
				return
			}
		}
		release.Sections[index].Items = append(release.Sections[index].Items, item)
	}

	heading := ""
	skipping := false
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			heading = cleanHeading(strings.TrimLeft(trimmed, "# "))
			lower := strings.ToLower(heading)
			skipping = strings.Contains(lower, "contributor")
			continue
		}
		if skipping || trimmed == "" || trimmed == "---" || isReleaseNoise(trimmed) {
			continue
		}
		text := strings.TrimSpace(strings.TrimLeft(trimmed, "-* "))
		text = releaseByPRRe.ReplaceAllString(text, "")
		text = releaseCommitRe.ReplaceAllString(text, "")
		title, text, keep := classifyChange(heading, text)
		if !keep {
			continue
		}
		if parsed, ok := newChangelogItem(text); ok {
			add(title, parsed)
		}
	}
	if len(release.Sections) == 0 {
		release.Sections = []changelogSection{{Items: []changelogItem{{Text: "Maintenance and internal changes."}}}}
	}
	return release
}

func isReleaseNoise(line string) bool {
	lower := strings.ToLower(line)
	for _, noise := range releaseNoiseLines {
		if strings.Contains(lower, noise) {
			return true
		}
	}
	return strings.HasPrefix(line, "_") && strings.HasSuffix(line, "_")
}

// classifyChange files a conventional-commit line ("fix(cast): keep a DLNA
// cast going") under the changelog's own headings, with the scope as a
// prefix, and drops the kinds that change nothing for the viewer.
func classifyChange(heading, text string) (title, cleaned string, keep bool) {
	match := conventionalRe.FindStringSubmatch(text)
	if match == nil {
		return releaseHeadingTitle(heading), text, true
	}
	kind, scope, subject := strings.ToLower(match[1]), match[2], match[3]
	switch kind {
	case "feat", "add":
		title = "Added"
	case "fix", "bug", "hotfix":
		title = "Fixed"
	case "perf", "refactor", "style", "update":
		title = "Changed"
	default:
		// docs, chore, ci, build, test, release: not something to read
		// before updating.
		return "", "", false
	}
	subject = capitalize(subject)
	if scope = strings.TrimSpace(scope); scope != "" && !strings.EqualFold(scope, "deps") {
		subject = capitalize(scope) + ": " + subject
	}
	return title, subject, true
}

// releaseHeadingTitle maps a release body's own headings onto the
// changelog's, so both kinds of notes read the same.
func releaseHeadingTitle(heading string) string {
	lower := strings.ToLower(heading)
	switch {
	case strings.Contains(lower, "feature") || strings.Contains(lower, "added"):
		return "Added"
	case strings.Contains(lower, "fix") || strings.Contains(lower, "bug"):
		return "Fixed"
	case lower == "" || strings.Contains(lower, "changelog") || strings.Contains(lower, "what's changed"):
		return "Changes"
	default:
		return heading
	}
}

// cleanHeading drops the decoration some headings carry ("🚀 Features").
func cleanHeading(heading string) string {
	return strings.TrimSpace(headingDecorRe.ReplaceAllString(strings.TrimSpace(heading), ""))
}

// newChangelogItem splits off the bold lead and flattens the rest of the
// markdown to text: the prompt draws its own emphasis.
func newChangelogItem(raw string) (changelogItem, bool) {
	raw = strings.TrimSpace(mdLinkRe.ReplaceAllString(raw, "$1"))
	if raw == "" {
		return changelogItem{}, false
	}
	var item changelogItem
	if match := changelogLeadRe.FindStringSubmatch(raw); match != nil {
		item.Lead = flattenMarkdown(match[1])
		item.Text = flattenMarkdown(match[2])
	} else {
		item.Text = flattenMarkdown(raw)
	}
	return item, item.Lead != "" || item.Text != ""
}

func flattenMarkdown(s string) string {
	s = mdEmphasisRe.ReplaceAllString(s, "$1$2")
	s = strings.ReplaceAll(s, "`", "")
	return strings.Join(strings.Fields(s), " ")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// changelogSummary picks the few changes worth reading before deciding: new
// features first, then fixes, newest release first. It returns their one-line
// headlines and how many other changes the full changelog holds.
func changelogSummary(releases []changelogRelease, limit int) ([]string, int) {
	order := []string{"Added", "Fixed", "Changed", ""}
	rank := func(title string) int {
		for i, t := range order {
			if strings.EqualFold(title, t) {
				return i
			}
		}
		return len(order)
	}
	total := 0
	var picked []string
	for pass := 0; pass <= len(order); pass++ {
		for _, release := range releases {
			for _, section := range release.Sections {
				if rank(section.Title) != pass {
					continue
				}
				for _, item := range section.Items {
					total++
					if len(picked) < limit {
						picked = append(picked, item.headline())
					}
				}
			}
		}
	}
	return picked, total - len(picked)
}

// joined is the whole item as one line of text, lead first.
func (item changelogItem) joined() string {
	switch {
	case item.Lead == "":
		return item.Text
	case item.Text == "":
		return item.Lead
	case strings.ContainsAny(item.Text[:1], ",.;:)"):
		// "**A menu when no devices are found**, instead of an error"
		return item.Lead + item.Text
	default:
		return item.Lead + " " + item.Text
	}
}

// headline is the item in one line: its lead, or the start of its text.
func (item changelogItem) headline() string {
	text := item.Lead
	if text == "" {
		text = item.Text
		if i := strings.Index(text, ". "); i > 0 {
			text = text[:i+1]
		}
	}
	return truncateRunes(strings.TrimSpace(text), 80)
}

// formatChangelogDate shows "2026-10-02" as "2 Oct 2026".
func formatChangelogDate(date string) string {
	if parsed, err := time.Parse("2006-01-02", strings.TrimSpace(date)); err == nil {
		return parsed.Format("2 Jan 2006")
	}
	return strings.TrimSpace(date)
}

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
