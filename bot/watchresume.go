package main

import (
	"fmt"
	"math/rand/v2"
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
// episode 5, as long as the show has a next episode, even one not out yet.
// After the last episode it reads [otakase:12@done · <a line from Otakase>]. A tag already further
// on is never moved back.

// A finished show's last episode is tagged done, and a line from Otakase
// follows it: [otakase:12@done] Story complete.
var watchResumeTag = regexp.MustCompile(`\[otakase:(\d+)@(\d+|done)\]`)

// watchResumeTagSpan is what writing a tag replaces: the tag, and after a
// done tag the line that came with it.
var watchResumeTagSpan = regexp.MustCompile(`\[otakase:\d+@(?:\d+\]|done\][^\n]*)`)

// watchDoneLines are the lines a done tag picks from, the same as the app's.
var watchDoneLines = []string{
	// お任せ: the promise to the viewer.
	"お任せ. Otakase saw it through.",
	"From first frame to last, お任せ.",
	"お任せ. Another journey entrusted.",
	"Otakase, with care until the finale.",
	"Your story was in our hands. お任せ.",
	// 任: the Otakase seal.
	"任. Another story entrusted.",
	"Chosen by Otakase. Sealed with 任.",
	"任. The final frame bears our seal.",
	"One story. One seal. Otakase.",
	"任. A journey signed by Otakase.",
	// 余韻: the feeling a finished story leaves. Listed twice, for more warmth.
	"余韻. Let the feeling linger.",
	"Otakase leaves you with 余韻.",
	"The credits fade. The 余韻 stays.",
	"余韻. Our final gift from Otakase.",
	"余韻. Let the feeling linger.",
	"Otakase leaves you with 余韻.",
	"The credits fade. The 余韻 stays.",
	"余韻. Our final gift from Otakase.",
	// 一期一会: the bond with a story.
	"一期一会. Worth every episode.",
	"一期一会. Otakase brought us here.",
	"One encounter, one story. 一期一会.",
	"Otakase treasures this 一期一会.",
	// 物語: the anime journey.
	"物語. Carefully chosen by Otakase.",
	"One 物語, a thousand memories.",
	"Otakase's seal upon this 物語.",
	"物語. From our hands to your heart.",
	// またね: see you at the next one. Listed twice, for more warmth.
	"またね. Otakase will be here.",
	"Otakase says またね, never goodbye.",
	"またね. The next story is on us.",
	"From Otakase, a quiet またね.",
	"またね. Otakase will be here.",
	"Otakase says またね, never goodbye.",
	"またね. The next story is on us.",
	"From Otakase, a quiet またね.",
	// 完: complete, the ending card.
	"完. Otakase seals the final frame.",
	"完. A story entrusted, completed.",
	"完. Signed and sealed by Otakase.",
	"完. Another Otakase journey fulfilled.",
}

var watchDoneLine = func() string { return watchDoneLines[rand.IntN(len(watchDoneLines))] }

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
		return func(notes string) string { return resumeAfterEpisode(notes, p.ep, p.show.Episodes) }
	}
	return func(notes string) string { return withResumeTag(notes, p.ep, p.secs) }
}

// resumeOf is where user is in the episode, or false when there is nothing
// worth saving. Hold the lock.
func (r *watchRoom) resumeOf(user string, now time.Time) (watchResume, bool) {
	m, ok := r.members[user]
	if !ok || r.phase != "play" || r.show == nil || !r.show.tracked() || r.ep < 1 {
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
	return withTag(notes, fmt.Sprintf("[otakase:%d@%d]", episode, secs))
}

func withTag(notes, tag string) string {
	if watchResumeTagSpan.MatchString(notes) {
		replaced := false
		return watchResumeTagSpan.ReplaceAllStringFunc(notes, func(string) string {
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

// resumeAfterEpisode is notes once episode finished has been watched
// through: the tag points at the start of the next episode when the show
// has one, aired or not (total is 0 while not known), and marks the last
// one done. A tag already further on is left alone.
func resumeAfterEpisode(notes string, finished, total int) string {
	if ep, _ := watchResumeIn(notes); ep > finished {
		return notes
	}
	if total > 0 && finished >= total {
		return withTag(notes, fmt.Sprintf("[otakase:%d@done] %s", finished, watchDoneLine()))
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

// watchOfferTTL is how long the leave screen can still count an episode.
const watchOfferTTL = 15 * time.Minute

type watchOffer struct {
	p  watchResume
	at time.Time
}

// countAsk is the episode to ask user about counting, or 0: they came in
// too late for the room to count it on its own, yet are near its end and
// haven't answered. Hold the lock.
func (r *watchRoom) countAsk(user string, now time.Time) int {
	m, ok := r.members[user]
	if !ok || m.counted || m.asked || r.phase != "play" || r.show == nil || !r.show.tracked() || r.ep < 1 || r.dur <= 0 {
		return 0
	}
	if m.from <= r.startAt+watchResumeLate || m.watched < watchResumeMin || r.position(now) < watchedEnough*r.dur {
		return 0
	}
	return r.ep
}

// offer keeps p for the leave screen to count. Hold the lock.
func (w *watchRooms) offer(user string, p watchResume, now time.Time) {
	if w.offers == nil {
		w.offers = map[string]watchOffer{}
	}
	for id, o := range w.offers {
		if now.Sub(o.at) > watchOfferTTL {
			delete(w.offers, id)
		}
	}
	w.offers[user] = watchOffer{p: p, at: now}
}

// watchCount is a late joiner's answer to whether the episode counts for
// them: from the room while they're in it, or from the leave screen.
func (b *bot) watchCount(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	now := watchNow()
	var save []watchResume
	b.watch.mu.Lock()
	if cur := b.watch.room(in.Room); cur != nil {
		if m, ok := cur.members[user]; ok {
			if ep := cur.countAsk(user, now); ep > 0 {
				m.asked = true
				if in.OK {
					m.counted = true
					save = append(save, watchResume{user: user, show: *cur.show, ep: ep, done: true, count: true})
				}
				cur.changed()
			}
			b.flushCount(save)
			b.watchAnswerView(w, cur, user, b.isAdmin(user))
			return
		}
	}
	if o, ok := b.watch.offers[user]; ok {
		delete(b.watch.offers, user)
		if in.OK && now.Sub(o.at) <= watchOfferTTL {
			save = append(save, o.p)
		}
	}
	b.watch.mu.Unlock()
	b.flushCount(save)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "counted": len(save) > 0})
}

// flushCount saves the episodes a member chose to count.
func (b *bot) flushCount(save []watchResume) {
	if len(save) > 0 {
		go b.saveResumes(save)
	}
}
