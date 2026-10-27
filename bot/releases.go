package main

import (
	"fmt"
	"log"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /changelog shows an Otakase release's notes from GitHub, or everything
// newer than the version a member has.

// releasesURL is a variable so tests can point it at a fake server.
var releasesURL = "https://api.github.com/repos/TheXykril/otakase/releases?per_page=50"

// Discord's limit for an embed description.
const embedTextLimit = 4096

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
}

func (r ghRelease) version() string { return strings.TrimPrefix(r.TagName, "v") }

var releaseCache = struct {
	sync.Mutex
	list    []ghRelease
	fetched time.Time
}{}

// fetchReleases lists recent releases, newest version first, cached for ten
// minutes.
func fetchReleases(token string) ([]ghRelease, error) {
	releaseCache.Lock()
	defer releaseCache.Unlock()
	if releaseCache.list != nil && time.Since(releaseCache.fetched) < 10*time.Minute {
		return releaseCache.list, nil
	}
	var list []ghRelease
	if err := getJSON(githubRequest("GET", releasesURL, token), &list); err != nil {
		return nil, err
	}
	list = slices.DeleteFunc(list, func(r ghRelease) bool { return r.Draft || r.TagName == "" })
	sortReleases(list)
	releaseCache.list, releaseCache.fetched = list, time.Now()
	return list, nil
}

// sortReleases orders releases newest version first.
func sortReleases(list []ghRelease) {
	slices.SortStableFunc(list, func(a, b ghRelease) int { return compareVersions(b.version(), a.version()) })
}

// parseVersion reads "26.7.0" or "v26.7" into numbers; missing parts are 0.
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(s)), "v")
	parts := strings.Split(s, ".")
	if s == "" || len(parts) > 3 {
		return v, false
	}
	for k, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[k] = n
	}
	return v, true
}

// compareVersions returns -1, 0 or 1 as a is older than, the same as or
// newer than b. Versions that don't parse sort as oldest.
func compareVersions(a, b string) int {
	va, oka := parseVersion(a)
	vb, okb := parseVersion(b)
	switch {
	case !oka && !okb:
		return strings.Compare(a, b)
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for k := range 3 {
		if va[k] != vb[k] {
			if va[k] < vb[k] {
				return -1
			}
			return 1
		}
	}
	return 0
}

var (
	notesHeadingRe = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*$`)
	ruleRe         = regexp.MustCompile(`^\s*([-*_]\s*){3,}$`)
	pullURLRe      = regexp.MustCompile(`https://github\.com/TheXykril/otakase/(pull|issues)/(\d+)\b`)
)

// embedMarkdown makes release notes read well in an embed: headings become
// bold lines, horizontal rules go, pull request links get short, and runs of
// blank lines shrink to one.
func embedMarkdown(notes string) string {
	notes = strings.ReplaceAll(notes, "\r\n", "\n")
	var out []string
	blank := true
	for line := range strings.SplitSeq(notes, "\n") {
		line = strings.TrimRight(line, " \t")
		if ruleRe.MatchString(line) {
			continue
		}
		if m := notesHeadingRe.FindStringSubmatch(line); m != nil {
			line = "**" + strings.Trim(m[1], "*") + "**"
		}
		if line == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, line)
	}
	text := strings.TrimSpace(strings.Join(out, "\n"))
	return pullURLRe.ReplaceAllString(text, "[#$2]($0)")
}

// fitText keeps text within limit runes, cutting at a line break when it can
// and ending with more.
func fitText(text string, limit int, more string) string {
	if len([]rune(text)) <= limit {
		return text
	}
	keep := limit - len([]rune(more)) - 2
	r := []rune(text)[:max(keep, 0)]
	cut := string(r)
	if i := strings.LastIndex(cut, "\n"); i > len(cut)/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, "\n ") + "\n\n" + more
}

// findRelease returns the release for version, or the newest when version
// is empty.
func findRelease(list []ghRelease, version string) (ghRelease, bool) {
	if len(list) == 0 {
		return ghRelease{}, false
	}
	if strings.TrimSpace(version) == "" {
		return list[0], true
	}
	for _, r := range list {
		if compareVersions(r.version(), version) == 0 || strings.EqualFold(r.TagName, strings.TrimSpace(version)) {
			return r, true
		}
	}
	return ghRelease{}, false
}

// newerReleases returns the releases newer than since, newest first.
func newerReleases(list []ghRelease, since string) []ghRelease {
	var out []ghRelease
	for _, r := range list {
		if compareVersions(r.version(), since) > 0 {
			out = append(out, r)
		}
	}
	return out
}

func releaseEmbed(r ghRelease) *discordgo.MessageEmbed {
	notes := embedMarkdown(r.Body)
	if notes == "" {
		notes = "No notes for this release."
	}
	return &discordgo.MessageEmbed{
		Title:       "Otakase " + r.version(),
		URL:         r.HTMLURL,
		Description: fitText(notes, embedTextLimit, fmt.Sprintf("[More on GitHub](%s)", r.HTMLURL)),
		Color:       shu,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Update with otakase -u"},
		Timestamp:   r.PublishedAt.Format(time.RFC3339),
	}
}

// sinceEmbed combines the notes of every release newer than since, newest
// first, keeping whole releases while they fit.
func sinceEmbed(newer []ghRelease, since string) *discordgo.MessageEmbed {
	more := fmt.Sprintf("[More on GitHub](%s/releases)", repoURL)
	var b strings.Builder
	shown := 0
	for _, r := range newer {
		part := fmt.Sprintf("__**Otakase %s**__ · <t:%d:D>\n%s", r.version(), r.PublishedAt.Unix(), embedMarkdown(r.Body))
		if shown > 0 {
			part = "\n\n" + part
		}
		if len([]rune(b.String()+part)) > embedTextLimit-len([]rune(more))-2 {
			if shown == 0 {
				b.WriteString(fitText(part, embedTextLimit, more))
				shown = -1
			}
			break
		}
		b.WriteString(part)
		shown++
	}
	text := b.String()
	if shown >= 0 && shown < len(newer) {
		text += "\n\n" + more
	}
	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("What's new since %s", strings.TrimPrefix(strings.TrimSpace(since), "v")),
		URL:         repoURL + "/releases",
		Description: text,
		Color:       shu,
		Footer:      &discordgo.MessageEmbedFooter{Text: newerCount(len(newer)) + " · update with otakase -u"},
	}
}

func newerCount(n int) string {
	if n == 1 {
		return "1 newer release"
	}
	return fmt.Sprintf("%d newer releases", n)
}

func changelogCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{Name: "changelog", Description: "What changed in an Otakase release",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "version", Description: "Release to show (default: the latest)", Autocomplete: true},
			{Type: discordgo.ApplicationCommandOptionString, Name: "since", Description: "Your version: show everything newer than it (otakase -v)", Autocomplete: true},
		}}
}

// changelog answers /changelog.
func (b *bot) changelog(i *discordgo.InteractionCreate) {
	opts := optionMap(i)
	version, since := "", ""
	if o, ok := opts["version"]; ok {
		version = strings.TrimSpace(o.StringValue())
	}
	if o, ok := opts["since"]; ok {
		since = strings.TrimSpace(o.StringValue())
	}
	b.deferThen(i, onlyAsker(i), func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		return changelogAnswer(b.cfg.GitHubToken, version, since)
	})
}

func changelogAnswer(token, version, since string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	list, err := fetchReleases(token)
	if err != nil {
		log.Printf("releases: %v", err)
		return errorEmbed("Couldn't reach GitHub right now. Releases: %s/releases", repoURL), nil
	}
	if since != "" {
		if _, ok := parseVersion(since); !ok {
			return errorEmbed("**%s** doesn't look like a version. Run `otakase -v` to see yours, e.g. 26.5.0.", clip(since, 40)), nil
		}
		newer := newerReleases(list, since)
		if len(newer) == 0 {
			return &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Nothing newer than **%s**: you're up to date.",
				strings.TrimPrefix(since, "v"))}, nil
		}
		return sinceEmbed(newer, since), linkButtons("Latest release", newer[0].HTMLURL)
	}
	r, ok := findRelease(list, version)
	if !ok {
		return errorEmbed("There's no release **%s**. All releases: %s/releases", clip(version, 40), repoURL), nil
	}
	return releaseEmbed(r), linkButtons("Release on GitHub", r.HTMLURL)
}

// releaseChoices lists release versions matching what was typed.
func releaseChoices(list []ghRelease, typed string) []*discordgo.ApplicationCommandOptionChoice {
	typed = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(typed)), "v")
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	for k, r := range list {
		v := r.version()
		if !strings.Contains(strings.ToLower(v), typed) {
			continue
		}
		name := v
		if k == 0 {
			name += " (latest)"
		}
		if !r.PublishedAt.IsZero() {
			name += " · " + r.PublishedAt.Format("2 Jan 2006")
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: v})
		if len(choices) == 25 {
			break
		}
	}
	return choices
}

// suggestReleases autocompletes /changelog's version options.
func (b *bot) suggestReleases(i *discordgo.InteractionCreate) {
	var typed string
	for _, o := range i.ApplicationCommandData().Options {
		if o.Focused {
			typed = o.StringValue()
		}
	}
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	if list, err := fetchReleases(b.cfg.GitHubToken); err == nil {
		choices = releaseChoices(list, typed)
	} else {
		log.Printf("releases: %v", err)
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	}); err != nil {
		log.Printf("release suggestions: %v", err)
	}
}
