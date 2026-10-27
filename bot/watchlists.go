package main

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The watch room's picker shows the host their own list under the
// search: Continue, every show they are watching, at the next episode, from
// where Otakase or the room last stopped when there is a saved
// [otakase:ep@secs] tag for it. Shows caught up with what has aired come
// after the rest, at the latest episode. A show moved out of Watching on
// either tracker leaves it, saved position or not; the note under the list
// says which shows were left out and why. The lists come from the
// member's linked AniList (read with their own token, so a private list
// shows to them) and MyAnimeList; nobody else's lists are ever read.

// watchListMax is how many shows the list shows.
const watchListMax = 50

// watchListTTL keeps a member's lists for a moment, so flipping tabs or
// reopening the picker doesn't ask the trackers again.
const watchListTTL = time.Minute

// watchListItem is a show on a member's list, at the episode to play.
type watchListItem struct {
	watchShow
	Next     int  `json:"next"`                // episode to play
	Resume   int  `json:"resume,omitempty"`    // seconds into Next to start from
	Progress int  `json:"progress,omitempty"`  // episodes watched
	CaughtUp bool `json:"caught_up,omitempty"` // seen all that's out; Next is the latest episode
	updated  int64
}

type watchLists struct {
	AniList  bool            `json:"anilist"`
	MAL      bool            `json:"mal"`
	Continue []watchListItem `json:"continue"`
	Note     string          `json:"note,omitempty"`
}

// watchListEntry is one list entry as a tracker has it.
type watchListEntry struct {
	show     watchShow
	status   string // the show's airing status
	progress int
	notes    string
	updated  int64
}

const watchListMediaFields = `id idMal isAdult episodes format seasonYear status title { romaji english } coverImage { large } nextAiringEpisode { episode }`

type watchListMediaJSON struct {
	watchMediaJSON
	Status string `json:"status"`
}

// anilistWatchList reads what a member is watching on AniList (CURRENT
// and REPEATING), most recently updated first.
var anilistWatchList = func(token string, userID int) ([]watchListEntry, error) {
	statuses := []string{"CURRENT", "REPEATING"}
	var r struct {
		Data struct {
			Page struct {
				MediaList []struct {
					Progress  int                `json:"progress"`
					Notes     *string            `json:"notes"`
					UpdatedAt int64              `json:"updatedAt"`
					Media     watchListMediaJSON `json:"media"`
				} `json:"mediaList"`
			} `json:"Page"`
		} `json:"data"`
	}
	query := `query ($id: Int, $s: [MediaListStatus], $n: Int) { Page(perPage: $n) { mediaList(userId: $id, type: ANIME, status_in: $s, sort: UPDATED_TIME_DESC) {
		progress notes updatedAt media { ` + watchListMediaFields + ` } } } }`
	if err := anilistSocialPost(token, query, map[string]any{"id": userID, "s": statuses, "n": watchListMax}, &r); err != nil {
		return nil, tokenErr(err)
	}
	out := []watchListEntry{}
	for _, e := range r.Data.Page.MediaList {
		notes := ""
		if e.Notes != nil {
			notes = *e.Notes
		}
		out = append(out, watchListEntry{show: e.Media.show(), status: e.Media.Status, progress: e.Progress, notes: notes, updated: e.UpdatedAt})
	}
	return out, nil
}

// malWatchList reads what a member is watching on MyAnimeList, with the
// shows found on AniList so the room can play them.
var malWatchList = func(token string) ([]watchListEntry, error) {
	status := "watching"
	var r struct {
		Data []struct {
			Node struct {
				ID int `json:"id"`
			} `json:"node"`
			ListStatus struct {
				Watched   int    `json:"num_episodes_watched"`
				Comments  string `json:"comments"`
				UpdatedAt string `json:"updated_at"`
			} `json:"list_status"`
		} `json:"data"`
	}
	fields := url.QueryEscape("list_status{num_episodes_watched,comments,updated_at}")
	path := "/users/@me/animelist?status=" + status + "&sort=list_updated_at&nsfw=true&limit=" + strconv.Itoa(watchListMax) + "&fields=" + fields
	if err := malGet(token, path, &r); err != nil {
		return nil, err
	}
	if len(r.Data) == 0 {
		return []watchListEntry{}, nil
	}
	ids := make([]int, 0, len(r.Data))
	for _, e := range r.Data {
		ids = append(ids, e.Node.ID)
	}
	var shows struct {
		Data struct {
			Page struct {
				Media []watchListMediaJSON `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := anilistSocialPost("", `query ($ids: [Int], $n: Int) { Page(perPage: $n) { media(idMal_in: $ids, type: ANIME) { `+watchListMediaFields+` } } }`,
		map[string]any{"ids": ids, "n": watchListMax}, &shows); err != nil {
		return nil, err
	}
	byMal := map[int]watchListMediaJSON{}
	for _, m := range shows.Data.Page.Media {
		byMal[m.IDMal] = m
	}
	out := []watchListEntry{}
	for _, e := range r.Data {
		m, ok := byMal[e.Node.ID]
		if !ok {
			// Not on AniList, so the room can't look it up; counted in the note.
			out = append(out, watchListEntry{show: watchShow{MalID: e.Node.ID}})
			continue
		}
		var updated int64
		if t, err := time.Parse(time.RFC3339, e.ListStatus.UpdatedAt); err == nil {
			updated = t.Unix()
		}
		out = append(out, watchListEntry{show: m.show(), status: m.Status, progress: e.ListStatus.Watched, notes: e.ListStatus.Comments, updated: updated})
	}
	return out, nil
}

// anilistOffWatching is which of shows (AniList ids) are on the member's
// AniList in a list other than Watching and Rewatching.
var anilistOffWatching = func(token string, userID int, shows []int) (map[int]bool, error) {
	off := map[int]bool{}
	if len(shows) == 0 {
		return off, nil
	}
	var r struct {
		Data struct {
			Page struct {
				MediaList []struct {
					MediaID int    `json:"mediaId"`
					Status  string `json:"status"`
				} `json:"mediaList"`
			} `json:"Page"`
		} `json:"data"`
	}
	query := `query ($id: Int, $m: [Int], $n: Int) { Page(perPage: $n) { mediaList(userId: $id, type: ANIME, mediaId_in: $m) { mediaId status } } }`
	if err := anilistSocialPost(token, query, map[string]any{"id": userID, "m": shows, "n": len(shows)}, &r); err != nil {
		return nil, tokenErr(err)
	}
	for _, e := range r.Data.Page.MediaList {
		if e.Status != "CURRENT" && e.Status != "REPEATING" {
			off[e.MediaID] = true
		}
	}
	return off, nil
}

// malOffWatching is which shows (MyAnimeList ids) are on the member's
// MyAnimeList in a list other than Watching.
var malOffWatching = func(token string) (map[int]bool, error) {
	var r struct {
		Data []struct {
			Node struct {
				ID int `json:"id"`
			} `json:"node"`
			ListStatus struct {
				Status string `json:"status"`
			} `json:"list_status"`
		} `json:"data"`
	}
	path := "/users/@me/animelist?nsfw=true&limit=1000&fields=" + url.QueryEscape("list_status{status}")
	if err := malGet(token, path, &r); err != nil {
		return nil, err
	}
	off := map[int]bool{}
	for _, e := range r.Data {
		if e.ListStatus.Status != "watching" {
			off[e.Node.ID] = true
		}
	}
	return off, nil
}

// dropOffWatching leaves out of one tracker's watching entries the shows the
// other tracker has in another list: moved on there, they were put down.
// The titles left out come back too.
func dropOffWatching(entries []watchListEntry, off func(watchShow) bool) (kept []watchListEntry, dropped []string) {
	kept = entries[:0:0]
	for _, e := range entries {
		if off(e.show) {
			dropped = append(dropped, e.show.Title)
			continue
		}
		kept = append(kept, e)
	}
	return kept, dropped
}

// watchResumeIn reads the saved position from notes.
func watchResumeIn(notes string) (ep, secs int) {
	m := watchResumeTag.FindStringSubmatch(notes)
	if m == nil {
		return 0, 0
	}
	ep, _ = strconv.Atoi(m[1])
	secs, _ = strconv.Atoi(m[2])
	return ep, secs
}

// watchListHidden counts the shows on a member's watching lists that
// Continue leaves out, by reason, for the note under it.
type watchListHidden struct {
	notOut   []string // not released yet
	adult    int      // 18+ outside an 18+ room
	notFound int      // on MyAnimeList but not on AniList
}

// mergeWatchList puts the trackers' entries together, one per show: the
// furthest progress, the later saved position, the latest update. Shows the
// room can't play stay out and are counted in hidden: not out yet, 18+
// outside an 18+ room, or missing on AniList. Caught-up shows come last, at
// their latest episode.
func mergeWatchList(lists [][]watchListEntry, adult bool) ([]watchListItem, watchListHidden) {
	type merged struct {
		item          watchListItem
		tagEp, tagSec int
	}
	var hidden watchListHidden
	skipped := map[int]bool{}
	byID := map[int]*merged{}
	var order []int
	for _, list := range lists {
		for _, e := range list {
			switch {
			case e.show.ID == 0:
				hidden.notFound++
				continue
			case e.show.Format == "MUSIC":
				continue
			case e.show.Adult && !adult:
				if !skipped[e.show.ID] {
					hidden.adult++
				}
				skipped[e.show.ID] = true
				continue
			case e.status == "NOT_YET_RELEASED":
				if !skipped[e.show.ID] {
					hidden.notOut = append(hidden.notOut, e.show.Title)
				}
				skipped[e.show.ID] = true
				continue
			}
			ep, secs := watchResumeIn(e.notes)
			m, ok := byID[e.show.ID]
			if !ok {
				m = &merged{item: watchListItem{watchShow: e.show}}
				byID[e.show.ID] = m
				order = append(order, e.show.ID)
			}
			m.item.Progress = max(m.item.Progress, e.progress)
			m.item.updated = max(m.item.updated, e.updated)
			if ep > m.tagEp || ep == m.tagEp && secs > m.tagSec {
				m.tagEp, m.tagSec = ep, secs
			}
		}
	}
	out := []watchListItem{}
	for _, id := range order {
		m := byID[id]
		it := m.item
		it.Next = it.Progress + 1
		if last := it.lastEpisode(); it.Next > last {
			// Caught up: listed anyway, at the latest episode.
			it.Next, it.CaughtUp = last, true
		}
		// The saved position only counts for the episode it names, as in the app.
		if m.tagEp == it.Next && m.tagSec >= watchResumeMin {
			it.Resume = m.tagSec
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CaughtUp != out[j].CaughtUp {
			return !out[i].CaughtUp
		}
		return out[i].updated > out[j].updated
	})
	if len(out) > watchListMax {
		out = out[:watchListMax]
	}
	return out, hidden
}

// watchListNote says which watching shows Continue left out and why.
func watchListNote(h watchListHidden, movedOn map[string][]string) string {
	var parts []string
	for _, tracker := range []string{"AniList", "MyAnimeList"} {
		if t := movedOn[tracker]; len(t) > 0 {
			parts = append(parts, strings.Join(t, ", ")+" (not in Watching on "+tracker+")")
		}
	}
	if len(h.notOut) > 0 {
		parts = append(parts, strings.Join(h.notOut, ", ")+" (not out yet)")
	}
	if h.adult > 0 {
		parts = append(parts, plural(h.adult, "18+ show")+" (only in an 18+ room)")
	}
	if h.notFound > 0 {
		parts = append(parts, plural(h.notFound, "MyAnimeList show")+" (not found on AniList)")
	}
	if len(parts) == 0 {
		return ""
	}
	return "Not listed: " + strings.Join(parts, "; ") + "."
}

type watchListCacheEntry struct {
	at    time.Time
	adult bool
	lists watchLists
}

var watchListCache = struct {
	sync.Mutex
	m map[string]watchListCacheEntry
}{m: map[string]watchListCacheEntry{}}

// userWatchLists reads user's own lists from the trackers they linked.
func (b *bot) userWatchLists(user string, adult bool) watchLists {
	now := time.Now()
	watchListCache.Lock()
	if c, ok := watchListCache.m[user]; ok && c.adult == adult && now.Sub(c.at) < watchListTTL {
		watchListCache.Unlock()
		return c.lists
	}
	watchListCache.Unlock()

	var al anilistLink
	var mal malLink
	var hasAL, hasMAL bool
	b.store.view(func(d *storeData) {
		al, hasAL = d.AniList[user]
		mal, hasMAL = d.MAL[user]
	})
	out := watchLists{AniList: hasAL && al.ID > 0, MAL: hasMAL && b.malReady(), Continue: []watchListItem{}}
	if !out.AniList && !out.MAL {
		return out
	}
	var alWatching, malWatching []watchListEntry
	var alErr, malErr error
	// A proven link reads with the member's token, so a private list
	// shows; otherwise only a public one does.
	alToken, malToken := "", ""
	if out.AniList {
		if al.Verified {
			alToken = al.Token
		}
		if alWatching, alErr = anilistWatchList(alToken, al.ID); alErr != nil {
			logWatchErr("anilist lists", user, alErr)
		}
	}
	if out.MAL {
		l, err := b.freshMAL(user, mal)
		if err == nil {
			malToken = l.AccessToken
			malWatching, err = malWatchList(malToken)
		}
		if malErr = err; err != nil {
			logWatchErr("mal lists", user, err)
		}
	}
	// With both linked, a show watched on one but moved on on the other
	// was put down: the trackers only disagree until the app or the
	// member syncs them.
	movedOn := map[string][]string{}
	if out.AniList && out.MAL && alErr == nil && malErr == nil {
		var ids []int
		for _, e := range malWatching {
			if e.show.ID > 0 {
				ids = append(ids, e.show.ID)
			}
		}
		if off, err := anilistOffWatching(alToken, al.ID, ids); err != nil {
			logWatchErr("anilist list status", user, err)
		} else {
			malWatching, movedOn["AniList"] = dropOffWatching(malWatching, func(s watchShow) bool { return s.ID > 0 && off[s.ID] })
		}
		if off, err := malOffWatching(malToken); err != nil {
			logWatchErr("mal list status", user, err)
		} else {
			alWatching, movedOn["MyAnimeList"] = dropOffWatching(alWatching, func(s watchShow) bool { return s.MalID > 0 && off[s.MalID] })
		}
	}
	var failed []string
	if alErr != nil {
		failed = append(failed, "AniList")
	}
	if malErr != nil {
		failed = append(failed, "MyAnimeList")
	}
	var hidden watchListHidden
	out.Continue, hidden = mergeWatchList([][]watchListEntry{alWatching, malWatching}, adult)
	out.Note = watchListNote(hidden, movedOn)
	for k := range out.Continue {
		if out.Continue[k].Cover != "" {
			out.Continue[k].Cover = "api/watch/cover?u=" + url.QueryEscape(out.Continue[k].Cover)
		}
	}
	if len(failed) > 0 {
		out.Note = strings.TrimSpace("Couldn't read your " + strings.Join(failed, " and ") + " list. Try again in a bit. " + out.Note)
		return out // not kept, so the next look asks again
	}
	watchListCache.Lock()
	watchListCache.m[user] = watchListCacheEntry{at: now, adult: adult, lists: out}
	for k, c := range watchListCache.m {
		if now.Sub(c.at) >= watchListTTL {
			delete(watchListCache.m, k)
		}
	}
	watchListCache.Unlock()
	return out
}

func init() {
	apiHandle("GET /activity/api/watch/lists", (*bot).watchListsAPI)
}

// watchListsAPI is the signed-in member's own lists for the picker.
func (b *bot) watchListsAPI(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	if !watchSearchLimit.allow(user, watchNow()) {
		apiError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	adult := false
	b.watch.mu.Lock()
	if cur := b.watch.room(r.URL.Query().Get("room")); cur != nil {
		adult = cur.adult
	}
	b.watch.mu.Unlock()
	writeJSON(w, http.StatusOK, b.userWatchLists(user, adult))
}
