package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Where the watch room's episodes come from: the otakase CLI the add-on
// ships finds the episode on the providers (`otakase -stream-info`), the
// same way the app does, without asking anything. Shows are picked from an
// AniList search; watching an episode moves the members' AniList and MAL
// progress on.

const (
	// The fast lookup only has to find one playable link; the full one also
	// asks every language page of the provider.
	watchResolveFastWait = 3 * time.Minute
	watchResolveWait     = 8 * time.Minute
	watchLanguagesWait   = 2 * time.Minute
	watchInfoTTL         = 30 * time.Minute
)

// streamInfo is `otakase -stream-info`'s answer.
type streamInfo struct {
	AnilistID     int                   `json:"anilist_id"`
	Episode       int                   `json:"episode"`
	Title         string                `json:"title"`
	Episodes      int                   `json:"episodes"`
	Provider      string                `json:"provider"`
	ProviderID    string                `json:"provider_id,omitempty"`
	Mode          string                `json:"mode"`
	Streams       []streamLink          `json:"streams"`
	AudioVariants map[string]streamLink `json:"audio_variants,omitempty"`
	Torrents      []struct {
		InfoHash string `json:"info_hash"`
	} `json:"torrents,omitempty"`
}

// watchResolve runs the fast CLI lookup, a variable for tests.
var watchResolve = func(parent context.Context, cli string, anilistID, episode int, dub, adult bool) (streamInfo, error) {
	return watchResolveCLI(parent, cli, anilistID, episode, dub, adult, true)
}

// watchResolveAudio completes the slower language-release lookup after the
// episode starts. It is separate so tests can control its completion.
var watchResolveAudio = func(parent context.Context, cli string, anilistID, episode int, dub, adult bool) (streamInfo, error) {
	return watchResolveCLI(parent, cli, anilistID, episode, dub, adult, false)
}

// watchResolveLanguages asks for the other language releases of a show the
// fast lookup already matched, without searching for it again.
var watchResolveLanguages = func(parent context.Context, cli, provider, providerID string, episode int) (streamInfo, error) {
	return watchRunCLI(parent, cli, watchLanguagesWait, true, "-stream-languages", fmt.Sprintf("%s:%s:%d", provider, providerID, episode))
}

func watchResolveCLI(parent context.Context, cli string, anilistID, episode int, dub, adult, fast bool) (streamInfo, error) {
	args := []string{"-stream-info", fmt.Sprintf("%d:%d", anilistID, episode)}
	if fast {
		args = append(args, "-stream-info-fast")
	}
	if dub {
		args = append(args, "-dub")
	}
	if adult {
		args = append(args, "-stream-adult")
	}
	wait := watchResolveWait
	if fast {
		wait = watchResolveFastWait
	}
	return watchRunCLI(parent, cli, wait, false, args...)
}

// watchRunCLI runs the CLI with a throwaway home and reads its JSON answer.
// An answer may carry no links when only languages were asked for.
func watchRunCLI(parent context.Context, cli string, wait time.Duration, languagesOnly bool, args ...string) (streamInfo, error) {
	ctx, cancel := context.WithTimeout(parent, wait)
	defer cancel()
	home, _ := os.MkdirTemp("", "otakase-watch")
	defer os.RemoveAll(home)
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if errors.Is(ctx.Err(), context.Canceled) {
		return streamInfo{}, ctx.Err()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return streamInfo{}, fmt.Errorf("episode source lookup timed out after %s", wait)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return streamInfo{}, errors.New("the otakase CLI isn't in this bot's image")
	}
	info, perr := parseStreamInfo(string(out))
	if languagesOnly && errors.Is(perr, errNoPlayableLink) {
		perr = nil
	}
	if perr == nil {
		markHLSLinks(parent, &info)
		return info, nil
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" {
			msg = err.Error()
		}
		if strings.Contains(msg, "flag provided but not defined") {
			return streamInfo{}, errWatchCLITooOld
		}
		return streamInfo{}, errors.New(clip(msg, 300))
	}
	return streamInfo{}, perr
}

var (
	errNoPlayableLink = errors.New("no playable link")
	errWatchCLITooOld = errors.New("the otakase CLI in this bot's image is too old for the watch room")
)

// parseStreamInfo reads the JSON line the CLI prints last.
func parseStreamInfo(out string) (streamInfo, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for k := len(lines) - 1; k >= 0; k-- {
		line := strings.TrimSpace(lines[k])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var info streamInfo
		if err := json.Unmarshal([]byte(line), &info); err != nil {
			return info, err
		}
		if len(info.Streams) == 0 && len(info.Torrents) == 0 {
			return info, errNoPlayableLink
		}
		return info, nil
	}
	return streamInfo{}, errors.New("no answer from the otakase CLI")
}

// watchInfos caches resolved episodes for a while, so the next episode can
// be looked up ahead and a re-pick doesn't search again.
type watchInfos struct {
	mu sync.Mutex
	m  map[string]watchInfoEntry
}

type watchInfoEntry struct {
	info  streamInfo
	err   error
	at    time.Time
	ready chan struct{}
}

func (c *watchInfos) get(ctx context.Context, cli string, id, ep int, dub, adult bool) (streamInfo, error) {
	key := fmt.Sprintf("%d:%d:%v:%v", id, ep, dub, adult)
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]watchInfoEntry{}
	}
	for waited := false; ; waited = true {
		e, ok := c.m[key]
		if !ok || e.ready == nil && !waited && (time.Since(e.at) >= watchInfoTTL || e.err != nil) {
			break
		}
		if e.ready == nil {
			// A fresh answer, or the one just waited for, failed or not.
			c.mu.Unlock()
			return e.info, e.err
		}
		// Someone else is looking it up. If their lookup was called off,
		// the entry is gone when it ends and this caller looks it up itself.
		c.mu.Unlock()
		select {
		case <-e.ready:
		case <-ctx.Done():
			return streamInfo{}, ctx.Err()
		}
		c.mu.Lock()
	}
	ready := make(chan struct{})
	c.m[key] = watchInfoEntry{ready: ready, at: time.Now()}
	c.mu.Unlock()
	info, err := watchResolve(ctx, cli, id, ep, dub, adult)
	c.mu.Lock()
	if current, exists := c.m[key]; exists && current.ready == ready {
		if ctx.Err() != nil {
			delete(c.m, key)
		} else {
			c.m[key] = watchInfoEntry{info: info, err: err, at: time.Now()}
		}
	}
	for k, old := range c.m {
		if old.ready == nil && time.Since(old.at) > watchInfoTTL {
			delete(c.m, k)
		}
	}
	c.mu.Unlock()
	close(ready)
	return info, err
}

// ── Picking a show ──

// watchShow is a show as the room shows it.
type watchShow struct {
	ID       int                   `json:"id"`
	MalID    int                   `json:"mal_id,omitempty"`
	Title    string                `json:"title"`
	Episodes int                   `json:"episodes,omitempty"` // 0 while airing with no total
	Aired    int                   `json:"aired,omitempty"`    // episodes out so far, for airing shows
	Cover    string                `json:"cover,omitempty"`
	Previews []watchEpisodePreview `json:"previews,omitempty"`
	Year     int                   `json:"year,omitempty"`
	Format   string                `json:"format,omitempty"`
	Adult    bool                  `json:"adult,omitempty"`
}

// lastEpisode is the highest episode that can be picked.
func (s watchShow) lastEpisode() int {
	switch {
	case s.Aired > 0:
		return s.Aired
	case s.Episodes > 0:
		return s.Episodes
	}
	return 1
}

type watchEpisodePreview struct {
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
}

const watchMediaFields = `id idMal isAdult episodes format seasonYear title { romaji english } coverImage { large } nextAiringEpisode { episode } streamingEpisodes { title thumbnail }`

type watchMediaJSON struct {
	ID       int    `json:"id"`
	IDMal    int    `json:"idMal"`
	IsAdult  bool   `json:"isAdult"`
	Episodes int    `json:"episodes"`
	Format   string `json:"format"`
	Year     int    `json:"seasonYear"`
	Title    struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
	} `json:"title"`
	Cover struct {
		Large string `json:"large"`
	} `json:"coverImage"`
	Previews []watchEpisodePreview `json:"streamingEpisodes"`
	Next     *struct {
		Episode int `json:"episode"`
	} `json:"nextAiringEpisode"`
}

func (m watchMediaJSON) show() watchShow {
	s := watchShow{ID: m.ID, MalID: m.IDMal, Title: m.Title.English, Episodes: m.Episodes, Cover: m.Cover.Large, Previews: m.Previews,
		Year: m.Year, Format: m.Format, Adult: m.IsAdult}
	if s.Title == "" {
		s.Title = m.Title.Romaji
	}
	if m.Next != nil && m.Next.Episode > 1 {
		s.Aired = m.Next.Episode - 1
	}
	return s
}

// watchSearch finds shows to watch; adult says whether 18+ ones may show.
var watchSearch = func(q string, adult bool) ([]watchShow, error) {
	vars := map[string]any{"q": q}
	filter := ", isAdult: false"
	if adult {
		filter = ""
	}
	var r struct {
		Data struct {
			Page struct {
				Media []watchMediaJSON `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	query := `query ($q: String) { Page(perPage: 12) { media(search: $q, type: ANIME, sort: SEARCH_MATCH` + filter +
		`, status_not: NOT_YET_RELEASED) { ` + watchMediaFields + ` } } }`
	if err := anilistPost("", query, vars, &r); err != nil {
		return nil, err
	}
	out := []watchShow{}
	for _, m := range r.Data.Page.Media {
		if m.Format == "MUSIC" || (m.IsAdult && !adult) {
			continue
		}
		out = append(out, m.show())
	}
	return out, nil
}

// watchShowByID reads one show.
var watchShowByID = func(id int) (watchShow, error) {
	var r struct {
		Data struct {
			Media *watchMediaJSON `json:"Media"`
		} `json:"data"`
	}
	if err := anilistPost("", `query ($id: Int) { Media(id: $id, type: ANIME) { `+watchMediaFields+` } }`, map[string]any{"id": id}, &r); err != nil {
		return watchShow{}, err
	}
	if r.Data.Media == nil {
		return watchShow{}, errors.New("no such show")
	}
	return r.Data.Media.show(), nil
}

// ── List progress ──

// anilistProgress sets a member's progress to episode unless it is already
// there or past it. The show is marked watching, or completed at its last
// episode; a rewatch stays a rewatch.
var anilistProgress = func(token string, show watchShow, episode int) (bool, error) {
	var r struct {
		Data struct {
			Media *struct {
				MediaListEntry *struct {
					Status   string `json:"status"`
					Progress int    `json:"progress"`
				} `json:"mediaListEntry"`
			} `json:"Media"`
		} `json:"data"`
	}
	if err := anilistSocialPost(token, `query ($id: Int) { Media(id: $id) { mediaListEntry { status progress } } }`, map[string]any{"id": show.ID}, &r); err != nil {
		return false, tokenErr(err)
	}
	status := "CURRENT"
	if r.Data.Media != nil && r.Data.Media.MediaListEntry != nil {
		e := r.Data.Media.MediaListEntry
		if e.Status == "COMPLETED" || e.Progress >= episode {
			return false, nil
		}
		if e.Status == "REPEATING" {
			status = "REPEATING"
		}
	}
	if show.Episodes > 0 && episode >= show.Episodes && status != "REPEATING" {
		status = "COMPLETED"
	}
	var w struct {
		Data struct {
			SaveMediaListEntry *struct {
				Progress int `json:"progress"`
			} `json:"SaveMediaListEntry"`
		} `json:"data"`
	}
	if err := anilistSocialPost(token, `mutation ($id: Int, $p: Int, $s: MediaListStatus) { SaveMediaListEntry(mediaId: $id, progress: $p, status: $s) { id progress } }`,
		map[string]any{"id": show.ID, "p": episode, "s": status}, &w); err != nil {
		return false, tokenErr(err)
	}
	return w.Data.SaveMediaListEntry != nil, nil
}

// watchedEnough is the share of an episode a member has to be in the room
// for before their list moves on.
const watchedEnough = 0.8

// countWatched moves the lists of the members who watched enough of the
// episode. It runs once per member and episode.
func (b *bot) countWatched(users []string, show watchShow, episode int) {
	if !b.settingBool("watch_room_progress") {
		return
	}
	var done []watchResume
	for _, user := range users {
		var al anilistLink
		var mal malLink
		var hasAL, hasMAL bool
		b.store.view(func(d *storeData) {
			al, hasAL = d.AniList[user]
			mal, hasMAL = d.MAL[user]
		})
		if hasAL && al.Verified && al.Token != "" {
			if _, err := anilistProgress(al.Token, show, episode); err != nil {
				logWatchErr("anilist progress", user, err)
			}
		}
		if hasMAL && show.MalID > 0 {
			if _, err := b.malProgress(user, mal, show, episode); err != nil {
				logWatchErr("mal progress", user, err)
			}
		}
		done = append(done, watchResume{user: user, show: show, ep: episode, done: true})
	}
	// The saved position moves on with the list, as in the app.
	b.saveResumes(done)
}

func logWatchErr(what, user string, err error) {
	log.Printf("%s of member %s: %v", what, user, err)
}
