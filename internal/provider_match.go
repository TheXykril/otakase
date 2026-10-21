package internal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// animeSearchTitle is the title to search providers for: the name in the
// user's chosen language, else romaji, else English.
func animeSearchTitle(anime *Anime) string {
	if anime == nil {
		return ""
	}
	for _, title := range []string{GetAnimeName(*anime), anime.Title.Romaji, anime.Title.English} {
		if title = strings.TrimSpace(title); title != "" {
			return title
		}
	}
	return ""
}

var searchTitlePunctuation = strings.NewReplacer(
	":", "", "'", "", "\"", "", "!", "", "?", "",
	"-", " ", "_", " ", ".", " ", "(", " ", ")", " ", "×", " x ",
)

var (
	// Seasons are written "2nd Season", "Season 2", "S2", "S02" or, for a
	// sequel, "II"; all of them normalise to "s2", and the first season to
	// nothing, so "Dandadan 2nd Season" and "Dandadan S2" compare equal.
	romanSeasonRE   = regexp.MustCompile(`\b(II|III|IV)\b`)
	ordinalSeasonRE = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th) season\b`)
	seasonWordRE    = regexp.MustCompile(`\bseason (\d{1,2})\b`)
	shortSeasonRE   = regexp.MustCompile(`\bs0*(\d{1,2})\b`)
	firstSeasonRE   = regexp.MustCompile(`\bs1\b`)
	romanSeasons    = strings.NewReplacer("III", "s3", "II", "s2", "IV", "s4")
)

// normalizeSearchTitle reduces a title to lower-case words, so hosts that
// punctuate a title differently, or write its season another way, still
// compare equal.
func normalizeSearchTitle(title string) string {
	title = romanSeasonRE.ReplaceAllStringFunc(strings.TrimSpace(title), romanSeasons.Replace)
	title = searchTitlePunctuation.Replace(strings.ToLower(title))
	title = strings.Join(strings.Fields(title), " ")
	title = ordinalSeasonRE.ReplaceAllString(title, "s$1")
	title = seasonWordRE.ReplaceAllString(title, "s$1")
	title = shortSeasonRE.ReplaceAllString(title, "s$1")
	title = firstSeasonRE.ReplaceAllString(title, "")
	return strings.Join(strings.Fields(title), " ")
}

// titleSeason is the season a normalised title names, or 0 when it names none.
func titleSeason(normalized string) int {
	if m := shortSeasonRE.FindStringSubmatch(normalized); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// animeSeason is the season the anime's own titles name; 1 when they name none.
func animeSeason(anime *Anime) int {
	for _, title := range []string{anime.Title.Romaji, anime.Title.English} {
		if n := titleSeason(normalizeSearchTitle(title)); n > 0 {
			return n
		}
	}
	return 1
}

// scoreProviderSearchOption rates how likely a search result is to be the
// anime. The host's own ranking is the tie-breaker; a cover image carrying
// the AniList id is the strongest sign, then an exact title.
func scoreProviderSearchOption(option SelectionOption, anime *Anime, query string, index, total int) int {
	title := optionTitle(option)
	score := total - index + titleScore(title, anime, query)
	if anime.AnilistId != 0 && strings.Contains(option.Thumbnail, strconv.Itoa(anime.AnilistId)) {
		score += 120
	}
	if anime.TotalEpisodes > 0 && strings.Contains(option.Label, fmt.Sprintf("(%d episodes)", anime.TotalEpisodes)) {
		score += 20
	}
	// A search for a sequel finds the first season too, and the first season
	// matches the broader title variants exactly. A result that names another
	// season is not this anime, however well its name matches; one that names
	// this anime's later season is the likeliest.
	switch want, got := animeSeason(anime), titleSeason(title); {
	case got != 0 && got != want:
		score -= 150
	case got != 0 && got == want:
		score += 40
	}
	return score
}

// titleScore rates how well a normalised result name matches the anime's
// titles: 100 for each exact match, 35 for each that contains the other.
func titleScore(title string, anime *Anime, query string) int {
	score := 0
	for _, target := range []string{query, anime.Title.English, anime.Title.Romaji, anime.Title.Japanese} {
		target = normalizeSearchTitle(target)
		switch {
		case target == "", title == "":
		case title == target:
			score += 100
		case strings.Contains(title, target) || strings.Contains(target, title):
			score += 35
		}
	}
	return score
}

// optionTitle is the name a result is compared by.
func optionTitle(option SelectionOption) string {
	if title := normalizeSearchTitle(option.Title); title != "" {
		return title
	}
	return normalizeSearchTitle(option.Label)
}

// rankProviderSearchOptions returns the index and score of the best result,
// the first of any tie, and the score of the runner-up, which equals the best
// on a tie. Scores are never below 1, so -1 means "none".
func rankProviderSearchOptions(options []SelectionOption, anime *Anime, query string) (bestIndex, bestScore, runnerUp int) {
	bestScore, runnerUp = -1, -1
	for i, option := range options {
		score := scoreProviderSearchOption(option, anime, query, i, len(options))
		if score > bestScore {
			runnerUp = bestScore
			bestIndex, bestScore = i, score
		} else if score > runnerUp {
			runnerUp = score
		}
	}
	return bestIndex, bestScore, runnerUp
}

// selectBestProviderSearchResult picks the likeliest result, however weak.
func selectBestProviderSearchResult(options []SelectionOption, anime *Anime, query string) (SelectionOption, bool) {
	if len(options) == 0 {
		return SelectionOption{}, false
	}
	best, _, _ := rankProviderSearchOptions(options, anime, query)
	return options[best], true
}

// confidentProviderSearchMatch picks a result only when the scoring leaves no
// doubt: the cover names the AniList id, or one exact title clearly beats the
// rest. That is what lets an AniList romaji title match a host that lists
// the show under its English name without asking.
func confidentProviderSearchMatch(options []SelectionOption, anime *Anime, query string) (SelectionOption, bool) {
	if len(options) == 0 {
		return SelectionOption{}, false
	}
	best, score, runnerUp := rankProviderSearchOptions(options, anime, query)
	if score >= 120 || (score >= 100 && (len(options) == 1 || score-runnerUp >= 30)) {
		return options[best], true
	}
	return SelectionOption{}, false
}

// findProviderIDForAnime maps an anime onto one provider's own show id: the
// stored one when it belongs to this provider, otherwise by searching.
func findProviderIDForAnime(provider Provider, config *Config, anime *Anime, mode string) (string, error) {
	if storedName, storedID := providerIDForAnime(anime); storedName == provider.Name() && storedID != "" {
		return storedID, nil
	}
	return searchProviderIDForAnime(provider, config, anime, mode)
}

// searchProviderIDForAnime maps an anime by searching, ignoring any stored id.
//
// It works through the same title variants as the initial search rather than
// trying the AniList title alone. Hosts shorten long titles: anineko carries
// "Rich Girl Caretaker: I'm Secretly the Caregiver of the Most Popular Girl in
// This Rich Kid School" as plain "Rich Girl Caretaker", so searching only the
// full title reported the show as uncarried on the one host that had the
// episode being asked for.
//
// Variants are ordered exact-first, so a host that answers the real title is
// never sent the broader simplified forms.
func searchProviderIDForAnime(provider Provider, config *Config, anime *Anime, mode string) (string, error) {
	query := animeSearchTitle(anime)
	if query == "" {
		return "", fmt.Errorf("cannot search %s without an anime title", provider.Name())
	}

	var lastErr error
	var weak SelectionOption
	weakVariant := ""
	for _, variant := range buildSearchQueryVariants(config, anime.Title, query) {
		options, err := provider.SearchAnime(variant, mode)
		if err != nil {
			lastErr = err
			continue
		}
		if len(options) == 0 {
			continue
		}
		best, _, _ := rankProviderSearchOptions(options, anime, variant)
		option := options[best]

		// A host that matches loosely answers a title it does not carry with
		// whatever shares a word: nyaa answers "DAN DA DAN Season 2" with
		// "Juuou Mujin Dandivine". A result whose name matches none of the
		// anime's titles is kept only until a later variant finds one that does.
		coverMatch := anime.AnilistId != 0 && strings.Contains(option.Thumbnail, strconv.Itoa(anime.AnilistId))
		if titleScore(optionTitle(option), anime, variant) == 0 && !coverMatch {
			if weakVariant == "" {
				weak, weakVariant = option, variant
			}
			continue
		}
		logProviderMapping(query, provider.Name(), option.Key, mode, variant)
		return option.Key, nil
	}
	if weakVariant != "" {
		logProviderMapping(query, provider.Name(), weak.Key, mode, weakVariant)
		return weak.Key, nil
	}

	// Report the title the user recognises, not whichever variant happened to be
	// tried last.
	if lastErr != nil {
		return "", fmt.Errorf("no %s search results for %q: %w", provider.Name(), query, lastErr)
	}
	return "", fmt.Errorf("no %s search results for %q", provider.Name(), query)
}

func logProviderMapping(query, provider, key, mode, variant string) {
	if variant != query {
		Log(fmt.Sprintf("Mapped %q to provider %s id %s for %s fallback via title variant %q",
			query, provider, key, mode, variant))
		return
	}
	Log(fmt.Sprintf("Mapped %q to provider %s id %s for %s fallback", query, provider, key, mode))
}

// storedProviderIDMatches reports whether providerID is the id already recorded
// for this anime under providerName, i.e. it came from the saved mapping rather
// than from a search performed just now.
func storedProviderIDMatches(anime *Anime, providerName, providerID string) bool {
	if anime == nil || providerID == "" {
		return false
	}
	storedName, storedID := providerIDForAnime(anime)
	return storedName == providerName && storedID == providerID
}
