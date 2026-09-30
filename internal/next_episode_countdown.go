package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The next-episode countdown for local playback.
//
// A cast has counted down to the next episode since it learned to play one
// (castAwaitNextEpisode); local mpv jumped straight on when the file ended, or,
// with NextEpisodePrompt, dropped to a menu once mpv had closed. This brings the
// cast behaviour into mpv itself: when the ending starts, mpv shows "Episode 13
// in 5s", Enter plays it at once, Esc lets the credits run.
//
// The main loop owns the timing, since it already ticks once a second and owns
// the playback position; the script only draws the line and reads the two keys.
// They talk through mpv's user-data properties, so neither has to listen for
// events the other sends.

const (
	// countdownProperty carries the line to show, or "" to hide it.
	countdownProperty = "user-data/otakase/countdown"
	// countdownAnswerProperty is where the script puts "play" or "cancel".
	countdownAnswerProperty = "user-data/otakase/next"

	countdownScriptName = "otakase-next.lua"

	// countdownFallbackLead is how long before the end the countdown starts
	// when nothing says where the ending is.
	countdownFallbackLead = 90
)

// countdownScript draws the countdown and binds Enter and Esc for as long as it
// is on screen, and not a moment longer: the keys are mpv's own otherwise.
const countdownScript = `-- Written by otakase; replaced on every start.
local overlay = mp.create_osd_overlay("ass-events")
local shown = false

local function hide()
  if not shown then return end
  shown = false
  overlay:remove()
  mp.remove_key_binding("otakase-next-play")
  mp.remove_key_binding("otakase-next-cancel")
end

local function answer(value)
  mp.set_property_native("user-data/otakase/next", value)
  hide()
end

mp.observe_property("user-data/otakase/countdown", "string", function(_, text)
  if text == nil or text == "" then
    hide()
    return
  end
  overlay.data = "{\\an3\\fs30\\bord2}" .. text .. "\\N{\\fs20}Enter play now  ·  Esc keep watching"
  overlay:update()
  if not shown then
    shown = true
    mp.add_forced_key_binding("ENTER", "otakase-next-play", function() answer("play") end)
    mp.add_forced_key_binding("ESC", "otakase-next-cancel", function() answer("cancel") end)
  end
end)
`

// countdownScriptArgs writes the script into the storage directory and returns
// the option that loads it. Nothing is returned when it cannot be written: the
// countdown still runs, shown with plain OSD text, it just cannot be answered.
func countdownScriptArgs(config *Config) []string {
	if config == nil || config.NextEpisodeCountdown <= 0 {
		return nil
	}
	dir := GetStoragePath()
	if strings.TrimSpace(config.StoragePath) != "" {
		dir = os.ExpandEnv(config.StoragePath)
	}
	path := filepath.Join(dir, countdownScriptName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		Log(fmt.Sprintf("countdown: could not create %s: %v", dir, err))
		return nil
	}
	if err := os.WriteFile(path, []byte(countdownScript), 0o644); err != nil {
		Log(fmt.Sprintf("countdown: could not write %s: %v", path, err))
		return nil
	}
	return []string{"--script=" + path}
}

// countdownStart is the playback position, in seconds, where the countdown
// begins: the start of the ending, or a fixed lead before the end when the
// ending is unknown. Zero means there is no countdown for this episode.
//
// An ending that SkipEd is going to skip gets none: the skip already runs the
// episode straight on to its last seconds, and a countdown racing it would
// offer to do what is about to happen anyway.
func countdownStart(config *Config, anime *Anime) int {
	if config == nil || anime == nil || config.NextEpisodeCountdown <= 0 {
		return 0
	}
	// NextEpisodePrompt means "always ask me": its menu stays in charge.
	if config.NextEpisodePrompt {
		return 0
	}
	duration := anime.Ep.Duration
	if duration <= 0 {
		return 0
	}
	if anime.TotalEpisodes > 0 && anime.Ep.Number >= anime.TotalEpisodes {
		return 0 // the last episode: the completion prompt comes next, not another episode
	}
	if availability := nextEpisodeAiring(anime, anime.Ep.Number+1); !availability.Aired {
		return 0
	}

	ed := anime.Ep.SkipTimes.Ed
	if ed.End > ed.Start && ed.Start > 0 && ed.Start < duration {
		if config.SkipEd {
			return 0
		}
		return ed.Start
	}
	if duration <= 2*countdownFallbackLead {
		return 0 // a short clip, not an episode with credits
	}
	return duration - countdownFallbackLead
}

// countdownAction is what one tick of the countdown decided.
type countdownAction int

const (
	countdownIdle countdownAction = iota
	countdownShow
	countdownAdvance
	countdownHide
)

// countdownState is the countdown for one episode.
type countdownState struct {
	mu        sync.Mutex
	key       string
	startedAt int // playback position the countdown began at, or -1
	done      bool
}

var localCountdown = &countdownState{startedAt: -1}

// tick decides what the countdown does now. position is the playback position,
// answer what the script reported ("", "play" or "cancel"). remaining is the
// seconds left to show when the action is countdownShow.
func (s *countdownState) tick(key string, start, position, seconds int, answer string) (countdownAction, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if key != s.key {
		s.key, s.startedAt, s.done = key, -1, false
	}
	if s.done || start <= 0 {
		return countdownIdle, 0
	}
	if position < start {
		if s.startedAt >= 0 {
			// Seeked back out of the ending: take the countdown down and let it
			// start over when the ending comes round again.
			s.startedAt = -1
			return countdownHide, 0
		}
		return countdownIdle, 0
	}

	switch answer {
	case "cancel":
		s.done = true
		return countdownHide, 0
	case "play":
		s.done = true
		return countdownAdvance, 0
	}

	if s.startedAt < 0 {
		s.startedAt = position
	}
	// Measured in playback time rather than wall time, so pausing pauses the
	// countdown too: a viewer who paused on the credits is not moved on.
	remaining := seconds - (position - s.startedAt)
	if remaining <= 0 {
		s.done = true
		return countdownAdvance, 0
	}
	return countdownShow, remaining
}

// countdownMessage is the line the countdown shows.
func countdownMessage(next, remaining int) string {
	return fmt.Sprintf("Episode %d in %ds", next, remaining)
}

// NextEpisodeCountdown runs one tick of the countdown from the main playback
// loop, which calls it once a second with the position it has just read.
//
// Moving on is done the way the end of the file already does it: the episode
// is recorded as watched to the end and mpv is told to quit, and the main loop,
// seeing playback stop past the completion threshold, starts the next episode.
// It reports whether it did that.
func NextEpisodeCountdown(config *Config, anime *Anime) bool {
	if anime == nil || anime.Ep.Player.SocketPath == "" {
		return false
	}
	socket := anime.Ep.Player.SocketPath
	start := countdownStart(config, anime)
	key := fmt.Sprintf("%d:%d", anime.AnilistId, anime.Ep.Number)

	answer := ""
	if start > 0 && anime.Ep.Player.PlaybackTime >= start {
		answer = readCountdownAnswer(socket)
	}
	seconds := 0
	if config != nil {
		seconds = config.NextEpisodeCountdown
	}
	action, remaining := localCountdown.tick(key, start, anime.Ep.Player.PlaybackTime, seconds, answer)

	switch action {
	case countdownShow:
		showCountdown(socket, countdownMessage(anime.Ep.Number+1, remaining))
	case countdownHide:
		showCountdown(socket, "")
	case countdownAdvance:
		showCountdown(socket, "")
		Log(fmt.Sprintf("countdown: moving on from episode %d", anime.Ep.Number))
		anime.Ep.Player.PlaybackTime = anime.Ep.Duration
		if _, err := MPVSendCommand(socket, []interface{}{"quit"}); err != nil {
			Log(fmt.Sprintf("countdown: could not close mpv: %v", err))
		}
		return true
	}
	return false
}

// showCountdown puts text on screen through the script, or through plain OSD
// text on an mpv without user-data properties (before 0.36).
func showCountdown(socket, text string) {
	if _, err := MPVSendCommand(socket, []interface{}{"set_property", countdownProperty, text}); err != nil {
		if text != "" {
			_, _ = MPVSendCommand(socket, []interface{}{"show-text", text, 1500})
		}
	}
}

// readCountdownAnswer takes the script's answer, clearing it so it is read once.
func readCountdownAnswer(socket string) string {
	value, err := MPVSendCommand(socket, []interface{}{"get_property", countdownAnswerProperty})
	if err != nil {
		return ""
	}
	answer, _ := value.(string)
	if answer != "" {
		_, _ = MPVSendCommand(socket, []interface{}{"set_property", countdownAnswerProperty, ""})
	}
	return answer
}
