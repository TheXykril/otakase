package cast

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	castproto "github.com/vishen/go-chromecast/cast"
)

// Seeking does nothing on a cast episode. The receiver accepts SEEK, answers
// without an error, and keeps playing where it was: the stream has no length it
// can seek within. It reports dur=-1.0 for the whole episode, because the remux
// writes no EXT-X-ENDLIST until it finishes, so the playlist looks live to it.
//
// The vendored Load leaves MediaItem.Duration at zero, so the receiver has only
// the playlist to go on. ffprobe reads the real length from the source before
// playback starts, and this sends it in the LOAD, which is the one other place
// the protocol offers to state it.
//
// Whether the receiver honours a duration it was told over one it cannot
// measure is not documented either way, so this is written to be provable on
// hardware: cast_playback logs what was sent, and a seek that starts working is
// the answer. If it does not work, the fallback is a server-side seek -- restart
// the remux at an offset -- which is a much larger change, so it waits on this.

// These mirror the constants the vendored application package keeps to itself.
// They are protocol values, fixed by the Cast receiver rather than by the
// library, which is why restating them here is safe.
const (
	castSender         = "sender-0"
	castReceiver       = "receiver-0"
	namespaceReceiver  = "urn:x-cast:com.google.cast.receiver"
	namespaceMedia     = "urn:x-cast:com.google.cast.media"
	DefaultReceiverApp = "CC1AD845"

	// castLoadContentType is what the LOAD tells the receiver the stream is. It
	// is stated rather than guessed, because the receiver picks its player from
	// this. Distinct from castContentType in server.go, which answers what to
	// serve each file as.
	castLoadContentType = "application/x-mpegURL"
)

// castRequestID numbers our messages.
//
// The vendored library counts its own request IDs up from zero in a package
// variable we cannot reach, and a duplicate ID would have its response handed
// to whichever sender was waiting for it. Starting far above anything one
// session will reach keeps the two sequences apart.
var castRequestID atomic.Int64

func init() { castRequestID.Store(1 << 20) }

func nextCastRequestID() int { return int(castRequestID.Add(1)) }

// messageSender is the part of the device connection a load needs. The vendored
// cast.Conn satisfies it, and so does a fake, which is what makes the payload
// and the ordering testable without a device.
type messageSender interface {
	Send(requestID int, payload castproto.Payload, sourceID, destinationID, namespace string) error
}

// loadMediaPayload assembles the LOAD the receiver is sent.
//
// Duration is the whole point of this path and is left at zero when unknown,
// which is exactly what the vendored Load always sends.
func loadMediaPayload(url, contentType string, duration float64) *castproto.LoadMediaCommand {
	media := castproto.MediaItem{
		ContentId:   url,
		ContentType: contentType,
		// Not LIVE: a live stream is one the receiver may not seek within at
		// all, which is the behaviour being worked around here.
		StreamType: "BUFFERED",
	}
	if duration > 0 {
		media.Duration = float32(duration)
	}
	return &castproto.LoadMediaCommand{
		PayloadHeader: castproto.LoadHeader,
		CurrentTime:   0,
		Autoplay:      true,
		Media:         media,
	}
}

// launchReceiverPayload assembles the LAUNCH that starts the media receiver.
func launchReceiverPayload() *castproto.LaunchRequest {
	return &castproto.LaunchRequest{
		PayloadHeader: castproto.LaunchHeader,
		AppId:         DefaultReceiverApp,
	}
}

// sendLaunchReceiver asks the device to start the Default Media Receiver.
func sendLaunchReceiver(conn messageSender) error {
	payload := launchReceiverPayload()
	id := nextCastRequestID()
	payload.SetRequestId(id)
	if err := conn.Send(id, payload, castSender, castReceiver, namespaceReceiver); err != nil {
		return fmt.Errorf("cast: could not start the receiver: %w", err)
	}
	return nil
}

// sendLoadMedia hands the receiver the stream and the length it should believe.
//
// transportID is the running receiver's own address, which only exists once it
// has launched -- a LOAD sent before that has nowhere to go, and the device
// answers nothing at all rather than an error worth reporting.
func sendLoadMedia(conn messageSender, transportID, url, contentType string, duration float64) error {
	if strings.TrimSpace(transportID) == "" {
		return fmt.Errorf("cast: the receiver has not finished launching")
	}
	payload := loadMediaPayload(url, contentType, duration)
	id := nextCastRequestID()
	payload.SetRequestId(id)
	if err := conn.Send(id, payload, castSender, transportID, namespaceMedia); err != nil {
		return fmt.Errorf("cast: could not hand the stream to the device: %w", err)
	}
	return nil
}

// awaitReceiver polls until the receiver has launched and reports its address,
// which is what a LOAD has to be addressed to.
//
// The library's own launch waits on a status response with a five second
// deadline, which a television waking from standby regularly misses. This polls
// instead, so a slow device is only slow.
func awaitReceiver(refresh func() error, transportID func() string, within time.Duration) (string, error) {
	deadline := time.Now().Add(within)
	var lastErr error
	for attempt := 1; ; attempt++ {
		if err := refresh(); err != nil {
			lastErr = err
		} else if id := strings.TrimSpace(transportID()); id != "" {
			return id, nil
		}
		if !time.Now().Before(deadline) {
			break
		}
		if attempt == 1 || attempt%5 == 0 {
			Log(fmt.Sprintf("cast: waiting for the receiver to finish launching (attempt %d)", attempt))
		}
		time.Sleep(launchRetryInterval)
	}
	if lastErr != nil {
		return "", fmt.Errorf("cast: the receiver did not finish launching: %w", lastErr)
	}
	return "", fmt.Errorf("cast: the receiver did not finish launching within %s", within)
}

// awaitMedia polls until the device reports a media session.
//
// Pause, seek and volume all name the media session, so a session that has not
// appeared yet makes every control fail with "media not yet initialised" --
// which looks exactly like a control that is not wired up.
func awaitMedia(refresh func() error, ready func() bool, within time.Duration) error {
	deadline := time.Now().Add(within)
	var lastErr error
	for {
		if err := refresh(); err != nil {
			lastErr = err
		} else if ready() {
			return nil
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(launchRetryInterval)
	}
	if lastErr != nil {
		return fmt.Errorf("cast: the device never reported what it was playing: %w", lastErr)
	}
	return fmt.Errorf("cast: the device never reported what it was playing within %s", within)
}
