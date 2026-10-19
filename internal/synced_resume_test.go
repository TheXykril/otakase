package internal

import (
	"sync"
	"testing"
	"time"
)

func TestTheSyncedResumeTagRoundTrips(t *testing.T) {
	tag := formatSyncedResumeTag(syncedResume{Episode: 5, Seconds: 754})
	if tag != "[otakase:5@754]" {
		t.Fatalf("tag = %q, want [otakase:5@754]", tag)
	}
	p, ok := parseSyncedResumeTag("rewatch with friends\n" + tag)
	if !ok || p != (syncedResume{Episode: 5, Seconds: 754}) {
		t.Fatalf("parsed %+v (ok=%t) from a note holding %s", p, ok, tag)
	}
}

func TestANoteWithoutATagHasNoSyncedResume(t *testing.T) {
	for _, notes := range []string{"", "great show", "[otakase:0@754]", "[otakase:5@0]", "[otakase:x@1]"} {
		if p, ok := parseSyncedResumeTag(notes); ok {
			t.Fatalf("%q parsed as %+v", notes, p)
		}
	}
}

func TestSettingTheTagKeepsWhatTheViewerWrote(t *testing.T) {
	cases := []struct {
		notes string
		want  string
	}{
		{"", "[otakase:6@90]"},
		{"   \n", "[otakase:6@90]"},
		{"watch the OVA after ep 12", "watch the OVA after ep 12\n[otakase:6@90]"},
		{"line one\n[otakase:5@754]\nline two", "line one\n[otakase:6@90]\nline two"},
		{"[otakase:5@754] and [otakase:4@12]", "[otakase:6@90] and "},
	}
	for _, c := range cases {
		if got := withSyncedResumeTag(c.notes, syncedResume{Episode: 6, Seconds: 90}); got != c.want {
			t.Errorf("withSyncedResumeTag(%q) = %q, want %q", c.notes, got, c.want)
		}
	}
}

func TestTheTrackerFurtherIntoTheShowWins(t *testing.T) {
	a := syncedResume{Episode: 5, Seconds: 900}
	b := syncedResume{Episode: 6, Seconds: 30}
	if got := laterSyncedResume(a, b); got != b {
		t.Fatalf("got %+v, want the later episode", got)
	}
	c := syncedResume{Episode: 5, Seconds: 1000}
	if got := laterSyncedResume(a, c); got != c {
		t.Fatalf("got %+v, want the later second", got)
	}
	if got := laterSyncedResume(syncedResume{}, a); got != a {
		t.Fatalf("got %+v, want the only valid one", got)
	}
}

func TestTheSyncedPositionOnlyAppliesToItsOwnEpisode(t *testing.T) {
	anime := &Anime{}
	anime.syncedResume = syncedResume{Episode: 5, Seconds: 754}
	anime.Ep.Number = 5
	if got := syncedResumeFor(anime); got != 754 {
		t.Fatalf("episode 5 got %ds, want 754", got)
	}
	anime.Ep.Number = 6
	if got := syncedResumeFor(anime); got != 0 {
		t.Fatalf("episode 6 got %ds from episode 5's position", got)
	}
}

type fakeResumePush struct {
	mu     sync.Mutex
	pushes []syncedResume
}

func (f *fakeResumePush) push(_ *Config, _ int, p syncedResume) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushes = append(f.pushes, p)
	return nil
}

func (f *fakeResumePush) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pushes)
}

func newTestResumePusher(clock *time.Time, fake *fakeResumePush) *resumePusher {
	r := &resumePusher{now: func() time.Time { return *clock }, push: fake.push}
	// The exit-cleanup registration is not what these tests are about.
	r.once.Do(func() {})
	return r
}

func waitForPushes(t *testing.T, fake *fakeResumePush, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for fake.count() < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := fake.count(); got != want {
		t.Fatalf("%d pushes, want %d", got, want)
	}
}

func TestPositionsArePacedAndTheLastOneIsFlushed(t *testing.T) {
	clock := time.Unix(1000, 0)
	fake := &fakeResumePush{}
	r := newTestResumePusher(&clock, fake)

	// Every poll reports a position; none goes out inside the first interval.
	for sec := 10; sec <= 50; sec += 10 {
		r.note(nil, 42, 5, sec)
		clock = clock.Add(10 * time.Second)
	}
	if fake.count() != 0 {
		t.Fatalf("pushed %d times inside the first interval", fake.count())
	}

	clock = clock.Add(syncedResumeInterval)
	r.note(nil, 42, 5, 120)
	waitForPushes(t, fake, 1)

	// Quitting soon after: the exit flush writes where playback stopped.
	clock = clock.Add(5 * time.Second)
	r.note(nil, 42, 5, 125)
	r.flush()
	waitForPushes(t, fake, 2)
	if last := fake.pushes[1]; last != (syncedResume{Episode: 5, Seconds: 125}) {
		t.Fatalf("flushed %+v, want episode 5 at 125s", last)
	}

	// Nothing new since: a second flush writes nothing.
	r.flush()
	if fake.count() != 2 {
		t.Fatalf("an unchanged position was written again")
	}
}

func TestTheFirstPollsBeforeAResumeSeekAreIgnored(t *testing.T) {
	clock := time.Unix(1000, 0)
	fake := &fakeResumePush{}
	r := newTestResumePusher(&clock, fake)

	r.note(nil, 42, 5, 1)
	r.flush()
	if fake.count() != 0 {
		t.Fatalf("a position of 1s replaced whatever the trackers held")
	}
}
