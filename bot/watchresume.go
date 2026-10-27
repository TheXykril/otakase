package main

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Where a member stopped in the watch room goes into their list entry the
// way Otakase saves it: one [otakase:5@754] tag (episode 5, 754 seconds in)
// in the AniList notes and the MyAnimeList comments. The app reads it back
// and resumes there. Anything else in the note stays as it was; only the
// tag is replaced. A show that is not on a list is left alone, since
// writing the note would add it.
//
// Once a member has watched an episode through (when the room moves their
// list on, or they leave with 80% or more of it played), the tag points at
// the start of the next episode, the same as the app: [otakase:6@0] after
// episode 5. When there is no next episode (the show's last, or the next
// one isn't out yet) it is taken out. A tag already further on is never
// moved back.

var watchResumeTag = regexp.MustCompile(`\[otakase:(\d+)@(\d+)\]`)

const (
	// watchResumeMin is the least a member has to have watched, and the
	// least far into the episode, for a position worth saving.
	watchResumeMin = 10
	// watchResumeLate is how far into an episode a member may join and
	// still have their position saved. Someone who came in later never saw
	// the start, so the room's position isn't where they left off.
	watchResumeLate = 60
)

// watchResume is a position to save for one member.
type watchResume struct {
	user string
	show watchShow
	ep   int
	secs int
	done bool // ep was watched through: the tag moves past it
	// count is set when the member left near the end of an episode the room
	// had not counted for them yet: their list moves on as they go.
	count bool
}

// edit is what saving p does to a note.
func (p watchResume) edit() func(string) string {
	if p.done {
		return func(notes string) string { return resumeAfterEpisode(notes, p.ep, p.show.outSoFar()) }
	}
	return func(notes string) string { return withResumeTag(notes, p.ep, p.secs) }
}

// resumeOf is where user is in the episode, or false when there is nothing
// worth saving. Hold the lock.
func (r *watchRoom) resumeOf(user string, now time.Time) (watchResume, bool) {
	m, ok := r.members[user]
	if !ok || r.phase != "play" || r.show == nil || r.ep < 1 {
		return watchResume{}, false
	}
	secs := int(r.position(now))
	if m.from > r.startAt+watchResumeLate || m.watched < watchResumeMin || secs < watchResumeMin {
		return watchResume{}, false
	}
	p := watchResume{user: user, show: *r.show, ep: r.ep, secs: secs, done: m.counted}
	if !m.counted && r.dur > 0 && float64(secs) >= watchedEnough*r.dur {
		// Leaving near the end is finishing it.
		p.done, p.count = true, true
	}
	return p, true
}

// keepResume queues user's position to be saved as they leave. Hold the
// lock; flushResumes writes them.
func (r *watchRoom) keepResume(user string, now time.Time) {
	if p, ok := r.resumeOf(user, now); ok {
		r.resumes = append(r.resumes, p)
	}
}

// flushResumes writes the queued positions. Hold the lock.
func (b *bot) flushResumes(r *watchRoom) {
	if len(r.resumes) == 0 {
		return
	}
	saves := r.resumes
	r.resumes = nil
	go b.saveResumes(saves)
}

// saveResumes writes each position to the member's linked lists, when they
// let the room update them.
func (b *bot) saveResumes(saves []watchResume) {
	if !b.settingBool("watch_room_progress") {
		return
	}
	for _, p := range saves {
		var al anilistLink
		var mal malLink
		var hasAL, hasMAL, off bool
		b.store.view(func(d *storeData) {
			al, hasAL = d.AniList[p.user]
			mal, hasMAL = d.MAL[p.user]
			off = d.WatchNoSync[p.user]
		})
		if off {
			continue
		}
		if p.count {
			// Moves the list on and the note with it.
			b.countWatched([]string{p.user}, p.show, p.ep)
			continue
		}
		if hasAL && al.Verified && al.Token != "" {
			if err := anilistResume(al.Token, p.show.ID, p.edit()); err != nil {
				logWatchErr("anilist resume", p.user, err)
			}
		}
		if hasMAL && p.show.MalID > 0 && b.malReady() {
			if l, err := b.freshMAL(p.user, mal); err != nil {
				logWatchErr("mal resume", p.user, err)
			} else if err := malResume(l.AccessToken, p.show.MalID, p.edit()); err != nil {
				logWatchErr("mal resume", p.user, err)
			}
		}
	}
}

// withResumeTag returns notes with the tag set to episode and secs. An
// existing tag is replaced where it stands; otherwise the tag goes on a
// line of its own after whatever the member wrote.
func withResumeTag(notes string, episode, secs int) string {
	tag := fmt.Sprintf("[otakase:%d@%d]", episode, secs)
	if watchResumeTag.MatchString(notes) {
		replaced := false
		return watchResumeTag.ReplaceAllStringFunc(notes, func(string) string {
			if replaced {
				return ""
			}
			replaced = true
			return tag
		})
	}
	trimmed := strings.TrimRight(notes, " \t\r\n")
	if trimmed == "" {
		return tag
	}
	return trimmed + "\n" + tag
}

// outSoFar is the episodes out, 0 when that isn't known.
func (s watchShow) outSoFar() int {
	if s.Aired > 0 {
		return s.Aired
	}
	return s.Episodes
}

// withoutResumeTag returns notes with the tag taken out, and whatever that
// leaves at the end trimmed.
func withoutResumeTag(notes string) string {
	if !watchResumeTag.MatchString(notes) {
		return notes
	}
	return strings.TrimRight(watchResumeTag.ReplaceAllString(notes, ""), " \t\r\n")
}

// resumeAfterEpisode is notes once episode finished has been watched
// through: the tag points at the start of the next episode, or is gone
// when there is no next episode yet (last is 0 while not known).
func resumeAfterEpisode(notes string, finished, last int) string {
	if ep, _ := watchResumeIn(notes); ep > finished {
		return notes
	}
	if last > 0 && finished >= last {
		return withoutResumeTag(notes)
	}
	return withResumeTag(notes, finished+1, 0)
}

// anilistResume edits the tag in the member's AniList notes for the show.
var anilistResume = func(token string, show int, edit func(string) string) error {
	var r struct {
		Data struct {
			Media *struct {
				MediaListEntry *struct {
					Notes *string `json:"notes"`
				} `json:"mediaListEntry"`
			} `json:"Media"`
		} `json:"data"`
	}
	if err := anilistSocialPost(token, `query ($id: Int) { Media(id: $id) { mediaListEntry { notes } } }`, map[string]any{"id": show}, &r); err != nil {
		return tokenErr(err)
	}
	if r.Data.Media == nil || r.Data.Media.MediaListEntry == nil {
		return nil
	}
	notes := ""
	if n := r.Data.Media.MediaListEntry.Notes; n != nil {
		notes = *n
	}
	next := edit(notes)
	if next == notes {
		return nil
	}
	var w struct {
		Data struct {
			SaveMediaListEntry *struct {
				ID int `json:"id"`
			} `json:"SaveMediaListEntry"`
		} `json:"data"`
	}
	if err := anilistSocialPost(token, `mutation ($id: Int, $n: String) { SaveMediaListEntry(mediaId: $id, notes: $n) { id } }`,
		map[string]any{"id": show, "n": next}, &w); err != nil {
		return tokenErr(err)
	}
	return nil
}

// malResume edits the tag in the member's MyAnimeList comments for the show.
var malResume = func(token string, malID int, edit func(string) string) error {
	var cur struct {
		Status *struct {
			Comments string `json:"comments"`
		} `json:"my_list_status"`
	}
	if err := malGet(token, "/anime/"+strconv.Itoa(malID)+"?fields="+url.QueryEscape("my_list_status{comments}"), &cur); err != nil {
		return err
	}
	if cur.Status == nil {
		return nil
	}
	next := edit(cur.Status.Comments)
	if next == cur.Status.Comments {
		return nil
	}
	form := url.Values{"comments": {next}}
	req, _ := http.NewRequest("PATCH", malAPI+"/anime/"+strconv.Itoa(malID)+"/my_list_status", strings.NewReader(form.Encode()))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := activityClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach MyAnimeList")
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("comments update: MyAnimeList answered %s", resp.Status)
	}
	return nil
}
