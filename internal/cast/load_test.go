package cast

import (
	"errors"
	"fmt"
	"testing"
	"time"

	castproto "github.com/vishen/go-chromecast/cast"
)

type sentMessage struct {
	requestID   int
	payload     castproto.Payload
	source      string
	destination string
	namespace   string
}

type fakeConn struct {
	sent []sentMessage
	err  error
}

func (f *fakeConn) Send(requestID int, payload castproto.Payload, sourceID, destinationID, namespace string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, sentMessage{requestID, payload, sourceID, destinationID, namespace})
	return nil
}

// The whole point of sending our own LOAD: the vendored one leaves Duration at
// zero, which is why the receiver has nothing but the playlist to go on, reports
// dur=-1.0 and refuses to seek.
func TestTheLoadTellsTheReceiverHowLongTheEpisodeIs(t *testing.T) {
	payload := loadMediaPayload("http://192.168.0.5:8010/playlist.m3u8", "application/x-mpegURL", 1480.02)

	if payload.Media.Duration != float32(1480.02) {
		t.Fatalf("duration sent as %v, want 1480.02", payload.Media.Duration)
	}
	// LIVE is what the receiver decides on its own for a playlist with no
	// EXT-X-ENDLIST, and a live stream is one it may refuse to seek within at
	// all. Saying BUFFERED is half the point of stating the duration.
	if payload.Media.StreamType != "BUFFERED" {
		t.Fatalf("stream type %q, want BUFFERED", payload.Media.StreamType)
	}
	if payload.Media.ContentId != "http://192.168.0.5:8010/playlist.m3u8" {
		t.Fatalf("content id %q", payload.Media.ContentId)
	}
	if !payload.Autoplay {
		t.Fatal("the episode was loaded without autoplay")
	}
}

func TestAnUnknownLengthSendsNoDuration(t *testing.T) {
	// A probe that failed must not turn into a claim that the episode is zero
	// seconds long: that is a worse lie than saying nothing, and saying nothing
	// is exactly what the vendored path does.
	for _, duration := range []float64{0, -1} {
		payload := loadMediaPayload("http://host/playlist.m3u8", "application/x-mpegURL", duration)
		if payload.Media.Duration != 0 {
			t.Fatalf("duration %v produced %v in the payload", duration, payload.Media.Duration)
		}
	}
}

func TestTheLoadIsAddressedToTheRunningReceiver(t *testing.T) {
	conn := &fakeConn{}
	if err := sendLoadMedia(conn, "transport-42", "http://host/playlist.m3u8", "application/x-mpegURL", 1480); err != nil {
		t.Fatal(err)
	}

	if len(conn.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(conn.sent))
	}
	got := conn.sent[0]
	// Addressed to the receiver's own transport, not to receiver-0: the media
	// namespace on the launcher's address reaches nothing, and the device
	// answers a LOAD sent there with silence rather than an error.
	if got.destination != "transport-42" {
		t.Fatalf("destination %q, want the receiver's transport id", got.destination)
	}
	if got.namespace != namespaceMedia {
		t.Fatalf("namespace %q, want %q", got.namespace, namespaceMedia)
	}
	if got.source != castSender {
		t.Fatalf("source %q, want %q", got.source, castSender)
	}
}

func TestALoadBeforeTheReceiverLaunchedIsRefused(t *testing.T) {
	conn := &fakeConn{}
	// Rather than sent into the void: without a transport id there is nothing to
	// address, and a silently discarded LOAD looks like a device that ignored a
	// perfectly good stream.
	if err := sendLoadMedia(conn, "   ", "http://host/playlist.m3u8", "application/x-mpegURL", 1480); err == nil {
		t.Fatal("a load with no transport id was sent anyway")
	}
	if len(conn.sent) != 0 {
		t.Fatalf("sent %d messages", len(conn.sent))
	}
}

func TestTheLaunchAsksForTheDefaultMediaReceiver(t *testing.T) {
	conn := &fakeConn{}
	if err := sendLaunchReceiver(conn); err != nil {
		t.Fatal(err)
	}

	got := conn.sent[0]
	launch, ok := got.payload.(*castproto.LaunchRequest)
	if !ok {
		t.Fatalf("payload is %T", got.payload)
	}
	if launch.AppId != DefaultReceiverApp {
		t.Fatalf("launched %q, want %q", launch.AppId, DefaultReceiverApp)
	}
	if got.namespace != namespaceReceiver || got.destination != castReceiver {
		t.Fatalf("launch addressed to %q on %q", got.destination, got.namespace)
	}
}

func TestOurRequestIDsStayClearOfTheLibrarysOwn(t *testing.T) {
	// The vendored library counts up from zero in a package variable we cannot
	// reach, and hands a response to whichever sender is waiting on that ID.
	// Overlapping sequences would cross the two senders' replies.
	first := nextCastRequestID()
	second := nextCastRequestID()
	if first < 1<<20 {
		t.Fatalf("request id %d is inside the library's range", first)
	}
	if second <= first {
		t.Fatalf("request ids did not advance: %d then %d", first, second)
	}
}

func TestAFailedSendIsReportedRatherThanAssumedToHaveWorked(t *testing.T) {
	conn := &fakeConn{err: errors.New("broken pipe")}
	if err := sendLoadMedia(conn, "transport-1", "http://host/playlist.m3u8", "application/x-mpegURL", 1480); err == nil {
		t.Fatal("a failed send was reported as success")
	}
	if err := sendLaunchReceiver(conn); err == nil {
		t.Fatal("a failed launch was reported as success")
	}
}

func TestWaitingForTheReceiverOutlastsATelevisionWakingUp(t *testing.T) {
	previous := launchRetryInterval
	launchRetryInterval = time.Millisecond
	defer func() { launchRetryInterval = previous }()

	// A television waking from standby answers nothing for a while and then
	// answers normally. The library's own launch gives up after five seconds,
	// which is the failure this whole path had to work around.
	attempts := 0
	transport := ""
	refresh := func() error {
		attempts++
		if attempts < 4 {
			return fmt.Errorf("context deadline exceeded")
		}
		transport = "transport-9"
		return nil
	}

	got, err := awaitReceiver(refresh, func() string { return transport }, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != "transport-9" {
		t.Fatalf("resolved transport %q", got)
	}
	if attempts < 4 {
		t.Fatalf("gave up after %d attempts", attempts)
	}
}

func TestAReceiverThatNeverLaunchesIsReported(t *testing.T) {
	previous := launchRetryInterval
	launchRetryInterval = time.Millisecond
	defer func() { launchRetryInterval = previous }()

	_, err := awaitReceiver(func() error { return nil }, func() string { return "" }, 5*time.Millisecond)
	if err == nil {
		t.Fatal("a receiver that never launched was reported as ready")
	}
}

func TestPlaybackWaitsForTheMediaSessionBeforeReturning(t *testing.T) {
	previous := launchRetryInterval
	launchRetryInterval = time.Millisecond
	defer func() { launchRetryInterval = previous }()

	// Every control names the media session, so returning before it exists
	// leaves pause, seek and volume failing with "media not yet initialised" --
	// indistinguishable from controls that were never wired up.
	polls := 0
	err := awaitMedia(func() error { polls++; return nil }, func() bool { return polls >= 3 }, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if polls < 3 {
		t.Fatalf("returned after %d polls", polls)
	}
}

func TestAnEpisodeThatNeverStartsPlayingIsReported(t *testing.T) {
	previous := launchRetryInterval
	launchRetryInterval = time.Millisecond
	defer func() { launchRetryInterval = previous }()

	if err := awaitMedia(func() error { return nil }, func() bool { return false }, 5*time.Millisecond); err == nil {
		t.Fatal("a device that never reported media was treated as playing")
	}
}
