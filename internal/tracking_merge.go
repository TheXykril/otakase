package internal

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Reconciling the AniList and MyAnimeList lists when both are tracked.

// currentFuzzyDate is today in the trackers' date form.
func currentFuzzyDate() FuzzyDate {
	now := time.Now()
	return FuzzyDate{Year: now.Year(), Month: int(now.Month()), Day: now.Day()}
}

// statusWeight ranks statuses by how far along they are, for entryFreshnessScore.
var statusWeight = map[string]int{
	"REPEATING": 60,
	"CURRENT":   50,
	"COMPLETED": 40,
	"PAUSED":    30,
	"PLANNING":  20,
	"DROPPED":   10,
}

// entryFreshnessScore settles which of two entries for one show is newer when
// their update times tie or are missing: the one further along wins. Progress
// dominates, then status, then score, rewatches and having a cover.
func entryFreshnessScore(entry Entry) int {
	total := 100*entry.Progress + statusWeight[entry.Status]
	total += int(10 * max(entry.Score, 0))
	total += max(entry.Repeat, 0)
	if entry.CoverImage != "" {
		total += 1
	}
	return total
}

// fillZero sets *field to fallback when it holds the zero value.
func fillZero[T comparable](field *T, fallback T) {
	var zero T
	if *field == zero {
		*field = fallback
	}
}

// mergeEntryMetadata fills what preferred is missing from fallback. The list
// state itself (status, progress, score, dates) always stays preferred's.
func mergeEntryMetadata(preferred, fallback Entry) Entry {
	own, other := &preferred.Media, fallback.Media

	fillZero(&preferred.ListID, fallback.ListID)
	fillZero(&own.ID, other.ID)
	fillZero(&own.MalID, other.MalID)
	fillZero(&own.Duration, other.Duration)
	fillZero(&own.Episodes, other.Episodes)
	fillZero(&own.Title.English, other.Title.English)
	fillZero(&own.Title.Romaji, other.Title.Romaji)
	fillZero(&own.Title.Japanese, other.Title.Japanese)
	fillZero(&own.Status, other.Status)
	// Only AniList knows the broadcast schedule. Right after a progress push
	// to MyAnimeList, its entry is the newer one and wins the merge; dropping
	// the schedule then would make the show look unscheduled everywhere -- no
	// airing countdown, and "caught up" indistinguishable from "episode not
	// found".
	fillZero(&own.NextAiringEpisode, other.NextAiringEpisode)
	fillZero(&own.Format, other.Format)
	// Adult is about the show, not the entry: one tracker saying so is enough.
	own.IsAdult = own.IsAdult || other.IsAdult
	fillZero(&preferred.CoverImage, fallback.CoverImage)
	if preferred.Repeat == 0 {
		preferred.Repeat = max(fallback.Repeat, 0)
	}
	return preferred
}

// incomingIsNewer decides which of two entries for one show wins a merge. An
// entry with an update time beats one without; otherwise the later time wins,
// and a tie goes to the entry further along.
func incomingIsNewer(existing, incoming Entry) bool {
	before, after := existing.UpdatedAt, incoming.UpdatedAt
	if before.IsZero() || after.IsZero() {
		if before.IsZero() != after.IsZero() {
			return before.IsZero()
		}
	} else if !before.Equal(after) {
		return after.After(before)
	}
	return entryFreshnessScore(incoming) > entryFreshnessScore(existing)
}

// mergeAnimeEntries keeps the newer entry's list state, topped up with the
// other's metadata.
func mergeAnimeEntries(existing, incoming Entry) Entry {
	newer, older := existing, incoming
	if incomingIsNewer(existing, incoming) {
		newer, older = incoming, existing
	}
	return mergeEntryMetadata(newer, older)
}

// mergeAnimeLists merges two lists show by show, the newer entry winning.
func mergeAnimeLists(primary, secondary AnimeList) AnimeList {
	combined := mapEntriesByMediaID(primary)
	for id, incoming := range mapEntriesByMediaID(secondary) {
		if current, both := combined[id]; both {
			incoming = mergeAnimeEntries(current, incoming)
		}
		combined[id] = incoming
	}
	return animeListFromEntryMap(combined)
}

// mapEntriesByMediaID indexes a list by AniList ID. Entries without one have
// nothing to be matched on and are left out.
func mapEntriesByMediaID(list AnimeList) map[int]Entry {
	byID := map[int]Entry{}
	for _, entry := range getEntriesByCategory(list, "ALL") {
		if id := entry.Media.ID; id != 0 {
			byID[id] = entry
		}
	}
	return byID
}

// sortedMediaIDs is the union of the indexes' keys in ascending order, so a
// plan built from maps comes out the same every time.
func sortedMediaIDs(indexes ...map[int]Entry) []int {
	var ids []int
	for _, index := range indexes {
		for id := range index {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func animeListFromEntryMap(byID map[int]Entry) AnimeList {
	var out AnimeList
	for _, id := range sortedMediaIDs(byID) {
		out.add(byID[id])
	}
	return out
}

// entriesEquivalentForSync reports whether writing desired over current would
// change anything a tracker stores. Metadata one side lacks is not a
// difference, and that includes a rewatch count MyAnimeList has not reported.
func entriesEquivalentForSync(current, desired Entry) bool {
	have := mergeEntryMetadata(current, desired)
	want := mergeEntryMetadata(desired, have)

	type syncedState struct {
		mediaID, malID, progress, repeat int
		score                            float64
		status                           string
		started, completed               FuzzyDate
	}
	stateOf := func(e Entry) syncedState {
		return syncedState{e.Media.ID, e.Media.MalID, e.Progress, e.Repeat, e.Score, e.Status, e.StartedAt, e.CompletedAt}
	}
	return stateOf(have) == stateOf(want)
}

// dualRemoteSyncPlan is the reconciled list and the writes that bring each
// tracker up to it.
type dualRemoteSyncPlan struct {
	Merged             AnimeList
	AniListUpdates     []Entry
	MyAnimeListUpdates []Entry
}

// buildDualRemoteSyncPlan reconciles the two lists. A show on one tracker
// only is copied to the other, except that an AniList-private entry never
// goes to MyAnimeList, which has no way to hide it.
func buildDualRemoteSyncPlan(aniList, myAnimeList AnimeList) dualRemoteSyncPlan {
	fromAniList := mapEntriesByMediaID(aniList)
	fromMAL := mapEntriesByMediaID(myAnimeList)
	noteAniListPrivateEntries(aniList)

	var plan dualRemoteSyncPlan
	toAniList := func(e Entry) { plan.AniListUpdates = append(plan.AniListUpdates, e) }
	toMAL := func(e Entry) { plan.MyAnimeListUpdates = append(plan.MyAnimeListUpdates, e) }

	reconciled := make(map[int]Entry, len(fromAniList)+len(fromMAL))
	for _, id := range sortedMediaIDs(fromAniList, fromMAL) {
		ani, onAniList := fromAniList[id]
		mal, onMAL := fromMAL[id]

		if !onAniList {
			reconciled[id] = mal
			toAniList(mal)
			continue
		}
		if !onMAL {
			reconciled[id] = ani
			if !ani.Private {
				toMAL(ani)
			}
			continue
		}

		winner := mergeAnimeEntries(ani, mal)
		// Privacy is AniList-only: take it from there whichever side won,
		// so it is neither lost nor carried over stale.
		winner.Private = ani.Private
		reconciled[id] = winner
		if !entriesEquivalentForSync(ani, winner) {
			toAniList(winner)
		}
		if !ani.Private && !entriesEquivalentForSync(mal, winner) {
			toMAL(winner)
		}
	}

	plan.Merged = animeListFromEntryMap(reconciled)
	return plan
}

// saveMergedListCaches stores the reconciled list as both trackers' cache, so
// the next launch starts from it. A failure only costs that head start.
func saveMergedListCaches(config *Config, aniListUser, myAnimeListUser *User, list AnimeList, logPrefix string) {
	caches := []struct {
		tracker string
		save    func() error
	}{
		{"AniList", func() error { return saveAniListAnimeListCache(config.StoragePath, aniListUser.Id, list) }},
		{"MyAnimeList", func() error { return saveMyAnimeListCache(config.StoragePath, myAnimeListUser.Id, list) }},
	}
	for _, cache := range caches {
		if saveErr := cache.save(); saveErr != nil {
			Log(fmt.Sprintf("%s: failed to save %s cache: %v", logPrefix, cache.tracker, saveErr))
		}
	}
}

// syncDualRemoteTrackers merges the two users' lists, gives both the result
// and queues the writes that bring each tracker in line.
func syncDualRemoteTrackers(config *Config, aniListToken string, aniListUser, myAnimeListUser *User) (AnimeList, error) {
	switch {
	case config == nil:
		return AnimeList{}, errors.New("missing config")
	case aniListUser == nil || myAnimeListUser == nil:
		return AnimeList{}, errors.New("missing tracker users")
	}

	// Merging is local and instant; the writes are neither, and nothing on
	// screen waits for them since the merged list is already what is shown.
	// So they go to the background and the launch carries on.
	plan := buildDualRemoteSyncPlan(aniListUser.AnimeList, myAnimeListUser.AnimeList)
	Log(fmt.Sprintf("Dual sync: %d AniList and %d MyAnimeList update(s) queued",
		len(plan.AniListUpdates), len(plan.MyAnimeListUpdates)))
	startDualSyncWrites(config, dualSyncWrites{
		aniListToken: aniListToken,
		aniList:      plan.AniListUpdates,
		myAnimeList:  plan.MyAnimeListUpdates,
	})

	for _, side := range []*User{aniListUser, myAnimeListUser} {
		side.AnimeList = plan.Merged
	}
	saveMergedListCaches(config, aniListUser, myAnimeListUser, plan.Merged, "Dual sync")
	return plan.Merged, nil
}

// bothTrackerTokens reads the saved AniList token and a current MyAnimeList
// one. stage, when set, is called before the MyAnimeList sign-in check.
func bothTrackerTokens(config *Config, stage func()) (aniListToken, myAnimeListToken string, err error) {
	if aniListToken, err = storedAniListToken(config); err != nil {
		return "", "", err
	}
	if stage != nil {
		stage()
	}
	if myAnimeListToken, err = GetMyAnimeListAccessToken(config); err != nil {
		return "", "", err
	}
	return aniListToken, myAnimeListToken, nil
}

// InitializeCombinedRemoteAnimeList starts a dual-tracking session: both
// lists from cache, reconciled, with the merge redone once the background
// refreshes land.
func InitializeCombinedRemoteAnimeList(config *Config, user *User) error {
	aniListToken, myAnimeListToken, tokenErr := bothTrackerTokens(config, func() {
		StartupStage("Signing in to MyAnimeList")
	})
	if tokenErr != nil {
		return tokenErr
	}

	StartupStage("Reading your AniList list")
	aniListUser := &User{Token: aniListToken}
	if loadErr := InitializeAniListUserAnimeList(config, aniListUser); loadErr != nil {
		return loadErr
	}
	StartupStage("Reading your MyAnimeList list")
	myAnimeListUser := &User{Token: myAnimeListToken}
	if loadErr := InitializeMyAnimeListUserAnimeList(config, myAnimeListUser); loadErr != nil {
		return loadErr
	}

	StartupStage("Checking MyAnimeList import")
	if importErr := maybeImportAniListToMyAnimeList(config, myAnimeListUser); importErr != nil {
		return importErr
	}

	StartupStage("Reconciling both trackers")
	merged, syncErr := syncDualRemoteTrackers(config, aniListToken, aniListUser, myAnimeListUser)
	if syncErr != nil {
		return syncErr
	}

	StartupStage("Building the menu")
	user.Token = aniListToken
	user.AnimeList = merged
	user.ListSync = NewAnimeListSync(merged)

	// That merge came from two caches, since both trackers answer from cache
	// and refresh behind us. Leaving it there would run the whole session on
	// stale data -- an episode out an hour ago still unreleased, yesterday's
	// progress -- while the refreshes arrive in the two sub-lists that nothing
	// reads any more.
	//
	// So merge again when they land, and only then call the launch refreshed.
	// Playback waits on that (briefly, with its own timeout), just as it
	// always has with a single tracker.
	go refreshCombinedRemoteAnimeList(config, user, aniListUser, myAnimeListUser)
	return nil
}

// combinedRefreshDeadline bounds the wait for both trackers. A refresh that
// never answers must not leave the list permanently marked "refreshing", since
// callers block on that.
const combinedRefreshDeadline = 20 * time.Second

// combinedRefreshDeadlineForTest lets a test exercise the give-up path without
// spending the real deadline waiting for it.
var combinedRefreshDeadlineForTest = combinedRefreshDeadline

// awaitTrackerRefreshes waits for each source's refresh to finish, and
// reports false if the deadline passes first.
func awaitTrackerRefreshes(sources []*User, limit time.Duration) bool {
	timeout := time.After(limit)
	for _, source := range sources {
		if source == nil || source.ListSync == nil {
			continue
		}
		select {
		case <-source.ListSync.RefreshDone():
		case <-timeout:
			return false
		}
	}
	return true
}

// queueRefreshSyncWrites writes what the refreshed lists still disagree on,
// after the launch's own writes finish. It never holds up the refresh, which
// playback waits on.
var queueRefreshSyncWrites = func(config *Config, writes dualSyncWrites) {
	if writes.count() == 0 {
		return
	}
	Log(fmt.Sprintf("Combined refresh: %d AniList and %d MyAnimeList update(s) queued",
		len(writes.aniList), len(writes.myAnimeList)))
	go func() {
		WaitForDualSyncWrites()
		startDualSyncWrites(config, writes)
	}()
}

func refreshCombinedRemoteAnimeList(config *Config, user, aniListUser, myAnimeListUser *User) {
	defer user.ListSync.MarkRefreshDone()

	if !awaitTrackerRefreshes([]*User{aniListUser, myAnimeListUser}, combinedRefreshDeadlineForTest) {
		Log("Combined refresh: a tracker did not answer in time; keeping the cached merge")
		return
	}

	again := buildDualRemoteSyncPlan(aniListUser.ListSync.Current(), myAnimeListUser.ListSync.Current())
	// The launch only wrote what the two caches disagreed on, so a change
	// made on a tracker's site since the last run reached the merged list
	// but not the other tracker until the next launch. Moving a show to
	// Watching on AniList left it in Planning on MyAnimeList, and anything
	// reading MyAnimeList (the Discord watch room) saw the old status.
	queueRefreshSyncWrites(config, dualSyncWrites{
		aniListToken: user.Token,
		aniList:      again.AniListUpdates,
		myAnimeList:  again.MyAnimeListUpdates,
	})
	if animeListEqual(user.ListSync.Current(), again.Merged) {
		return
	}

	// Published through ListSync only: writing user.AnimeList here would race
	// with the launch reading it, and readers go through ListSync.Current()
	// anyway. The notify makes menus already on screen redraw.
	user.ListSync.Replace(again.Merged, true)
	Log("Combined refresh: merged list updated from both trackers")
	saveMergedListCaches(config, aniListUser, myAnimeListUser, again.Merged, "Combined refresh")
}

// refreshOrInitialize refreshes a tracker's list, and loads it from scratch
// when the refresh fails.
func refreshOrInitialize(config *Config, user *User, refresh, initialize func(*Config, *User) error) error {
	if refreshErr := refresh(config, user); refreshErr != nil {
		return initialize(config, user)
	}
	return nil
}

// RefreshCombinedRemoteAnimeList re-reads both trackers and publishes the
// merge, queuing whatever writes it calls for.
func RefreshCombinedRemoteAnimeList(config *Config, user *User) error {
	aniListToken, myAnimeListToken, tokenErr := bothTrackerTokens(config, nil)
	if tokenErr != nil {
		return tokenErr
	}

	aniListUser := &User{Token: aniListToken}
	myAnimeListUser := &User{Token: myAnimeListToken}
	loads := []struct {
		who                 *User
		refresh, initialize func(*Config, *User) error
	}{
		{aniListUser, RefreshAniListUserAnimeList, InitializeAniListUserAnimeList},
		{myAnimeListUser, RefreshMyAnimeListUserAnimeList, InitializeMyAnimeListUserAnimeList},
	}
	for _, load := range loads {
		if loadErr := refreshOrInitialize(config, load.who, load.refresh, load.initialize); loadErr != nil {
			return loadErr
		}
	}

	merged, syncErr := syncDualRemoteTrackers(config, aniListToken, aniListUser, myAnimeListUser)
	if syncErr != nil {
		return syncErr
	}
	user.Token = aniListToken
	publishAnimeList(user, merged)
	return nil
}
