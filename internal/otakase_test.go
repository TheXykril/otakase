package internal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClearLogFileStartsAnEmptyLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "not-yet", "otakase-debug.log")

	if err := ClearLogFile(logPath); err != nil {
		t.Fatalf("a missing folder should be created: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("the last run"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ClearLogFile(logPath); err != nil {
		t.Fatalf("an existing log should be emptied: %v", err)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("the log should exist: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("the log kept %d bytes", info.Size())
	}
}

func TestSplitImageNotice(t *testing.T) {
	image, text, ok := splitImageNotice(imageNotice("/tmp/cover.jpg", "Frieren - Episode 3"))
	if !ok || image != "/tmp/cover.jpg" || text != "Frieren - Episode 3" {
		t.Fatalf("round trip gave (%q, %q, %v)", image, text, ok)
	}
	for _, message := range []string{"Frieren - Episode 3", "-i /tmp/cover.jpg", "-i"} {
		if _, _, ok := splitImageNotice(message); ok {
			t.Errorf("%q is not an image notice", message)
		}
	}
}

func TestExitErrorWorthShowing(t *testing.T) {
	refused := errors.New("command failed after 3 attempts: dial unix /tmp/mpv.sock: connect: connection refused")
	cases := []struct {
		name   string
		err    error
		socket string
		want   bool
	}{
		{name: "no error", want: false},
		{name: "error without a player", err: errors.New("no links"), want: true},
		{name: "player already gone", err: refused, socket: "/tmp/mpv.sock", want: false},
		{name: "another player's socket", err: refused, socket: "/tmp/other.sock", want: true},
		{name: "player failed otherwise", err: errors.New("broken pipe"), socket: "/tmp/mpv.sock", want: true},
	}
	for _, tc := range cases {
		if got := exitErrorWorthShowing(tc.err, tc.socket); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStatusLabel(t *testing.T) {
	options := listStatusOptions()
	if got := statusLabel(options, "PAUSED"); got != "On Hold" {
		t.Errorf("PAUSED = %q", got)
	}
	if got := statusLabel(options, "REPEATING"); got != "Rewatching" {
		t.Errorf("REPEATING = %q", got)
	}
	for _, status := range []string{"", "SOMETHING_NEW"} {
		if got := statusLabel(options, status); got != "None" {
			t.Errorf("%q = %q, want None", status, got)
		}
	}
}

func TestGetEntriesByCategoryListsEachShowOnce(t *testing.T) {
	list := AnimeList{
		Watching: []Entry{
			{Media: Media{ID: 1}, Status: "CURRENT"},
			{Media: Media{ID: 1}, Status: "CURRENT"},
		},
		Rewatching: []Entry{{Media: Media{ID: 3}, Status: "REPEATING"}},
		Completed:  []Entry{{Media: Media{ID: 2}, Status: "COMPLETED"}, {Media: Media{ID: 3}, Status: "COMPLETED"}},
	}

	ids := func(entries []Entry) []int {
		var out []int
		for _, entry := range entries {
			out = append(out, entry.Media.ID)
		}
		return out
	}
	if got := ids(getEntriesByCategory(list, "ALL")); len(got) != 3 {
		t.Errorf("ALL should hold three shows once each, got %v", got)
	}
	if got := ids(getEntriesByCategory(list, "CURRENT")); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("CURRENT should be Watching then Rewatching, got %v", got)
	}
	if got := ids(getEntriesByCategory(list, "COMPLETED")); len(got) != 2 {
		t.Errorf("COMPLETED = %v", got)
	}
	if got := getEntriesByCategory(list, "NOPE"); got == nil || len(got) != 0 {
		t.Errorf("an unknown list should be empty, got %#v", got)
	}
}

func TestOnWatchingList(t *testing.T) {
	list := AnimeList{
		Watching:   []Entry{{Media: Media{ID: 1}}},
		Rewatching: []Entry{{Media: Media{ID: 2}}},
		Planning:   []Entry{{Media: Media{ID: 3}}},
	}
	for id, want := range map[int]bool{1: true, 2: true, 3: false, 4: false} {
		if got := onWatchingList(list, id); got != want {
			t.Errorf("show %d: got %v, want %v", id, got, want)
		}
	}
}

func TestNeedsProviderSearchWithoutAUsableRow(t *testing.T) {
	if !needsProviderSearch(&Config{}, &Anime{}, nil) {
		t.Error("a show with no history row has to be searched for")
	}
	if !needsProviderSearch(&Config{}, &Anime{}, &Anime{ProviderName: "allanime"}) {
		t.Error("a row without a provider id has to be searched for")
	}
	if needsProviderSearch(&Config{}, &Anime{}, &Anime{ProviderId: "abc"}) {
		t.Error("a row with an id from before provider names were kept is used as is")
	}
}

func TestEpisodeCountFromLabel(t *testing.T) {
	if count, ok := episodeCountFromLabel("Frieren (28 episodes)"); !ok || count != 28 {
		t.Errorf("got %d, %v", count, ok)
	}
	for _, label := range []string{"Frieren", "Frieren (28 eps)", "(x episodes)"} {
		if _, ok := episodeCountFromLabel(label); ok {
			t.Errorf("%q has no count", label)
		}
	}
}

func TestStartOver(t *testing.T) {
	anime := &Anime{SkipRemoteSync: true, Ep: Episode{Number: 7, Resume: true, Player: playingVideo{PlaybackTime: 300}}}
	startOver(anime, true)
	if !anime.Rewatching || anime.SkipRemoteSync || anime.Ep.Number != 1 || anime.Ep.Resume || anime.Ep.Player.PlaybackTime != 0 {
		t.Fatalf("a rewatch should start at the top of episode 1 and write to the tracker: %+v", anime)
	}
	startOver(anime, false)
	if anime.Rewatching || !anime.SkipRemoteSync {
		t.Fatalf("watching again without a rewatch should leave the tracker alone: %+v", anime)
	}
}

func TestNextEpisodeLabel(t *testing.T) {
	anime := &Anime{TotalEpisodes: 12, Ep: Episode{Number: 11}}
	if got := nextEpisodeLabel(anime, "Yes, start episode 12"); got != "Yes, start episode 12" {
		t.Errorf("mid-show: %q", got)
	}
	anime.Ep.Number = 12
	if got := nextEpisodeLabel(anime, "Yes, start episode 13"); got != "Yes, finish series" {
		t.Errorf("last episode: %q", got)
	}
	// An unknown total never counts as the end.
	if atLastKnownEpisode(&Anime{Ep: Episode{Number: 400}}) {
		t.Error("no total, no last episode")
	}
}

func TestSequelDisplayTitle(t *testing.T) {
	sequel := &SequelInfo{ID: 9, Title: AnimeTitle{Romaji: "Sousou no Frieren 2", English: "Frieren 2"}}
	if got := sequelDisplayTitle(&Config{AnimeNameLanguage: "english"}, sequel); got != "Frieren 2" {
		t.Errorf("english: %q", got)
	}
	if got := sequelDisplayTitle(&Config{}, sequel); got != "Sousou no Frieren 2" {
		t.Errorf("default: %q", got)
	}
	if got := sequelDisplayTitle(nil, &SequelInfo{ID: 9}); got != "9" {
		t.Errorf("untitled: %q", got)
	}
	if got := sequelStatusText("NOT_YET_RELEASED"); got != "not yet released" {
		t.Errorf("status: %q", got)
	}
}

func TestSequelPromptsOfferOnlyWhatApplies(t *testing.T) {
	sequels := []SequelInfo{
		{ID: 1, Title: AnimeTitle{Romaji: "First"}, Status: "FINISHED"},
		{ID: 2, Title: AnimeTitle{Romaji: "Second"}, Status: "NOT_YET_RELEASED"},
	}
	withPromptSelect(t, func([]SelectionOption) (SelectionOption, error) {
		return SelectionOption{Key: "1"}, nil
	})
	picked, ok := selectSequel(&Config{}, sequels)
	if !ok || picked.ID != 2 {
		t.Fatalf("the second row should pick the second sequel, got ok=%v %+v", ok, picked)
	}

	for _, key := range []string{"skip", "-1", "-2", "7"} {
		withPromptSelect(t, func([]SelectionOption) (SelectionOption, error) {
			return SelectionOption{Key: key}, nil
		})
		if _, ok := selectSequel(&Config{}, sequels); ok {
			t.Errorf("%q should pick no sequel", key)
		}
	}

	var offered []SelectionOption
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		offered = options
		return SelectionOption{Key: "skip"}, nil
	})
	if summary := promptSequelAction(&Config{}, picked, "token"); summary != "sequel skipped" {
		t.Fatalf("summary = %q", summary)
	}
	for _, option := range offered {
		if option.Key == "watching" {
			t.Fatalf("a sequel not out yet cannot be watched: %+v", offered)
		}
	}
}

func TestLastEpisodeCompletionReportsSkippedWrites(t *testing.T) {
	previous := GetGlobalConfig()
	config := &Config{ScoreOnCompletion: true, TrackingRemote: TrackingRemoteAniList}
	SetGlobalConfig(config)
	t.Cleanup(func() { SetGlobalConfig(previous) })

	output := captureStdout(t, func() {
		HandleLastEpisodeCompletion(config, &Anime{
			AnilistId:      1,
			TotalEpisodes:  12,
			SkipRemoteSync: true,
			Ep:             Episode{Number: 12},
		}, "token")
	})
	for _, want := range []string{"Completion summary:", "rating skipped", "tracker updates skipped", "sequel check skipped"} {
		if !strings.Contains(output, want) {
			t.Errorf("summary is missing %q: %q", want, output)
		}
	}
}

func TestLastEpisodeCompletionIgnoresOtherEpisodes(t *testing.T) {
	output := captureStdout(t, func() {
		HandleLastEpisodeCompletion(&Config{}, &Anime{TotalEpisodes: 12, Ep: Episode{Number: 11}}, "token")
		HandleLastEpisodeCompletion(&Config{}, &Anime{Ep: Episode{Number: 1}}, "token")
	})
	if output != "" {
		t.Fatalf("only the last episode wraps a show up, got %q", output)
	}
}

// Every action the menus offer has to be answered by the home menu. One that
// is not falls through to being looked up as a list, which shows an empty one
// and no sign of what went wrong.
func TestEveryMenuActionHasAHandler(t *testing.T) {
	_, footer := SplitMenuOrder(
		"CURRENT,ALL,PLANNING,PAUSED,DROPPED,REWATCHING,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,TRACKER,PROVIDER")
	if len(footer) != 6 {
		t.Fatalf("expected six actions, got %d", len(footer))
	}
	keys := []string{listsMenuKey, nextListKey, "CAST", "STATS", "COMMUNITY", "MOVIES", "SURPRISE"}
	for _, action := range footer {
		keys = append(keys, action.Key)
	}
	for _, key := range keys {
		if _, ok := homeMenuActions[key]; !ok {
			t.Errorf("%s is offered but nothing handles it", key)
		}
		if _, isList := menuCategoryLabels[key]; isList {
			t.Errorf("%s is both an action and a list", key)
		}
		if !isMainMenuKey(key) {
			t.Errorf("%s chosen inside a list would not reach the home menu", key)
		}
	}
}

func TestHomeMenuTreatsListsAsLists(t *testing.T) {
	m := &homeMenu{config: &Config{}, anime: &Anime{}, user: &User{}}
	for key := range menuCategoryLabels {
		if got := m.act(key); got != menuList {
			t.Errorf("%s should open the list, got outcome %d", key, got)
		}
	}
}
