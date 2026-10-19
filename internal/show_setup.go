package internal

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// trackerEpisodeDuration answers what Ep.Duration should hold before anything
// has measured the episode, given what it holds now and what the tracker says.
//
// The tracker reports an average episode length in minutes; Ep.Duration is
// seconds. It is the only length known before playback starts, and casting has
// nothing better: the receiver reports dur=-1 for the whole episode, because
// the remux writes no EXT-X-ENDLIST until it finishes, so without this the cast
// panel has no total to show at all.
//
// A measured duration always wins. mpv overwrites this with the real length of
// the file it opened, and a finished remux does the same for a cast, so this
// only fills the gap where nothing has measured anything yet.
func trackerEpisodeDuration(current int, media Media) int {
	if current > 0 || media.Duration <= 0 {
		return current
	}
	return media.Duration * 60
}

// Setup asks which show to watch and gets it ready for the playback loop:
// its tracker entry, the provider it plays from, and the episode and position
// to start at.
func Setup(userConfig *Config, anime *Anime, user *User, databaseAnimes *[]Anime) {
	// On a cache hit this is instant and the user id comes from the cached
	// list, so the menu does not wait on AniList; the real list is fetched
	// in the background (see InitializeUserAnimeList).
	if err := InitializeUserAnimeList(userConfig, user); err != nil {
		Log(fmt.Sprintf("Failed to initialize anime list: %v", err))
		Exit(fmt.Errorf("Failed to get user data\nYou can reset the token by running `otakase -change-token`"))
	}
	// Every question on the way can send the viewer back to the menu, which
	// starts the choice over.
	for {
		if prepareShow(userConfig, anime, user, databaseAnimes) {
			return
		}
	}
}

// prepareShow is one pass of Setup. It reports false when the viewer went
// back to the menu.
func prepareShow(userConfig *Config, anime *Anime, user *User, databaseAnimes *[]Anime) bool {
	link, fromLink := chooseShow(userConfig, anime, user, databaseAnimes)
	waitForListRefresh(userConfig, user)

	stored := LocalFindAnime(*databaseAnimes, anime.AnilistId, "")
	entry, ok := trackerEntry(userConfig, anime, user, fromLink)
	if !ok {
		return false
	}
	if entry.Media.Status == "NOT_YET_RELEASED" {
		handleUnreleasedAnime(userConfig, user, anime, *entry)
		EnterAppScreen()
		return false
	}
	applyTrackerEntry(userConfig, anime, entry)
	query := anime.Title.Romaji

	// Set when the show starts over from episode 1, which no saved
	// position applies to.
	restarting := false
	if entry.Status == "COMPLETED" {
		var proceed bool
		if restarting, proceed = askAboutCompletedShow(userConfig, anime, user); !proceed {
			return false
		}
	}

	remap := needsProviderSearch(userConfig, anime, stored)
	if remap {
		Log("Anime not found in database for current provider, searching in animeList...")
		outcome, err := ResolveAnimeProviderMapping(userConfig, anime, query, entry)
		if err != nil {
			Log(fmt.Sprintf("Provider mapping failed: %v", err))
			Exit(err)
		}
		switch outcome {
		case ProviderMappingBack:
			backToMenu(anime)
			return false
		case ProviderMappingQuit:
			Exit(nil)
		}
	}
	if stored != nil {
		applyStoredProgress(userConfig, anime, user, stored, entry, remap, restarting)
	}

	// Which episode is playing is settled above, so this is where the resume
	// position can be taken from the row that holds it: the one written for
	// that episode, by the provider about to play it. What was used instead
	// was the row LocalFindAnime returned, which is the furthest-ahead row for
	// the show -- the right answer for "where is this show up to" and the
	// wrong one for "where in this episode was I". A show watched through
	// three providers, one of them an episode ahead, therefore resumed from
	// that row: a nine minute position replaced by a one second one, which
	// the resume gate then rejected as too early to bother with, so nothing
	// resumed and nothing said why.
	//
	// Only when something already intends to resume. An explicitly cleared
	// position -- the viewer choosing the tracker's episode over the local
	// one -- stays cleared.
	if anime.Ep.Resume || anime.Ep.Player.PlaybackTime > 0 {
		if row := LocalFindEpisode(*databaseAnimes, anime.AnilistId, anime.Ep.Number, CurrentAnimeProviderName(anime)); row != nil {
			anime.Ep.Player.PlaybackTime = row.Ep.Player.PlaybackTime
			anime.Ep.Resume = row.Ep.Player.PlaybackTime > 0
		} else {
			// No row for this episode at all, so the position belongs to a
			// different one and carrying it would resume an episode the
			// viewer has not started.
			anime.Ep.Player.PlaybackTime = 0
			anime.Ep.Resume = false
		}
		Log(fmt.Sprintf("Resume position for episode %d on %s: %ds (resume=%t)",
			anime.Ep.Number, CurrentAnimeProviderName(anime), anime.Ep.Player.PlaybackTime, anime.Ep.Resume))
	}

	// Another device may have got further into this episode. Not on a
	// rewatch, which starts every episode over.
	if restarting {
		anime.Ep.Player.PlaybackTime = 0
		anime.Ep.Resume = false
	} else {
		ApplySyncedResume(userConfig, anime)
	}

	if !offerWatchingList(userConfig, anime, user) {
		return false
	}

	// The tracker's progress wins when it is further on than all of the above.
	if current, err := FindAnimeByAnilistID(user.AnimeList, strconv.Itoa(anime.AnilistId)); err == nil {
		if next := nextEpisodeFromProgress(current.Progress); next > anime.Ep.Number {
			anime.Ep.Number = next
			anime.Ep.Player.PlaybackTime = 0
			anime.Ep.Resume = false
		}
	}

	searched := settleEpisodeCount(userConfig, anime, user, query)
	settleStartEpisode(userConfig, anime, entry, link, searched)

	if fromLink {
		trackerNext := 0
		if entry, err := FindAnimeByAnilistID(user.AnimeList, strconv.Itoa(anime.AnilistId)); err == nil {
			trackerNext = nextEpisodeFromProgress(entry.Progress)
		}
		applyLinkEpisode(userConfig, anime, link.Episode, trackerNext)
	}
	return true
}

// backToMenu leaves the show for the menu. -c is dropped with it, or the
// menu would be skipped straight back to the same show.
func backToMenu(anime *Anime) {
	Out("Going back to main menu...")
	LeaveAppScreen()
	anime.Ep.ContinueLast = false
}

// chooseShow sets anime.AnilistId to the show to watch: the one an
// otakase:// link names, the last one watched under -c, or the one picked
// from the home menu. It returns the link when one was used.
func chooseShow(userConfig *Config, anime *Anime, user *User, databaseAnimes *[]Anime) (OpenLink, bool) {
	for {
		// An otakase:// link names the show itself, in place of any menu.
		if link, ok := takeOpenLink(); ok {
			anime.Ep.ContinueLast = false
			anime.AnilistId = link.AnilistID
			return link, true
		}

		if anime.Ep.ContinueLast {
			id := readLastAnimeID(userConfig, "No last watched anime found", "Invalid anime ID in last-anime file")
			stored := LocalFindAnime(*databaseAnimes, id, "")
			if stored == nil {
				Exit(fmt.Errorf("Last watched anime not found in database"))
			}
			anime.AnilistId = stored.AnilistId
			return OpenLink{}, false
		}

		menu := &homeMenu{
			config:  userConfig,
			anime:   anime,
			user:    user,
			history: databaseAnimes,
			// There is no menu in front of the lists: otakase opens on
			// Watching, and an action returns to whichever list it was
			// started from rather than to the first one.
			active: launchList(userConfig),
		}
		if menu.choose() {
			return OpenLink{}, false
		}
		// A show picked from Discord: it is waiting as an otakase:// link.
	}
}

// readLastAnimeID reads the show Continue Last Session resumes, ending the
// run with readFailed or badID when it cannot.
func readLastAnimeID(userConfig *Config, readFailed, badID string) int {
	data, err := os.ReadFile(LastAnimeFile(userConfig.StoragePath))
	if err != nil {
		Log("Error reading last-anime file: " + err.Error())
		Exit(errors.New(readFailed))
	}
	id, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		Log("Error converting last-anime id: " + err.Error())
		Exit(errors.New(badID))
	}
	return id
}

// homeMenu is the lists a show is picked from, and the actions reached from
// them.
type homeMenu struct {
	config  *Config
	anime   *Anime
	user    *User
	history *[]Anime

	// active is the list on screen.
	active string
	// pending is an action chosen by its key from inside a list, to be
	// answered on the next turn.
	pending SelectionOption
}

// menuOutcome is where an action leaves the home menu.
type menuOutcome int

const (
	// menuList is no action at all: the key names a list to show.
	menuList menuOutcome = iota
	// menuStay shows the menu again.
	menuStay
	// menuChosen has set anime.AnilistId.
	menuChosen
	// menuRestart starts the choice over, for a show that arrives as a
	// link.
	menuRestart
)

// choose runs the menu until a show is picked, and reports false when the
// choice has to start over instead.
func (m *homeMenu) choose() bool {
	for {
		request := SelectionOption{Key: m.active}
		if m.pending.Key != "" {
			request, m.pending = m.pending, SelectionOption{}
		}

		switch m.act(request.Key) {
		case menuChosen:
			return true
		case menuRestart:
			return false
		case menuStay:
			continue
		}
		if m.pickFromList(request.Key) {
			return true
		}
	}
}

// refreshList takes the latest list from the background refresh, if one runs.
func (m *homeMenu) refreshList() {
	if m.user.ListSync != nil {
		m.user.AnimeList = m.user.ListSync.Current()
	}
}

// pick makes id the chosen show.
func (m *homeMenu) pick(id int) menuOutcome {
	m.anime.AnilistId = id
	return menuChosen
}

// openPage runs one of the pages reached from the menu, on a clear screen,
// and comes back to the menu after it.
func openPage(page func()) menuOutcome {
	EnterAppScreen()
	page()
	EnterAppScreen()
	return menuStay
}

// homeMenuActions answers each action the home menu offers, by key. One
// table serves every route an action arrives by -- a toolbar button, a key
// pressed inside a list, a footer entry -- so none of them can miss it and
// fall through to being looked up as a list, which shows an empty one.
var homeMenuActions = map[string]func(m *homeMenu) menuOutcome{
	listsMenuKey: func(m *homeMenu) menuOutcome {
		// rofi has no tabs, so its toolbar opens the lists instead.
		m.refreshList()
		return openPage(func() {
			if list, ok := pickList(m.config, m.user.AnimeList, m.active); ok {
				m.active = list
			}
		})
	},
	nextListKey: func(m *homeMenu) menuOutcome {
		m.active = nextList(m.config, m.active)
		return menuStay
	},
	"PROVIDER": func(m *homeMenu) menuOutcome {
		return openPage(func() { ChangeProvider(m.config) })
	},
	"TRACKER": func(m *homeMenu) menuOutcome {
		return openPage(func() { ChangeTracker(m.config, m.user) })
	},
	"UPDATE": func(m *homeMenu) menuOutcome {
		return openPage(func() { UpdateAnimeEntry(m.config, m.user) })
	},
	"UNTRACKED": func(m *homeMenu) menuOutcome {
		return openPage(func() { WatchUntracked(m.config) })
	},
	"REMAP_PROVIDER": func(m *homeMenu) menuOutcome {
		return openPage(func() { RemapProviderAnime(m.config, m.user, m.history) })
	},
	// A page to read rather than a list to pick from.
	"STATS": func(m *homeMenu) menuOutcome {
		return openPage(func() { ShowWatchStats(m.config) })
	},
	// Its own menus, history and player loop.
	"MOVIES": func(m *homeMenu) menuOutcome {
		return openPage(func() { WatchMovies(m.config) })
	},
	// A toggle, so it returns to the menu rather than going on
	// to a show: the entry it just changed is the thing the
	// viewer wants to see the new state of.
	"CAST": func(m *homeMenu) menuOutcome {
		toggleCastToDevice(m.config)
		EnterAppScreen()
		return menuStay
	},
	"COMMUNITY": func(m *homeMenu) menuOutcome {
		play := false
		openPage(func() { play = ShowCommunity(m.config) })
		if play {
			return menuRestart
		}
		return menuStay
	},
	"CONTINUE_LAST": (*homeMenu).continueLast,
	"SURPRISE": func(m *homeMenu) menuOutcome {
		EnterAppScreen()
		m.refreshList()
		id, ok := SurpriseMe(m.config, m.user.AnimeList)
		EnterAppScreen()
		if !ok {
			return menuStay
		}
		return m.pick(id)
	},
}

// act answers a menu action, or returns menuList for a key that is not one.
func (m *homeMenu) act(key string) menuOutcome {
	if action, ok := homeMenuActions[key]; ok {
		return action(m)
	}
	// A continue-watching row: the same as choosing the show
	// from its list, which resumes from the watch history.
	if id, ok := resumeRowAnilistID(key); ok {
		return m.pick(id)
	}
	return menuList
}

// continueLast answers the terminal's ^l, or rofi's Continue button with the
// rows switched off. Where there are several recent shows they are offered,
// since the rows lead Watching only.
func (m *homeMenu) continueLast() menuOutcome {
	m.refreshList()
	rows := continueWatchingRows(m.config, &m.user.AnimeList)
	switch len(rows) {
	case 0:
		m.anime.Ep.ContinueLast = true
		return m.pick(readLastAnimeID(m.config, "Error reading last-anime file", "Error converting last-anime id to integer"))
	case 1:
		id, _ := resumeRowAnilistID(rows[0].Key)
		return m.pick(id)
	}

	EnterAppScreen()
	picked, err := DynamicSelect(rows)
	if err != nil {
		Log(fmt.Sprintf("Failed to select a show to continue: %v", err))
		Exit(fmt.Errorf("Failed to select a show to continue"))
	}
	if picked = NormalizeSelectionKey(picked); SelectionMeansQuit(picked) {
		Exit(nil)
	}
	EnterAppScreen()
	if id, ok := resumeRowAnilistID(picked.Key); ok {
		return m.pick(id)
	}
	return menuStay
}

// pickFromList shows the list key names, or the active one. It reports true
// once a show is picked, and false when an action was chosen instead, which
// is left in m.pending.
func (m *homeMenu) pickFromList(key string) bool {
	cfg := m.config
	m.refreshList()
	if menuCategoryLabels[canonicalCategoryKey(key)] != "" {
		m.active = canonicalCategoryKey(key)
	}

	preview := cfg.RofiSelection && cfg.ImagePreview
	var options []SelectionOption
	var previews map[string]RofiSelectPreview
	if preview {
		previews = buildMainPreviewOptions(cfg, m.user.AnimeList, m.active)
	} else {
		options = buildMainListOptions(cfg, m.user.AnimeList, m.active)
	}
	tabs, actions := mainMenuLayout(cfg, m.user.AnimeList)

	for {
		var picked SelectionOption
		var err error
		if preview {
			picked, err = DynamicSelectPreviewWithRefresh(previews, true, &PreviewSelectionRefreshConfig{
				Updates:   m.user.ListSync.Updates(),
				Refreshed: m.user.ListSync.RefreshDone(),
				BuildOptions: func(list AnimeList) map[string]RofiSelectPreview {
					return buildMainPreviewOptions(cfg, list, m.active)
				},
				Main:       true,
				Prompt:     menuCategoryLabels[m.active],
				Categories: tabs,
				Actions:    actions,
			})
		} else {
			// Tab moves between categories without leaving the list.
			// The whole list is already in memory, so switching one
			// is a filter rather than a fetch. m.active follows the tab
			// so a background refresh rebuilds what is on screen rather
			// than the category first opened, and an action returns to
			// it.
			rows := append(append([]SelectionOption(nil), options...), SelectionOption{Key: "add_new", Label: "Add new anime"})
			picked, err = DynamicSelectWithRefresh(rows, &SelectionRefreshConfig{
				Updates:   m.user.ListSync.Updates(),
				Refreshed: m.user.ListSync.RefreshDone(),
				BuildOptions: func(list AnimeList) []SelectionOption {
					return buildMainListOptions(cfg, list, m.active)
				},
				Main:           true,
				Prompt:         menuCategoryLabels[m.active],
				Categories:     tabs,
				ActiveCategory: m.active,
				Actions:        actions,
				LoadCategory: func(key string) []SelectionOption {
					m.active = key
					return buildMainListOptions(cfg, m.user.AnimeList, key)
				},
			})
		}
		if err != nil {
			Log(fmt.Sprintf("Error selecting anime: %v", err))
			Exit(fmt.Errorf("Error selecting anime"))
		}
		Log(picked)

		// Nothing is shown before this list, so there is nothing behind it
		// to go back to: back quits as well.
		picked = NormalizeSelectionKey(picked)
		if SelectionMeansQuit(picked) || SelectionMeansBack(picked) {
			Exit(nil)
		}
		// An action chosen from the list -- a toolbar button, a key,
		// a continue-watching row -- is answered by the dispatch in
		// choose rather than duplicated here.
		if isMainMenuKey(picked.Key) {
			m.pending = picked
			EnterAppScreen()
			return false
		}
		if picked.Key == "" {
			continue
		}

		if picked.Key == "add_new" || picked.Label == "add_new" {
			added := AddNewAnime(cfg, m.anime, m.user, m.history)
			if added.Key == "-2" {
				EnterAppScreen()
				continue
			}
			picked = added
		}
		id, err := strconv.Atoi(picked.Key)
		if err != nil {
			Log(fmt.Sprintf("Error converting Anilist ID: %v", err))
			Exit(fmt.Errorf("Error converting Anilist ID"))
		}
		m.anime.AnilistId = id
		return true
	}
}

// waitForListRefresh holds the start of playback for the background list
// refresh, so the progress it starts from is the tracker's latest. It waits
// at most 10 seconds and then goes on with the cached list.
func waitForListRefresh(userConfig *Config, user *User) {
	if user.ListSync == nil {
		return
	}
	endBusy := BeginBusy(userConfig, "Refreshing your anime list")
	// RefreshDone is closed when the refresh finishes, so this cannot miss
	// it to the menu, which already drained the updates channel.
	select {
	case <-user.ListSync.RefreshDone():
		Log("Background refresh done, using latest anime list for playback")
	case <-time.After(10 * time.Second):
		Log("Timed out waiting for background anime list refresh; using cached list")
	}
	endBusy()
	user.AnimeList = user.ListSync.Current()
}

// trackerEntry is the list entry for the chosen show. Without remote
// tracking, or for a show opened from a link that is not on the list yet, it
// is made up from AniList's details. It reports false when the viewer goes
// back to the menu.
func trackerEntry(userConfig *Config, anime *Anime, user *User, fromLink bool) (*Entry, bool) {
	entry, err := FindAnimeByAnilistID(user.AnimeList, strconv.Itoa(anime.AnilistId))
	if err == nil {
		return entry, true
	}
	// A show opened from a link need not be on the list yet; it is
	// offered for the watching list further down.
	if UsesRemoteTracking(userConfig) && !fromLink {
		Log(fmt.Sprintf("Can not find the anime in tracked anime list: %v", err))
		Exit(fmt.Errorf("Can not find the anime in tracked anime list"))
	}

	show, err := GetAnimeDataByID(anime.AnilistId, "")
	if err != nil {
		if fromLink {
			Log(fmt.Sprintf("Link: no AniList show %d: %v", anime.AnilistId, err))
			OpenLinkNotice(userConfig, fmt.Sprintf("Could not find the linked show (AniList %d). Starting %s as usual.", anime.AnilistId, DisplayName))
			EnterAppScreen()
			return nil, false
		}
		Log(fmt.Sprintf("Failed to fetch fallback anime data: %v", err))
		Exit(fmt.Errorf("Failed to get anime data"))
	}
	return &Entry{
		Media: Media{
			ID:       show.AnilistId,
			MalID:    show.MalId,
			Episodes: show.TotalEpisodes,
			Duration: ConvertSecondsToMinutes(show.Ep.Duration),
			Title:    show.Title,
			Status:   "FINISHED",
		},
		Status:     "CURRENT",
		CoverImage: show.CoverImage,
	}, true
}

// applyTrackerEntry takes what the tracker knows about the show, and starts
// at the episode after its progress.
func applyTrackerEntry(userConfig *Config, anime *Anime, entry *Entry) {
	media := entry.Media
	anime.Title = media.Title
	// Before anything asks a provider for episodes: the audio remembered
	// for this show decides which ones.
	applyShowAudioMode(userConfig, anime)
	anime.TotalEpisodes = media.Episodes
	anime.IsAdult = media.IsAdult
	anime.Ep.Duration = trackerEpisodeDuration(anime.Ep.Duration, media)
	anime.CoverImage = entry.CoverImage
	if media.MalID != 0 {
		anime.MalId = media.MalID
	}
	if anime.MalId == 0 {
		anime.MalId, _ = GetAnimeMalID(anime.AnilistId)
	}
	anime.IsAiring = media.Status == "RELEASING" || media.Status == "NOT_YET_RELEASED"
	anime.Rewatching = entry.Status == "REPEATING"
	anime.Repeat = entry.Repeat
	anime.StartedAt = entry.StartedAt
	anime.CompletedAt = entry.CompletedAt
	anime.Ep.Number = nextEpisodeFromProgress(entry.Progress)
}

// askAboutCompletedShow asks what to do with a show already on Completed. It
// reports whether the show starts over from episode 1, and false for proceed
// when the viewer goes back to the menu.
func askAboutCompletedShow(userConfig *Config, anime *Anime, user *User) (restart, proceed bool) {
	Out("This anime is completed. Start rewatch from episode 1? Continue without updating tracker? Open details?")
	picked, err := promptSelect([]SelectionOption{
		{Key: "rewatch", Label: "Start rewatch from episode 1", Icon: icons.Rewatching},
		{Key: "continue", Label: "Continue without updating tracker", Icon: icons.Play},
		{Key: "details", Label: "Open details", Icon: icons.Info},
	})
	if err != nil {
		Log(fmt.Sprintf("Error in completed anime prompt: %v", err))
		Exit(fmt.Errorf("Failed to select completed anime action"))
	}

	switch picked.Key {
	case "rewatch":
		if err := StartAnimeRewatch(user.Token, *anime); err != nil {
			Log(fmt.Sprintf("Error starting anime rewatch: %v", err))
			Exit(fmt.Errorf("Failed to move anime to rewatching"))
		}
		startOver(anime, true)
		Out("Moved anime to Rewatching and restarting from episode 1.")
		if err := RefreshUserAnimeList(userConfig, user); err != nil {
			Log("Error refreshing anime list: " + err.Error())
			Exit(err)
		}
		return true, true
	case "continue":
		// Watched again for its own sake: the tracker keeps the show as
		// completed and is not written to.
		startOver(anime, false)
		Out("Starting episode 1 without updating your tracker.")
		return true, true
	case "details":
		openInDefaultBrowser(aniListPage(anime.AnilistId))
	case "-1":
		Exit(nil)
	}
	anime.Ep.ContinueLast = false
	return false, false
}

// startOver sets the show back to the start of episode 1, as a rewatch the
// tracker counts or as a session it does not hear about.
func startOver(anime *Anime, rewatch bool) {
	anime.Rewatching = rewatch
	anime.SkipRemoteSync = !rewatch
	anime.Ep.Number = 1
	anime.Ep.Player.PlaybackTime = 0
	anime.Ep.Resume = false
}

// needsProviderSearch reports whether the show has to be looked up on the
// providers again rather than played from the one its history row names.
func needsProviderSearch(userConfig *Config, anime *Anime, stored *Anime) bool {
	switch {
	case stored == nil, stored.ProviderId == "":
		return true
	case stored.ProviderName == "":
		return false
	case !ProviderStackContains(userConfig, stored.ProviderName):
		return true
	case !providerSuitsShow(userConfig, anime, stored.ProviderName):
		// Mapped before AdultContent routed the show elsewhere, often onto
		// a same-named series on a general host.
		return true
	}
	return false
}

// applyStoredProgress weighs the watch history's row for the show against the
// tracker's progress to settle the episode to start, and the position in it.
func applyStoredProgress(userConfig *Config, anime *Anime, user *User, stored *Anime, entry *Entry, remapped, restarting bool) {
	if !remapped {
		anime.ProviderId = stored.ProviderId
		anime.ProviderName = stored.ProviderName
	}
	anime.Ep.Player.PlaybackTime = stored.Ep.Player.PlaybackTime
	if anime.Ep.Number == stored.Ep.Number {
		anime.Ep.Resume = true
	}

	trackerNext := nextEpisodeFromProgress(entry.Progress)
	switch {
	case stored.Ep.Number > trackerNext:
		chooseBetweenLocalAndTracker(userConfig, anime, user, stored.Ep.Number, trackerNext)
	case stored.Ep.Number < trackerNext && !restarting:
		// The tracker is ahead: episodes were marked watched by hand,
		// or on another device. The local row is simply stale, and
		// following it would offer an episode already seen.
		Log(fmt.Sprintf("Local history episode (%d) is behind the tracker (%d); using the tracker", stored.Ep.Number, trackerNext))
		anime.Ep.Number = trackerNext
		anime.Ep.Player.PlaybackTime = 0
		anime.Ep.Resume = false
	default:
		anime.Ep.Number = stored.Ep.Number
	}
}

// chooseBetweenLocalAndTracker asks which episode to go on from when the watch
// history is further on than the tracker, which happens when an update to
// the tracker failed or was skipped.
func chooseBetweenLocalAndTracker(userConfig *Config, anime *Anime, user *User, localNext, trackerNext int) {
	tracker := RemoteTrackingDisplayName(userConfig)
	Log(fmt.Sprintf("Local history episode (%d) is ahead of %s episode (%d), prompting user", localNext, tracker, trackerNext))
	Out(fmt.Sprintf("Otakase has episode %d. %s has episode %d. Pick one.", localNext, tracker, trackerNext))
	picked, err := DynamicSelect([]SelectionOption{
		{Key: "update_upstream", Label: fmt.Sprintf("Use %s episode %d", DisplayName, localNext)},
		{Key: "use_anilist", Label: fmt.Sprintf("Use %s episode %d", tracker, trackerNext)},
	})
	if err != nil {
		Log("Error in episode conflict selection: " + err.Error())
		return
	}

	switch picked.Key {
	case "-1":
		Exit(nil)
	case "update_upstream":
		// The history holds the next episode to watch, so the progress is
		// the one before it.
		progress := localNext - 1
		if err := UpdateAnimeProgress(user.Token, anime.AnilistId, progress); err != nil {
			Log(fmt.Sprintf("Error updating %s progress to %d: %v", tracker, progress, err))
		} else {
			Log(fmt.Sprintf("Updated %s progress to %d", tracker, progress))
		}
		anime.Ep.Number = localNext
		anime.Ep.Resume = true
	case "use_anilist":
		anime.Ep.Number = trackerNext
		anime.Ep.Player.PlaybackTime = 0
		anime.Ep.Resume = false
	}
}

// onWatchingList reports whether the show is on Watching or Rewatching.
func onWatchingList(list AnimeList, anilistID int) bool {
	for _, bucket := range [][]Entry{list.Watching, list.Rewatching} {
		for _, entry := range bucket {
			if entry.Media.ID == anilistID {
				return true
			}
		}
	}
	return false
}

// offerWatchingList offers to put a show that is not being watched yet on
// the Watching list. It reports false when the viewer goes back to the menu.
func offerWatchingList(userConfig *Config, anime *Anime, user *User) bool {
	if !ShouldWriteRemoteTracking(userConfig, anime) || anime.Rewatching || onWatchingList(user.AnimeList, anime.AnilistId) {
		return true
	}
	picked, err := DynamicSelect([]SelectionOption{
		{Key: "yes", Label: "Add to watching list", Icon: icons.Add},
		{Key: "no", Label: "Continue without adding", Icon: icons.Play},
	})
	if err != nil {
		Log("Error in selection: " + err.Error())
		Exit(err)
	}

	switch picked.Key {
	case "yes":
		if err := AddAnimeToWatchingList(anime.AnilistId, user.Token); err != nil {
			Log("Error adding anime to watching list: " + err.Error())
			Exit(err)
		}
		if err := RefreshUserAnimeList(userConfig, user); err != nil {
			Log("Error refreshing anime list: " + err.Error())
			Exit(err)
		}
	case "-1":
		Exit(nil)
	case "-2":
		backToMenu(anime)
		return false
	}
	return true
}

// settleEpisodeCount fills in a total the tracker did not have. Each source
// is asked only when the ones before it had nothing: the tracker's own
// details again, the mapped provider, the whole provider stack, and last the
// provider's search results. It reports whether it got as far as the search.
func settleEpisodeCount(userConfig *Config, anime *Anime, user *User, query string) bool {
	if anime.TotalEpisodes == 0 {
		details, err := GetAnimeDataByID(anime.AnilistId, user.Token)
		Log(details)
		if err != nil {
			Log(fmt.Sprintf("Error getting updated anime data: %v", err))
		} else {
			anime.TotalEpisodes = details.TotalEpisodes
			Log(fmt.Sprintf("Updated total episodes: %d", anime.TotalEpisodes))
		}
	}

	if anime.TotalEpisodes == 0 {
		Out("AniList/MAL did not return total episodes. Attempting to retrieve from provider episode list.")
		providerName, providerID := AnimeProviderID(anime)
		total, err := GetProviderTotalEpisodes(QualifyProviderID(providerName, providerID), userConfig.SubOrDub)
		if err != nil {
			Log(fmt.Sprintf("Failed to retrieve total episodes from provider episode list: %v", err))
		} else {
			anime.TotalEpisodes = total
			Out(fmt.Sprintf("Retrieved total episodes from provider: %d", anime.TotalEpisodes))
		}
	}

	if anime.TotalEpisodes == 0 {
		Out("Attempting to determine total episodes from the full provider stack.")
		stackQuery := animeSearchTitle(anime)
		if strings.TrimSpace(stackQuery) == "" {
			stackQuery = query
		}
		total, err := determineProviderTotalEpisodes(userConfig, stackQuery, anime, userConfig.SubOrDub)
		if err != nil {
			Log(fmt.Sprintf("Failed to determine total episodes from provider stack: %v", err))
		} else {
			anime.TotalEpisodes = total
			Out(fmt.Sprintf("Retrieved total episodes from provider stack: %d", anime.TotalEpisodes))
		}
	}

	if anime.TotalEpisodes > 0 {
		return false
	}
	totalFromSearchResults(userConfig, anime, query)
	return true
}

// episodeCountLabel finds the episode count a provider's search result shows
// in its label, e.g. "Frieren (28 episodes)".
var episodeCountLabel = regexp.MustCompile(`\((\d+) episodes\)`)

// episodeCountFromLabel reads the episode count out of a search result's label.
func episodeCountFromLabel(label string) (int, bool) {
	match := episodeCountLabel.FindStringSubmatch(label)
	if len(match) < 2 {
		return 0, false
	}
	count, err := strconv.Atoi(match[1])
	return count, err == nil
}

// totalFromSearchResults is the last resort for the total: the episode count
// the mapped provider's own search result shows.
func totalFromSearchResults(userConfig *Config, anime *Anime, query string) {
	Out("Attempting to retrieve total episodes from anime search results.")
	results, err := SearchAnime(query, userConfig.SubOrDub)
	if err != nil {
		Out(fmt.Sprintf("Failed to retrieve anime list: %v", err))
		return
	}
	for _, result := range results {
		providerName, providerID, ok := ParseProviderQualifiedID(result.Key)
		if !ok {
			providerName = configuredProviderNames(userConfig)[0]
			providerID = result.Key
		}
		if providerID != anime.ProviderId || providerName != CurrentAnimeProviderName(anime) {
			continue
		}
		if count, ok := episodeCountFromLabel(result.Label); ok {
			anime.TotalEpisodes = count
			Out(fmt.Sprintf("Retrieved total episodes: %d", anime.TotalEpisodes))
			return
		}
	}
}

// settleStartEpisode makes sure the episode to start exists, asking when the
// total is still unknown or is behind the episode chosen. searched is
// settleEpisodeCount's answer: after a search the episode is the one after
// the tracker's progress, whatever the history said.
func settleStartEpisode(userConfig *Config, anime *Anime, entry *Entry, link OpenLink, searched bool) {
	if !searched {
		if anime.TotalEpisodes < anime.Ep.Number && (link.Episode == 0 || link.Episode > anime.TotalEpisodes) {
			askToStartFromBeginning(userConfig, anime)
		}
		return
	}
	switch {
	case anime.TotalEpisodes > 0:
		anime.Ep.Number = nextEpisodeFromProgress(entry.Progress)
		return
	case link.Episode > 0:
		// The link already says which episode.
		anime.Ep.Number = link.Episode
		return
	}

	// Not knowing the total is a reason to ask which episode to
	// start from, not a reason to insist on an answer: the tracker
	// knows the progress either way. Backing out carries on from
	// it, which is what happens when the total is known, rather
	// than closing the program over a question.
	fromProgress := nextEpisodeFromProgress(entry.Progress)
	Out("Still unable to determine total episodes.")
	episode, cancelled, err := promptEpisodeCancelable(userConfig, "Episode",
		"Which episode do you want to start from?",
		fmt.Sprintf("a number · esc to continue from %d, your %s progress", fromProgress, RemoteTrackingDisplayName(userConfig)))
	if err != nil {
		Log("Invalid episode input: " + err.Error())
		cancelled = true
	}
	if cancelled {
		episode = fromProgress
	}
	anime.Ep.Number = episode
}

// askToStartFromBeginning handles an episode past the show's total, which a
// history from a longer listing or a changed total leaves behind.
func askToStartFromBeginning(userConfig *Config, anime *Anime) {
	Log(fmt.Sprintf("Weird case: anime.TotalEpisodes < anime.Ep.Number: %v < %v", anime.TotalEpisodes, anime.Ep.Number))
	// The question has an answer either way, so backing out of it is
	// the same as declining: start at the last episode. Only the rofi
	// half used to close the program over a failed read.
	answer, cancelled, err := promptCancelable(userConfig, "Episode",
		"Start this anime from the beginning?",
		fmt.Sprintf("y or n · esc to start at episode %d", anime.TotalEpisodes))
	if err != nil {
		Log("Error getting user input: " + err.Error())
		cancelled = true
	}
	if !cancelled && isAffirmativeAnswer(answer) {
		anime.Ep.Number = 1
	} else {
		anime.Ep.Number = anime.TotalEpisodes
	}
}
