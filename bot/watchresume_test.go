package main

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestWithResumeTag(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "[otakase:5@754]"},
		{"loved it  \n", "loved it\n[otakase:5@754]"},
		{"rewatch with Sam [otakase:3@20] later", "rewatch with Sam [otakase:5@754] later"},
		{"[otakase:1@1]\n[otakase:2@2]", "[otakase:5@754]\n"},
	}
	for _, c := range cases {
		if got := withResumeTag(c.in, 5, 754); got != c.want {
			t.Errorf("withResumeTag(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWatchRoomSavesWhereMembersStop(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) {
		d.WatchTickets = map[string]int{"a": 1}
		d.AniList = map[string]anilistLink{
			"a": {ID: 1, Verified: true, Token: "ta"},
			"b": {ID: 2, Verified: true, Token: "tb"},
			"c": {ID: 3, Verified: true, Token: "tc"},
			"d": {ID: 4, Verified: true, Token: "td"},
		}
		d.WatchNoSync = map[string]bool{"d": true}
	})
	var mu sync.Mutex
	var got []string
	old := anilistResume
	anilistResume = func(token string, show, ep, secs int) error {
		mu.Lock()
		got = append(got, fmt.Sprintf("%s:%d:%d@%d", token, show, ep, secs))
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { anilistResume = old })
	tick := func(n int, keep ...string) {
		r := b.watch.only()
		for k := 0; k < n; k++ {
			*now = now.Add(watchTick)
			b.watch.mu.Lock()
			for _, id := range keep {
				if m, ok := r.members[id]; ok {
					m.seen = *now
				}
			}
			b.watch.mu.Unlock()
			b.watchStep(r, *now)
		}
	}
	wait := func(n int) []string {
		t.Helper()
		for k := 0; k < 200; k++ {
			mu.Lock()
			l := len(got)
			mu.Unlock()
			if l >= n {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		out := append([]string(nil), got...)
		sort.Strings(out)
		return out
	}

	for _, u := range []string{"a", "b", "d"} {
		watchPost(b, (*bot).watchOpen, u, watchIn{Room: "r", Channel: "vc"}, nil)
	}
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 7, Ep: 4}, nil)
	waitLoaded(t, b)
	watchAllReady(b)
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 0, Playing: true, Dur: 1400}, nil)
	tick(40, "a", "b", "d")

	// Stopping saves where b is.
	var left map[string]any
	watchPost(b, (*bot).watchLeave, "b", watchIn{Room: "r"}, &left)
	if left["ep"] != float64(4) || left["secs"] != float64(80) {
		t.Fatalf("leave answered %v", left)
	}
	if l := wait(1); len(l) != 1 || l[0] != "tb:7:4@80" {
		t.Fatalf("after b stopped: %v", l)
	}

	// c joins too late in the episode to have a place of their own.
	watchPost(b, (*bot).watchOpen, "c", watchIn{Room: "r", Channel: "vc"}, nil)
	tick(10, "a", "c", "d")

	// d switched list updates off; the room closing saves a.
	watchPost(b, (*bot).watchEnd, "a", watchIn{Room: "r"}, nil)
	if l := wait(2); len(l) != 2 || l[0] != "ta:7:4@100" || l[1] != "tb:7:4@80" {
		t.Fatalf("after the room closed: %v", l)
	}
	time.Sleep(20 * time.Millisecond)
	if l := wait(3); len(l) != 2 {
		t.Fatalf("saved for someone who shouldn't be: %v", l)
	}
}
