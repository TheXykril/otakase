package cast

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vishen/go-chromecast/application"
	"github.com/vishen/go-chromecast/cast"
)

// Span is a stretch of an episode worth skipping, in seconds.
type Span struct {
	Start float64
	End   float64
}

// Progress is where the device is in the episode.
type Progress struct {
	Position float64
	Duration float64
	// Idle reports that the device has stopped playing -- the episode ended,
	// or something else took the device over.
	Idle bool
}

// ShouldMarkComplete reports whether enough of the episode has played to count
// as watched, by the same threshold mpv playback uses.
func ShouldMarkComplete(p Progress, thresholdPercent int) bool {
	if thresholdPercent <= 0 || p.Duration <= 0 {
		return false
	}
	return p.Position/p.Duration*100 >= float64(thresholdPercent)
}

// NextSkip reports where to seek to, if the position is inside a span.
//
// The end of a span is not inside it: seeking there when the player has just
// arrived would seek to where it already is, on every poll, forever.
func NextSkip(position float64, spans []Span) (float64, bool) {
	for _, span := range spans {
		if span.End <= span.Start {
			continue
		}
		if position >= span.Start && position < span.End {
			return span.End, true
		}
	}
	return 0, false
}

// Session is a connected cast device.
type Session struct {
	app *application.Application

	// lastStatus is the last device state logged, so polling once a second
	// does not fill the log with the same line.
	lastStatus string
}

// Connect opens a connection to a device and takes over its media receiver.
func Connect(d Device) (*Session, error) {
	app := application.NewApplication()
	if err := app.Start(d.Addr.String(), d.Port); err != nil {
		return nil, fmt.Errorf("cast: could not connect to %s: %w", d.Name, err)
	}
	return &Session{app: app}, nil
}

// Play loads a URL on the device and starts it.
//
// The content type is stated rather than guessed: the stream is served from a
// directory with no meaningful extension handling, and the Default Media
// Receiver picks its player from this.
//
// Play returns as soon as the device has been told to start, rather than
// waiting for the episode to finish: the detach flag passed to Load is what
// makes that happen, and the caller's polling loop -- watched threshold,
// opening/ending skips, deferred Stop -- depends on Play not blocking.
func (s *Session) Play(url string) error {
	return s.PlayWithin(url, DefaultLaunchTimeout)
}

// DefaultLaunchTimeout is how long PlayWithin keeps trying to get the receiver
// running.
//
// The vendored library waits five seconds for any response and then gives up
// (application.sendAndWait). Launching the Default Media Receiver on a TV that
// is waking from standby regularly takes longer than that, and the error it
// produces -- `unable to change to appID "CC1AD845": context deadline exceeded`
// -- reads like a broken device rather than an impatient client. Patching the
// vendored timeout would be undone by the next `go mod vendor`, so the retry
// lives here instead.
const DefaultLaunchTimeout = 90 * time.Second

// launchRetryInterval is the pause between attempts. The device is booting an
// app; polling it faster does not make that quicker.
var launchRetryInterval = 2 * time.Second

// PlayWithin loads a URL as an HLS stream, retrying the launch until the
// deadline passes.
//
// Only the timeout is retried. A device that refuses the stream refuses it the
// same way every time, so retrying that would turn a clear failure into a long
// one.
func (s *Session) PlayWithin(url string, within time.Duration) error {
	return s.loadWithin(url, "application/x-mpegURL", within)
}

// PlayImage loads a still image on the device -- the idle/rating card shown
// between episodes and at season end. It uses the same launch-retry behavior
// as PlayWithin: a device waking from standby needs the same grace whether
// what it is asked to show is a stream or a picture.
func (s *Session) PlayImage(url string) error {
	return s.loadWithin(url, "image/png", DefaultLaunchTimeout)
}

// loadWithin is the shared retry loop under Play and PlayImage, parameterized
// on content type so a still image does not have to pretend to be HLS.
func (s *Session) loadWithin(url, contentType string, within time.Duration) error {
	return playWithin(func(u string) error {
		return s.app.Load(u, 0, contentType, false, true, false)
	}, url, within)
}

// playWithin holds the retry rule, separated from the device so it can be
// tested without one.
func playWithin(load func(string) error, url string, within time.Duration) error {
	deadline := time.Now().Add(within)
	var lastErr error
	for attempt := 1; ; attempt++ {
		err := load(url)
		if err == nil {
			return nil
		}
		lastErr = err
		if !errors.Is(err, context.DeadlineExceeded) || !time.Now().Before(deadline) {
			break
		}
		Log(fmt.Sprintf("cast: the device has not finished launching yet (attempt %d): %v", attempt, err))
		time.Sleep(launchRetryInterval)
	}
	return fmt.Errorf("cast: could not start playback: %w", lastErr)
}

// defaultMediaReceiverAppID is the Default Media Receiver's Cast app ID.
// PlayWithin's doc comment above names it in the error text this project
// already logs; this is the same ID as a named constant.
const defaultMediaReceiverAppID = "CC1AD845"

// ReceiverIsAvailable reports whether the device is idle or already running
// the Default Media Receiver -- safe to load a stream or a card onto without
// interrupting a different app the viewer switched to.
//
// A status read that fails is treated as available rather than unavailable:
// this guards against pulling the device away from something else, not
// against a transient network hiccup, and the caller's own next call (the
// Load this guards) will fail loudly on its own if the device is genuinely
// unreachable.
func (s *Session) ReceiverIsAvailable() bool {
	if err := s.app.Update(); err != nil {
		return true
	}
	app, _, _ := s.app.Status()
	return app == nil || app.IsIdleScreen || app.AppId == defaultMediaReceiverAppID
}

// Progress asks the device where it is.
func (s *Session) Progress() (Progress, error) {
	if err := s.app.Update(); err != nil {
		return Progress{}, fmt.Errorf("cast: could not read the device status: %w", err)
	}

	app, media, _ := s.app.Status()
	s.logStatus(app, media)
	// The receiver going back to its idle screen is the clearest signal the
	// episode is over. It is checked first because the vendored library never
	// clears a media status once it has seen one -- relying on PlayerState
	// alone would leave a caller polling a frozen "PLAYING" forever after the
	// session goes away (the viewer stops the cast from the Google Home app,
	// or casts something else to the same device).
	if app == nil || app.IsIdleScreen {
		return Progress{Idle: true}, nil
	}
	if media == nil {
		return Progress{Idle: true}, nil
	}
	return Progress{
		Position: float64(media.CurrentTime),
		Duration: float64(media.Media.Duration),
		Idle:     media.PlayerState == "IDLE",
	}, nil
}

// SeekToTime jumps to a position, which is how a skip happens on a device with
// no IPC socket to seek through.
func (s *Session) SeekToTime(seconds float64) error {
	if err := s.app.SeekToTime(float32(seconds)); err != nil {
		return fmt.Errorf("cast: could not seek: %w", err)
	}
	return nil
}

// Pause holds playback on the device.
func (s *Session) Pause() error {
	if err := s.app.Pause(); err != nil {
		return fmt.Errorf("cast: could not pause: %w", err)
	}
	return nil
}

// Unpause resumes playback on the device.
func (s *Session) Unpause() error {
	if err := s.app.Unpause(); err != nil {
		return fmt.Errorf("cast: could not resume: %w", err)
	}
	return nil
}

// clampVolume holds a level inside the 0..1 range the receiver accepts.
func clampVolume(level float64) float64 {
	if level < 0 {
		return 0
	}
	if level > 1 {
		return 1
	}
	return level
}

// SetVolume sets the device volume, on a 0..1 scale.
//
// The level is clamped rather than rejected: a viewer holding the volume key
// at either end should stop moving, not be shown an error.
func (s *Session) SetVolume(level float64) error {
	if err := s.app.SetVolume(float32(clampVolume(level))); err != nil {
		return fmt.Errorf("cast: could not set the volume: %w", err)
	}
	return nil
}

// Volume is the device's last known volume on a 0..1 scale, or 0 if it has not
// reported one. It reads what the last status refreshed, and does not itself
// talk to the device.
func (s *Session) Volume() float64 {
	v := s.app.Volume()
	if v == nil {
		return 0
	}
	return float64(v.Level)
}

// Stop ends playback and disconnects, leaving the device on its home screen
// rather than holding a stream that is about to stop being served.
func (s *Session) Stop() error {
	return s.app.Close(true)
}

// logStatus records what the device says about itself whenever that changes.
//
// A cast that does not start looks the same from the outside whatever the
// cause, and the device's own player state is the only thing that tells the
// difference: BUFFERING means it accepted the load and is trying to fetch,
// IDLE with an idleReason of ERROR means it tried and gave up, and an idle
// screen means it never took the media at all.
func (s *Session) logStatus(app *cast.Application, media *cast.Media) {
	status := "app=<nil>"
	if app != nil {
		status = fmt.Sprintf("app=%q idleScreen=%t statusText=%q", app.DisplayName, app.IsIdleScreen, app.StatusText)
	}
	if media == nil {
		status += " media=<nil>"
	} else {
		status += fmt.Sprintf(" player=%s idleReason=%q pos=%.1f dur=%.1f contentType=%q",
			media.PlayerState, media.IdleReason, media.CurrentTime, media.Media.Duration, media.Media.ContentType)
	}
	if status == s.lastStatus {
		return
	}
	s.lastStatus = status
	Log("cast: device status: " + status)
}
