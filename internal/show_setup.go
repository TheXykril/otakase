package internal

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

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
