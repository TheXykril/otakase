package internal

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Synced resume carries the last watch position between devices in the list
// entry itself: one tag in the AniList notes and the MyAnimeList comments,
// [otakase:5@754] meaning episode 5, 754 seconds in. Anything else the viewer
// wrote in the note is left exactly as it was; only the tag is replaced.
//
// The history file stays the source for this device. The tag only matters to
// a device that has not played that far, and it is only trusted for the
// episode it names.

var syncedResumeTagPattern = regexp.MustCompile(`\[otakase:(\d+)@(\d+)\]`)

const (
	// syncedResumeInterval is how often a playing episode's position is written
	// to the trackers. The history file is written every poll; the trackers are
	// rate limited and only need to be roughly current, because the exit
	// cleanup writes the final position.
	syncedResumeInterval = 60 * time.Second
	// syncedResumeMinSeconds keeps the first polls of an episode -- before a
	// resume seek has landed -- from replacing a real position with a near-zero
	// one.
	syncedResumeMinSeconds = 10
	// syncedResumeMinChange is the smallest move worth a write.
	syncedResumeMinChange = 5
	// syncedResumeFetchTimeout bounds the lookup made before playback starts.
	// A slow tracker costs the synced position, never the episode.
	syncedResumeFetchTimeout = 4 * time.Second
)

type syncedResume struct {
	Episode int
	Seconds int
}

func (p syncedResume) valid() bool {
	return p.Episode > 0 && p.Seconds > 0
}

func formatSyncedResumeTag(p syncedResume) string {
	return fmt.Sprintf("[otakase:%d@%d]", p.Episode, p.Seconds)
}

func parseSyncedResumeTag(notes string) (syncedResume, bool) {
	match := syncedResumeTagPattern.FindStringSubmatch(notes)
	if match == nil {
		return syncedResume{}, false
	}
	episode, errEp := strconv.Atoi(match[1])
	seconds, errSec := strconv.Atoi(match[2])
	p := syncedResume{Episode: episode, Seconds: seconds}
	if errEp != nil || errSec != nil || !p.valid() {
		return syncedResume{}, false
	}
	return p, true
}

// withSyncedResumeTag returns notes with the tag set to p. An existing tag is
// replaced where it stands; otherwise the tag goes on a line of its own after
// whatever the viewer wrote.
func withSyncedResumeTag(notes string, p syncedResume) string {
	tag := formatSyncedResumeTag(p)
	if syncedResumeTagPattern.MatchString(notes) {
		replaced := false
		return syncedResumeTagPattern.ReplaceAllStringFunc(notes, func(string) string {
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

// laterSyncedResume picks the position further into the show, which is the
// one to trust when two trackers disagree.
func laterSyncedResume(a, b syncedResume) syncedResume {
	if !a.valid() {
		return b
	}
	if !b.valid() {
		return a
	}
	if a.Episode != b.Episode {
		if a.Episode > b.Episode {
			return a
		}
		return b
	}
	if a.Seconds >= b.Seconds {
		return a
	}
	return b
}

// syncedResumeWorthPushing answers whether next is far enough from what the
// trackers already hold to be worth a write.
func syncedResumeWorthPushing(pushed, next syncedResume) bool {
	if !next.valid() || next.Seconds < syncedResumeMinSeconds {
		return false
	}
	if pushed.Episode != next.Episode {
		return true
	}
	diff := next.Seconds - pushed.Seconds
	if diff < 0 {
		diff = -diff
	}
	return diff >= syncedResumeMinChange
}

// ApplySyncedResume looks up the position another device left for this show
// and uses it when it is for the episode about to play and further in than
// what this device has.
func ApplySyncedResume(config *Config, anime *Anime) {
	if anime == nil || anime.Untracked || anime.AnilistId == 0 || !UsesRemoteTracking(config) {
		return
	}
	endBusy := BeginBusy(config, "Checking where you left off")
	remote, ok := fetchSyncedResume(config, anime.AnilistId)
	endBusy()
	if !ok || remote.Episode != anime.Ep.Number {
		return
	}
	anime.syncedResume = remote
	if remote.Seconds > anime.Ep.Player.PlaybackTime {
		Log(fmt.Sprintf("Synced resume: episode %d from %ds (local %ds)", remote.Episode, remote.Seconds, anime.Ep.Player.PlaybackTime))
		anime.Ep.Player.PlaybackTime = remote.Seconds
		anime.Ep.Resume = true
	}
}

// syncedResumeFor is the synced position for the episode anime is on, or zero.
func syncedResumeFor(anime *Anime) int {
	if anime == nil || anime.syncedResume.Episode != anime.Ep.Number {
		return 0
	}
	return anime.syncedResume.Seconds
}

func fetchSyncedResume(config *Config, anilistID int) (syncedResume, bool) {
	type result struct {
		p  syncedResume
		ok bool
	}
	results := make(chan result, 2)
	pending := 0

	lookup := func(name string, fetch func() (string, bool, error)) {
		pending++
		go func() {
			notes, onList, err := fetch()
			if err != nil {
				Log(fmt.Sprintf("Synced resume: %s lookup failed: %v", name, err))
			}
			p, ok := parseSyncedResumeTag(notes)
			results <- result{p: p, ok: err == nil && onList && ok}
		}()
	}
	if UsesAniListTracking(config) {
		lookup("AniList", func() (string, bool, error) { return fetchAniListEntryNotes(anilistID) })
	}
	if UsesMyAnimeListTracking(config) {
		lookup("MyAnimeList", func() (string, bool, error) {
			malID, err := resolveMyAnimeListID(anilistID)
			if err != nil {
				return "", false, err
			}
			return fetchMyAnimeListEntryComments(config, malID)
		})
	}

	var best syncedResume
	timeout := time.After(syncedResumeFetchTimeout)
	for ; pending > 0; pending-- {
		select {
		case r := <-results:
			if r.ok {
				best = laterSyncedResume(best, r.p)
			}
		case <-timeout:
			Log("Synced resume: tracker lookup timed out")
			return best, best.valid()
		}
	}
	return best, best.valid()
}

// pushSyncedResume writes p into every configured tracker's entry for the
// show. A show that is not on a list is left alone: writing the note would
// add it.
func pushSyncedResume(config *Config, anilistID int, p syncedResume) error {
	// The trackers are written side by side: this runs from the exit cleanup,
	// which has a short budget for everything.
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	keep := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	update := func(fetch func() (string, bool, error), save func(string) error) {
		defer wg.Done()
		notes, onList, err := fetch()
		if err != nil || !onList {
			keep(err)
			return
		}
		if updated := withSyncedResumeTag(notes, p); updated != notes {
			keep(save(updated))
		}
	}
	if UsesAniListTracking(config) {
		wg.Add(1)
		go update(
			func() (string, bool, error) { return fetchAniListEntryNotes(anilistID) },
			func(notes string) error { return saveAniListEntryNotes(anilistID, notes) },
		)
	}
	if UsesMyAnimeListTracking(config) {
		wg.Add(1)
		go func() {
			malID, err := resolveMyAnimeListID(anilistID)
			if err != nil {
				keep(err)
				wg.Done()
				return
			}
			update(
				func() (string, bool, error) { return fetchMyAnimeListEntryComments(config, malID) },
				func(comments string) error { return saveMyAnimeListEntryComments(config, malID, comments) },
			)
		}()
	}
	wg.Wait()
	return firstErr
}

func aniListResumeHeaders() map[string]string {
	token := ""
	if user := GetGlobalUser(); user != nil {
		token = user.Token
	}
	return map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  "application/json",
	}
}

func fetchAniListEntryNotes(mediaID int) (string, bool, error) {
	query := `query($mediaId: Int) { Media(id: $mediaId, type: ANIME) { mediaListEntry { id notes } } }`
	response, err := makePostRequest("https://graphql.anilist.co", query, map[string]interface{}{"mediaId": mediaID}, aniListResumeHeaders())
	if err != nil {
		return "", false, err
	}
	data, _ := response["data"].(map[string]interface{})
	media, _ := data["Media"].(map[string]interface{})
	entry, ok := media["mediaListEntry"].(map[string]interface{})
	if !ok {
		return "", false, nil
	}
	notes, _ := entry["notes"].(string)
	return notes, true, nil
}

func saveAniListEntryNotes(mediaID int, notes string) error {
	query := `mutation($mediaId: Int, $notes: String) { SaveMediaListEntry(mediaId: $mediaId, notes: $notes) { id } }`
	_, err := makePostRequest("https://graphql.anilist.co", query, map[string]interface{}{"mediaId": mediaID, "notes": notes}, aniListResumeHeaders())
	return err
}

func fetchMyAnimeListEntryComments(config *Config, malID int) (string, bool, error) {
	var response struct {
		MyListStatus *struct {
			Comments string `json:"comments"`
		} `json:"my_list_status"`
	}
	form := url.Values{"fields": {"my_list_status{comments}"}}
	if err := myAnimeListRequest(config, http.MethodGet, fmt.Sprintf("/anime/%d", malID), form, &response); err != nil {
		return "", false, err
	}
	if response.MyListStatus == nil {
		return "", false, nil
	}
	return response.MyListStatus.Comments, true, nil
}

func saveMyAnimeListEntryComments(config *Config, malID int, comments string) error {
	if isAniListPrivateMyAnimeListID(malID) {
		return nil
	}
	var response myAnimeListListStatus
	form := url.Values{"comments": {comments}}
	return myAnimeListRequest(config, http.MethodPut, fmt.Sprintf("/anime/%d/my_list_status", malID), form, &response)
}

// resumePusher paces tracker writes for the episode that is playing. The
// latest position is always held; a write goes out when the interval has
// passed, and whatever is still unwritten goes out from the exit cleanup.
type resumePusher struct {
	mu        sync.Mutex
	config    *Config
	anilistID int
	pending   syncedResume
	pushed    syncedResume
	lastPush  time.Time
	busy      bool
	once      sync.Once

	now  func() time.Time
	push func(config *Config, anilistID int, p syncedResume) error
}

var syncedResumePusher = &resumePusher{now: time.Now, push: pushSyncedResume}

// NoteSyncedResume records where anime is, for the trackers.
func NoteSyncedResume(config *Config, anime *Anime) {
	if anime == nil || anime.Untracked || !ShouldWriteRemoteTracking(config, anime) {
		return
	}
	syncedResumePusher.note(config, anime.AnilistId, anime.Ep.Number, anime.Ep.Player.PlaybackTime)
}

func (r *resumePusher) note(config *Config, anilistID, episode, seconds int) {
	if anilistID == 0 {
		return
	}
	next := syncedResume{Episode: episode, Seconds: seconds}
	if !next.valid() || seconds < syncedResumeMinSeconds {
		return
	}
	r.once.Do(func() { RegisterExitCleanup(r.flush) })

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if anilistID != r.anilistID {
		// Another show: whatever the last one had not written yet still goes.
		if r.anilistID != 0 && syncedResumeWorthPushing(r.pushed, r.pending) {
			go r.push(r.config, r.anilistID, r.pending)
		}
		r.anilistID = anilistID
		r.pushed = syncedResume{}
		r.lastPush = now
	}
	if r.lastPush.IsZero() {
		r.lastPush = now
	}
	r.config = config
	r.pending = next

	if r.busy || now.Sub(r.lastPush) < syncedResumeInterval || !syncedResumeWorthPushing(r.pushed, next) {
		return
	}
	r.busy = true
	r.lastPush = now
	go r.run(config, anilistID, next)
}

func (r *resumePusher) run(config *Config, anilistID int, p syncedResume) {
	err := r.push(config, anilistID, p)
	if err != nil {
		Log(fmt.Sprintf("Synced resume: could not save episode %d at %ds: %v", p.Episode, p.Seconds, err))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.busy = false
	if err == nil && r.anilistID == anilistID {
		r.pushed = p
	}
}

// flush writes the last position synchronously. It runs from the exit
// cleanups, so it is the write that matters most: the one another device
// will resume from.
func (r *resumePusher) flush() {
	r.mu.Lock()
	config, anilistID, p := r.config, r.anilistID, r.pending
	due := anilistID != 0 && syncedResumeWorthPushing(r.pushed, p)
	r.mu.Unlock()
	if !due {
		return
	}
	r.run(config, anilistID, p)
}
