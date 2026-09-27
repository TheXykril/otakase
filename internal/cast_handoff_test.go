package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The referrer is the point of this file. Episode.StreamReferrer,
// SubtitleURL, StreamHeaders and Mode are all tagged json:"-", so marshalling
// an Anime drops them silently -- and without the referrer the remux cannot
// fetch the stream from the provider at all. A cast handed off without it
// fails with no obvious cause.
func TestCastSessionRoundTripKeepsTheReferrer(t *testing.T) {
	config := testCastConfig(t)
	anime := testCastAnime()
	anime.Ep.StreamReferrer = "https://megaplay.buzz/"
	anime.Ep.StreamHeaders = map[string]string{"Origin": "https://megaplay.buzz"}
	anime.Ep.Links = []string{"https://example.test/master.m3u8?token=abc"}

	path, err := writeCastSession(config, anime, "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}

	session, err := readCastSession(path)
	if err != nil {
		t.Fatalf("readCastSession: %v", err)
	}

	if session.StreamReferrer != "https://megaplay.buzz/" {
		t.Errorf("referrer = %q, want %q", session.StreamReferrer, "https://megaplay.buzz/")
	}
	if session.StreamHeaders["Origin"] != "https://megaplay.buzz" {
		t.Errorf("headers = %v, lost Origin", session.StreamHeaders)
	}
	if len(session.Links) != 1 || session.Links[0] != anime.Ep.Links[0] {
		t.Errorf("links = %v, want %v", session.Links, anime.Ep.Links)
	}
	if session.Device != "Office TV" {
		t.Errorf("device = %q, want %q", session.Device, "Office TV")
	}
}

// The file holds a stream URL with an authentication token, so it is not
// readable by other users and does not outlive the handoff.
func TestCastSessionFileIsPrivateAndDeletedOnRead(t *testing.T) {
	config := testCastConfig(t)

	path, err := writeCastSession(config, testCastAnime(), "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600: this file contains a tokened URL", mode)
	}

	if _, err := readCastSession(path); err != nil {
		t.Fatalf("readCastSession: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the session file outlived the read that consumed it")
	}
}

// A child from a different build must refuse a file it does not understand
// rather than misreading a tokened URL out of it.
func TestReadCastSessionRejectsAnUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	if err := os.WriteFile(path, []byte(`{"version":9999}`), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	if _, err := readCastSession(path); err == nil {
		t.Error("a session file from an unknown version was accepted")
	}
}

// Review Focus 5. If the spawn succeeds but the terminal dies before reading,
// nothing deletes the file -- and it holds a tokened URL. The sweep is what
// stops those accumulating.
func TestSweepCastSessionsRemovesStaleFiles(t *testing.T) {
	config := testCastConfig(t)

	stale, err := writeCastSession(config, testCastAnime(), "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	fresh, err := writeCastSession(config, testCastAnime(), "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}

	sweepCastSessions(config, time.Hour)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a two-hour-old session file survived the sweep")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("the sweep deleted a session file that had just been written")
	}
}

// Review Focus 1. $TERMINAL and CastTerminal routinely carry arguments --
// "flatpak run org.x.Term", "kitty --single-instance". exec.Command on the
// whole string looks for a binary with spaces in its name and fails, which is
// the same trap internal/editor.go documents for EDITOR.
func TestResolveCastTerminalSplitsArguments(t *testing.T) {
	config := &Config{CastTerminal: "flatpak run org.x.Term"}
	found := func(name string) (string, error) { return "/usr/bin/" + name, nil }

	command, err := resolveCastTerminal(config, found)
	if err != nil {
		t.Fatalf("resolveCastTerminal: %v", err)
	}
	want := []string{"/usr/bin/flatpak", "run", "org.x.Term"}
	if len(command) != len(want) {
		t.Fatalf("command = %v, want %v", command, want)
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command = %v, want %v", command, want)
		}
	}
}

// Configuration beats the environment, which beats whatever is installed.
func TestResolveCastTerminalPrefersConfigThenEnvThenPath(t *testing.T) {
	onlyFoot := func(name string) (string, error) {
		if name == "foot" || name == "myterm" || name == "envterm" {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}

	t.Setenv("TERMINAL", "envterm")

	command, err := resolveCastTerminal(&Config{CastTerminal: "myterm"}, onlyFoot)
	if err != nil || command[0] != "/usr/bin/myterm" {
		t.Errorf("with CastTerminal set, got %v (%v), want myterm", command, err)
	}

	command, err = resolveCastTerminal(&Config{}, onlyFoot)
	if err != nil || command[0] != "/usr/bin/envterm" {
		t.Errorf("with only $TERMINAL set, got %v (%v), want envterm", command, err)
	}

	t.Setenv("TERMINAL", "")
	command, err = resolveCastTerminal(&Config{}, onlyFoot)
	if err != nil || command[0] != "/usr/bin/foot" {
		t.Errorf("with neither set, got %v (%v), want foot from PATH", command, err)
	}
}

// A machine with no terminal emulator must say so, so the caller can fall back
// to casting in place rather than losing the episode.
func TestResolveCastTerminalReportsWhenNoneIsInstalled(t *testing.T) {
	t.Setenv("TERMINAL", "")
	none := func(string) (string, error) { return "", exec.ErrNotFound }

	if _, err := resolveCastTerminal(&Config{}, none); err == nil {
		t.Error("no terminal is installed, but resolveCastTerminal found one")
	}
}

// Critical 1. The spawned process never reaches StartPlayback, which is the
// only place that resolves episode links. StartNextEpisode clears Ep.Links "to
// force fetching new ones", so without a resolve of its own the rofi cast
// played episode 1, counted down ten seconds and printed "cast: no episode
// links".
func TestCastSessionEpisodeResolvesLinksWhenItHasNone(t *testing.T) {
	config := testCastConfig(t)
	anime := testCastAnime()
	anime.Ep.Links = nil

	resolved := 0
	restoreResolve := stubEpisodeLinkResolve(t, func(cfg *Config, a *Anime, entry *Entry) (ProviderEpisodeResult, bool) {
		resolved++
		return ProviderEpisodeResult{
			Links:        []string{"https://example.test/ep5.m3u8"},
			Mode:         "sub",
			ProviderName: "test-provider",
		}, true
	})
	defer restoreResolve()

	var castWith []string
	restoreCast := stubCastEpisodeForSession(t, func(cfg *Config, a *Anime) error {
		castWith = append([]string{}, a.Ep.Links...)
		return nil
	})
	defer restoreCast()

	if err := castSessionEpisode(config, anime); err != nil {
		t.Fatalf("castSessionEpisode: %v", err)
	}
	if resolved != 1 {
		t.Errorf("resolved %d times, want 1", resolved)
	}
	if len(castWith) != 1 || castWith[0] != "https://example.test/ep5.m3u8" {
		t.Errorf("cast received links %v, want the resolved one", castWith)
	}
}

// Links already in hand -- the first episode out of the handoff file, or a
// prefetched next one -- must not be thrown away and fetched again.
func TestCastSessionEpisodeKeepsLinksItAlreadyHas(t *testing.T) {
	config := testCastConfig(t)
	anime := testCastAnime()
	anime.Ep.Links = []string{"https://example.test/handed-off.m3u8"}

	restoreResolve := stubEpisodeLinkResolve(t, func(cfg *Config, a *Anime, entry *Entry) (ProviderEpisodeResult, bool) {
		t.Error("resolved links that were already present")
		return ProviderEpisodeResult{}, false
	})
	defer restoreResolve()

	restoreCast := stubCastEpisodeForSession(t, func(cfg *Config, a *Anime) error { return nil })
	defer restoreCast()

	if err := castSessionEpisode(config, anime); err != nil {
		t.Fatalf("castSessionEpisode: %v", err)
	}
}

// A resolve the viewer backed out of, or that found nothing, must end the
// season rather than reach CastEpisode with an empty list.
func TestCastSessionEpisodeStopsWhenNothingResolves(t *testing.T) {
	config := testCastConfig(t)
	anime := testCastAnime()
	anime.Ep.Links = nil

	restoreResolve := stubEpisodeLinkResolve(t, func(cfg *Config, a *Anime, entry *Entry) (ProviderEpisodeResult, bool) {
		return ProviderEpisodeResult{}, false
	})
	defer restoreResolve()

	restoreCast := stubCastEpisodeForSession(t, func(cfg *Config, a *Anime) error {
		t.Error("cast was reached with no links")
		return nil
	})
	defer restoreCast()

	if err := castSessionEpisode(config, anime); err == nil {
		t.Error("castSessionEpisode returned no error when nothing resolved")
	}
}

// TotalEpisodes and Rewatching decide whether the season ends and whether the
// tracker is written. Without them in the handoff file advanceDecision sees a
// show that never ends and a viewer who is not rewatching, so the spawned
// process would run past the finale and overwrite a rewatcher's completed
// entry.
func TestCastSessionCarriesTotalEpisodesAndRewatching(t *testing.T) {
	config := testCastConfig(t)
	anime := testCastAnime()
	anime.TotalEpisodes = 12
	anime.Rewatching = true

	path, err := writeCastSession(config, anime, "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}
	session, err := readCastSession(path)
	if err != nil {
		t.Fatalf("readCastSession: %v", err)
	}

	rebuilt := castSessionToAnime(session)
	if rebuilt.TotalEpisodes != 12 {
		t.Errorf("TotalEpisodes = %d, want 12", rebuilt.TotalEpisodes)
	}
	if !rebuilt.Rewatching {
		t.Error("Rewatching was lost across the handoff")
	}
}

// A file written by the build installed before this change has version 1 and
// none of the new fields. It must still load, with zero values, rather than
// leaving a viewer who upgraded mid-cast with a window that refuses the file.
func TestCastSessionReadsAnOlderVersion(t *testing.T) {
	config := testCastConfig(t)
	dir := castSessionDir(config)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "session-old.json")
	old := `{"version":1,"anilist_id":424242,"episode_number":5,"links":["https://example.test/a.m3u8"]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	session, err := readCastSession(path)
	if err != nil {
		t.Fatalf("readCastSession refused an older file: %v", err)
	}
	if session.EpisodeNumber != 5 {
		t.Errorf("episode = %d, want 5", session.EpisodeNumber)
	}
	if session.TotalEpisodes != 0 || session.Rewatching {
		t.Errorf("missing fields did not default to zero: total=%d rewatching=%v",
			session.TotalEpisodes, session.Rewatching)
	}
}

// A file from a build newer than this one may describe things this one cannot
// do, so it is still refused rather than guessed at.
func TestCastSessionRefusesANewerVersion(t *testing.T) {
	config := testCastConfig(t)
	dir := castSessionDir(config)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "session-new.json")
	if err := os.WriteFile(path, []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := readCastSession(path); err == nil {
		t.Error("readCastSession accepted a version this build does not understand")
	}
}

// The regression the Critical-1 fix created. Carrying TotalEpisodes is what
// made HandleLastEpisodeCompletion reachable inside the spawned process at
// all, and that function reaches four fields of the Anime the handoff file
// never carried. So a viewer finishing the finale of a show they were on their
// fourth rewatch of, from a rofi cast, had AniList told repeat=1 with today's
// date as the start date -- silently, and not recoverable from our side. The
// same episode cast from the terminal writes it correctly, which is the kind
// of bug nobody suspects the cast feature of.
//
// This walks the whole path rather than the round trip alone, because the two
// halves can each be right while the composition is wrong: the file carries
// the field and the completion still reads a different one.
func TestRewatchCompletionFromAHandedOffCastKeepsTheRealCountAndStartDate(t *testing.T) {
	config := testCastConfig(t)

	// What the launching process holds, having loaded the entry from AniList.
	launching := testCastAnime()
	launching.Rewatching = true
	launching.TotalEpisodes = 12
	launching.Ep.Number = 12
	launching.Repeat = 3
	launching.StartedAt = FuzzyDate{Year: 2024, Month: 1, Day: 15}

	path, err := writeCastSession(config, launching, "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}
	session, err := readCastSession(path)
	if err != nil {
		t.Fatalf("readCastSession: %v", err)
	}

	// What the spawned process actually holds by the time it finishes the season.
	spawned := castSessionToAnime(session)
	completedAt := FuzzyDate{Year: 2026, Month: 9, Day: 26}
	repeat, startedAt := anilistRewatchCompletion(*spawned, completedAt)

	if repeat != 4 {
		t.Errorf("repeat = %d, want 4: the handoff lost the rewatch count, so the "+
			"tracker is told this was the first pass", repeat)
	}
	if startedAt != launching.StartedAt {
		t.Errorf("startedAt = %+v, want %+v: the real start date was overwritten",
			startedAt, launching.StartedAt)
	}
	if startedAt == completedAt {
		t.Error("startedAt fell back to the completion date, which stamps today " +
			"over the date the viewer actually started")
	}
}

// The other two fields TotalEpisodes made reachable are not data but gates:
// IsAiring decides whether the score prompt and the COMPLETED write happen at
// all, and SkipRemoteSync is the viewer having answered "continue without
// updating tracker". Both default to false in a struct decoded from JSON, so
// losing them does the opposite of the safe thing -- a still-airing finale gets
// marked completed, and an opted-out tracker entry gets written anyway.
func TestCastSessionCarriesTheCompletionGates(t *testing.T) {
	config := testCastConfig(t)
	anime := testCastAnime()
	anime.TotalEpisodes = 12
	anime.Rewatching = true
	anime.IsAiring = true
	anime.SkipRemoteSync = true

	path, err := writeCastSession(config, anime, "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}
	session, err := readCastSession(path)
	if err != nil {
		t.Fatalf("readCastSession: %v", err)
	}
	rebuilt := castSessionToAnime(session)

	if !rebuilt.IsAiring {
		t.Error("IsAiring was lost: a finale AniList still reports as releasing " +
			"would be marked COMPLETED")
	}
	if !rebuilt.SkipRemoteSync {
		t.Error("SkipRemoteSync was lost: a tracker the viewer opted out of " +
			"would be written to anyway")
	}
	if ShouldWriteRemoteTracking(&Config{TrackingRemote: "anilist"}, rebuilt) {
		t.Error("ShouldWriteRemoteTracking ignored the carried SkipRemoteSync: " +
			"the opted-out entry would be written to")
	}
}

// And the fields must survive a real write/read, not just a struct copy --
// which is what makes this a test of the handoff rather than of the struct.
func TestCastSessionRoundTripsRewatchCountAndDates(t *testing.T) {
	config := testCastConfig(t)
	anime := testCastAnime()
	anime.Repeat = 2
	anime.StartedAt = FuzzyDate{Year: 2023, Month: 6, Day: 1}
	anime.CompletedAt = FuzzyDate{Year: 2023, Month: 11, Day: 20}

	path, err := writeCastSession(config, anime, "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}
	session, err := readCastSession(path)
	if err != nil {
		t.Fatalf("readCastSession: %v", err)
	}
	rebuilt := castSessionToAnime(session)

	if rebuilt.Repeat != 2 {
		t.Errorf("Repeat = %d, want 2", rebuilt.Repeat)
	}
	if rebuilt.StartedAt != (FuzzyDate{Year: 2023, Month: 6, Day: 1}) {
		t.Errorf("StartedAt = %+v, want 2023-06-01", rebuilt.StartedAt)
	}
	if rebuilt.CompletedAt != (FuzzyDate{Year: 2023, Month: 11, Day: 20}) {
		t.Errorf("CompletedAt = %+v, want 2023-11-20", rebuilt.CompletedAt)
	}
}

// A viewer who started on the same day they finished has no StartedAt to
// preserve, and the entry should say so rather than record a zero date.
func TestAnilistRewatchCompletionFallsBackToTheCompletionDate(t *testing.T) {
	anime := Anime{Repeat: 0}
	completedAt := FuzzyDate{Year: 2026, Month: 9, Day: 26}

	repeat, startedAt := anilistRewatchCompletion(anime, completedAt)

	if repeat != 1 {
		t.Errorf("repeat = %d, want 1", repeat)
	}
	if startedAt != completedAt {
		t.Errorf("startedAt = %+v, want the completion date %+v", startedAt, completedAt)
	}
}

// stubEpisodeLinkResolve replaces the resolve behind ResolveEpisodeLinks.
func stubEpisodeLinkResolve(t *testing.T, fn func(*Config, *Anime, *Entry) (ProviderEpisodeResult, bool)) func() {
	t.Helper()
	previous := episodeLinkResolver
	episodeLinkResolver = fn
	return func() { episodeLinkResolver = previous }
}

// stubCastEpisodeForSession replaces the cast the spawned loop runs.
func stubCastEpisodeForSession(t *testing.T, fn func(*Config, *Anime) error) func() {
	t.Helper()
	previous := castEpisodeForSession
	castEpisodeForSession = fn
	return func() { castEpisodeForSession = previous }
}

// Important 4. SetGlobalUser used to sit under "if UsesRemoteTracking", so
// with TrackingRemote=none GetGlobalUser stayed nil, AdvanceAfterEpisode's
// nil-user guard returned false, and the spawned cast silently stopped after
// episode 1 -- having just shown the viewer a countdown saying the next one was
// starting. The same guard also skipped the LocalUpdateAnime write, which needs
// no tracker at all.
func TestCastSessionUserExistsWithTrackingOff(t *testing.T) {
	previous := GetGlobalUser()
	t.Cleanup(func() { SetGlobalUser(previous) })
	SetGlobalUser(nil)

	config := testCastConfig(t)
	config.TrackingRemote = "none"

	prepareCastSessionUser(config)

	if GetGlobalUser() == nil {
		t.Fatal("no user was set with remote tracking off")
	}
}

// And the advance itself must then run: continue mid-season, and write local
// history, with no tracker configured.
func TestCastAdvanceWorksWithTrackingOff(t *testing.T) {
	storage := t.TempDir()
	config := &Config{StoragePath: storage, TrackingRemote: "none", PercentageToMarkComplete: 85}
	databaseFile := filepath.Join(storage, "curd_history.txt")

	anime := &Anime{AnilistId: 424242, ProviderId: "p", TotalEpisodes: 12}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 5
	// The global anime is deliberately left alone: StartNextEpisode reports
	// progress from a goroutine that reads it, and this package's globals are
	// unguarded, so a cleanup writing it back would race that goroutine.

	if !AdvanceAfterEpisode(config, anime, prepareCastSessionUser(config), databaseFile, func() bool { return true }) {
		t.Fatal("the advance declined to continue with remote tracking off")
	}
	if anime.Ep.Number != 6 {
		t.Errorf("episode = %d after advancing, want 6", anime.Ep.Number)
	}
	if _, err := os.Stat(databaseFile); err != nil {
		t.Errorf("local history was not written with remote tracking off: %v", err)
	}
}

// Carrying TotalEpisodes made HandleLastEpisodeCompletion reachable in the
// spawned process for the first time, and that path reads fields the handoff
// file did not carry. Repeat is the one that destroys data: a viewer finishing
// their fourth rewatch from a rofi cast had AniList told "repeat 1", and with
// StartedAt zero the original start date was overwritten with today.
func TestCastSessionCarriesTheRewatchEntry(t *testing.T) {
	config := testCastConfig(t)
	anime := testCastAnime()
	anime.Rewatching = true
	anime.Repeat = 3
	anime.StartedAt = FuzzyDate{Year: 2023, Month: 4, Day: 11}
	anime.CompletedAt = FuzzyDate{Year: 2023, Month: 6, Day: 20}
	anime.IsAiring = true
	anime.SkipRemoteSync = true

	path, err := writeCastSession(config, anime, "")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}
	session, err := readCastSession(path)
	if err != nil {
		t.Fatalf("readCastSession: %v", err)
	}
	restored := castSessionToAnime(session)

	if restored.Repeat != 3 {
		t.Errorf("Repeat = %d, want 3", restored.Repeat)
	}
	if restored.StartedAt != anime.StartedAt {
		t.Errorf("StartedAt = %v, want %v", restored.StartedAt, anime.StartedAt)
	}
	if restored.CompletedAt != anime.CompletedAt {
		t.Errorf("CompletedAt = %v, want %v", restored.CompletedAt, anime.CompletedAt)
	}
	if !restored.IsAiring {
		t.Error("IsAiring was lost, so a still-airing show would be marked COMPLETED")
	}
	if !restored.SkipRemoteSync {
		t.Error("SkipRemoteSync was lost, so a viewer who declined tracking would be written anyway")
	}

	// The consequence, at the function that does the writing.
	completedAt := FuzzyDate{Year: 2026, Month: 9, Day: 27}
	repeat, startedAt := anilistRewatchCompletion(*restored, completedAt)
	if repeat != 4 {
		t.Errorf("repeat written = %d, want 4", repeat)
	}
	if startedAt != anime.StartedAt {
		t.Errorf("startedAt written = %v, want the original %v", startedAt, anime.StartedAt)
	}
}

// The two tracking-off tests call prepareCastSessionUser directly, so deleting
// its call site left the suite green while the spawned cast stopped advancing
// after episode 1 again. This covers the wiring instead of the helper.
func TestPrepareCastSessionWiresTheUserAndTheGlobalAnime(t *testing.T) {
	previousUser := GetGlobalUser()
	previousAnime := GetGlobalAnime()
	t.Cleanup(func() {
		SetGlobalUser(previousUser)
		SetGlobalAnime(previousAnime)
	})
	SetGlobalUser(nil)

	config := testCastConfig(t)
	config.TrackingRemote = "none"
	config.RofiSelection = true

	anime, databaseFile := prepareCastSession(config, &castSessionFile{
		Version:       castSessionVersion,
		AnilistID:     4321,
		Title:         "Mushishi",
		EpisodeNumber: 7,
		TotalEpisodes: 26,
		Device:        "Bedroom TV",
	})

	if GetGlobalUser() == nil {
		t.Error("no user was set, so AdvanceAfterEpisode would decline to continue")
	}
	if GetGlobalAnime().AnilistId != 4321 {
		t.Errorf("global anime = %d, want the episode being cast", GetGlobalAnime().AnilistId)
	}
	if anime.Ep.Number != 7 || anime.TotalEpisodes != 26 {
		t.Errorf("anime = ep %d of %d, want ep 7 of 26", anime.Ep.Number, anime.TotalEpisodes)
	}
	if config.RofiSelection {
		t.Error("RofiSelection was left on, so the spawned process would try to draw a rofi menu")
	}
	if !config.CastToDevice || config.CastDevice != "Bedroom TV" {
		t.Errorf("cast target = %v/%q, want true/\"Bedroom TV\"", config.CastToDevice, config.CastDevice)
	}
	if databaseFile == "" {
		t.Error("no history file path")
	}
}
