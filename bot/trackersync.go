package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Members who link both AniList (with access) and MyAnimeList get the two
// lists kept in line by the bot, the way the app does at launch: for every
// show on both lists that the trackers disagree on (status or episodes
// watched), the newer change is written to the other tracker. It runs in the
// background when a member opens the Activity or the watch room's picker, at
// most once per trackerSyncEvery. Shows on only one list are left alone:
// copying whole lists over is the app's tracker sync action, which asks first.

// trackerSyncEvery is how often one member's lists are compared.
const trackerSyncEvery = 30 * time.Minute

// trackerSyncMaxWrites caps the writes of one run; the rest wait for the next.
const trackerSyncMaxWrites = 100

// trackerSyncPause spaces the writes out: both trackers rate limit, and
// MyAnimeList stalls a throttled write instead of refusing it.
var trackerSyncPause = func() { time.Sleep(500 * time.Millisecond) }

// trackerEntry is a show on one tracker's list, in AniList's status names.
type trackerEntry struct {
	aniID    int // AniList id, from AniList's side
	status   string
	progress int
	updated  int64
}

// trackerWrite is a list entry to set on one tracker.
type trackerWrite struct {
	aniID, malID int
	status       string
	progress     int
}

// planTrackerSync compares the two lists, keyed by MyAnimeList id, and says
// what to write where: the newer entry wins, a tie or a match writes nothing.
func planTrackerSync(anilist, mal map[int]trackerEntry) (toAniList, toMAL []trackerWrite) {
	for malID, a := range anilist {
		m, ok := mal[malID]
		if !ok || a.aniID == 0 || a.status == m.status && a.progress == m.progress {
			continue
		}
		switch {
		case a.updated > m.updated:
			toMAL = append(toMAL, trackerWrite{aniID: a.aniID, malID: malID, status: a.status, progress: a.progress})
		case m.updated > a.updated:
			toAniList = append(toAniList, trackerWrite{aniID: a.aniID, malID: malID, status: m.status, progress: m.progress})
		}
	}
	return toAniList, toMAL
}

// anilistFullList reads a member's whole AniList anime list, keyed by
// MyAnimeList id; shows AniList has no MyAnimeList id for can't be matched.
var anilistFullList = func(token string, userID int) (map[int]trackerEntry, error) {
	var r struct {
		Data struct {
			Collection struct {
				Lists []struct {
					Entries []struct {
						MediaID   int    `json:"mediaId"`
						Status    string `json:"status"`
						Progress  int    `json:"progress"`
						UpdatedAt int64  `json:"updatedAt"`
						Media     struct {
							IDMal int `json:"idMal"`
						} `json:"media"`
					} `json:"entries"`
				} `json:"lists"`
			} `json:"MediaListCollection"`
		} `json:"data"`
	}
	query := `query ($id: Int) { MediaListCollection(userId: $id, type: ANIME) { lists { entries { mediaId status progress updatedAt media { idMal } } } } }`
	if err := anilistSocialPost(token, query, map[string]any{"id": userID}, &r); err != nil {
		return nil, tokenErr(err)
	}
	out := map[int]trackerEntry{}
	for _, l := range r.Data.Collection.Lists {
		for _, e := range l.Entries {
			if e.Media.IDMal > 0 {
				out[e.Media.IDMal] = trackerEntry{aniID: e.MediaID, status: e.Status, progress: e.Progress, updated: e.UpdatedAt}
			}
		}
	}
	return out, nil
}

// malToAniListStatus is MyAnimeList's statuses in AniList's names.
var malToAniListStatus = map[string]string{
	"watching": "CURRENT", "completed": "COMPLETED", "on_hold": "PAUSED", "dropped": "DROPPED", "plan_to_watch": "PLANNING",
}

// malFullList reads a member's whole MyAnimeList anime list.
var malFullList = func(token string) (map[int]trackerEntry, error) {
	out := map[int]trackerEntry{}
	fields := url.QueryEscape("list_status{status,num_episodes_watched,is_rewatching,updated_at}")
	for offset := 0; offset < 10000; offset += 1000 {
		var r struct {
			Data []struct {
				Node struct {
					ID int `json:"id"`
				} `json:"node"`
				ListStatus struct {
					Status     string `json:"status"`
					Watched    int    `json:"num_episodes_watched"`
					Rewatching bool   `json:"is_rewatching"`
					UpdatedAt  string `json:"updated_at"`
				} `json:"list_status"`
			} `json:"data"`
			Paging struct {
				Next string `json:"next"`
			} `json:"paging"`
		}
		path := "/users/@me/animelist?nsfw=true&limit=1000&offset=" + strconv.Itoa(offset) + "&fields=" + fields
		if err := malGet(token, path, &r); err != nil {
			return nil, err
		}
		for _, e := range r.Data {
			status := malToAniListStatus[e.ListStatus.Status]
			if e.ListStatus.Rewatching {
				status = "REPEATING"
			}
			var at int64
			if t, err := time.Parse(time.RFC3339, e.ListStatus.UpdatedAt); err == nil {
				at = t.Unix()
			}
			out[e.Node.ID] = trackerEntry{status: status, progress: e.ListStatus.Watched, updated: at}
		}
		if r.Paging.Next == "" {
			break
		}
	}
	return out, nil
}

// anilistSetEntry writes a status and progress to a member's AniList.
var anilistSetEntry = func(token string, w trackerWrite) error {
	var r struct{}
	if err := anilistSocialPost(token, `mutation ($id: Int, $p: Int, $s: MediaListStatus) { SaveMediaListEntry(mediaId: $id, progress: $p, status: $s) { id } }`,
		map[string]any{"id": w.aniID, "p": w.progress, "s": w.status}, &r); err != nil {
		return tokenErr(err)
	}
	return nil
}

// malSetEntry writes a status and episodes watched to a member's MyAnimeList.
// A rewatch is Watching there with the rewatching flag on.
var malSetEntry = func(token string, w trackerWrite) error {
	status := "watching"
	for mal, al := range malToAniListStatus {
		if al == w.status {
			status = mal
		}
	}
	form := url.Values{"status": {status}, "num_watched_episodes": {strconv.Itoa(w.progress)}, "is_rewatching": {strconv.FormatBool(w.status == "REPEATING")}}
	req, _ := http.NewRequest("PATCH", malAPI+"/anime/"+strconv.Itoa(w.malID)+"/my_list_status", strings.NewReader(form.Encode()))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := activityClient.Do(req)
	if err != nil {
		return errors.New("couldn't reach MyAnimeList")
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list update: MyAnimeList answered %s", resp.Status)
	}
	return nil
}

var trackerSyncs = struct {
	sync.Mutex
	last    map[string]time.Time
	running map[string]bool
}{last: map[string]time.Time{}, running: map[string]bool{}}

// syncTrackersSoon starts a sync of user's two lists in the background,
// unless one ran lately, is running, or there is nothing to sync.
func (b *bot) syncTrackersSoon(user string) {
	if !b.settingBool("tracker_sync") {
		return
	}
	var al anilistLink
	var mal malLink
	var hasAL, hasMAL bool
	b.store.view(func(d *storeData) {
		al, hasAL = d.AniList[user]
		mal, hasMAL = d.MAL[user]
	})
	if !hasAL || !al.Verified || al.Token == "" || al.ID == 0 || !hasMAL || !b.malReady() {
		return
	}
	now := appNow()
	trackerSyncs.Lock()
	if trackerSyncs.running[user] || now.Sub(trackerSyncs.last[user]) < trackerSyncEvery {
		trackerSyncs.Unlock()
		return
	}
	trackerSyncs.running[user], trackerSyncs.last[user] = true, now
	trackerSyncs.Unlock()
	go func() {
		defer func() {
			trackerSyncs.Lock()
			delete(trackerSyncs.running, user)
			trackerSyncs.Unlock()
		}()
		if n, err := b.syncTrackers(user, al, mal); err != nil {
			logWatchErr("tracker sync", user, err)
		} else if n > 0 {
			log.Printf("tracker sync of member %s: %d update(s)", user, n)
		}
	}()
}

// syncTrackers brings a member's AniList and MyAnimeList in line and says
// how many entries it wrote.
func (b *bot) syncTrackers(user string, al anilistLink, mal malLink) (int, error) {
	l, err := b.freshMAL(user, mal)
	if err != nil {
		return 0, err
	}
	aniList, err := anilistFullList(al.Token, al.ID)
	if err != nil {
		return 0, err
	}
	malList, err := malFullList(l.AccessToken)
	if err != nil {
		return 0, err
	}
	toAniList, toMAL := planTrackerSync(aniList, malList)
	written := 0
	for _, w := range toAniList {
		if written >= trackerSyncMaxWrites {
			break
		}
		if err := anilistSetEntry(al.Token, w); err != nil {
			return written, err
		}
		written++
		trackerSyncPause()
	}
	for _, w := range toMAL {
		if written >= trackerSyncMaxWrites {
			break
		}
		if err := malSetEntry(l.AccessToken, w); err != nil {
			return written, err
		}
		written++
		trackerSyncPause()
	}
	if written > 0 {
		// The picker's lists read the trackers again next time.
		watchListCache.Lock()
		delete(watchListCache.m, user)
		watchListCache.Unlock()
	}
	return written, nil
}
