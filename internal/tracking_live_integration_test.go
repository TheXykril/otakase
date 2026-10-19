package internal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Live tests against the real AniList and MyAnimeList accounts in the user's
// config. Both lists are put back as they were afterwards. Opt in with
// OTAKASE_RUN_TRACKER_LIVE_INTEGRATION=1; -short always skips them.

const liveTrackerEnv = "OTAKASE_RUN_TRACKER_LIVE_INTEGRATION"

// liveTitles are looked up on AniList once per run and used as test data.
var liveTitles = []string{
	"Cowboy Bebop",
	"Odd Taxi",
	"Barakamon",
	"Death Parade",
	"A Place Further than the Universe",
}

// liveShow is one of liveTitles with the ids both trackers know it by.
type liveShow struct {
	Title            string
	AniListID, MalID int
}

func (s liveShow) entry(status string, progress int, score float64) Entry {
	media := Media{ID: s.AniListID, MalID: s.MalID, Title: AnimeTitle{English: s.Title, Romaji: s.Title}}
	return Entry{Media: media, Status: status, Progress: progress, Score: score}
}

// day is a full date for test entries.
func day(year, month, date int) FuzzyDate {
	return FuzzyDate{Year: year, Month: month, Day: date}
}

// withDates returns entry with its start and finish dates set.
func withDates(entry Entry, started, completed FuzzyDate) Entry {
	dated := entry
	dated.StartedAt = started
	dated.CompletedAt = completed
	return dated
}

// liveTrackers is the suite's handle on both accounts.
type liveTrackers struct {
	config *Config
	token  string
	shows  []liveShow
}

func TestTrackerLiveIntegration(t *testing.T) {
	live := startLiveTrackers(t)
	s := live.shows

	t.Run("dual-sync keeps latest remote update", func(t *testing.T) {
		live.reset(t)
		stale := withDates(s[0].entry("CURRENT", 2, 3), day(2026, 1, 2), FuzzyDate{})
		fresh := withDates(s[0].entry("PAUSED", 4, 8), day(2026, 3, 4), FuzzyDate{})
		live.seedAniList(t, stale)
		waitForRemoteTimestampTick()
		live.seedMyAnimeList(t, fresh)

		live.refreshBoth(t)
		live.expectOnBoth(t, fresh)
	})

	t.Run("dual-sync copies AniList-only entry to MyAnimeList", func(t *testing.T) {
		live.reset(t)
		only := withDates(s[1].entry("CURRENT", 5, 6), day(2025, 12, 24), FuzzyDate{})
		live.seedAniList(t, only)

		live.refreshBoth(t)
		expectEntry(t, live.myAnimeList(t), only)
	})

	t.Run("dual-sync copies MyAnimeList-only entry to AniList", func(t *testing.T) {
		live.reset(t)
		only := s[2].entry("PLANNING", 0, 7)
		live.seedMyAnimeList(t, only)

		live.refreshBoth(t)
		expectEntry(t, live.aniList(t), only)
	})

	t.Run("replace MyAnimeList with AniList", func(t *testing.T) {
		live.reset(t)
		kept := withDates(s[0].entry("CURRENT", 3, 5), day(2026, 2, 1), FuzzyDate{})
		dropped := s[3].entry("PAUSED", 1, 9)
		live.seedAniList(t, kept)
		live.seedMyAnimeList(t, dropped)

		if err := ReplaceMyAnimeListWithAniList(live.config); err != nil {
			t.Fatalf("replacing MyAnimeList: %v", err)
		}
		after := live.myAnimeList(t)
		expectEntry(t, after, kept)
		expectMissing(t, after, dropped.Media.ID)
	})

	t.Run("replace AniList with MyAnimeList", func(t *testing.T) {
		live.reset(t)
		kept := withDates(s[1].entry("PAUSED", 2, 7), day(2026, 2, 5), FuzzyDate{})
		dropped := s[4].entry("CURRENT", 6, 4)
		live.seedMyAnimeList(t, kept)
		live.seedAniList(t, dropped)

		if err := ReplaceAniListWithMyAnimeList(live.config); err != nil {
			t.Fatalf("replacing AniList: %v", err)
		}
		after := live.aniList(t)
		expectEntry(t, after, kept)
		expectMissing(t, after, dropped.Media.ID)
	})

	t.Run("merge remote lists keeps union and newest conflict", func(t *testing.T) {
		live.reset(t)
		aniOnly := s[2].entry("CURRENT", 2, 6)
		malOnly := s[3].entry("PLANNING", 0, 0)
		staleSide := withDates(s[4].entry("CURRENT", 1, 5), day(2026, 1, 10), FuzzyDate{})
		freshSide := withDates(s[4].entry("PAUSED", 4, 9), day(2026, 4, 11), FuzzyDate{})
		live.seedAniList(t, aniOnly)
		live.seedAniList(t, staleSide)
		live.seedMyAnimeList(t, malOnly)
		waitForRemoteTimestampTick()
		live.seedMyAnimeList(t, freshSide)

		if err := MergeRemoteLists(live.config); err != nil {
			t.Fatalf("merging: %v", err)
		}
		live.expectOnBoth(t, aniOnly, malOnly, freshSide)
	})

	t.Run("tracker mode transitions local anilist myanimelist both", func(t *testing.T) {
		live.reset(t)
		onAniList := s[0].entry("CURRENT", 3, 6)
		onMAL := s[1].entry("PAUSED", 1, 8)
		live.seedAniList(t, onAniList)
		live.seedMyAnimeList(t, onMAL)

		// New storage signed in to both trackers, one show in local history.
		storage := t.TempDir()
		source := os.ExpandEnv(live.config.StoragePath)
		for _, tokenFile := range []string{"anilist_token.json", "myanimelist_token.json"} {
			copyFileForTest(t, filepath.Join(source, tokenFile), filepath.Join(storage, tokenFile))
		}
		local := s[2]
		if err := LocalUpdateAnime(HistoryFile(storage), local.AniListID, "local-provider-id", 7, 0, 24, local.Title, "anikoto"); err != nil {
			t.Fatalf("seeding history: %v", err)
		}
		cfg := *live.config
		cfg.StoragePath = storage
		cfg.MyAnimeListImported = true
		SetGlobalConfig(&cfg)

		modeList := func(remote string) AnimeList {
			t.Helper()
			cfg.TrackingRemote = remote
			session := &User{}
			SetGlobalUser(session)
			if remote != TrackingRemoteNone {
				if err := EnsureConfiguredTrackersReady(&cfg, session); err != nil {
					t.Fatalf("signing in for %s: %v", remote, err)
				}
			}
			if err := RefreshUserAnimeList(&cfg, session); err != nil {
				t.Fatalf("loading %s: %v", remote, err)
			}
			return session.AnimeList
		}

		expectEntry(t, modeList(TrackingRemoteNone), Entry{Media: Media{ID: local.AniListID}, Status: "CURRENT", Progress: 6})

		aniOnly := modeList(TrackingRemoteAniList)
		expectEntry(t, aniOnly, onAniList)
		expectMissing(t, aniOnly, local.AniListID)

		expectEntry(t, modeList(TrackingRemoteMyAnimeList), onMAL)

		dual := modeList(TrackingRemoteBoth)
		expectEntry(t, dual, onAniList)
		expectEntry(t, dual, onMAL)
	})
}

func TestTrackerLiveRuntimeIntegration(t *testing.T) {
	live := startLiveTrackers(t)
	s := live.shows

	t.Run("runtime helpers update both trackers in dual mode", func(t *testing.T) {
		live.reset(t)
		live.track(TrackingRemoteBoth)
		id := s[0].AniListID

		steps := []struct {
			what string
			run  func() error
		}{
			{"add to watching", func() error { return AddAnimeToWatchingList(id, live.token) }},
			{"update progress", func() error { return UpdateAnimeProgress(live.token, id, 3) }},
			{"update status", func() error { return UpdateAnimeStatus(live.token, id, "PAUSED") }},
		}
		for _, step := range steps {
			if err := step.run(); err != nil {
				t.Fatalf("%s: %v", step.what, err)
			}
		}
		withStdin(t, "9\n", func() {
			if err := RateAnime(live.token, id); err != nil {
				t.Fatalf("rating: %v", err)
			}
		})

		live.expectOnBoth(t, s[0].entry("PAUSED", 3, 9))
	})

	t.Run("runtime rewatch cycle updates both trackers", func(t *testing.T) {
		live.reset(t)
		cfg := live.track(TrackingRemoteBoth)
		show := s[1]

		finished := withDates(show.entry("COMPLETED", 12, 8), day(2025, 1, 1), day(2025, 2, 2))
		live.seedAniList(t, finished)
		live.seedMyAnimeList(t, finished)
		seeded, found := entryFor(live.aniList(t), show.AniListID)
		if !found {
			t.Fatal("seeded entry not on AniList")
		}
		lastEpisode := seeded.Progress

		rewatched := Anime{
			AnilistId:   show.AniListID,
			MalId:       show.MalID,
			StartedAt:   finished.StartedAt,
			CompletedAt: finished.CompletedAt,
			Ep:          Episode{Number: lastEpisode},
		}
		if err := StartAnimeRewatch(live.token, rewatched); err != nil {
			t.Fatalf("starting the rewatch: %v", err)
		}
		live.expectOnBoth(t, show.entry("REPEATING", 0, 8))

		if err := CompleteAnimeRewatch(live.token, rewatched); err != nil {
			t.Fatalf("finishing the rewatch: %v", err)
		}
		counted := show.entry("COMPLETED", lastEpisode, 8)
		counted.Repeat = 1
		expectEntry(t, live.aniList(t), counted)
		expectEntry(t, live.myAnimeList(t), show.entry("COMPLETED", lastEpisode, 8))

		session := &User{}
		SetGlobalConfig(cfg)
		SetGlobalUser(session)
		if err := RefreshUserAnimeList(cfg, session); err != nil {
			t.Fatalf("refreshing after the rewatch: %v", err)
		}
		expectEntry(t, session.AnimeList, counted)
	})

	t.Run("runtime AniList-only updates sync later in dual mode", func(t *testing.T) {
		live.reset(t)
		live.track(TrackingRemoteAniList)
		id := s[2].AniListID

		if err := AddAnimeToWatchingList(id, live.token); err != nil {
			t.Fatalf("adding on AniList: %v", err)
		}
		if err := UpdateAnimeProgress(live.token, id, 4); err != nil {
			t.Fatalf("progress on AniList: %v", err)
		}
		expectMissing(t, live.myAnimeList(t), id)

		live.refreshBoth(t)
		expectEntry(t, live.myAnimeList(t), s[2].entry("CURRENT", 4, 0))
	})

	t.Run("runtime MyAnimeList-only updates sync later in dual mode", func(t *testing.T) {
		live.reset(t)
		live.track(TrackingRemoteMyAnimeList)
		id := s[3].AniListID

		if err := AddAnimeToWatchingList(id, live.token); err != nil {
			t.Fatalf("adding on MyAnimeList: %v", err)
		}
		if err := UpdateAnimeStatus(live.token, id, "PAUSED"); err != nil {
			t.Fatalf("pausing on MyAnimeList: %v", err)
		}
		expectMissing(t, live.aniList(t), id)

		live.refreshBoth(t)
		expectEntry(t, live.aniList(t), s[3].entry("PAUSED", 0, 0))
	})

	t.Run("next episode runtime path updates local history and tracker progress", func(t *testing.T) {
		live.reset(t)
		cfg := live.track(TrackingRemoteBoth)
		show := s[4]
		if err := AddAnimeToWatchingList(show.AniListID, live.token); err != nil {
			t.Fatalf("seeding: %v", err)
		}

		storage := t.TempDir()
		playing := &Anime{
			AnilistId:     show.AniListID,
			MalId:         show.MalID,
			Title:         AnimeTitle{English: show.Title, Romaji: show.Title},
			TotalEpisodes: 12,
			ProviderId:    "runtime-provider-id",
			Ep:            Episode{Number: 1},
		}
		StartNextEpisode(playing, cfg, HistoryFile(storage), live.token)
		waitForAsyncTrackerUpdate()

		if playing.Ep.Number != 2 {
			t.Fatalf("now on episode %d, want 2", playing.Ep.Number)
		}
		expectEntry(t, BuildLocalAnimeList(storage), Entry{Media: Media{ID: show.AniListID}, Status: "CURRENT", Progress: 1})
		live.expectOnBoth(t, show.entry("CURRENT", 1, 0))
	})
}

func TestLiveTrackerContextForcesTerminalPrompts(t *testing.T) {
	if os.Getenv(liveTrackerEnv) == "1" {
		t.Skip("the live suite itself covers this")
	}

	cfg := Config{RofiSelection: true, ImagePreview: true, AlternateScreen: true}
	forceTerminalTestMode(&cfg)

	if cfg.RofiSelection || cfg.ImagePreview || cfg.AlternateScreen {
		t.Errorf("terminal prompt mode not forced: %+v", cfg)
	}
	if !cfg.MyAnimeListImportDismissed {
		t.Error("the import offer should be dismissed")
	}
}

// liveConfigPath is the config the live suite signs in with.
func liveConfigPath(t *testing.T) string {
	t.Helper()
	if explicit := os.Getenv("OTAKASE_CONFIG_PATH"); explicit != "" {
		return explicit
	}
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		t.Fatalf("finding the home dir: %v", homeErr)
	}
	return filepath.Join(home, ".config", AppName, ConfigFileName())
}

// startLiveTrackers signs in to both accounts from the user's config and
// arranges for both lists to be restored when the test ends.
func startLiveTrackers(t *testing.T) *liveTrackers {
	t.Helper()
	switch {
	case testing.Short():
		t.Skip("live tracker tests do not run with -short")
	case os.Getenv(liveTrackerEnv) != "1":
		t.Skip("set " + liveTrackerEnv + "=1 to run live tracker integration tests")
	}
	t.Setenv("OTAKASE_TEST_AUTO_CONFIRM_REMOTE_SYNC", "1")

	cfg, loadErr := LoadConfig(liveConfigPath(t))
	if loadErr != nil {
		t.Fatalf("loading config: %v", loadErr)
	}
	cfg.TrackingConfigured, cfg.TrackingLocal = true, true
	forceTerminalTestMode(&cfg)

	savedConfig, savedUser := GetGlobalConfig(), GetGlobalUser()
	session := &User{}
	SetGlobalConfig(&cfg)
	SetGlobalUser(session)
	if err := EnsureConfiguredTrackersReady(&cfg, session); err != nil {
		t.Fatalf("signing in: %v", err)
	}
	token, tokenErr := storedAniListToken(&cfg)
	if tokenErr != nil {
		t.Fatalf("reading the AniList token: %v", tokenErr)
	}

	live := &liveTrackers{config: &cfg, token: token, shows: resolveLiveShows(t)}
	beforeAniList, beforeMAL := live.aniList(t), live.myAnimeList(t)
	t.Cleanup(func() {
		live.reset(t)
		if err := upsertAnimeListToAniList(token, beforeAniList); err != nil {
			t.Fatalf("restoring AniList: %v", err)
		}
		if err := upsertAnimeListToMyAnimeList(&cfg, beforeMAL); err != nil {
			t.Fatalf("restoring MyAnimeList: %v", err)
		}
		SetGlobalConfig(savedConfig)
		SetGlobalUser(savedUser)
	})
	return live
}

// forceTerminalTestMode keeps every prompt in the terminal, where the tests
// can answer it, and skips the import offer.
func forceTerminalTestMode(cfg *Config) {
	cfg.RofiSelection, cfg.ImagePreview, cfg.AlternateScreen = false, false, false
	cfg.MyAnimeListImportDismissed = true
}

var liveShowLookup struct {
	once  sync.Once
	shows []liveShow
	err   error
}

// resolveLiveShows looks the test shows up once per run.
func resolveLiveShows(t *testing.T) []liveShow {
	t.Helper()
	liveShowLookup.once.Do(func() {
		for _, title := range liveTitles {
			show, err := lookUpLiveShow(title)
			if err != nil {
				liveShowLookup.err = err
				return
			}
			liveShowLookup.shows = append(liveShowLookup.shows, show)
		}
	})
	if liveShowLookup.err != nil {
		t.Fatalf("looking up the test shows: %v", liveShowLookup.err)
	}
	return liveShowLookup.shows
}

// lookUpLiveShow takes AniList's top search hit for title and its MAL id.
func lookUpLiveShow(title string) (liveShow, error) {
	hits, searchErr := SearchAnimeAnilist(title, "")
	switch {
	case searchErr != nil:
		return liveShow{}, searchErr
	case len(hits) == 0:
		return liveShow{}, fmt.Errorf("no AniList result for %q", title)
	}
	aniListID, convErr := strconv.Atoi(hits[0].Key)
	if convErr != nil {
		return liveShow{}, convErr
	}
	malID, mapErr := GetAnimeMalID(aniListID)
	switch {
	case mapErr != nil:
		return liveShow{}, mapErr
	case malID == 0:
		return liveShow{}, fmt.Errorf("AniList title %q has no MAL mapping", title)
	}
	return liveShow{Title: title, AniListID: aniListID, MalID: malID}, nil
}

// reset empties both lists.
func (live *liveTrackers) reset(t *testing.T) {
	t.Helper()
	for name, wipe := range map[string]func(*Config) error{
		"AniList":     wipeAniListRemote,
		"MyAnimeList": wipeMyAnimeListRemote,
	} {
		if err := wipe(live.config); err != nil {
			t.Fatalf("wiping %s: %v", name, err)
		}
	}
}

func (live *liveTrackers) seedAniList(t *testing.T, entry Entry) {
	t.Helper()
	if err := saveAniListTrackedEntry(live.token, entry); err != nil {
		t.Fatalf("putting %d on AniList: %v", entry.Media.ID, err)
	}
}

func (live *liveTrackers) seedMyAnimeList(t *testing.T, entry Entry) {
	t.Helper()
	if err := saveMyAnimeListTrackedEntry(live.config, entry); err != nil {
		t.Fatalf("putting %d on MyAnimeList: %v", entry.Media.ID, err)
	}
}

// track makes remote the global tracking mode, as the runtime helpers read it.
func (live *liveTrackers) track(remote string) *Config {
	cfg := *live.config
	cfg.TrackingRemote = remote
	SetGlobalConfig(&cfg)
	return &cfg
}

// refreshBoth runs a dual-tracking refresh, which reconciles the two lists.
func (live *liveTrackers) refreshBoth(t *testing.T) {
	t.Helper()
	cfg := live.track(TrackingRemoteBoth)
	session := &User{}
	SetGlobalUser(session)
	if err := RefreshUserAnimeList(cfg, session); err != nil {
		t.Fatalf("dual refresh: %v", err)
	}
}

// aniList reads the AniList list as it is now.
func (live *liveTrackers) aniList(t *testing.T) AnimeList {
	t.Helper()
	list, err := fetchAniListAnimeListFromToken(live.token)
	if err != nil {
		t.Fatalf("reading AniList: %v", err)
	}
	return list
}

// myAnimeList reads the MyAnimeList list as it is now.
func (live *liveTrackers) myAnimeList(t *testing.T) AnimeList {
	t.Helper()
	list, err := FetchLatestMyAnimeList(live.config, &User{})
	if err != nil {
		t.Fatalf("reading MyAnimeList: %v", err)
	}
	return list
}

// expectOnBoth checks every want against both trackers' current lists.
func (live *liveTrackers) expectOnBoth(t *testing.T, wants ...Entry) {
	t.Helper()
	for _, list := range []AnimeList{live.aniList(t), live.myAnimeList(t)} {
		for _, want := range wants {
			expectEntry(t, list, want)
		}
	}
}

func entryFor(list AnimeList, mediaID int) (Entry, bool) {
	found, ok := mapEntriesByMediaID(list)[mediaID]
	return found, ok
}

// expectEntry checks the list state of want's show. A zero repeat count or
// date in want is not checked.
func expectEntry(t *testing.T, list AnimeList, want Entry) {
	t.Helper()

	id := want.Media.ID
	got, ok := entryFor(list, id)
	if !ok {
		t.Fatalf("media ID %d not on the list", id)
	}
	if got.Status != want.Status || got.Progress != want.Progress || got.Score != want.Score {
		t.Fatalf("media ID %d is %s/%d/%.1f, want %s/%d/%.1f",
			id, got.Status, got.Progress, got.Score, want.Status, want.Progress, want.Score)
	}
	if want.Repeat != 0 && got.Repeat != want.Repeat {
		t.Fatalf("media ID %d rewatched %d times, want %d", id, got.Repeat, want.Repeat)
	}
	var unset FuzzyDate
	if want.StartedAt != unset && got.StartedAt != want.StartedAt {
		t.Fatalf("media ID %d started %+v, want %+v", id, got.StartedAt, want.StartedAt)
	}
	if want.CompletedAt != unset && got.CompletedAt != want.CompletedAt {
		t.Fatalf("media ID %d completed %+v, want %+v", id, got.CompletedAt, want.CompletedAt)
	}
}

func expectMissing(t *testing.T, list AnimeList, mediaID int) {
	t.Helper()
	if stray, ok := entryFor(list, mediaID); ok {
		t.Fatalf("media ID %d should be gone, found %+v", mediaID, stray)
	}
}

func copyFileForTest(t *testing.T, from, to string) {
	t.Helper()
	contents, readErr := os.ReadFile(from)
	if readErr != nil {
		t.Fatalf("reading %s: %v", from, readErr)
	}
	if writeErr := os.WriteFile(to, contents, 0o600); writeErr != nil {
		t.Fatalf("writing %s: %v", to, writeErr)
	}
}

// waitForRemoteTimestampTick lets the trackers' update times, kept to the
// second, tell two writes apart.
func waitForRemoteTimestampTick() { time.Sleep(2 * time.Second) }

// waitForAsyncTrackerUpdate gives the background tracker writes started by
// StartNextEpisode time to land.
func waitForAsyncTrackerUpdate() { time.Sleep(3 * time.Second) }

// withStdin runs fn with input waiting on standard input.
func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()

	fake, createErr := os.CreateTemp(t.TempDir(), "stdin-*")
	if createErr != nil {
		t.Fatalf("creating the stdin file: %v", createErr)
	}
	defer fake.Close()
	if _, err := io.WriteString(fake, input); err != nil {
		t.Fatalf("filling the stdin file: %v", err)
	}
	if _, err := fake.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rewinding the stdin file: %v", err)
	}

	saved := os.Stdin
	os.Stdin = fake
	defer func() { os.Stdin = saved }()
	fn()
}
