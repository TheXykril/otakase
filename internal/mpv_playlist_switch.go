package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	playlistMediaChangeWait = 30 * time.Second
	playlistDurationWait    = 15 * time.Second
	playlistMediaPoll       = 150 * time.Millisecond
)

// watchPlaylistSelection follows the viewer's picks until the session ends.
//
// A pick shows first in the path: mpv opens the row's placeholder. A move of
// playlist-pos is the second signal, for rows mpv switched to without
// loading anything yet. Either way the episode comes from the row's title or
// the controller's own rows, never from the placeholder's frame size, which
// mpv can report for a row other than the one picked.
func (pc *PlaylistController) watchPlaylistSelection() {
	for !pc.closed() {
		time.Sleep(playlistWatchPoll)
		if pc.busy.Load() || PlaylistSwitchInProgress() {
			continue
		}
		// mpv can refuse the socket for a moment while alive, so this skips a
		// tick rather than stopping; the session's end closes done.
		if !IsMPVRunning(pc.socket) {
			continue
		}

		path := mpvStringProperty(pc.socket, "path")
		pos, posErr := pc.intProperty("playlist-pos")

		if isPlaceholderPath(path) {
			// Scrubbing through rows passes over placeholders; only one that
			// stays loaded is a pick.
			time.Sleep(playlistPickDebounce)
			if !isPlaceholderPath(mpvStringProperty(pc.socket, "path")) {
				continue
			}
			if posErr != nil || pos < 0 {
				pos, _ = pc.intProperty("playlist-pos")
			}
			pc.lastRow = pos
			row, err := pc.rowAt(pos)
			if err != nil {
				Log(fmt.Sprintf("MPV playlist: placeholder pick unresolvable at pos %d: %v", pos, err))
				continue
			}
			if pc.pickEpisode(row) {
				return
			}
			continue
		}

		if posErr != nil || pos < 0 || pos == pc.lastRow {
			continue
		}
		// The row is read now, before mpv can move on from it.
		pc.lastRow = pos
		row, err := pc.rowAt(pos)
		if err != nil {
			Log(fmt.Sprintf("MPV playlist: resolve pos %d failed: %v", pos, err))
			pc.pickFromPlaceholderPath()
			continue
		}
		playing := pc.playingEpisode()
		if row.Ep == playing {
			// The live stream is still on: a blip, unless it was the row for
			// the other audio.
			if normalizeTranslationType(row.Audio) != normalizeTranslationType(pc.nowAudio) {
				markPlaylistSwitchStart()
				_, _ = MPVSendCommand(pc.socket, []interface{}{"set_property", "pause", true})
				pc.switchEpisode(row, playing)
			}
			continue
		}
		if pc.pickEpisode(row) {
			return
		}
	}
}

func (pc *PlaylistController) playingEpisode() int {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.nowEp
}

// pickEpisode switches to the row the viewer picked. It reports true when the
// pick was mpv running off the end of the episode and the session ended.
func (pc *PlaylistController) pickEpisode(row playlistRow) bool {
	playing := pc.playingEpisode()
	// Checked before anything is announced: "Loading episode 1…" followed by
	// nothing is how this looked stuck.
	if pc.endSessionIfEpisodeOver(row.Ep) {
		return true
	}
	markPlaylistSwitchStart()
	_, _ = MPVSendCommand(pc.socket, []interface{}{"set_property", "pause", true})
	_, _ = MPVSendCommand(pc.socket, []interface{}{"show-text", fmt.Sprintf("Loading episode %d…", row.Ep), 5000})
	pc.switchEpisode(row, playing)
	return false
}

// pickFromPlaceholderPath handles a row that could not be resolved: the
// placeholder loaded may still say which episode it is, and otherwise the
// stream that was playing carries on.
func (pc *PlaylistController) pickFromPlaceholderPath() {
	if ep, ok := placeholderEpisode(mpvStringProperty(pc.socket, "path")); ok {
		markPlaylistSwitchStart()
		_, _ = MPVSendCommand(pc.socket, []interface{}{"set_property", "pause", true})
		pc.switchEpisode(playlistRow{Ep: ep, Audio: pc.baseAudio, Title: pc.rowTitle(ep, pc.baseAudio)}, pc.playingEpisode())
		return
	}
	_, _ = MPVSendCommand(pc.socket, []interface{}{"set_property", "pause", false})
	markPlaylistSwitchDone()
}

// rowAt is the row at pos, read from its title when that names an
// episode and from the controller's own rows otherwise. pos is the index the
// caller saw; it is not read again here.
func (pc *PlaylistController) rowAt(pos int) (playlistRow, error) {
	if title := pc.playlistEntryTitle(pos); title != "" {
		if ep, mode, ok := parsePlaylistEpisodeTitle(title, pc.baseAudio); ok {
			return playlistRow{Ep: ep, Audio: mode, Title: title}, nil
		}
	}
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if pos < 0 || pos >= len(pc.rows) {
		return playlistRow{}, fmt.Errorf("playlist pos %d out of range (rows=%d)", pos, len(pc.rows))
	}
	return pc.rows[pos], nil
}

func (pc *PlaylistController) playlistEntryTitle(pos int) string {
	value, err := MPVSendCommand(pc.socket, []interface{}{"get_property", fmt.Sprintf("playlist/%d/title", pos)})
	if err != nil {
		return ""
	}
	title, _ := value.(string)
	return strings.TrimSpace(title)
}

var playlistEpisodeTitleRE = regexp.MustCompile(`(?i)\b(?:episode|ep)\s*(\d+)\b`)

// parsePlaylistEpisodeTitle reads the episode, and the audio when tagged, from
// a row title rowTitle wrote.
func parsePlaylistEpisodeTitle(title, baseAudio string) (ep int, mode string, ok bool) {
	mode = normalizeTranslationType(baseAudio)
	if mode == "" {
		mode = "sub"
	}
	upper := strings.ToUpper(title)
	switch {
	case strings.Contains(upper, "(DUB)") || strings.Contains(upper, "· DUB") || strings.HasSuffix(strings.TrimSpace(upper), "DUB"):
		mode = "dub"
	case strings.Contains(upper, "(SUB)"):
		mode = "sub"
	}
	m := playlistEpisodeTitleRE.FindStringSubmatch(title)
	if m == nil {
		return 0, mode, false
	}
	ep, err := strconv.Atoi(m[1])
	if err != nil || ep <= 0 {
		return 0, mode, false
	}
	return ep, mode, true
}

// endSessionIfEpisodeOver handles MPV running off the end of the episode.
//
// The episode's real stream is one entry in a playlist whose other rows are
// placeholders, so when it finishes MPV does not stop -- it moves on, and the
// next row is a black lavfi clip that runs for a day. otakase used to chase that as
// though the user had chosen episode 1; refusing to chase it is not enough
// either, because MPV then sits on blackness forever and the episode never ends,
// so it is never marked watched.
//
// Ending playback here puts things back the way they are without a playlist:
// MPV closes, the main loop sees playback stop with the episode watched, and
// records it.
//
// The distinguishing signal is an empty path. MPV reports one while idle between
// files, which is what running off the end looks like; a row the user actually
// picked has been loaded and reports its own path. Without that check, choosing
// an episode to rewatch right after finishing one would quit instead.
func (pc *PlaylistController) endSessionIfEpisodeOver(target int) bool {
	if !pc.episodeFinished() {
		return false
	}
	if strings.TrimSpace(mpvStringProperty(pc.socket, "path")) != "" {
		return false
	}
	Log(fmt.Sprintf("MPV playlist: episode %d finished and MPV ran off the end (target was ep %d); ending playback",
		pc.nowEp, target))
	if _, err := MPVSendCommand(pc.socket, []interface{}{"quit"}); err != nil {
		Log(fmt.Sprintf("MPV playlist: could not end playback: %v", err))
	}
	return true
}

// episodeFinished reports whether the episode playing has already been watched
// far enough to count as complete. Past that point the controller's job is done:
// the main loop owns what happens next, including marking progress.
func (pc *PlaylistController) episodeFinished() bool {
	if pc.anime == nil || pc.anime.Ep.Duration <= 0 {
		return false
	}
	return int(PercentageWatched(pc.anime.Ep.Player.PlaybackTime, pc.anime.Ep.Duration)) >= pc.completeThreshold()
}

func (pc *PlaylistController) completeThreshold() int {
	if pc.config != nil && pc.config.PercentageToMarkComplete > 0 {
		return pc.config.PercentageToMarkComplete
	}
	return 85
}

// markLeftEpisodeOnSwitch decides, without asking mid-playback, whether
// leaving playing for target marks playing watched on the trackers. Only
// moving on to the next episode after watching this one through does; going
// back never lowers progress, and skipping ahead is left to the end of the
// session.
func markLeftEpisodeOnSwitch(playing, target int, percentageWatched float64, threshold int) bool {
	return target == playing+1 && percentageWatched > 0 && int(percentageWatched) >= threshold
}

// switchEpisode plays row in place of playing. A switch that fails goes
// back to the episode that was playing.
func (pc *PlaylistController) switchEpisode(row playlistRow, playing int) {
	// An episode watched to the completion threshold is over, and what moves
	// the playlist position then is MPV reaching end-of-file, not the user
	// choosing anything. Acting on it hijacks the end of the episode: MPV lands
	// on the first placeholder, that reads as "play episode 1", and the switch
	// replaces a finished episode with a failed one -- so the episode is never
	// marked watched and the tracker is never updated. Clearing the switch
	// lets the main loop see the episode as complete.
	if pc.episodeFinished() {
		Log(fmt.Sprintf("MPV playlist: ignoring move to ep %d; episode %d already finished", row.Ep, playing))
		markPlaylistSwitchDone()
		return
	}

	playingMode := pc.nowAudio
	if playingMode == "" {
		playingMode = pc.baseAudio
	}
	markPlaylistSwitchStart()
	_, _ = MPVSendCommand(pc.socket, []interface{}{"set_property", "pause", true})

	markLeft := false
	if row.Ep != playing {
		watched := PercentageWatched(pc.anime.Ep.Player.PlaybackTime, pc.anime.Ep.Duration)
		markLeft = markLeftEpisodeOnSwitch(playing, row.Ep, watched, pc.completeThreshold())
	}
	Log(fmt.Sprintf("MPV playlist: SWITCH ep %d → %d (%s) markLeft=%v", playing, row.Ep, row.Audio, markLeft))
	_, _ = MPVSendCommand(pc.socket, []interface{}{"show-text", fmt.Sprintf("Loading episode %d…", row.Ep), 8000})
	Out(fmt.Sprintf("Loading episode %d…", row.Ep))

	if err := pc.playRow(row); err != nil {
		Log(fmt.Sprintf("MPV playlist: failed to play ep %d: %v", row.Ep, err))
		Out(fmt.Sprintf("Could not play episode %d: %v", row.Ep, err))
		back := playlistRow{Ep: playing, Audio: playingMode, Title: pc.rowTitle(playing, playingMode)}
		if err := pc.playRow(back); err != nil {
			Log(fmt.Sprintf("MPV playlist: recovery play failed: %v", err))
			markPlaylistSwitchDone()
		}
		return
	}

	pc.finalizePlaylistEpisodeChange(playing, row.Ep, row.Audio, markLeft)

	go func() {
		time.Sleep(playlistSettleDelay)
		if pc.closed() {
			return
		}
		if err := pc.rebuild(); err != nil {
			Log(fmt.Sprintf("MPV playlist rebuild after switch: %v", err))
		}
	}()
}

// playRow resolves row's episode and loads it in the running mpv. The
// switch flag the caller set is cleared once the new stream plays.
func (pc *PlaylistController) playRow(row playlistRow) error {
	pc.busy.Store(true)
	defer pc.busy.Store(false)

	mode := normalizeTranslationType(row.Audio)
	if mode == "" {
		mode = pc.baseAudio
	}
	config := *pc.config
	config.SubOrDub = mode
	anime := pc.anime
	target := row.Ep

	// A switch that fails puts the episode back as it was. Without this a
	// failed switch left the episode looking unwatched: the completion check
	// divided by a duration that was now zero, decided the episode was
	// abandoned, and the tracker was never updated.
	saved := anime.Ep
	fail := func(err error) error {
		anime.Ep = saved
		return err
	}

	// Nothing of the previous episode may be left for the main loop to seek
	// or resume into the new stream.
	anime.Ep.Number = target
	anime.Ep.Resume = false
	anime.Ep.Player.PlaybackTime = 0
	anime.Ep.Duration = 0
	anime.Ep.IsCompleted = false
	anime.Ep.Links = nil
	anime.Ep.NextEpisode = NextEpisode{}
	anime.Ep.StreamReferrer = ""
	anime.Ep.SubtitleURL = ""
	anime.Ep.SubtitleTracks = nil
	anime.Ep.SkipTimes = SkipTimes{}

	oldPath := mpvStringProperty(pc.socket, "path")

	// The playing audio only: this runs in the background and must not open
	// an audio prompt.
	result, err := ResolveEpisodeURL(config, anime, target)
	if err != nil {
		return fail(err)
	}
	if len(result.Links) == 0 {
		return fail(fmt.Errorf("no streams for episode %d", target))
	}
	anime.Ep.Links = result.Links
	applyStreamPlaybackHints(anime, result.Links, result.LinkHints)
	link := BestLink(result.Links)
	if link == "" {
		return fail(fmt.Errorf("empty stream link for episode %d", target))
	}

	title := fmt.Sprintf("%s - Episode %d", GetAnimeName(*anime), target)
	if mode != pc.baseAudio {
		title += " (" + strings.ToUpper(mode) + ")"
	}
	anime.Ep.Player.SocketPath = pc.socket
	if err := loadEpisodeInRunningMPV(pc.socket, link, title, anime); err != nil {
		return fail(err)
	}
	// time-pos alone proves nothing: the old file has one too.
	if !waitForMPVMediaChange(pc.socket, oldPath, link, playlistMediaChangeWait) {
		return fail(fmt.Errorf("episode %d did not become the active media (still on previous file?)", target))
	}
	for _, property := range [][2]interface{}{{"time-pos", 0}, {"pause", false}, {"force-media-title", title}, {"title", title}} {
		_, _ = MPVSendCommand(pc.socket, []interface{}{"set_property", property[0], property[1]})
	}

	// Started stays true so the main loop keeps watching without a gap.
	anime.Ep.Started = true
	if result.ProviderName != "" {
		anime.ProviderName = result.ProviderName
	}
	if result.ProviderID != "" {
		anime.ProviderId = result.ProviderID
	}
	go pc.readDuration(target)

	pc.mu.Lock()
	previousMode := pc.nowAudio
	pc.nowEp = target
	pc.nowAudio = mode
	// The other audio is always relative to what now plays, so after sub→dub
	// the extra row offered is sub.
	pc.otherAudio = alternateTranslationType(mode)
	pickedOtherRow := normalizeTranslationType(row.Audio) == pc.otherAudio
	// Loading replaced the playlist with this one file.
	pc.lastRow = 0
	pc.rows = []playlistRow{{Ep: target, Audio: mode, Title: title}}
	pc.mu.Unlock()

	markPlaylistSwitchDone()

	// Picking the other audio row is a choice about the show, not the episode:
	// it is what the next launch should play. Only a switch that got the
	// audio asked for counts; one that fell back to the same audio chose
	// nothing.
	if previousMode != "" && mode != previousMode && normalizeTranslationType(row.Audio) == mode {
		rememberShowAudioMode(pc.config.StoragePath, anime.AnilistId, mode)
	}

	go func() {
		// The source can change between episodes after a fallback. The
		// episode title follows once finalizePlaylistEpisodeChange has it.
		SendMPVSkinState(GetGlobalConfig(), anime, pc.socket, false)
		// Every skip source, not only AniSkip: a show tracked on AniList alone
		// has no MyAnimeList id.
		resolution := ApplySkipTimes(anime, target, GetGlobalConfig(), GetProvider())
		// Marks belong to the episode they were made against.
		UpdateSkipMarker(anime, resolution.IDs)
	}()

	// The other audio row for the new episode, once the rows are back.
	go func() {
		time.Sleep(playlistSettleDelay + time.Second)
		if pc.closed() || pickedOtherRow {
			return
		}
		pc.offerOtherAudio()
	}()

	Out(fmt.Sprintf("Playing episode %d (%s)", target, mode))
	return nil
}

// readDuration records the new file's duration once mpv knows it.
func (pc *PlaylistController) readDuration(ep int) {
	for deadline := time.Now().Add(playlistDurationWait); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		if pc.closed() {
			return
		}
		value, err := MPVSendCommand(pc.socket, []interface{}{"get_property", "duration"})
		if duration := mpvNumber(value); err == nil && duration > 1 {
			pc.anime.Ep.Duration = int(duration + 0.5)
			Log(fmt.Sprintf("MPV playlist: episode %d duration %ds", ep, pc.anime.Ep.Duration))
			return
		}
	}
}

// loadEpisodeInRunningMPV replaces the file playing with link, paused at its
// start for the caller to unpause. Replacing clears the rest of the playlist.
//
// The referrer, --sub-file, the extra headers and the HLS bitrate cap an mpv
// started with apply to every file it loads after, so each is set again for
// this episode, unless the user's own MpvArgs fixed it.
func loadEpisodeInRunningMPV(socket, link, title string, anime *Anime) error {
	if socket == "" || link == "" {
		return fmt.Errorf("missing socket or link")
	}
	config := GetGlobalConfig()
	userArgs := func(has func([]string) bool) bool { return config != nil && has(config.MpvArgs) }

	if anime != nil {
		referrer := strings.TrimSpace(anime.Ep.StreamReferrer)
		if referrer == "" {
			referrer = streamReferrer(CurrentAnimeProviderName(anime))
		}
		if referrer != "" {
			_, _ = MPVSendCommand(socket, []interface{}{"set_property", "referrer", referrer})
		}
	}
	if !userArgs(hasMPVSubtitleArg) {
		clearMPVSubtitleFiles(MPVSendCommand, socket)
	}
	if anime != nil && !userArgs(hasMPVHeaderArg) {
		resetMPVStreamHeaders(MPVSendCommand, socket, anime.Ep.StreamHeaders)
	}
	if anime != nil && !userArgs(hasMPVHLSBitrateArg) {
		resetMPVHLSBitrate(MPVSendCommand, socket, anime.Ep.StreamHLSBitrate)
	}

	options := "force-media-title=" + escapeMPVOptionValue(title)
	if _, err := MPVSendCommand(socket, []interface{}{"loadfile", link, "replace", 0, options}); err != nil {
		// Older mpv takes no index and options here.
		if _, err := MPVSendCommand(socket, []interface{}{"loadfile", link, "replace"}); err != nil {
			return fmt.Errorf("loadfile: %w", err)
		}
		_, _ = MPVSendCommand(socket, []interface{}{"set_property", "force-media-title", title})
	}
	_, _ = MPVSendCommand(socket, []interface{}{"set_property", "pause", true})
	_, _ = MPVSendCommand(socket, []interface{}{"set_property", "time-pos", 0})

	if anime == nil {
		return nil
	}
	subtitle := strings.TrimSpace(anime.Ep.SubtitleURL)
	tracks := anime.Ep.SubtitleTracks
	if subtitle != "" || len(tracks) > 0 {
		go func() {
			_ = waitForMPVFileReady(socket, link, subtitleAttachWait)
			if subtitle != "" {
				_, _ = MPVSendCommand(socket, []interface{}{"sub-add", subtitle, "select"})
			}
			addAlternateSubtitles(MPVSendCommand, socket, subtitle, tracks)
		}()
	}
	return nil
}

func mpvStringProperty(socket, name string) string {
	value, err := MPVSendCommand(socket, []interface{}{"get_property", name})
	if err != nil || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

// waitForMPVMediaChange waits until mpv plays something other than oldPath:
// newLink, or with oldPath known, any real file that is not it.
func waitForMPVMediaChange(socket, oldPath, newLink string, timeout time.Duration) bool {
	oldKey, newKey := mediaURLKey(oldPath), mediaURLKey(newLink)
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(playlistMediaPoll) {
		path := mpvStringProperty(socket, "path")
		if path == "" || strings.HasPrefix(path, "av://") || path == oldPath {
			continue
		}
		key := mediaURLKey(path)
		// The same stream with a fresh token is still the old file.
		if key == oldKey && newKey != oldKey {
			continue
		}
		isNew := key == newKey || strings.Contains(path, newKey) || strings.Contains(newLink, path)
		if !isNew && oldPath == "" {
			continue
		}
		if position, err := MPVSendCommand(socket, []interface{}{"get_property", "time-pos"}); err == nil && position != nil {
			return true
		}
	}
	Log("MPV playlist: timed out waiting for the new episode to load")
	return false
}

// mediaURLKey is a stream URL without its query and fragment, which carry
// tokens that change between requests for the same file.
func mediaURLKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// escapeMPVOptionValue quotes a value for loadfile's option list.
func escapeMPVOptionValue(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `,`, `\,`, `=`, `\=`).Replace(value) + `"`
}

// finalizePlaylistEpisodeChange records the switch: local history, the last
// played show, the episode's details, the trackers when markLeft, and a
// prefetch of the episode after.
func (pc *PlaylistController) finalizePlaylistEpisodeChange(from, to int, mode string, markLeft bool) {
	if pc.anime == nil || pc.config == nil {
		return
	}
	anime := pc.anime
	if mode = normalizeTranslationType(mode); mode == "" {
		mode = pc.baseAudio
	}
	if mode != "" {
		pc.config.SubOrDub = mode
		pc.nowAudio = mode
	}

	anime.Ep.NextEpisode = NextEpisode{}
	anime.Ep.IsCompleted = false
	anime.Ep.LastWasSkipped = false
	anime.Ep.Resume = false
	anime.Ep.Number = to
	anime.Ep.Player.PlaybackTime = 0
	anime.Ep.IsFiller = IsFiller(anime.FillerEpisodes, to)
	if anime.MalId > 0 {
		LoadEpisodeDetails(anime, anime.MalId, to)
		// The duration comes from mpv; the one Jikan lists is not the file's.
		anime.Ep.Duration = 0
		if len(anime.FillerEpisodes) > 0 {
			anime.Ep.IsFiller = IsFiller(anime.FillerEpisodes, to)
		}
	}

	writeLastPlayedAnimeID(pc.config.StoragePath, anime.AnilistId, anime.IsAdult)
	if history := localHistoryPath(pc.config.StoragePath); history != "" {
		err := LocalUpdateAnime(history, anime.AnilistId, anime.ProviderId, to, 0, 0, GetAnimeName(*anime), CurrentAnimeProviderName(anime))
		if err != nil {
			Log(fmt.Sprintf("MPV playlist: LocalUpdateAnime: %v", err))
		}
	}

	if markLeft && from > 0 {
		if user := GetGlobalUser(); user != nil && user.Token != "" && !anime.Rewatching {
			go func(token string) {
				if err := UpdateAnimeProgress(token, anime.AnilistId, from); err != nil {
					Log(fmt.Sprintf("MPV playlist: UpdateAnimeProgress(%d): %v", from, err))
					return
				}
				Out(fmt.Sprintf("Upstream progress → episode %d", from))
			}(user.Token)
		} else if anime.Rewatching {
			Log("MPV playlist: rewatching — skipped remote progress update")
		}
		Out(fmt.Sprintf("Playing episode %d · upstream progress set to %d", to, from))
	} else {
		Out(fmt.Sprintf("Playing episode %d · last played updated (no upstream change)", to))
	}

	pc.startPrefetch(to)

	title := fmt.Sprintf("%s - Episode %d", GetAnimeName(*anime), to)
	_, _ = MPVSendCommand(pc.socket, []interface{}{"set_property", "force-media-title", title})
	_, _ = MPVSendCommand(pc.socket, []interface{}{"set_property", "title", title})
	// The skin's top bar names the episode, now that Jikan has been asked.
	go SendMPVSkinState(pc.config, anime, pc.socket, true)
}

func localHistoryPath(storagePath string) string {
	storagePath = strings.TrimSpace(os.ExpandEnv(storagePath))
	if storagePath == "" {
		return ""
	}
	return HistoryFile(storagePath)
}

func writeLastPlayedAnimeID(storagePath string, anilistID int, adult bool) {
	storagePath = strings.TrimSpace(os.ExpandEnv(storagePath))
	if anilistID <= 0 || storagePath == "" {
		return
	}
	file := LastAnimeFile(storagePath)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		Log(fmt.Sprintf("MPV playlist: mkdir for last-anime file: %v", err))
		return
	}
	if err := os.WriteFile(file, []byte(strconv.Itoa(anilistID)), 0o644); err != nil {
		Log(fmt.Sprintf("MPV playlist: write last-anime file: %v", err))
	}
	noteRecentShow(storagePath, anilistID, adult, time.Now())
}

// startPrefetch looks the episode after ep up in the background.
func (pc *PlaylistController) startPrefetch(ep int) {
	pc.prefetch.Add(1)
	go func() {
		defer pc.prefetch.Done()
		pc.prefetchNextEpisode(ep)
	}()
}

// WaitForPrefetch blocks until every in-flight prefetch has finished.
func (pc *PlaylistController) WaitForPrefetch() {
	if pc != nil {
		pc.prefetch.Wait()
	}
}

func (pc *PlaylistController) prefetchNextEpisode(ep int) {
	if pc == nil || pc.config == nil || pc.anime == nil {
		return
	}
	// With no mpv to hand the links to, resolving them is wasted work.
	if strings.TrimSpace(pc.socket) == "" || pc.socket == "android-intent" {
		return
	}
	next := ep + 1
	if pc.anime.TotalEpisodes > 0 && next > pc.anime.TotalEpisodes {
		return
	}
	config := *pc.config
	config.SubOrDub = pc.baseAudio
	copied := *pc.anime
	result, err := ResolveEpisodeURL(config, &copied, next)
	if err != nil || len(result.Links) == 0 {
		Log(fmt.Sprintf("MPV playlist: prefetch ep %d: %v", next, err))
		return
	}
	pc.mu.Lock()
	pc.anime.Ep.NextEpisode = NextEpisode{
		Number:       next,
		Links:        result.Links,
		ProviderName: result.ProviderName,
		ProviderId:   result.ProviderID,
		Mode:         result.Mode,
		LinkHints:    result.LinkHints,
	}
	pc.mu.Unlock()
}
