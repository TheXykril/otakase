package internal

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Heartbeats tell the Otakase Discord bot that a linked app is alive: once at
// launch, every minute while an episode plays (in mpv or on a cast device),
// and once when playback stops. They carry how long was actually watched
// since the last one, and -- only with DiscordShareWatching on -- what.

// discordHeartbeatInterval is the time between heartbeats during playback.
// Tests shorten it.
var discordHeartbeatInterval = 60 * time.Second

// discordMPVPollInterval is how often the mpv watcher reads the position.
var discordMPVPollInterval = 5 * time.Second

type heartbeatWatching struct {
	AnilistID int    `json:"anilist_id"`
	Title     string `json:"title"`
	Episode   int    `json:"episode"`
	Episodes  int    `json:"episodes"`
}

type heartbeatPayload struct {
	Version        string             `json:"version"`
	DevBuild       bool               `json:"dev_build"`
	OS             string             `json:"os"`
	ShareWatching  bool               `json:"share_watching"`
	Watching       *heartbeatWatching `json:"watching"`
	WatchedSeconds int                `json:"watched_seconds"`
}

// isDevBuildVersion reports whether version is one of the dev builds
// (26.8.0-dev.3), the builds testers run.
func isDevBuildVersion(version string) bool {
	_, pre := splitPrerelease(version)
	return strings.HasPrefix(strings.ToLower(pre), "dev")
}

func newHeartbeatPayload(config *Config, watching *heartbeatWatching, seconds int) heartbeatPayload {
	share := config.DiscordShareWatching
	if !share {
		// Not sent at all unless the user opted in, rather than sent and
		// left to the bot to ignore.
		watching = nil
	}
	if seconds < 0 {
		seconds = 0
	}
	return heartbeatPayload{
		Version:        Version(),
		DevBuild:       isDevBuildVersion(Version()),
		OS:             runtime.GOOS,
		ShareWatching:  share,
		Watching:       watching,
		WatchedSeconds: seconds,
	}
}

// sendDiscordHeartbeat sends one heartbeat if the app is linked. It returns
// nil without a request when it is not.
func sendDiscordHeartbeat(config *Config, watching *heartbeatWatching, seconds int) error {
	if !discordEnabled(config) {
		return nil
	}
	link := loadDiscordLink(config)
	if link.Token == "" {
		return nil
	}
	var answer struct {
		OK          bool   `json:"ok"`
		DiscordName string `json:"discord_name"`
	}
	_, err := discordPostJSON(context.Background(), config, "/v1/heartbeat", link.Token, newHeartbeatPayload(config, watching, seconds), &answer)
	if err != nil {
		return err
	}
	// The member may have renamed themselves since linking.
	if answer.DiscordName != "" && answer.DiscordName != link.DiscordName {
		link.DiscordName = answer.DiscordName
		if err := saveDiscordLink(config, link); err != nil {
			Log(fmt.Sprintf("Discord: could not save the new name: %v", err))
		}
	}
	return nil
}

// watchMeter turns position readings into time spent watching: the position
// moving forward by about as much as the clock did. A paused player does not
// move, a seek moves far more than the clock, and both count nothing; faster
// playback counts the wall time, not the episode time.
type watchMeter struct {
	mu      sync.Mutex
	have    bool
	lastPos float64
	lastAt  time.Time
	watched float64
}

func (m *watchMeter) observe(position float64, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.have {
		elapsed := now.Sub(m.lastAt).Seconds()
		moved := position - m.lastPos
		// Up to 4x speed plus a little slack for the reading's own delay;
		// anything beyond is a seek.
		if moved > 0 && elapsed > 0 && moved <= elapsed*4+2 {
			if moved < elapsed {
				m.watched += moved
			} else {
				m.watched += elapsed
			}
		}
	}
	m.have, m.lastPos, m.lastAt = true, position, now
}

// restart forgets the last reading, for a new episode whose position starts
// over without that being a seek back.
func (m *watchMeter) restart() {
	m.mu.Lock()
	m.have = false
	m.mu.Unlock()
}

// take returns the whole seconds watched since the last take, keeping the
// fraction for the next one.
func (m *watchMeter) take() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	whole := int(m.watched)
	m.watched -= float64(whole)
	return whole
}

// discordWatchSession is one stretch of playback reported to the bot. A nil
// session (not linked, or the features off) accepts every call and does
// nothing, so call sites need no checks of their own.
type discordWatchSession struct {
	config *Config
	meter  watchMeter
	send   func(watching *heartbeatWatching, seconds int) error

	mu       sync.Mutex
	watching heartbeatWatching
	observed bool

	kick       chan struct{}
	stop       chan struct{}
	stopOnce   sync.Once
	finishOnce sync.Once
	finished   chan struct{}
	loopDone   chan struct{}
	cancelExit func()
}

func startDiscordWatch(config *Config, anime *Anime) *discordWatchSession {
	if !discordLinked(config) || anime == nil {
		return nil
	}
	session := &discordWatchSession{
		config: config,
		send: func(watching *heartbeatWatching, seconds int) error {
			return sendDiscordHeartbeat(config, watching, seconds)
		},
		watching: heartbeatWatching{
			AnilistID: anime.AnilistId,
			Title:     GetAnimeName(*anime),
			Episode:   anime.Ep.Number,
			Episodes:  anime.TotalEpisodes,
		},
		kick:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		finished: make(chan struct{}),
		loopDone: make(chan struct{}),
	}
	// Quitting otakase does not unwind the playback loops, so the closing
	// heartbeat is also sent from the exit cleanups (bounded at two seconds).
	session.cancelExit = RegisterExitCleanup(session.finish)
	go session.run()
	return session
}

func (s *discordWatchSession) run() {
	defer close(s.loopDone)
	ticker := time.NewTicker(discordHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-s.kick:
		case <-ticker.C:
		}
		watching := s.current()
		if err := s.send(&watching, s.meter.take()); err != nil {
			Log(fmt.Sprintf("Discord: heartbeat failed: %v", err))
		}
	}
}

func (s *discordWatchSession) current() heartbeatWatching {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.watching
}

// observe records the playback position of episode. The first reading sends
// a heartbeat at once, so "now watching" does not lag a minute behind.
func (s *discordWatchSession) observe(episode int, position float64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	first := !s.observed
	s.observed = true
	changed := episode > 0 && episode != s.watching.Episode
	if changed {
		s.watching.Episode = episode
	}
	s.mu.Unlock()
	if changed {
		s.meter.restart()
	}
	s.meter.observe(position, time.Now())
	if first || changed {
		select {
		case s.kick <- struct{}{}:
		default:
		}
	}
}

// end stops the session and sends the closing heartbeat in the background.
func (s *discordWatchSession) end() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stop) })
	go s.finish()
}

// finish sends the closing heartbeat once, whoever gets there first: end, or
// the exit cleanup when otakase quits mid-episode.
func (s *discordWatchSession) finish() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.finishOnce.Do(func() {
		// A heartbeat still in flight goes first, so the closing one is last.
		<-s.loopDone
		defer close(s.finished)
		if err := s.send(nil, s.meter.take()); err != nil {
			Log(fmt.Sprintf("Discord: closing heartbeat failed: %v", err))
		}
		if s.cancelExit != nil {
			s.cancelExit()
		}
	})
}

// StartDiscordMPVWatch reports an mpv playback to the bot: it reads the
// position every few seconds and ends when mpv goes away or done closes.
// It returns at once and does nothing when the app is not linked.
func StartDiscordMPVWatch(config *Config, anime *Anime, socketPath string, done <-chan struct{}) {
	if socketPath == "" || socketPath == "cast" || socketPath == "android-intent" || !discordLinked(config) {
		return
	}
	startWatchWithMe(config, anime, socketPath)
	session := startDiscordWatch(config, anime)
	if session == nil {
		return
	}
	trackRemotePlayer(socketPath)
	go func() {
		defer untrackRemotePlayer(socketPath)
		defer session.end()
		ticker := time.NewTicker(discordMPVPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			value, err := MPVSendCommand(socketPath, []interface{}{"get_property", "time-pos"})
			if err != nil {
				if MPVConnectionGone(err) && !IsMPVRunning(socketPath) {
					return
				}
				continue
			}
			if position, ok := value.(float64); ok {
				session.observe(anime.Ep.Number, position)
			}
		}
	}()
}

// StartDiscordBackground is the launch's share of the Discord features: one
// heartbeat if linked, and the news refreshed when the cached copy is old.
// Both run in the background and never hold the launch up.
func StartDiscordBackground(config *Config) {
	if !discordEnabled(config) {
		return
	}
	go func() {
		if err := sendDiscordHeartbeat(config, nil, 0); err != nil {
			Log(fmt.Sprintf("Discord: launch heartbeat failed: %v", err))
		}
	}()
	go refreshDiscordNewsIfStale(config)
	StartDiscordInbox(config)
}
