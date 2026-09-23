package cast

import (
	"fmt"

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
	if err := s.app.Load(url, 0, "application/x-mpegURL", false, true, false); err != nil {
		return fmt.Errorf("cast: could not start playback: %w", err)
	}
	return nil
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
