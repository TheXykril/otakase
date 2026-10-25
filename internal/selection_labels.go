package internal

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/providers"
)

// labelEpisodeCountRE finds the "(N episodes)" a search label carries.
var labelEpisodeCountRE = regexp.MustCompile(`\((\d+)\s+episodes?\)`)

// FormatAnimeSearchLabel is a search result's label: the title, followed by its
// episode count when that is known.
func FormatAnimeSearchLabel(title string, count int) string {
	if title = strings.TrimSpace(title); title == "" || count <= 0 {
		return title
	}
	return fmt.Sprintf("%s (%d episodes)", title, count)
}

func TrackerEpisodeHint(config *Config, entry *Entry) string {
	episodes := trackedEpisodes(entry)
	if episodes <= 0 {
		return ""
	}
	subject := RemoteTrackingDisplayName(config) + " entry"
	if title := mediaDisplayTitle(entry.Media, config); title != "" {
		subject += fmt.Sprintf(" for %q", title)
	}
	return fmt.Sprintf("Your %s has %d episodes — pick the matching title below.", subject, episodes)
}

// trackedEpisodes is the tracker's episode count for entry, or 0 without one.
func trackedEpisodes(entry *Entry) int {
	if entry == nil {
		return 0
	}
	return entry.Media.Episodes
}

// episodeCountFromSelectionOption reads a result's episode count from its
// label or title, or failing that from the provider's own search item.
func episodeCountFromSelectionOption(opt SelectionOption) (int, bool) {
	for _, text := range [...]string{opt.Label, opt.Title} {
		match := labelEpisodeCountRE.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		if count, err := strconv.Atoi(match[1]); err == nil && count > 0 {
			return count, true
		}
	}
	if item, ok := opt.ExtraData.(providers.SearchHit); ok && item.Episodes > 0 {
		return item.Episodes, true
	}
	return 0, false
}

// selectionOptionBaseTitle is a result's title without the count, note or
// provider tag its label may have after it.
func selectionOptionBaseTitle(opt SelectionOption) string {
	if opt.Title != "" {
		return strings.TrimSpace(opt.Title)
	}
	base := strings.TrimSpace(opt.Label)
	for _, sep := range []string{" (", " — ", " ["} {
		if cut := strings.Index(base, sep); cut > 0 {
			return strings.TrimSpace(base[:cut])
		}
	}
	return base
}

// ensureEpisodeCountInLabel rewrites a result's label to show its episode
// count, when the count is known but the label leaves it out.
func ensureEpisodeCountInLabel(opt SelectionOption) SelectionOption {
	if labelEpisodeCountRE.MatchString(opt.Label) {
		return opt
	}
	count, ok := episodeCountFromSelectionOption(opt)
	title := selectionOptionBaseTitle(opt)
	if !ok || title == "" {
		return opt
	}

	// The provider tag stays at the end, after the count.
	tag := ""
	if at := strings.LastIndex(opt.Label, " ["); at > 0 && strings.HasSuffix(opt.Label, "]") {
		tag = opt.Label[at:]
	}
	opt.Label = FormatAnimeSearchLabel(title, count) + tag
	if opt.Title == "" {
		opt.Title = title
	}
	return opt
}

// ProviderSearchOptionsForDisplay labels provider search results with their
// episode counts and ticks the ones whose count matches the tracker entry,
// which is usually the right season.
func ProviderSearchOptionsForDisplay(results []SelectionOption, entry *Entry) []SelectionOption {
	if len(results) == 0 {
		return results
	}
	tracked := trackedEpisodes(entry)
	labelled := make([]SelectionOption, 0, len(results))
	for _, opt := range results {
		opt = ensureEpisodeCountInLabel(opt)
		if n, ok := episodeCountFromSelectionOption(opt); ok && n == tracked {
			opt.Label += " ✓"
		}
		labelled = append(labelled, opt)
	}
	return labelled
}

// ManualProviderSearchEnabled reports whether provider matches are always
// picked by hand rather than automatically.
func ManualProviderSearchEnabled(config *Config) bool {
	if config == nil {
		return false
	}
	return config.ManualProviderSearch
}

// mediaFormatNames spells out the tracker formats that do not read well as
// written. The rest (TV, OVA, ONA) are shown as they are.
var mediaFormatNames = map[string]string{
	"TV_SHORT": "TV Short",
	"MOVIE":    "Movie",
	"SPECIAL":  "Special",
	"MUSIC":    "Music",
}

func displayMediaFormat(raw string) string {
	format := strings.ToUpper(strings.TrimSpace(raw))
	if name, ok := mediaFormatNames[format]; ok {
		return name
	}
	return strings.ReplaceAll(format, "_", " ")
}

func formatEpisodeCountHint(count int) string {
	if count <= 0 {
		return "unknown episode count"
	}
	return fmt.Sprintf("%d episodes", count)
}

// ManualProviderSearchHint says what is being searched for, so the right
// provider entry can be picked by hand: title, format, episode count and
// sub or dub.
func ManualProviderSearchHint(config *Config, entry *Entry, query, subOrDub string) string {
	title, format := strings.TrimSpace(query), ""
	if e := entry; e != nil {
		title = cmp.Or(mediaDisplayTitle(e.Media, config), title)
		format = displayMediaFormat(e.Media.Format)
	}

	const tail = " — select the matching provider entry below."
	parts := slices.DeleteFunc([]string{
		fmt.Sprintf("Searching for %q", cmp.Or(title, "unknown title")),
		format,
		formatEpisodeCountHint(trackedEpisodes(entry)),
		strings.ToLower(normalizeTranslationType(subOrDub)),
	}, func(part string) bool { return part == "" })
	return strings.Join(parts, " · ") + tail
}

func promptProviderSearchSelectionWithHint(config *Config, results []SelectionOption, entry *Entry, hint string) (SelectionOption, error) {
	if hint = strings.TrimSpace(hint); hint != "" {
		Out(hint)
	}
	return DynamicSelect(ProviderSearchOptionsForDisplay(results, entry))
}

func promptProviderSearchSelection(config *Config, results []SelectionOption, entry *Entry) (SelectionOption, error) {
	return promptProviderSearchSelectionWithHint(config, results, entry, TrackerEpisodeHint(config, entry))
}
