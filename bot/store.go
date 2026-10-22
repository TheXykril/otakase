package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// store keeps what has to survive a restart (warnings, voice rooms,
// suggestions, linked AniList accounts) in a small JSON file in the data directory.
type store struct {
	mu   sync.Mutex
	path string
	data storeData
}

type storeData struct {
	Warnings map[string][]warning `json:"warnings"`
	Rooms    map[string]room      `json:"rooms"` // voice channel id → room
	// Suggestions by message id (also the id of its thread).
	Suggestions map[string]suggestion `json:"suggestions"`
	// Shows each member follows for episode DMs, and when airing was
	// last checked.
	Follows        map[string][]followedShow `json:"follows,omitempty"`
	FollowsChecked time.Time                 `json:"follows_checked,omitzero"`
	// Linked AniList accounts by Discord user id.
	AniList map[string]anilistLink `json:"anilist,omitempty"`
	// Today's airing board, edited through the day.
	Airing *airingBoard `json:"airing,omitempty"`
	// Linked Otakase apps by token hash.
	AppLinks map[string]appLink `json:"app_links,omitempty"`
	// Otakase watch time and Tester role by Discord user id.
	AppStats map[string]appStats `json:"app_stats,omitempty"`
	// What waits for each linked app (by token hash), see appinbox.go.
	AppInbox map[string][]inboxItem `json:"app_inbox,omitempty"`
	// Support posts made from the app by thread id, for who may mark them solved.
	AppReports map[string]appReport `json:"app_reports,omitempty"`
	// Bot version that ran last, for the update note to staff.
	BotVersion string `json:"bot_version,omitempty"`
	// Runtime settings changed with /settings, over the add-on options.
	Settings map[string]string `json:"settings,omitempty"`
	// The pinned known issues post in the support forum.
	KnownIssuesPost string `json:"known_issues_post,omitempty"`
	// Recent support posts, newest first, for similar post links.
	SupportPosts []supportPost `json:"support_posts,omitempty"`
	// Support posts reported on GitHub by thread id, until the fix ships.
	BugReports map[string]bugReport `json:"bug_reports,omitempty"`
	// Planned watch parties with their RSVPs, by event id.
	Parties map[string]watchParty `json:"parties,omitempty"`
	// Spoiler guard mode: off, nudge or hide ("" = nudge).
	SpoilerGuard string `json:"spoiler_guard,omitempty"`
	// Guess the anime points by Discord user id.
	Guess map[string]guessScore `json:"guess,omitempty"`
	// Chat XP by Discord user id.
	XP map[string]int `json:"xp,omitempty"`
	// The pick-your-roles card.
	RolesCard *rolesCard `json:"roles_card,omitempty"`
	// Warnings and timeouts members can appeal, by appeal id.
	Appeals map[string]appeal `json:"appeals,omitempty"`
	// Join verification mode and the members waiting at the check.
	Verify *verifyData `json:"verify,omitempty"`
	// The last season preview posted.
	SeasonPreview *seasonPost `json:"season_preview,omitempty"`
	// Helper points by Discord user id.
	Helpers map[string]helperStat `json:"helpers,omitempty"`
	// Automatic answers with Helped / Didn't help buttons by message id,
	// and the votes per answer.
	Answers     map[string]answerMsg   `json:"answers,omitempty"`
	AnswerStats map[string]answerTally `json:"answer_stats,omitempty"`
	// Support posts made with /support: thread id → member who asked.
	FormPosts map[string]string `json:"form_posts,omitempty"`
	// Texts changed on the dashboard (welcome DM, FAQ answers…) by key.
	Texts map[string]string `json:"texts,omitempty"`
	// Notes staff added to the top of the known issues post.
	KnownNotes []string `json:"known_notes,omitempty"`
	// The bot's rotating statuses, when staff replaced the built-in ones.
	Statuses []string `json:"statuses,omitempty"`
	// Level → role id, when staff picked the level roles by hand.
	LevelRoles map[int]string `json:"level_roles,omitempty"`
	// Points from the other games by Discord user id.
	Games map[string]gameScore `json:"games,omitempty"`
	// The last week whose winner got the Game Champion role, and who did.
	ChampionWeek string   `json:"champion_week,omitempty"`
	Champions    []string `json:"champions,omitempty"`
	// Today's daily puzzle, everyone's tries at it and their daily stats.
	Daily      *dailyPuzzle          `json:"daily,omitempty"`
	DailyPlays map[string]dailyPlay  `json:"daily_plays,omitempty"`
	DailyStats map[string]dailyStats `json:"daily_stats,omitempty"`
	// Emoji riddles members added, and the built-in ones staff turned off.
	Riddles    []riddle `json:"riddles,omitempty"`
	RiddlesOff []string `json:"riddles_off,omitempty"`
	// Watch bingo points given this month by Discord user id.
	Bingo map[string]bingoProgress `json:"bingo,omitempty"`
	// Members on a break from games (no playing, no points), by user id.
	GameBans    map[string]gameBan `json:"game_bans,omitempty"`
	HiddenNames map[string]bool    `json:"hidden_names,omitempty"` // shown as Player N in the games
	GamesOff    []string           `json:"games_off,omitempty"`    // games staff turned off
	// Linked role connections (OAuth2 tokens, last values pushed) by Discord user id.
	LinkedRoles map[string]linkedRole `json:"linked_roles,omitempty"`
	// Latest release each member was DMed about, by Discord user id.
	UpdateNudges map[string]string `json:"update_nudges,omitempty"`
	// Latest release each linked app got an update notice for, by token hash.
	AppNudges map[string]string `json:"app_nudges,omitempty"`
	// Watch badges, streaks and year wraps from heartbeats by Discord user id.
	WatchStats map[string]watchStats `json:"watch_stats,omitempty"`
	// The server club's show and who caught up, see watchclub.go.
	Club *watchClub `json:"club,omitempty"`
	// Shared watch time per show by ISO week, then AniList id.
	ServerWatch map[string]map[int]serverShow `json:"server_watch,omitempty"`
	// The last week whose most watched shows were posted.
	ServerWatchPosted string `json:"server_watch_posted,omitempty"`
	// Today's featured anime, picked once a day.
	Featured *featuredPick `json:"featured,omitempty"`
	// Each member's time with today's featured anime and its bonus.
	FeaturedWatch map[string]featuredWatch `json:"featured_watch,omitempty"`
	// Otakase settings backups by Discord user id, see appbackup.go.
	AppBackups map[string]appBackup `json:"app_backups,omitempty"`
	// Cheat code hunts by Discord user id, and the codes they found, see
	// cheathunt.go.
	CheatHunts map[string]cheatHunt `json:"cheat_hunts,omitempty"`
	CheatCodes map[string]cheatCode `json:"cheat_codes,omitempty"`
	// Time capsule messages by id, and the last year the capsule was opened
	// in the server, see capsule.go.
	Capsules      map[string]capsuleNote `json:"capsules,omitempty"`
	CapsuleOpened int                    `json:"capsule_opened,omitempty"`
}

type warning struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	Reason string    `json:"reason"`
}

type room struct {
	Owner   string    `json:"owner"`
	Created time.Time `json:"created"`
	Used    bool      `json:"used"`
	// Watch party rooms: the event they belong to, and kept until then
	// even when empty.
	Event     string    `json:"event,omitempty"`
	KeepUntil time.Time `json:"keep_until,omitempty"`
	Panel     string    `json:"panel,omitempty"` // room controls message
}

// dataDir is OTAKASE_DATA_DIR, else /data (the Home Assistant add-on's
// persistent folder) when it exists, else the working directory.
func dataDir() string {
	if d := os.Getenv("OTAKASE_DATA_DIR"); d != "" {
		return d
	}
	if fi, err := os.Stat("/data"); err == nil && fi.IsDir() {
		return "/data"
	}
	return "."
}

func openStore(path string) *store {
	st := &store{path: path}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &st.data); err != nil {
			log.Printf("%s: %v", path, err)
		}
	}
	fillMaps(&st.data)
	return st
}

// fillMaps makes the maps the code writes to without checking.
func fillMaps(d *storeData) {
	if d.Warnings == nil {
		d.Warnings = map[string][]warning{}
	}
	if d.Rooms == nil {
		d.Rooms = map[string]room{}
	}
	if d.Suggestions == nil {
		d.Suggestions = map[string]suggestion{}
	}
	if d.AppLinks == nil {
		d.AppLinks = map[string]appLink{}
	}
	if d.AppStats == nil {
		d.AppStats = map[string]appStats{}
	}
	if d.AppReports == nil {
		d.AppReports = map[string]appReport{}
	}
	if d.Parties == nil {
		d.Parties = map[string]watchParty{}
	}
	if d.AniList == nil {
		d.AniList = map[string]anilistLink{}
	}
	if d.BugReports == nil {
		d.BugReports = map[string]bugReport{}
	}
}

// restore replaces everything with a backup, keeping a copy of what was
// there next to the data file.
func (st *store) restore(raw []byte) error {
	var d storeData
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}
	fillMaps(&d)
	st.mu.Lock()
	old, _ := json.MarshalIndent(st.data, "", "  ")
	st.mu.Unlock()
	if err := os.WriteFile(st.path+".before-restore", old, 0o600); err != nil {
		return fmt.Errorf("keeping a copy of the current data: %w", err)
	}
	st.update(func(cur *storeData) { *cur = d })
	return nil
}

// backup is the data as the file holds it.
func (st *store) backup() []byte {
	st.mu.Lock()
	defer st.mu.Unlock()
	b, _ := json.MarshalIndent(st.data, "", "  ")
	return b
}

// update runs fn with the lock held and saves the result.
func (st *store) update(fn func(d *storeData)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.data)
	b, _ := json.MarshalIndent(st.data, "", "  ")
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("saving %s: %v", st.path, err)
		return
	}
	if err := os.Rename(tmp, st.path); err != nil {
		log.Printf("saving %s: %v", st.path, err)
	}
}

func (st *store) view(fn func(d *storeData)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.data)
}

func storePath() string { return filepath.Join(dataDir(), "otakase-bot.json") }
