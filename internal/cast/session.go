package cast

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vishen/go-chromecast/application"
	castproto "github.com/vishen/go-chromecast/cast"
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

	// conn is the same connection the application uses, kept so a LOAD can be
	// sent with a duration the vendored Load has no argument for. See load.go.
	conn messageSender

	// lastStatus is the last device state logged, so polling once a second
	// does not fill the log with the same line.
	lastStatus string
}

// Connect opens a connection to a device and takes over its media receiver.
func Connect(d Device) (*Session, error) {
	app := application.NewApplication()

	// The application builds its own connection and keeps it private. Installing
	// the same kind of connection here keeps a reference to it, which is what
	// lets PlayWithDuration send a LOAD the library cannot express -- see
	// load.go. SetConn before Start: Start is what opens it.
	conn := castproto.NewConnection()
	app.SetConn(conn)

	if err := app.Start(d.Addr.String(), d.Port); err != nil {
		return nil, fmt.Errorf("cast: could not connect to %s: %w", d.Name, err)
	}
	return &Session{app: app, conn: conn}, nil
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

// PlayWithin loads a URL, retrying the launch until the deadline passes.
//
// Only the timeout is retried. A device that refuses the stream refuses it the
// same way every time, so retrying that would turn a clear failure into a long
// one.
func (s *Session) PlayWithin(url string, within time.Duration) error {
	return playWithin(func(u string) error {
		return s.app.Load(u, 0, castLoadContentType, false, true, false)
	}, url, within)
}

// PlayWithDuration loads a URL and tells the device how long it is.
//
// The receiver reports dur=-1.0 for a stream whose playlist has no
// EXT-X-ENDLIST, and refuses to seek inside one, so an episode cast this way
// cannot be skipped through. The LOAD message can carry a duration, which the
// vendored Load has no argument for, so this sends its own.
//
// A duration of zero sends exactly what the vendored path sends, so a caller
// with no probed length loses nothing by coming through here.
func (s *Session) PlayWithDuration(url string, duration float64, within time.Duration) error {
	if s.conn == nil {
		// A session built without a connection reference -- only a test does
		// this -- can still cast, just without the duration.
		return s.PlayWithin(url, within)
	}

	if err := sendLaunchReceiver(s.conn); err != nil {
		return s.fallBackToVendoredLoad(url, within, err)
	}

	transportID, err := awaitReceiver(s.app.Update, func() (string, string) {
		app, _, _ := s.app.Status()
		if app == nil {
			return "", ""
		}
		return app.AppId, app.TransportId
	}, within)
	if err != nil {
		return s.fallBackToVendoredLoad(url, within, err)
	}

	// Update once more before the LOAD: this is what performs the CONNECT on the
	// running receiver's own namespace, without which it discards the LOAD.
	if err := s.app.Update(); err != nil {
		return s.fallBackToVendoredLoad(url, within, fmt.Errorf("cast: could not reach the receiver after it launched: %w", err))
	}

	Log(fmt.Sprintf("cast: loading with duration=%.1f on %s", duration, transportID))
	if err := sendLoadMedia(s.conn, transportID, url, castLoadContentType, duration); err != nil {
		return s.fallBackToVendoredLoad(url, within, err)
	}

	// The media session has to exist before a pause or a seek can name it, and
	// it is the status response that fills it in.
	if err := awaitMedia(s.app.Update, func() bool {
		_, media, _ := s.app.Status()
		return media != nil
	}, within); err != nil {
		return s.fallBackToVendoredLoad(url, within, err)
	}
	return nil
}

// fallBackToVendoredLoad plays the episode the way it was played before the
// duration was ever sent.
//
// The duration in the LOAD is an experiment: it is how seeking might start
// working, and an episode is worth more than a seek. Every failure in the path
// above therefore ends here rather than at the viewer, who would otherwise get
// a device that connected and then played nothing -- which is precisely what one
// wrong transport id did on hardware.
func (s *Session) fallBackToVendoredLoad(url string, within time.Duration, cause error) error {
	Log(fmt.Sprintf("cast: loading with a stated duration did not work (%v), playing without one", cause))
	return s.PlayWithin(url, within)
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
func (s *Session) logStatus(app *castproto.Application, media *castproto.Media) {
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
