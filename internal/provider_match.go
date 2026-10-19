package internal

import (
	"fmt"
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
	"-", " ", "_", " ", ".", " ", "(", " ", ")", " ",
)

// normalizeSearchTitle reduces a title to lower-case words, so hosts that
// punctuate a title differently still compare equal.
func normalizeSearchTitle(title string) string {
	title = searchTitlePunctuation.Replace(strings.ToLower(strings.TrimSpace(title)))
	return strings.Join(strings.Fields(title), " ")
}

// scoreProviderSearchOption rates how likely a search result is to be the
// anime. The host's own ranking is the tie-breaker; a cover image carrying
// the AniList id is the strongest sign, then an exact title.
func scoreProviderSearchOption(option SelectionOption, anime *Anime, query string, index, total int) int {
	title := normalizeSearchTitle(option.Title)
	if title == "" {
		title = normalizeSearchTitle(option.Label)
	}

	score := total - index
	for _, target := range []string{query, anime.Title.English, anime.Title.Romaji, anime.Title.Japanese} {
		target = normalizeSearchTitle(target)
		switch {
		case target == "":
		case title == target:
			score += 100
		case strings.Contains(title, target) || strings.Contains(target, title):
			score += 35
		}
	}
	if anime.AnilistId != 0 && strings.Contains(option.Thumbnail, strconv.Itoa(anime.AnilistId)) {
		score += 120
	}
	if anime.TotalEpisodes > 0 && strings.Contains(option.Label, fmt.Sprintf("(%d episodes)", anime.TotalEpisodes)) {
		score += 20
	}
	return score
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
	for _, variant := range buildSearchQueryVariants(config, anime.Title, query) {
		options, err := provider.SearchAnime(variant, mode)
		if err != nil {
			lastErr = err
			continue
		}
		option, ok := selectBestProviderSearchResult(options, anime, variant)
		if !ok {
			continue
		}

		if variant != query {
			Log(fmt.Sprintf("Mapped %q to provider %s id %s for %s fallback via title variant %q",
				query, provider.Name(), option.Key, mode, variant))
		} else {
			Log(fmt.Sprintf("Mapped %q to provider %s id %s for %s fallback", query, provider.Name(), option.Key, mode))
		}
		return option.Key, nil
	}

	// Report the title the user recognises, not whichever variant happened to be
	// tried last.
	if lastErr != nil {
		return "", fmt.Errorf("no %s search results for %q: %w", provider.Name(), query, lastErr)
	}
	return "", fmt.Errorf("no %s search results for %q", provider.Name(), query)
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
