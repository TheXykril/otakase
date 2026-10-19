package internal

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// mpv's own playlist doubles as an episode picker. One row is the real
// stream; every other row is a black lavfi clip with an #EXTINF title. When
// the viewer lands on one of those, the controller resolves that episode and
// swaps it in.

const (
	// Wait this long before touching the playlist at all, and again before
	// putting the rows back after a switch.
	playlistSettleDelay  = 3 * time.Second
	playlistPoll         = 400 * time.Millisecond
	playlistStableReads  = 3
	playlistStableWait   = 45 * time.Second
	playlistWatchPoll    = 150 * time.Millisecond
	playlistPickDebounce = 80 * time.Millisecond

	// Placeholder clips last a day. The episode number is packed into the
	// frame size so no two rows share a path (lavfi won't take a #fragment).
	placeholderSeconds = 86400
	placeholderMinSide = 16
	placeholderRowSize = 200
)

var placeholderSizeRE = regexp.MustCompile(`s=(\d+)x(\d+)`)

// placeholderFor is the lavfi path standing in for episode ep.
func placeholderFor(ep int) string {
	if ep < 0 {
		ep = 0
	}
	w := placeholderMinSide + ep%placeholderRowSize
	h := placeholderMinSide + ep/placeholderRowSize
	return fmt.Sprintf("av://lavfi:color=c=0x101010:s=%dx%d:d=%d", w, h, placeholderSeconds)
}

// placeholderEpisode undoes placeholderFor.
func placeholderEpisode(path string) (int, bool) {
	size := placeholderSizeRE.FindStringSubmatch(path)
	if len(size) != 3 {
		return 0, false
	}
	w, _ := strconv.Atoi(size[1])
	h, _ := strconv.Atoi(size[2])
	if min(w, h) < placeholderMinSide {
		return 0, false
	}
	ep := (w - placeholderMinSide) + (h-placeholderMinSide)*placeholderRowSize
	return ep, ep > 0
}

func isPlaceholderPath(path string) bool {
	return strings.HasPrefix(path, "av://lavfi:") || strings.Contains(path, "lavfi:color=")
}

// While an episode switch runs, time-pos disappears for a bit. The playback
// monitor checks this flag so it doesn't read that as mpv being closed.
var playlistSwitching atomic.Bool

// PlaylistSwitchInProgress is true while the playlist is swapping episodes.
func PlaylistSwitchInProgress() bool { return playlistSwitching.Load() }

func markPlaylistSwitchStart() { playlistSwitching.Store(true) }

func markPlaylistSwitchDone() { playlistSwitching.Store(false) }

// playlistRow describes one entry of the mpv playlist.
type playlistRow struct {
	Ep    int
	Audio string // sub | dub
	Title string
}

// PlaylistController owns the episode rows of a single mpv session.
//
// Rows only go in after playback has settled so the first frames are never
// held up, and no row is resolved to a stream until someone picks it.
type PlaylistController struct {
	config *Config
	anime  *Anime          // the session's show; Ep is rewritten on every switch
	socket string          // mpv IPC endpoint
	done   <-chan struct{} // closed when the session ends

	mu       sync.Mutex
	rows     []playlistRow
	busy     atomic.Bool // our own playlist edits, not the viewer's
	lastRow  int
	episodes []int

	baseAudio  string // audio the session started in
	otherAudio string // audio offered as the extra row
	nowEp      int
	nowAudio   string

	// prefetch counts in-flight next-episode lookups.
	prefetch sync.WaitGroup
}

// playlistAudioMode is the audio the playlist rows are built for.
//
// That is whatever is playing, not what the config asks for. With
// AutoAudioFallback a sub-only show plays sub even for a dub user, and rows
// built for dub would all fail to resolve.
//
// Ep.Mode must be checked for emptiness before normalising it: normalising
// "" gives "sub", which would override a dub preference.
func playlistAudioMode(anime *Anime, config *Config) string {
	if anime != nil && strings.TrimSpace(anime.Ep.Mode) != "" {
		return normalizeTranslationType(anime.Ep.Mode)
	}
	if config != nil {
		return normalizeTranslationType(config.SubOrDub)
	}
	return "sub"
}

// StartPlaylistController sets up the episode playlist in the background
// and keeps reacting to picks until done is closed. No-op when the feature is
// off or the player has no IPC socket.
func StartPlaylistController(config *Config, anime *Anime, socket string, done <-chan struct{}) {
	switch {
	case config == nil, anime == nil, !config.MpvEpisodePlaylist:
		return
	case socket == "", socket == "android-intent":
		return
	}
	audio := playlistAudioMode(anime, config)
	pc := &PlaylistController{
		config:     config,
		anime:      anime,
		socket:     socket,
		done:       done,
		lastRow:    -1,
		baseAudio:  audio,
		otherAudio: alternateTranslationType(audio),
		nowEp:      anime.Ep.Number,
		nowAudio:   audio,
	}
	go pc.loop()
}

// closed reports whether the session is over.
func (pc *PlaylistController) closed() bool {
	select {
	case <-pc.done:
		return true
	default:
	}
	return false
}

func (pc *PlaylistController) loop() {
	if !pc.waitUntilPlaying() {
		return
	}
	err := pc.build()
	switch {
	case err != nil:
		// Rows that made it in are still usable, so carry on.
		Log("MPV playlist build failed: " + err.Error())
	default:
		Log(fmt.Sprintf("MPV episode playlist ready (%d episodes)", len(pc.episodes)))
	}
	if pc.closed() {
		return
	}
	pc.offerOtherAudio()
	pc.watchPlaylistSelection()
}

// waitUntilPlaying returns true once time-pos has been readable a few polls
// in a row, i.e. the stream is past its initial buffering.
func (pc *PlaylistController) waitUntilPlaying() bool {
	settle := time.NewTimer(playlistSettleDelay)
	defer settle.Stop()
	select {
	case <-pc.done:
		return false
	case <-settle.C:
	}

	deadline := time.Now().Add(playlistStableWait)
	good := 0
	for time.Now().Before(deadline) {
		if pc.closed() {
			return false
		}
		pos, err := MPVSendCommand(pc.socket, []interface{}{"get_property", "time-pos"})
		if err == nil && mpvNumber(pos) >= 0 {
			good++
			if good >= playlistStableReads {
				return true
			}
		} else {
			good = 0
		}
		time.Sleep(playlistPoll)
	}
	Log("playlist: mpv never settled, no playlist this time")
	return false
}

// episodeNumbers gets the show's episodes in the playing audio from the
// provider, falling back to 1..total.
func (pc *PlaylistController) episodeNumbers() []int {
	provider, id := AnimeProviderID(pc.anime)
	qualified := QualifyProviderID(provider, id)
	if id != "" {
		list, err := EpisodesList(qualified, pc.baseAudio)
		if err != nil {
			Log(fmt.Sprintf("MPV playlist: episode list from %s failed, counting from the total: %v", provider, err))
		} else if eps := episodeNumbersFrom(list); len(eps) > 0 {
			return eps
		}
	}

	if pc.anime.TotalEpisodes <= 0 && id != "" {
		if total, err := GetProviderTotalEpisodes(qualified, pc.baseAudio); err == nil {
			pc.anime.TotalEpisodes = total
		}
	}
	var eps []int
	for ep := 1; ep <= pc.anime.TotalEpisodes; ep++ {
		eps = append(eps, ep)
	}
	return eps
}

// episodeNumbersFrom turns a provider's episode strings into sorted unique
// ints. Half-episodes like 12.5 fold into the whole one before them.
func episodeNumbersFrom(raw []string) []int {
	var eps []int
	have := map[int]struct{}{}
	for _, s := range raw {
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || f < 1 {
			continue
		}
		ep := int(f)
		if _, dup := have[ep]; dup {
			continue
		}
		have[ep] = struct{}{}
		eps = append(eps, ep)
	}
	sort.Ints(eps)
	return eps
}

// rowTitle names a row: show, episode, filler tag, the audio when it differs
// from what's playing, and an arrow on the current row.
func (pc *PlaylistController) rowTitle(ep int, audio string) string {
	var b strings.Builder
	if show := strings.TrimSpace(GetAnimeName(*pc.anime)); show != "" {
		b.WriteString(show + " - ")
	}
	fmt.Fprintf(&b, "Episode %d", ep)
	if pc.anime.FillerEpisodes != nil && IsFiller(pc.anime.FillerEpisodes, ep) {
		b.WriteString(" [Filler]")
	}
	audio = normalizeTranslationType(audio)
	if pc.nowAudio != "" && audio != pc.nowAudio {
		b.WriteString(" (" + strings.ToUpper(audio) + ")")
	}
	if ep == pc.nowEp && audio == pc.nowAudio {
		b.WriteString("  ◀")
	}
	return b.String()
}

func (pc *PlaylistController) build() error {
	eps := pc.episodeNumbers()
	if len(eps) == 0 {
		return fmt.Errorf("no episode list from provider")
	}
	pc.anime.TotalEpisodes = max(pc.anime.TotalEpisodes, eps[len(eps)-1])

	cur := pc.anime.Ep.Number
	if cur < 1 {
		cur = eps[0]
	}
	// Tracker progress can name an episode the provider doesn't list; give it
	// a row anyway.
	if !slices.Contains(eps, cur) {
		eps = append(eps, cur)
		sort.Ints(eps)
	}
	pc.episodes = eps

	pc.mu.Lock()
	pc.nowEp = cur
	pc.mu.Unlock()

	pc.busy.Store(true)
	defer pc.busy.Store(false)

	// Let the playlist OSD show our titles instead of lavfi paths.
	_, _ = MPVSendCommand(pc.socket, []interface{}{"set_property", "osd-playlist-entry", "title"})
	want := pc.fillAround(eps, cur, pc.baseAudio)

	pc.lastRow = want
	pos, err := pc.intProperty("playlist-pos")
	if err != nil {
		return nil
	}
	pc.lastRow = pos
	if pos != want {
		Log(fmt.Sprintf("MPV playlist-pos=%d want=%d (leaving as-is to avoid stutter)", pos, want))
	}
	return nil
}

// rebuild restores the rows after loadfile replaced the playlist with the
// newly picked episode alone.
func (pc *PlaylistController) rebuild() error {
	if pc.closed() || pc.anime == nil {
		return nil
	}
	if len(pc.episodes) == 0 {
		pc.episodes = pc.episodeNumbers()
		if len(pc.episodes) == 0 {
			return fmt.Errorf("no episodes for rebuild")
		}
	}
	cur := pc.nowEp
	if cur < 1 {
		cur = pc.anime.Ep.Number
	}
	audio := pc.nowAudio
	if audio == "" {
		audio = pc.baseAudio
	}

	pc.busy.Store(true)
	markPlaylistSwitchStart()
	defer func() {
		markPlaylistSwitchDone()
		pc.busy.Store(false)
	}()

	idx := pc.fillAround(pc.episodes, cur, audio)
	pc.mu.Lock()
	pc.lastRow = idx
	n := len(pc.rows)
	pc.mu.Unlock()
	Log(fmt.Sprintf("MPV playlist rebuilt around ep %d (%d entries)", cur, n))
	return nil
}

// fillAround puts placeholder rows before and after the playing entry, which
// is left untouched. Returns the playing entry's new index.
func (pc *PlaylistController) fillAround(eps []int, cur int, audio string) int {
	var earlier, later []playlistRow
	for _, ep := range eps {
		if ep == cur {
			continue
		}
		row := playlistRow{Ep: ep, Audio: audio, Title: pc.rowTitle(ep, audio)}
		if ep < cur {
			earlier = append(earlier, row)
		} else {
			later = append(later, row)
		}
	}

	// The playing entry is a real file, so its title comes from
	// force-media-title rather than the M3U.
	title := pc.rowTitle(cur, audio)
	for _, prop := range []string{"force-media-title", "title"} {
		_, _ = MPVSendCommand(pc.socket, []interface{}{"set_property", prop, title})
	}

	if err := pc.appendRows(later, false); err != nil {
		Log(fmt.Sprintf("MPV playlist: adding later episodes: %v", err))
	}
	if err := pc.appendRows(earlier, true); err != nil {
		Log(fmt.Sprintf("MPV playlist: adding earlier episodes: %v", err))
	}

	all := slices.Concat(earlier, []playlistRow{{Ep: cur, Audio: audio, Title: title}}, later)
	pc.mu.Lock()
	pc.rows = all
	pc.mu.Unlock()
	return len(earlier)
}

// appendRows loads rows into mpv via a throwaway M3U (the only way to set an
// entry title) and, if atFront, moves them to the top keeping their order.
func (pc *PlaylistController) appendRows(rows []playlistRow, atFront bool) error {
	if len(rows) == 0 {
		return nil
	}
	m3u, err := pc.writeRowsM3U(rows)
	if err != nil {
		return err
	}
	defer os.Remove(m3u)

	start, _ := pc.intProperty("playlist-count")
	if _, err := MPVSendCommand(pc.socket, []interface{}{"loadlist", m3u, "append"}); err != nil {
		return fmt.Errorf("loadlist: %w", err)
	}
	end, err := pc.intProperty("playlist-count")
	if err != nil {
		return err
	}
	n := end - start
	if n <= 0 {
		return fmt.Errorf("loadlist added 0 entries")
	}
	if atFront {
		for i := range n {
			from := start + i
			if _, err := MPVSendCommand(pc.socket, []interface{}{"playlist-move", from, i}); err != nil {
				Log(fmt.Sprintf("MPV playlist: moving row %d to %d: %v", from, i, err))
			}
		}
	}
	return nil
}

// writeRowsM3U writes rows to a temp M3U in the storage dir (or the system
// temp dir) and returns its path.
func (pc *PlaylistController) writeRowsM3U(rows []playlistRow) (string, error) {
	var body strings.Builder
	body.WriteString("#EXTM3U\n")
	for _, row := range rows {
		fmt.Fprintf(&body, "#EXTINF:-1,%s\n%s\n", m3uTitle(row.Title), placeholderFor(row.Ep))
	}

	tmpDir := os.TempDir()
	if pc.config != nil && strings.TrimSpace(pc.config.StoragePath) != "" {
		tmpDir = os.ExpandEnv(pc.config.StoragePath)
		_ = os.MkdirAll(tmpDir, 0o755)
	}
	f, err := os.CreateTemp(tmpDir, "otakase-playlist-*.m3u")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_, werr := f.WriteString(body.String())
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(name)
		return "", werr
	}
	return name, nil
}

// m3uTitle flattens a title onto a single #EXTINF line.
func m3uTitle(s string) string {
	s = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(s))
	if s != "" {
		return s
	}
	return "Episode"
}

// intProperty reads an integer property such as playlist-pos; non-numbers
// come back as -1.
func (pc *PlaylistController) intProperty(name string) (int, error) {
	v, err := MPVSendCommand(pc.socket, []interface{}{"get_property", name})
	if err != nil {
		return -1, err
	}
	return int(math.Round(mpvNumber(v))), nil
}

// offerOtherAudio adds a row for the current episode in the other audio, if
// some provider actually carries it. Only that one episode gets resolved.
func (pc *PlaylistController) offerOtherAudio() {
	if pc.closed() || pc.anime == nil {
		return
	}
	audio := pc.otherAudio
	cfg := *pc.config
	cfg.SubOrDub = audio
	res, err := ResolveEpisodeURL(cfg, pc.anime, pc.nowEp)
	if err != nil || len(res.Links) == 0 {
		Log(fmt.Sprintf("MPV playlist: no %s stream for ep %d (%v)", audio, pc.nowEp, err))
		return
	}

	pc.busy.Store(true)
	defer pc.busy.Store(false)

	pc.mu.Lock()
	idx := max(pc.rowIndexLocked(pc.nowEp, pc.nowAudio), 0) + 1
	row := playlistRow{Ep: pc.nowEp, Audio: audio, Title: pc.rowTitle(pc.nowEp, audio)}
	pc.mu.Unlock()

	if err := pc.insertRowAt(idx, row); err != nil {
		Log(fmt.Sprintf("MPV playlist: failed to add %s entry: %v", audio, err))
		return
	}
	pc.mu.Lock()
	pc.rows = slices.Insert(pc.rows, min(idx, len(pc.rows)), row)
	pc.mu.Unlock()
	Log(fmt.Sprintf("MPV playlist: added %s option for ep %d", audio, pc.nowEp))
}

// insertRowAt appends row and then moves it to index.
func (pc *PlaylistController) insertRowAt(index int, row playlistRow) error {
	countBefore, _ := pc.intProperty("playlist-count")
	if err := pc.appendRows([]playlistRow{row}, false); err != nil {
		return err
	}
	countAfter, err := pc.intProperty("playlist-count")
	if err != nil {
		return err
	}
	if countAfter <= countBefore {
		return fmt.Errorf("placeholder not added")
	}
	last := countAfter - 1
	to := min(max(index, 0), last)
	if to == last {
		return nil
	}
	_, err = MPVSendCommand(pc.socket, []interface{}{"playlist-move", last, to})
	return err
}

// rowIndexLocked finds ep in audio; failing that, ep in either audio;
// failing that, the index ep would have in a complete list. Hold pc.mu.
func (pc *PlaylistController) rowIndexLocked(ep int, audio string) int {
	want := normalizeTranslationType(audio)
	fallback := -1
	for i, row := range pc.rows {
		if row.Ep != ep {
			continue
		}
		if normalizeTranslationType(row.Audio) == want {
			return i
		}
		if fallback < 0 {
			fallback = i
		}
	}
	if fallback >= 0 {
		return fallback
	}
	return max(ep-1, 0)
}
