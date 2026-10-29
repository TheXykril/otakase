package internal

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/providers"
)

// ProviderMappingOutcome is how a provider search for a show ended.
type ProviderMappingOutcome int

// How a provider search can end.
const (
	ProviderMappingOK   ProviderMappingOutcome = iota // the show is mapped
	ProviderMappingBack                               // the user backed out
	ProviderMappingQuit                               // the user quit, or a menu broke
)

// providerMappingSearchState is where a provider search for a show stands.
// It starts on the whole stack at once; "try next provider" switches to one
// provider at a time, in stack order.
type providerMappingSearchState struct {
	query string // the term being searched for
	// allProviders is the provider stack, in order.
	allProviders []string
	// sequential is set once "try next provider" is used; from then on only
	// allProviders[providerIndex] is searched.
	sequential    bool // one provider at a time
	providerIndex int  // the one provider, when sequential
	// queryVariants holds alternate titles to try when the primary query finds
	// nothing, so a romaji-only AniList title still resolves on English-indexed
	// providers. It is cleared once the user types a query of their own.
	queryVariants []string
}

// setManualQuery records a user-supplied search term and drops the automatic title
// variants, so an explicit search is never silently replaced by a title guess.
func (st *providerMappingSearchState) setManualQuery(query string) {
	st.query = query
	st.queryVariants = nil
}

// currentProvider is the one provider being searched, or "" when the search
// spans the stack or has run past its end.
func (st *providerMappingSearchState) currentProvider() string {
	if !st.sequential || st.providerIndex >= len(st.allProviders) {
		return ""
	}
	return st.allProviders[st.providerIndex]
}

// activeProviders is what the next search runs against: the whole stack, the
// one provider being tried, or nothing once the stack has run out.
func (st *providerMappingSearchState) activeProviders() []string {
	if st.sequential {
		if name := st.currentProvider(); name != "" {
			return []string{name}
		}
		return nil
	}
	return st.allProviders
}

// currentProviderLabel names the search scope in prompts: "every source" or
// "source 2", never the site.
func (st *providerMappingSearchState) currentProviderLabel() string {
	if st.sequential {
		if name := st.currentProvider(); name != "" {
			return strings.Trim(sourceTag(name), "[]")
		}
		return ""
	}
	return "every source"
}

// nextProviderLabel names the provider "try next provider" would search, or
// "" when there is none. From the whole stack that is its first provider.
func (st *providerMappingSearchState) nextProviderLabel() string {
	var next int
	switch {
	case st.sequential:
		next = st.providerIndex + 1
	case len(st.allProviders) < 2:
		// Searching the only provider on its own would repeat the search.
		return ""
	}
	if next >= len(st.allProviders) {
		return ""
	}
	return st.allProviders[next]
}

// advanceToNextProvider moves the search one provider along the stack. It
// reports false when there is nowhere left to go.
func (st *providerMappingSearchState) advanceToNextProvider() bool {
	switch {
	case st.sequential:
		if len(st.allProviders) == 0 {
			return false
		}
		st.providerIndex++
		return st.providerIndex < len(st.allProviders)
	case len(st.allProviders) < 2:
		return false
	default:
		st.sequential, st.providerIndex = true, 0
		return true
	}
}

// resetToAllProviders goes back to searching the whole stack at once.
func (st *providerMappingSearchState) resetToAllProviders() {
	st.sequential, st.providerIndex = false, 0
}

func searchAnimeForMapping(config *Config, state *providerMappingSearchState, mode string) ([]SelectionOption, error) {
	names := state.activeProviders()
	if len(names) == 0 {
		return nil, nil // the stack has run out
	}

	runSearch := func(query string) ([]SelectionOption, error) {
		return searchAnimeWithProviders(names, query, mode)
	}
	defer BeginBusy(config, "Searching "+state.currentProviderLabel())()

	queries := state.queryVariants
	if len(queries) == 0 {
		queries = []string{state.query}
	}

	var firstErr error
	for _, query := range queries {
		results, err := runSearch(query)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			Log(fmt.Sprintf("Search variant %q failed: %v", query, err))
			continue
		}
		if len(results) > 0 {
			if query != state.query {
				Log(fmt.Sprintf("Search variant %q matched after %q found nothing", query, state.query))
				// Adopt the query that worked so recovery prompts and any later
				// retry stay on the term that actually resolves this show.
				state.query = query
			}
			return results, nil
		}
	}

	return nil, firstErr
}

// optionDisplayName is the most readable name a search result has.
func optionDisplayName(option SelectionOption) string {
	for _, name := range []string{option.Label, option.Title} {
		if name != "" {
			return name
		}
	}
	return option.Key
}

// confirmProviderMatch asks whether to trust a matched provider entry.
//
// A spawned cast window has nobody to ask, so it accepts. That is the better of
// the two answers here rather than a policy imposed on it: this is only reached
// on an exact title and episode-count match, and the alternative -- "select
// manually" -- is a menu that cannot be shown, which would end the episode
// instead. See wiki/Developer-Cast-Window-Prompts.md.
func confirmProviderMatch(config *Config, option SelectionOption, reason string) bool {
	Out(fmt.Sprintf("Match found by %s: %s", reason, optionDisplayName(option)))
	if castWindowNonInteractive() {
		Out("Using it -- there is no one at the keyboard to confirm.")
		return true
	}
	answer, err := promptSelect([]SelectionOption{
		{Key: "use", Label: "Use this match", Icon: icons.Yes},
		{Key: "manual", Label: "Select manually", Icon: icons.AllShows},
	})
	if err != nil {
		Log(fmt.Sprintf("Match confirmation menu failed: %v", err))
		return false
	}
	return answer.Key == "use"
}

var (
	// anilistCoverID is the AniList media id in an AniList cover URL.
	anilistCoverID = regexp.MustCompile(`anilistcdn/media/anime/cover/(?:large|medium)/(?:bx)?(\d+)`)
	// malImageFile is the file name of a MyAnimeList picture, which only
	// identifies the show once looked up among its pictures on Jikan.
	malImageFile = regexp.MustCompile(`myanimelist\.net/images/anime/[^/]+/([^/]+\.jpg)`)
	// posterMalID is the MAL id in hosts' "/posters/<id>.webp" covers.
	posterMalID = regexp.MustCompile(`/posters/(\d+)(?:\.webp)?`)
)

// ensureMalID looks up the anime's MAL id from its AniList id when it is not
// known yet. It returns the id, 0 when the lookup found nothing.
func ensureMalID(anime *Anime) int {
	if anime.MalId == 0 {
		id, _ := GetAnimeMalID(anime.AnilistId)
		anime.MalId = id
	}
	return anime.MalId
}

// autoMatchProviderListing picks the search result that is the anime without
// asking, when one can be told apart: by a MAL id the host gives (as the key,
// in its extra data or in its cover's path), by a cover image that names the
// show, or by a confident title match. As a last resort a result with the
// exact title and episode count is offered for confirmation. It returns the
// picked result's key.
func autoMatchProviderListing(config *Config, anime *Anime, animeList []SelectionOption, userQuery string, anilistEntry *Entry) (string, bool) {
	if malID := ensureMalID(anime); malID != 0 {
		wanted := strconv.Itoa(malID)
		for _, option := range animeList {
			if option.Key == wanted {
				Log(fmt.Sprintf("Provider key %s is the MAL id; using it", option.Key))
				return option.Key, true
			}
		}
	}

	pictures := malPictureLoader(anime)
	for n, candidate := range animeList {
		Log(fmt.Sprintf("Auto-match candidate %d: key=%q label=%q thumb=%q", n, candidate.Key, candidate.Label, candidate.Thumbnail))
		if how := providerListingMatch(anime, candidate, pictures); how != "" {
			Log(fmt.Sprintf("Matched provider id %s by %s", candidate.Key, how))
			return candidate.Key, true
		}
	}

	if match, ok := confidentProviderSearchMatch(animeList, anime, userQuery); ok {
		Log(fmt.Sprintf("Matched provider id %s by a confident title match (%s)", match.Key, match.Label))
		return match.Key, true
	}

	if anilistEntry == nil {
		return "", false
	}
	// Same title, and the episode count comes from the tracker: worth
	// offering, not worth taking unasked.
	for _, option := range animeList {
		if option.Title != userQuery {
			continue
		}
		if confirmProviderMatch(config, option, "title and episode count") {
			Log(fmt.Sprintf("User confirmed the title match %s", option.Key))
			return option.Key, true
		}
		break
	}
	Log(fmt.Sprintf("Nothing matched %q (%d episodes) on its own; asking the user", userQuery, anilistEntry.Media.Episodes))
	return "", false
}

// providerListingMatch says how a search result is known to be the anime, or
// "" when it is not.
func providerListingMatch(anime *Anime, option SelectionOption, malPictures func() []string) string {
	if malID := malIDFromProviderExtraData(option.ExtraData); malID != 0 && malID == anime.MalId {
		return "the MAL id in its search data"
	}
	thumbnail := option.Thumbnail
	switch {
	case strings.Contains(thumbnail, "anilist.co"):
		if m := anilistCoverID.FindStringSubmatch(thumbnail); m != nil && m[1] == strconv.Itoa(anime.AnilistId) {
			return "its AniList cover"
		}
	case strings.Contains(thumbnail, "myanimelist.net"):
		m := malImageFile.FindStringSubmatch(thumbnail)
		if m == nil {
			break
		}
		for _, picture := range malPictures() {
			if strings.Contains(picture, m[1]) {
				return "its MyAnimeList cover " + m[1]
			}
		}
	case strings.Contains(thumbnail, "/posters/"):
		if m := posterMalID.FindStringSubmatch(thumbnail); m != nil && anime.MalId != 0 && m[1] == strconv.Itoa(anime.MalId) {
			return "the MAL id in its poster"
		}
	}
	return ""
}

// malPictureLoader returns a function that fetches the anime's pictures from
// Jikan on its first call and remembers them. Most searches have no
// MyAnimeList covers, and those never pay for the request.
func malPictureLoader(anime *Anime) func() []string {
	var pictures []string
	loaded := false
	return func() []string {
		if loaded {
			return pictures
		}
		loaded = true
		if ensureMalID(anime) == 0 {
			return nil
		}
		urls, err := ShowPictures(anime.MalId)
		if err != nil {
			Log(fmt.Sprintf("Failed to fetch Jikan pictures: %v", err))
		}
		pictures = urls
		return pictures
	}
}

// nextProviderOption is the "try next provider" entry, when there is a next
// provider to try.
func nextProviderOption(state *providerMappingSearchState) []SelectionOption {
	next := state.nextProviderLabel()
	if next == "" {
		return nil
	}
	return []SelectionOption{{Key: "next_provider", Label: fmt.Sprintf("Try next source (%s)", strings.Trim(sourceTag(next), "[]")), Icon: icons.Skip}}
}

// recoveryAction reads a recovery menu's answer, folding the menu's own exit
// entries into "back" and "quit".
func recoveryAction(selected SelectionOption) string {
	selected = NormalizeSelectionKey(selected)
	switch {
	case SelectionMeansQuit(selected):
		return "quit"
	case SelectionMeansBack(selected) || selected.Key == "back":
		return "back"
	default:
		return selected.Key
	}
}

// promptProviderSearchRecovery is the menu shown when a search found nothing
// or failed. reason replaces the default "no results" line when set.
func promptProviderSearchRecovery(config *Config, state *providerMappingSearchState, reason string) (action string, err error) {
	options := []SelectionOption{{Key: "custom", Label: "Search with a different name", Icon: icons.Search}}
	options = append(options, nextProviderOption(state)...)
	options = append(options, SelectionOption{Key: "back", Label: "Back to menu", Icon: icons.Back})
	options = withAskServerOption(config, options, state.query)

	headline := reason
	if headline == "" {
		headline = fmt.Sprintf("No results found for '%s' on %s.", state.query, state.currentProviderLabel())
	}
	picked, menuErr := selectWithOptionalMessage(config, options, "Search", headline)
	if menuErr != nil {
		return "", menuErr
	}
	action = recoveryAction(picked)
	if action == askServerKey {
		askServerWhereToWatch(config, state.query, 0, headline+fmt.Sprintf("\nSources searched: %d", len(state.allProviders)))
		return promptProviderSearchRecovery(config, state, reason)
	}
	return action, nil
}

// promptProviderMatchRecovery is the menu shown when a search found results
// but none of them could be matched to the show automatically.
func promptProviderMatchRecovery(config *Config, state *providerMappingSearchState) (action string, err error) {
	options := append([]SelectionOption{
		{Key: "pick", Label: "Pick from search results", Icon: icons.AllShows},
		{Key: "custom", Label: "Search with a different name", Icon: icons.Search},
	}, nextProviderOption(state)...)
	options = append(options, SelectionOption{Key: "back", Label: "Back to menu", Icon: icons.Back})

	picked, menuErr := selectWithOptionalMessage(config, options, "Match", "We didn't find an automatic match.")
	if menuErr != nil {
		return "", menuErr
	}
	return recoveryAction(picked), nil
}

// selectWithOptionalMessage shows note above a menu. In Rofi it goes in
// the menu's own message line, so it does not raise a notification per line.
func selectWithOptionalMessage(config *Config, menu []SelectionOption, title, note string) (SelectionOption, error) {
	switch {
	case config != nil && config.RofiSelection:
		return RofiSelectWithMessage(menu, false, title, note)
	case strings.TrimSpace(note) != "":
		// Out, not fmt.Println: in raw mode a bare \n does not return the
		// cursor, so this printed diagonally, and when the cast panel owns the
		// alternate buffer it landed in the middle of the held frame. Out
		// handles both. Reachable here on the episode 2+ cast recovery path.
		Out(note)
	}
	return promptSelectOrdered(menu)
}

// activeProviderName is the provider an unqualified result came from: the
// one being searched on its own, else the head of the stack.
func activeProviderName(config *Config, state *providerMappingSearchState) string {
	if state != nil {
		if single := state.currentProvider(); single != "" {
			return single
		}
	}
	stack := configuredProviderNames(config)
	if len(stack) == 0 {
		return firstEnabledProviderName()
	}
	return stack[0]
}

// providerNameFromSelectionLabel reads the provider from the " [name]" a
// stacked search appends to a label.
func providerNameFromSelectionLabel(text string) string {
	label := strings.TrimSpace(text)
	open := strings.LastIndex(label, " [")
	if open <= 0 || !strings.HasSuffix(label, "]") {
		return ""
	}
	return normalizeProviderName(strings.TrimSpace(label[open+2 : len(label)-1]))
}

// providerNameFromSelection works out which provider a picked result belongs
// to: from a qualified key, the label's suffix, or the search scope.
func providerNameFromSelection(config *Config, state *providerMappingSearchState, option SelectionOption) string {
	name, _, qualified := ParseProviderQualifiedID(option.Key)
	if !qualified {
		name = providerNameFromSelectionLabel(option.Label)
	}
	if name == "" {
		name = activeProviderName(config, state)
	}
	return name
}

// assignProviderMapping stores id on the anime. A qualified id names its own
// provider; for a bare one, owner says which provider it came from.
func assignProviderMapping(anime *Anime, id string, owner func() string) {
	if name, rawID, ok := ParseProviderQualifiedID(id); ok {
		anime.ProviderName, anime.ProviderId = name, rawID
		return
	}
	anime.ProviderId, anime.ProviderName = id, owner()
}

// applyMatchedProviderMapping files the id auto-matching put in
// anime.ProviderId under the provider it came from.
func applyMatchedProviderMapping(config *Config, state *providerMappingSearchState, anime *Anime) {
	assignProviderMapping(anime, anime.ProviderId, func() string {
		return activeProviderName(config, state)
	})
}

// applySelectedProviderMapping maps the anime to a result the user picked.
func applySelectedProviderMapping(config *Config, state *providerMappingSearchState, anime *Anime, selected SelectionOption) {
	assignProviderMapping(anime, selected.Key, func() string {
		return providerNameFromSelection(config, state, selected)
	})
}

// promptCustomProviderSearchQuery asks what to search the providers for,
// starting from the name that has been tried so far.
//
// Escape, or an emptied field, cancels: this question is one keypress away from
// a list of results, and changing your mind about it should leave the search as
// it was rather than run it again for nothing.
func promptCustomProviderSearchQuery(config *Config, currentQuery, hint string) (string, bool, error) {
	return customProviderQueryFromAnswer(currentQuery, func() (string, bool, error) {
		return promptCancelableWithDefault(config, "Search", "Search under a different name", hint, currentQuery)
	})
}

// customProviderQueryFromAnswer turns an answer into the name to search for.
//
// The asking is a parameter so the rule worth pinning -- that backing out
// leaves the search as it was, rather than clearing it -- can be tested without
// a terminal, which the prompt itself needs.
func customProviderQueryFromAnswer(currentQuery string, ask func() (string, bool, error)) (string, bool, error) {
	answer, cancelled, err := ask()
	switch {
	case err != nil:
		return currentQuery, false, err
	case cancelled:
		return currentQuery, true, nil
	default:
		return answer, false, nil
	}
}

// ResolveAnimeProviderMapping finds the anime on the providers and stores the
// match in anime.ProviderName and anime.ProviderId.
func ResolveAnimeProviderMapping(config *Config, anime *Anime, query string, anilistEntry *Entry) (ProviderMappingOutcome, error) {
	manualOnly := ManualProviderSearchEnabled(config)
	return resolveAnimeProviderMapping(config, anime, query, anilistEntry, manualOnly)
}

// resolveAnimeProviderMapping searches until the anime is mapped or the user
// gives up. With manualOnly the user always picks from the results; without
// it a result is taken unasked when it can be told to be the anime.
func resolveAnimeProviderMapping(config *Config, anime *Anime, query string, anilistEntry *Entry, manualOnly bool) (ProviderMappingOutcome, error) {
	search := &providerMappingSearchState{
		query:         query,
		allProviders:  providerNamesForShow(config, anime),
		queryVariants: buildSearchQueryVariants(config, anime.Title, query),
	}
	if len(search.queryVariants) > 1 {
		Log(fmt.Sprintf("Search title variants for %q: %v", query, search.queryVariants))
	}

	for {
		Log(fmt.Sprintf("Provider search: query=%q mode=%s scope=%s", search.query, config.SubOrDub, search.currentProviderLabel()))
		outcome, again, err := runMappingSearch(config, search, anime, query, anilistEntry, manualOnly)
		if err != nil {
			return ProviderMappingQuit, err
		}
		if !again {
			return outcome, nil
		}
	}
}

// runMappingSearch runs the search once and acts on the results: it maps the
// anime, or hands over to a recovery menu. again means the search changed and
// should run once more.
func runMappingSearch(config *Config, search *providerMappingSearchState, anime *Anime, query string, anilistEntry *Entry, manualOnly bool) (outcome ProviderMappingOutcome, again bool, err error) {
	results, searchErr := searchAnimeForMapping(config, search, config.SubOrDub)
	if searchErr != nil || len(results) == 0 {
		reason := ""
		if searchErr != nil {
			Log(fmt.Sprintf("Provider search failed: %v", searchErr))
			reason = fmt.Sprintf("Search failed for '%s': %v", search.query, searchErr)
		}
		action, menuErr := promptProviderSearchRecovery(config, search, reason)
		if menuErr != nil {
			return ProviderMappingQuit, false, menuErr
		}
		return handleProviderMappingAction(config, search, action, query)
	}

	anime.ProviderId = ""
	if !manualOnly {
		if key, ok := autoMatchProviderListing(config, anime, results, search.query, anilistEntry); ok {
			anime.ProviderId = key
			applyMatchedProviderMapping(config, search, anime)
			return ProviderMappingOK, false, nil
		}
	}
	return pickProviderListing(config, search, anime, results, anilistEntry, query, manualOnly)
}

// pickProviderListing has the user pick the anime from search results, or
// choose another way on from the recovery menu. With manualOnly the results
// come first; otherwise the menu does, since a match was already looked for
// and not found. searchAgain means the search changed and should run again.
func pickProviderListing(config *Config, state *providerMappingSearchState, anime *Anime, animeList []SelectionOption, anilistEntry *Entry, query string, manualOnly bool) (outcome ProviderMappingOutcome, searchAgain bool, err error) {
	choose := func() (SelectionOption, error) {
		if !manualOnly {
			Out("Select the correct anime from the search results.")
			return promptProviderSearchSelection(config, animeList, anilistEntry)
		}
		hint := ManualProviderSearchHint(config, anilistEntry, state.query, config.SubOrDub)
		return promptProviderSearchSelectionWithHint(config, animeList, anilistEntry, hint)
	}

	showResults := manualOnly
	for {
		if showResults {
			selected, selectErr := choose()
			switch {
			case selectErr != nil:
				Log(fmt.Sprintf("Failed to select anime: %v", selectErr))
			case selected.Key == "-2":
				return ProviderMappingBack, false, nil
			case selected.Key != "-1":
				applySelectedProviderMapping(config, state, anime, selected)
				return ProviderMappingOK, false, nil
			}
			// Otherwise it was the results menu's own back entry ("-1"),
			// which leads on to the recovery menu.
		}

		action, err := promptProviderMatchRecovery(config, state)
		if err != nil {
			return ProviderMappingQuit, false, err
		}
		switch action {
		case "pick":
			showResults = true
		case "custom", "next_provider":
			return handleProviderMappingAction(config, state, action, query)
		case "back":
			return ProviderMappingBack, false, nil
		case "quit":
			return ProviderMappingQuit, false, nil
		default:
			showResults = manualOnly
		}
	}
}

// RemapProviderAnime lets the user pick a show from their lists and map it to
// a provider entry by hand, replacing the saved mapping.
func RemapProviderAnime(userConfig *Config, user *User, databaseAnimes *[]Anime) {
	if userConfig == nil || user == nil {
		return
	}
	entry, ok := pickListEntryToRemap(userConfig, user)
	if !ok {
		return
	}

	if databaseAnimes != nil {
		saved := LocalFindAnime(*databaseAnimes, entry.Media.ID, "")
		if saved == nil {
			Out("No saved match yet.")
		} else {
			Out("Current match: " + sourceTag(CurrentAnimeProviderName(saved)))
		}
	}

	// Mapped from scratch, so nothing of the old mapping steers the search.
	media := entry.Media
	anime := Anime{
		AnilistId:     media.ID,
		MalId:         media.MalID,
		Title:         media.Title,
		TotalEpisodes: media.Episodes,
		IsAdult:       media.IsAdult,
		CoverImage:    entry.CoverImage,
	}
	query := mediaDisplayTitle(media, userConfig)

	outcome, err := resolveAnimeProviderMapping(userConfig, &anime, query, entry, true)
	switch {
	case err != nil:
		Log(fmt.Sprintf("Provider remap failed: %v", err))
		Out("Failed to update the match.")
		return
	case outcome != ProviderMappingOK:
		return
	}
	historyPath := HistoryFile(userConfig.StoragePath)
	if err := persistRemappedProvider(historyPath, databaseAnimes, entry, &anime, query); err != nil {
		Log(fmt.Sprintf("Failed to save remapped provider: %v", err))
		Out("Match picked, but it could not be saved.")
		return
	}
	Out("Match updated: " + sourceTag(anime.ProviderName))
}

// pickListEntryToRemap has the user pick a show from a freshly synced copy
// of their lists.
func pickListEntryToRemap(userConfig *Config, user *User) (*Entry, bool) {
	if err := RefreshUserAnimeList(userConfig, user); err != nil {
		Log(fmt.Sprintf("Remap: list refresh failed, using the cached lists: %v", err))
	}
	if sync := user.ListSync; sync != nil {
		user.AnimeList = sync.Current()
	}

	menu := buildCategorySelectionOptions(user.AnimeList, "ALL")
	if len(menu) == 0 {
		Out("No anime found in your lists.")
		return nil, false
	}
	pick := func() (SelectionOption, error) { return DynamicSelect(menu) }
	if userConfig.RofiSelection && userConfig.ImagePreview {
		pick = func() (SelectionOption, error) {
			return DynamicSelectPreview(buildCategoryPreviewOptions(user.AnimeList, "ALL"), false)
		}
	}
	picked, err := pick()
	switch {
	case err != nil:
		Log(fmt.Sprintf("Remap: list menu failed: %v", err))
		return nil, false
	case picked.Key == "-1" || picked.Key == "-2":
		return nil, false
	}

	entry, err := FindAnimeByAnilistID(user.AnimeList, picked.Key)
	if err != nil {
		Log(fmt.Sprintf("Remap: picked id %s is not in the lists: %v", picked.Key, err))
		Out("Could not find the selected anime in your lists.")
		return nil, false
	}
	return entry, true
}

// persistRemappedProvider saves a new mapping to the history, keeping the
// episode and position already saved for the show. A show not in the history
// yet starts at the episode after its tracker progress.
func persistRemappedProvider(historyPath string, history *[]Anime, entry *Entry, anime *Anime, title string) error {
	if anime == nil || entry == nil {
		return errors.New("missing anime data")
	}

	episode, position, duration := 1, 0, 0
	if history != nil {
		if prev := LocalFindAnime(*history, anime.AnilistId, ""); prev != nil {
			episode, position, duration = prev.Ep.Number, prev.Ep.Player.PlaybackTime, prev.Ep.Duration
			if name := GetAnimeName(*prev); name != "" {
				title = name
			}
		}
	}
	if episode < 1 {
		episode = max(nextEpisodeFromProgress(entry.Progress), 1)
	}

	err := LocalRemapAnimeProvider(historyPath, anime.AnilistId, anime.ProviderName, anime.ProviderId, title, episode, position, duration)
	if err == nil && history != nil {
		*history = LocalGetAllAnime(historyPath)
	}
	return err
}

// handleProviderMappingAction carries out a recovery menu choice. It reports
// whether to search again; when not, the outcome is where the search ended.
func handleProviderMappingAction(config *Config, state *providerMappingSearchState, action, defaultQuery string) (ProviderMappingOutcome, bool, error) {
	switch action {
	case "custom":
		return searchUnderNewName(config, state, defaultQuery)
	case "next_provider":
		return searchNextProvider(config, state, defaultQuery)
	case "back":
		return ProviderMappingBack, false, nil
	case "quit":
		return ProviderMappingQuit, false, nil
	default:
		return ProviderMappingOK, true, nil
	}
}

// searchUnderNewName asks for a different search term and, given one, starts
// the search over on the whole stack.
func searchUnderNewName(config *Config, state *providerMappingSearchState, defaultQuery string) (ProviderMappingOutcome, bool, error) {
	hint := fmt.Sprintf("currently %q · enter to search · esc to go back", state.query)
	newQuery, cancelled, err := promptCustomProviderSearchQuery(config, state.query, hint)
	if err != nil {
		return ProviderMappingQuit, false, err
	}
	if cancelled {
		// The name is unchanged, so searching again would repeat a search
		// that has already failed. Offer the other ways out instead.
		action, recoveryErr := promptProviderSearchRecovery(config, state, "Search name unchanged.")
		if recoveryErr != nil {
			return ProviderMappingQuit, false, recoveryErr
		}
		return handleProviderMappingAction(config, state, action, defaultQuery)
	}
	state.setManualQuery(newQuery)
	state.resetToAllProviders()
	return ProviderMappingOK, true, nil
}

// searchNextProvider narrows the search to the next provider on the stack,
// or says there is none and offers the recovery menu again.
func searchNextProvider(config *Config, state *providerMappingSearchState, defaultQuery string) (ProviderMappingOutcome, bool, error) {
	if state.advanceToNextProvider() {
		return ProviderMappingOK, true, nil
	}
	Out("No more sources left to try.")
	action, err := promptProviderSearchRecovery(config, state, "No more sources left to try.")
	if err != nil {
		return ProviderMappingQuit, false, err
	}
	return handleProviderMappingAction(config, state, action, defaultQuery)
}

// untrackedProviderSearchSelection shows the results of an untracked search,
// with the manual-search hint when that mode is on.
func untrackedProviderSearchSelection(config *Config, query string, results []SelectionOption) (SelectionOption, error) {
	if !ManualProviderSearchEnabled(config) {
		return promptProviderSearchSelection(config, results, nil)
	}
	hint := ManualProviderSearchHint(config, nil, query, config.SubOrDub)
	return promptProviderSearchSelectionWithHint(config, results, nil, hint)
}

// RemapAnimeProviderOnEpisodeFailure drops the anime's mapping and searches
// for it again, after its episode could not be found. It reports whether a
// new mapping was made.
func RemapAnimeProviderOnEpisodeFailure(config *Config, anime *Anime, anilistEntry *Entry) bool {
	query := animeSearchTitle(anime)
	if len(query) == 0 {
		return false
	}

	Out("Wrong match is common — search again and pick the right title.")
	anime.ProviderId, anime.ProviderName = "", ""
	anime.Ep.NextEpisode = NextEpisode{} // belonged to the old mapping

	result, err := ResolveAnimeProviderMapping(config, anime, query, anilistEntry)
	if err != nil {
		Log(fmt.Sprintf("Searching again after a failed episode: %v", err))
	}
	return err == nil && result == ProviderMappingOK
}

// translationModeOf is the configured audio, normalised; sub without a config.
func translationModeOf(config *Config) string {
	if config == nil {
		return "sub"
	}
	return normalizeTranslationType(config.SubOrDub)
}

// episodeLinkFailureRecoveryOptions lists what can be done about an episode
// that resolved nowhere, most useful first. The menu keeps this order.
func episodeLinkFailureRecoveryOptions(mode string, withAudio bool) []SelectionOption {
	options := []SelectionOption{{Key: "remap", Label: "Search for this anime again", Icon: icons.Search}}
	if withAudio {
		alternate := alternateTranslationType(mode)
		options = append(options, SelectionOption{Key: "audio", Label: fmt.Sprintf("Try other audio (%s)", alternate), Icon: icons.Audio})
	}
	return append(options, SelectionOption{Key: "episode", Label: "Change episode number (if this one is wrong)", Icon: icons.Number})
}

// episodeLinkFailureDiagnosis says which episode was looked for, where, and
// why that failed, in a few lines.
func episodeLinkFailureDiagnosis(config *Config, anime *Anime, lastErr error) string {
	title, episode := "this anime", 0
	if anime != nil {
		episode = anime.Ep.Number
		if named := strings.TrimSpace(GetAnimeName(*anime)); named != "" {
			title = named
		}
	}
	tried := strings.Join(providerNamesForShow(config, anime), ", ")
	if tried == "" {
		tried = "none"
	}

	diagnosis := fmt.Sprintf("Couldn't find Episode %d (%s) for %q\nTried: %s", episode, translationModeOf(config), title, tried)
	if lastErr == nil {
		return diagnosis
	}
	reason := strings.TrimSpace(lastErr.Error())
	if reason == "" {
		return diagnosis
	}
	// One short line; the full error is in the log.
	if len(reason) > 160 {
		reason = reason[:157] + "..."
	}
	return diagnosis + "\nReason: " + reason
}

// promptEpisodeLinkFailureRecovery shows the diagnosis and the recovery menu
// and returns remap, audio, episode, back or quit.
//
// With no viewer at the keyboard it takes the one answer that needs no menu --
// search for the show again -- and the caller bounds that to a single attempt
// before backing out. See wiki/Developer-Cast-Window-Prompts.md.
func promptEpisodeLinkFailureRecovery(config *Config, anime *Anime, lastErr error, includeAudio bool) string {
	diagnosis := episodeLinkFailureDiagnosis(config, anime, lastErr)
	if castWindowNonInteractive() {
		// Out rather than fmt.Println: the cast window is in raw mode, where a
		// bare \n does not return the cursor and the diagnosis walks diagonally
		// across the screen. Out already handles that.
		Out(diagnosis)
		return "remap"
	}

	options := episodeLinkFailureRecoveryOptions(translationModeOf(config), includeAudio)
	showName, showID := "", 0
	if anime != nil {
		showName, showID = GetAnimeName(*anime), anime.AnilistId
	}
	options = withAskServerOption(config, options, showName)

	// In Rofi the diagnosis goes in the message line rather than a
	// notification per line; in the terminal it is printed above the menu.
	picked, err := selectWithOptionalMessage(config, options, "Playback recovery", diagnosis)
	if err != nil {
		return "back" // a menu that cannot be shown counts as backing out
	}

	switch action := recoveryAction(picked); action {
	case "remap", "audio", "episode", "quit":
		return action
	case askServerKey:
		askServerWhereToWatch(config, showName, showID, diagnosis)
		return promptEpisodeLinkFailureRecovery(config, anime, lastErr, includeAudio)
	}
	return "back"
}

// episodeRecovery is what resolveEpisodeLinksWithRecovery remembers between
// attempts at the same episode.
type episodeRecovery struct {
	config *Config
	anime  *Anime
	entry  *Entry

	// offerAudio is whether the menu still offers the other audio. It goes
	// once that language is known to have nothing either.
	offerAudio     bool
	triedAutoAudio bool
	// triedRemap bounds a spawned cast window, which has nobody to back out
	// of the menu and answers it itself: one automatic remap, then stop.
	// Ignored when a viewer is present.
	triedRemap bool
}

// autoAudio switches to the other audio unasked, once, when the config
// allows it. A show carried only in the other language is the common case
// here, and the menu's answer for it is always the same.
func (r *episodeRecovery) autoAudio() (ProviderEpisodeResult, bool) {
	if !r.config.AutoAudioFallback || !r.offerAudio || r.triedAutoAudio {
		return ProviderEpisodeResult{}, false
	}
	r.triedAutoAudio = true
	alt, err := ResolveEpisodeURLAlternateModeAuto(*r.config, r.anime, r.anime.Ep.Number, nil)
	if err == nil && len(alt.Links) > 0 {
		Log(fmt.Sprintf("Automatically fell back to %s for ep %d", alt.Mode, r.anime.Ep.Number))
		return alt, true
	}
	Log(fmt.Sprintf("Automatic audio fallback did not help for ep %d: %v", r.anime.Ep.Number, err))
	// Nothing in either language: the menu must not re-offer audio.
	r.offerAudio = false
	return ProviderEpisodeResult{}, false
}

// remap searches for the show again. It reports whether to give up.
func (r *episodeRecovery) remap() (giveUp bool) {
	if !castWindowNonInteractive() {
		// Whether or not a new mapping came of it, the episode is tried
		// again and the menu returns if it still fails.
		RemapAnimeProviderOnEpisodeFailure(r.config, r.anime, r.entry)
		return false
	}
	// The one answer available without a viewer, spent once. A successful
	// remap retries the resolve; a second arrival here means it did not help,
	// and a third would loop forever.
	if r.triedRemap {
		Out(fmt.Sprintf("Could not find a stream for episode %d; stopping the cast.", r.anime.Ep.Number))
		return true
	}
	r.triedRemap = true
	Out("Searched for the show again; retrying the episode.")
	if !RemapAnimeProviderOnEpisodeFailure(r.config, r.anime, r.entry) {
		Out("Could not find the show again; stopping the cast.")
		return true
	}
	return false
}

// otherAudio tries the other audio because the user asked for it, so it is
// offered even if declined before.
func (r *episodeRecovery) otherAudio() (ProviderEpisodeResult, bool) {
	alt, err := ResolveEpisodeURLAlternateModeWithPrompt(*r.config, r.anime, r.anime.Ep.Number, nil)
	if err == nil && len(alt.Links) > 0 {
		return alt, true
	}
	if err == nil {
		err = noStreamsError{mode: alternateTranslationType(r.config.SubOrDub)}
	}
	if isNoStreamsError(err) {
		r.offerAudio = false
	}
	Out("Still no playable stream after trying other audio.")
	return ProviderEpisodeResult{}, false
}

// changeEpisode asks for a different episode number. Backing out returns to
// the menu this was chosen from; a number that is not one is asked for again
// by the prompt rather than costing the menu.
func (r *episodeRecovery) changeEpisode() {
	hint := "a number · esc to go back"
	if name, id := AnimeProviderID(r.anime); id != "" {
		listed, err := EpisodesList(QualifyProviderID(name, id), r.config.SubOrDub)
		if err == nil && len(listed) > 0 {
			hint = fmt.Sprintf("the source lists up to %v · esc to go back", listed[len(listed)-1])
		}
	}
	number, cancelled, err := promptEpisodeCancelable(r.config, "Episode",
		"Change the episode number, if this one is wrong",
		hint)
	switch {
	case err != nil:
		Log("Episode number prompt failed: " + err.Error())
	case !cancelled:
		r.anime.Ep.Number = number
		r.anime.Ep.NextEpisode = NextEpisode{}
	}
}

// resolveEpisodeLinksWithRecovery resolves the anime's current episode for
// playing, and only once that has failed in every way it tries by itself
// shows the recovery menu. ok is false when the user backs out.
func resolveEpisodeLinksWithRecovery(config *Config, anime *Anime, anilistEntry *Entry) (ProviderEpisodeResult, bool) {
	var none ProviderEpisodeResult
	if config == nil || anime == nil {
		return none, false
	}

	rec := &episodeRecovery{config: config, anime: anime, entry: anilistEntry, offerAudio: true}
	for {
		resolved, err := ResolveEpisodeURLForPlayback(*config, anime, anime.Ep.Number)
		if err == nil && len(resolved.Links) > 0 {
			return resolved, true
		}
		if err == nil {
			err = errors.New("no episode links found")
		}
		Log(fmt.Sprintf("Episode %d did not resolve for playback: %v", anime.Ep.Number, err))

		if alt, ok := rec.autoAudio(); ok {
			return alt, true
		}

		switch promptEpisodeLinkFailureRecovery(config, anime, err, rec.offerAudio) {
		case "remap":
			if rec.remap() {
				return none, false
			}
		case "audio":
			if alt, ok := rec.otherAudio(); ok {
				return alt, true
			}
		case "episode":
			rec.changeEpisode()
		default:
			return none, false
		}
	}
}

// ResolveUntrackedProviderSearch asks for the show to watch untracked. The
// picked option is returned alongside its id, because its title and any MAL id
// the provider attached are the only things that can identify the show later:
// an untracked show has no tracker entry to look them up from.
func ResolveUntrackedProviderSearch(config *Config, initialQuery string) (providerID, providerName string, picked SelectionOption, back bool, err error) {
	search := &providerMappingSearchState{query: initialQuery, allProviders: configuredProviderNames(config)}

	// afterMenu carries out a recovery menu's answer. It reports whether to
	// search again; when not, back says whether the user went back.
	afterMenu := func(action string, menuErr error) (searchAgain, back bool, err error) {
		switch {
		case menuErr != nil:
			return false, false, menuErr
		case action == "back":
			return false, true, nil
		case action == "quit":
			return false, false, nil
		}
		_, searchAgain, err = handleProviderMappingAction(config, search, action, initialQuery)
		return searchAgain, false, err
	}

	for {
		var searchAgain bool
		results, searchErr := searchAnimeForMapping(config, search, config.SubOrDub)
		switch {
		case searchErr != nil:
			Log(fmt.Sprintf("Untracked provider search failed: %v", searchErr))
			searchAgain, back, err = afterMenu(promptProviderSearchRecovery(config, search, fmt.Sprintf("Search failed for '%s': %v", search.query, searchErr)))
		case len(results) == 0:
			searchAgain, back, err = afterMenu(promptProviderSearchRecovery(config, search, ""))
		default:
			selected, selectErr := untrackedProviderSearchSelection(config, search.query, results)
			if selectErr == nil && selected.Key == "-2" {
				return "", "", SelectionOption{}, true, nil
			}
			if selectErr == nil && selected.Key != "-1" {
				if name, rawID, ok := ParseProviderQualifiedID(selected.Key); ok {
					return rawID, name, selected, false, nil
				}
				return selected.Key, providerNameFromSelection(config, search, selected), selected, false, nil
			}
			if selectErr != nil {
				Log(fmt.Sprintf("Failed to select anime: %v", selectErr))
			}
			searchAgain, back, err = afterMenu(promptProviderMatchRecovery(config, search))
		}
		if !searchAgain {
			return "", "", SelectionOption{}, back, err
		}
	}
}

// malIDFromProviderExtraData is the MAL id a provider attached to a search
// result, or 0.
func malIDFromProviderExtraData(data any) int {
	switch hit := data.(type) {
	case providers.SearchHit:
		return hit.MalID
	default:
		return 0
	}
}
