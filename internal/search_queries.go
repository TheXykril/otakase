package internal

import (
	"regexp"
	"strings"
)

// Providers index shows under wildly different titles: anipub matches the English
// release title, anineko tolerates either, and AllAnime prefers romaji. Searching a
// single AniList title therefore loses shows that every provider actually carries.
// buildSearchQueryVariants expands one title into an ordered candidate list so a
// search can fall through until a provider recognises the show.

var (
	parentheticalRE = regexp.MustCompile(`\s*\([^)]*\)`)
	seasonSuffixRE  = regexp.MustCompile(`(?i)\s+(?:season\s+\d+|\d+(?:st|nd|rd|th)\s+season|part\s+\d+|cour\s+\d+)\s*$`)
	multiSpaceRE    = regexp.MustCompile(`\s+`)
)

// preferredTitleOrder returns the anime titles ordered by the configured display
// language, so the language the user reads is also the one searched first.
func preferredTitleOrder(config *CurdConfig, title AnimeTitle) []string {
	english := strings.TrimSpace(title.English)
	romaji := strings.TrimSpace(title.Romaji)
	japanese := strings.TrimSpace(title.Japanese)

	language := "english"
	if config != nil && strings.TrimSpace(config.AnimeNameLanguage) != "" {
		language = strings.ToLower(strings.TrimSpace(config.AnimeNameLanguage))
	}

	switch language {
	case "romaji", "romanji", "japanese":
		return []string{romaji, english, japanese}
	default:
		return []string{english, romaji, japanese}
	}
}

// simplifyQuery trims the decorations that provider search indexes rarely carry:
// parenthetical qualifiers, trailing season/part markers, and subtitle clauses.
func simplifyQuery(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	variants := make([]string, 0, 4)
	add := func(candidate string) {
		candidate = strings.TrimSpace(multiSpaceRE.ReplaceAllString(candidate, " "))
		candidate = strings.Trim(candidate, " -:,~")
		if candidate != "" && !strings.EqualFold(candidate, query) {
			variants = append(variants, candidate)
		}
	}

	withoutParens := parentheticalRE.ReplaceAllString(query, "")
	add(withoutParens)

	withoutSeason := seasonSuffixRE.ReplaceAllString(withoutParens, "")
	add(withoutSeason)

	// Providers commonly index only the part before a subtitle separator, e.g.
	// "Rich Girl Caretaker: I'm Secretly the Caregiver of ..." is listed as the
	// leading clause alone on some catalogues.
	for _, separator := range []string{":", " - ", " – ", " — "} {
		if idx := strings.Index(withoutSeason, separator); idx > 0 {
			add(withoutSeason[:idx])
			break
		}
	}

	return variants
}

// buildSearchQueryVariants produces the ordered, de-duplicated set of queries to
// try for an anime. The caller's explicit query always leads so a manually typed
// search is never overridden by a title guess.
func buildSearchQueryVariants(config *CurdConfig, title AnimeTitle, primary string) []string {
	ordered := make([]string, 0, 12)
	seen := make(map[string]struct{}, 12)

	add := func(candidate string) {
		candidate = strings.TrimSpace(multiSpaceRE.ReplaceAllString(candidate, " "))
		if candidate == "" {
			return
		}
		key := strings.ToLower(candidate)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		ordered = append(ordered, candidate)
	}

	add(primary)
	titles := preferredTitleOrder(config, title)
	for _, candidate := range titles {
		add(candidate)
	}

	// Simplified forms come last: they are broader and likelier to match the wrong
	// show, so they only run once every exact title has failed.
	for _, candidate := range append([]string{primary}, titles...) {
		for _, simplified := range simplifyQuery(candidate) {
			add(simplified)
		}
	}

	return ordered
}
