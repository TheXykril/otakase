package internal

import (
	"strings"
	"testing"
	"time"
)

// resetCastControlsForTest puts the process-wide controls back to untouched and
// restores the real terminal setup afterwards. The singleton is package state
// that every test in this package shares.
func resetCastControlsForTest(t *testing.T) {
	t.Helper()
	previous := castControlsBegin
	castControls.mu.Lock()
	castControls.begun, castControls.usable, castControls.live = false, false, nil
	castControls.mu.Unlock()
	t.Cleanup(func() {
		castControlsBegin = previous
		castControls.mu.Lock()
		castControls.begun, castControls.usable, castControls.live = false, false, nil
		castControls.mu.Unlock()
	})
}

// Every key a viewer can press, decoded from the bytes a terminal in raw mode
// actually delivers.
func TestDecodeCastKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want castCommand
		used int
	}{
		{"space pauses", []byte(" "), castCmdPauseToggle, 1},
		{"q stops", []byte("q"), castCmdStop, 1},
		{"ctrl-c stops", []byte{0x03}, castCmdStop, 1},
		{"right seeks forward", []byte{0x1b, '[', 'C'}, castCmdSeekForward, 3},
		{"left seeks back", []byte{0x1b, '[', 'D'}, castCmdSeekBack, 3},
		{"up raises volume", []byte{0x1b, '[', 'A'}, castCmdVolumeUp, 3},
		{"down lowers volume", []byte{0x1b, '[', 'B'}, castCmdVolumeDown, 3},
		{"an unknown key is consumed, not acted on", []byte("z"), castCmdNone, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, used := decodeCastKey(tc.in)
			if got != tc.want || used != tc.used {
				t.Errorf("decodeCastKey(%q) = (%v, %d), want (%v, %d)", tc.in, got, used, tc.want, tc.used)
			}
		})
	}
}

// Review Focus 2. A terminal may deliver an arrow key's three bytes across
// separate reads. Consuming a partial sequence turns one arrow into a stray
// "[" and a wrong command, so an incomplete escape must consume nothing and
// wait for the rest.
func TestDecodeCastKeyWaitsForASplitEscapeSequence(t *testing.T) {
	for _, partial := range [][]byte{{0x1b}, {0x1b, '['}} {
		got, used := decodeCastKey(partial)
		if got != castCmdNone || used != 0 {
			t.Errorf("decodeCastKey(%v) = (%v, %d), want (castCmdNone, 0)", partial, got, used)
		}
	}

	if got, used := decodeCastKey([]byte{0x1b, '[', 'C'}); got != castCmdSeekForward || used != 3 {
		t.Errorf("completed sequence = (%v, %d), want (castCmdSeekForward, 3)", got, used)
	}
}

// The status line is the only thing on screen for twenty minutes, so it shows
// where the episode is, whether it is moving, and what the volume is.
func TestCastStatusLine(t *testing.T) {
	line := castStatusLine(724, 1451, "PLAYING", 0.6)

	for _, want := range []string{"12:04", "24:11", "PLAYING", "60%"} {
		if !strings.Contains(line, want) {
			t.Errorf("status line %q does not contain %q", line, want)
		}
	}
}

// A device that has not reported a duration yet must not render as "/ 0:00" or
// divide by zero building the bar.
func TestCastStatusLineToleratesAnUnknownDuration(t *testing.T) {
	line := castStatusLine(12, 0, "BUFFERING", 0.5)

	if !strings.Contains(line, "BUFFERING") {
		t.Errorf("status line %q lost the player state", line)
	}
	if strings.Contains(line, "NaN") || strings.Contains(line, "Inf") {
		t.Errorf("status line %q has a number built by dividing by zero", line)
	}
}

// Critical 2. CastEpisode used to run exactly once per process, so
// startCastControls could start a reader goroutine and enter raw mode every
// time it was called. Now that a cast plays a whole season, a second call must
// not start a second reader: two goroutines blocked on the same stdin split
// the keypresses between them, and a second term.MakeRaw snapshots the *raw*
// termios as the state to restore, which leaves the viewer's shell raw.
func TestCastControlsStartOneReaderForTheProcess(t *testing.T) {
	resetCastControlsForTest(t)

	starts := 0
	castControlsBegin = func(config *Config) bool {
		starts++
		return true
	}

	first, releaseFirst, ok := startCastControls(nil)
	if !ok {
		t.Fatal("first subscription was refused")
	}
	releaseFirst()

	second, releaseSecond, ok := startCastControls(nil)
	if !ok {
		t.Fatal("second subscription was refused")
	}
	defer releaseSecond()

	if starts != 1 {
		t.Errorf("the reader was started %d times, want 1", starts)
	}
	if first == second {
		t.Error("the second subscription reused the first one's channel")
	}

	// Exactly one live subscription: a decoded key reaches the episode that is
	// playing now and nothing else.
	castDeliverCommand(castCmdStop)

	select {
	case got := <-second:
		if got != castCmdStop {
			t.Errorf("live subscription got %v, want castCmdStop", got)
		}
	default:
		t.Error("the live subscription did not receive the command")
	}
	select {
	case got := <-first:
		t.Errorf("the released subscription received %v; it is not live any more", got)
	default:
	}
}

// A release must not unsubscribe whoever took over after it: a double release
// from a defer plus an explicit call would otherwise silence the live episode.
func TestCastControlsReleaseOnlyClearsItsOwnSubscription(t *testing.T) {
	resetCastControlsForTest(t)
	castControlsBegin = func(config *Config) bool { return true }

	// The interleave the comparison guards: the next episode subscribes before
	// the previous episode's release has run. Releasing first and then calling
	// it a second time does not reach the comparison at all, because the release
	// is wrapped in sync.Once and the second call returns without doing
	// anything -- that version of this test passed with the comparison deleted.
	_, releaseFirst, _ := startCastControls(nil)

	second, releaseSecond, _ := startCastControls(nil)
	defer releaseSecond()

	releaseFirst() // runs for the first time, but its subscription is stale

	castDeliverCommand(castCmdPauseToggle)
	select {
	case <-second:
	default:
		t.Error("a stale release from the previous episode cleared the live subscription")
	}
}

// A run with no terminal has no controls, and asking again must not retry the
// terminal setup on every episode.
func TestCastControlsRememberThereIsNoTerminal(t *testing.T) {
	resetCastControlsForTest(t)

	starts := 0
	castControlsBegin = func(config *Config) bool {
		starts++
		return false
	}

	if _, _, ok := startCastControls(nil); ok {
		t.Error("controls were reported available with no terminal")
	}
	if _, _, ok := startCastControls(nil); ok {
		t.Error("controls were reported available on the second ask")
	}
	if starts != 1 {
		t.Errorf("the terminal was set up %d times, want 1", starts)
	}
}

// Minor 6. castSetRawMode used to disagree with the tty across episodes: raw
// mode was entered per episode and the restores registered for the exit ran
// FIFO, so the flag ended up false while the terminal was still raw and Out
// printed bare \n into it, walking the output diagonally. With one entry and
// one restore for the process, the flag and the tty cannot come apart -- and a
// whole season leaves the terminal cooked.
func TestCastControlsLeaveTheTerminalCookedAfterASeason(t *testing.T) {
	resetCastControlsForTest(t)
	resetExitCleanupsForTest()
	t.Cleanup(resetExitCleanupsForTest)
	t.Cleanup(func() { castSetRawMode(false) })

	// Stands in for castControlsBeginTerminal, which needs a tty: the same
	// bookkeeping, without term.MakeRaw.
	entries := 0
	castControlsBegin = func(config *Config) bool {
		entries++
		castSetRawMode(true)
		RegisterExitCleanup(func() { castSetRawMode(false) })
		return true
	}

	for episode := 0; episode < 6; episode++ {
		_, release, ok := startCastControls(nil)
		if !ok {
			t.Fatalf("episode %d was refused controls", episode+1)
		}
		release()
	}

	if entries != 1 {
		t.Errorf("raw mode was entered %d times, want 1", entries)
	}
	if !castRawModeActive() {
		t.Error("the raw-mode flag went false while the terminal was still raw")
	}

	runExitCleanups()

	if castRawModeActive() {
		t.Error("the terminal was left raw after the exit cleanups ran")
	}
}

// Seeking restarts ffmpeg, so a held key must not mean one restart per press:
// eight restarts, each throwing away the work of the one before it, for a viewer
// who meant a single jump.
func TestAHeldSeekKeyBecomesOneJump(t *testing.T) {
	commands := make(chan castCommand, 8)
	for i := 0; i < 5; i++ {
		commands <- castCmdSeekForward
	}

	steps, pending := coalesceSeeks(castCmdSeekForward, commands)
	if steps != 6 {
		t.Fatalf("six presses collapsed to %d steps", steps)
	}
	if len(pending) != 0 {
		t.Fatalf("seeks were handed back as pending: %v", pending)
	}
}

func TestSeeksInOppositeDirectionsCancelOut(t *testing.T) {
	commands := make(chan castCommand, 8)
	commands <- castCmdSeekBack
	commands <- castCmdSeekForward

	// A viewer overshooting and correcting inside one burst means the net move,
	// and a restart for each direction would be two restarts to arrive where
	// they started.
	if steps, _ := coalesceSeeks(castCmdSeekForward, commands); steps != 1 {
		t.Fatalf("forward, back, forward netted %d steps, want 1", steps)
	}
}

func TestOtherPressesInABurstAreHandedBackInOrder(t *testing.T) {
	commands := make(chan castCommand, 8)
	commands <- castCmdSeekForward
	commands <- castCmdPauseToggle
	commands <- castCmdVolumeUp

	steps, pending := coalesceSeeks(castCmdSeekForward, commands)
	if steps != 2 {
		t.Fatalf("two seeks collapsed to %d steps", steps)
	}
	// Dropped presses are the failure this guards: a viewer whose stop or pause
	// vanished because it arrived while they were still holding the arrow.
	if len(pending) != 2 || pending[0] != castCmdPauseToggle || pending[1] != castCmdVolumeUp {
		t.Fatalf("pending presses came back as %v", pending)
	}
}

func TestANonSeekPressIsLeftEntirelyAlone(t *testing.T) {
	commands := make(chan castCommand, 8)
	commands <- castCmdSeekForward

	// Only a seek starts a burst. A pause must not swallow the seek behind it.
	steps, pending := coalesceSeeks(castCmdPauseToggle, commands)
	if steps != 0 || pending != nil {
		t.Fatalf("a pause collapsed %d steps and %v", steps, pending)
	}
	if len(commands) != 1 {
		t.Fatal("a pause consumed the press behind it")
	}
}

func TestABurstStopsAtAnEmptyQueueRatherThanWaiting(t *testing.T) {
	// Draining what is queued, not waiting for a viewer who might press again:
	// waiting would delay every single seek by however long the window was.
	commands := make(chan castCommand, 8)
	done := make(chan int, 1)
	go func() {
		steps, _ := coalesceSeeks(castCmdSeekForward, commands)
		done <- steps
	}()

	select {
	case steps := <-done:
		if steps != 1 {
			t.Fatalf("a lone press collapsed to %d steps", steps)
		}
	case <-time.After(time.Second):
		t.Fatal("coalesceSeeks waited for a press that never came")
	}
}

func TestAClosedChannelEndsTheBurst(t *testing.T) {
	commands := make(chan castCommand, 8)
	commands <- castCmdSeekForward
	close(commands)

	// The reader closes the channel between episodes, and a drain that treated
	// the closed channel as an endless stream of zero commands would spin.
	if steps, _ := coalesceSeeks(castCmdSeekForward, commands); steps != 2 {
		t.Fatalf("a burst ending in a closed channel collapsed to %d steps", steps)
	}
}
