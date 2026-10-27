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

func TestResumeAfterEpisode(t *testing.T) {
	old := watchDoneLine
	watchDoneLine = func() string { return "The Voices" }
	t.Cleanup(func() { watchDoneLine = old })
	cases := []struct {
		in              string
		finished, total int
		want            string
	}{
		{"", 5, 12, "[otakase:6@0]"},
		{"loved it", 5, 12, "loved it\n[otakase:6@0]"},
		{"loved it\n[otakase:5@1300]", 5, 12, "loved it\n[otakase:6@0]"},
		{"[otakase:5@1300]", 5, 0, "[otakase:6@0]"},
		// The last episode is marked done.
		{"loved it\n[otakase:12@1300]", 12, 12, "loved it\n[otakase:12@done] The Voices"},
		{"[otakase:12@done] The Voices", 3, 12, "[otakase:12@done] The Voices"},
		// Watching an earlier episode again leaves a tag further on alone.
		{"[otakase:9@300]", 5, 12, "[otakase:9@300]"},
	}
	for _, c := range cases {
		if got := resumeAfterEpisode(c.in, c.finished, c.total); got != c.want {
			t.Errorf("resumeAfterEpisode(%q, %d, %d) = %q, want %q", c.in, c.finished, c.total, got, c.want)
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
	anilistResume = func(token string, show int, edit func(string) string) error {
		mu.Lock()
		got = append(got, fmt.Sprintf("%s:%d:%s", token, show, edit("")))
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
	if l := wait(1); len(l) != 1 || l[0] != "tb:7:[otakase:4@80]" {
		t.Fatalf("after b stopped: %v", l)
	}

	// c joins too late in the episode to have a place of their own.
	watchPost(b, (*bot).watchOpen, "c", watchIn{Room: "r", Channel: "vc"}, nil)
	tick(10, "a", "c", "d")

	// d switched list updates off; the room closing saves a.
	watchPost(b, (*bot).watchEnd, "a", watchIn{Room: "r"}, nil)
	if l := wait(2); len(l) != 2 || l[0] != "ta:7:[otakase:4@100]" || l[1] != "tb:7:[otakase:4@80]" {
		t.Fatalf("after the room closed: %v", l)
	}
	time.Sleep(20 * time.Millisecond)
	if l := wait(3); len(l) != 2 {
		t.Fatalf("saved for someone who shouldn't be: %v", l)
	}
}

func TestLeavingNearTheEndFinishesTheEpisode(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) {
		d.WatchTickets = map[string]int{"a": 1}
		d.AniList = map[string]anilistLink{"a": {ID: 1, Verified: true, Token: "ta"}, "b": {ID: 2, Verified: true, Token: "tb"}}
	})
	var mu sync.Mutex
	var got []string
	oldProgress, oldResume := anilistProgress, anilistResume
	anilistProgress = func(token string, show watchShow, ep int) (bool, error) {
		mu.Lock()
		got = append(got, fmt.Sprintf("progress %s:%d:%d", token, show.ID, ep))
		mu.Unlock()
		return true, nil
	}
	anilistResume = func(token string, show int, edit func(string) string) error {
		mu.Lock()
		got = append(got, fmt.Sprintf("note %s:%d:%s", token, show, edit("[otakase:4@300]")))
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { anilistProgress, anilistResume = oldProgress, oldResume })

	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 7, Ep: 4}, nil)
	waitLoaded(t, b)
	watchAllReady(b)
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 0, Playing: true, Dur: 1400}, nil)
	r := b.watch.only()
	for k := 0; k < 10; k++ {
		*now = now.Add(watchTick)
		b.watch.mu.Lock()
		for _, m := range r.members {
			m.seen = *now
		}
		b.watch.mu.Unlock()
		b.watchStep(r, *now)
	}
	// Into the ending, then b goes.
	b.watch.mu.Lock()
	r.setPos(1200, true, *now)
	b.watch.mu.Unlock()

	var left map[string]any
	watchPost(b, (*bot).watchLeave, "b", watchIn{Room: "r"}, &left)
	if left["done"] != true || left["ep"] != float64(4) {
		t.Fatalf("leave answered %v", left)
	}
	for k := 0; k < 200; k++ {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	sort.Strings(got)
	if len(got) != 2 || got[0] != "note tb:7:[otakase:5@0]" || got[1] != "progress tb:7:4" {
		t.Fatalf("saved %v, want b's list and note moved past episode 4", got)
	}
}

func TestLateJoinersAreAskedToCount(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) {
		d.WatchTickets = map[string]int{"a": 1}
		d.AniList = map[string]anilistLink{
			"a": {ID: 1, Verified: true, Token: "ta"},
			"c": {ID: 3, Verified: true, Token: "tc"},
			"d": {ID: 4, Verified: true, Token: "td"},
		}
	})
	var mu sync.Mutex
	var got []string
	oldProgress, oldResume := anilistProgress, anilistResume
	anilistProgress = func(token string, show watchShow, ep int) (bool, error) {
		mu.Lock()
		got = append(got, fmt.Sprintf("progress %s:%d", token, ep))
		mu.Unlock()
		return true, nil
	}
	anilistResume = func(token string, show int, edit func(string) string) error {
		mu.Lock()
		got = append(got, fmt.Sprintf("note %s:%s", token, edit("[otakase:4@300]")))
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { anilistProgress, anilistResume = oldProgress, oldResume })
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
	tick := func(n int) {
		r := b.watch.only()
		for k := 0; k < n; k++ {
			*now = now.Add(watchTick)
			b.watch.mu.Lock()
			for _, m := range r.members {
				m.seen = *now
			}
			b.watch.mu.Unlock()
			b.watchStep(r, *now)
		}
	}

	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 7, Ep: 4}, nil)
	waitLoaded(t, b)
	watchAllReady(b)
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 0, Playing: true, Dur: 1400}, nil)
	tick(60)
	// c and d come in two minutes late.
	watchPost(b, (*bot).watchOpen, "c", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchOpen, "d", watchIn{Room: "r", Channel: "vc"}, nil)
	tick(10)
	r := b.watch.only()
	var v watchView
	watchPost(b, (*bot).watchCount, "c", watchIn{Room: "r", OK: true}, &v)
	if len(wait(1)) != 0 {
		t.Fatalf("counted before the end: %v", got)
	}
	b.watch.mu.Lock()
	r.setPos(1200, true, *now)
	b.watch.mu.Unlock()
	tick(1)
	b.watch.mu.Lock()
	ask := b.watchViewOf(r, "c", false, *now).CountAsk
	b.watch.mu.Unlock()
	if ask != 4 {
		t.Fatalf("c was asked about episode %d", ask)
	}

	// c says yes in the room; d leaves and says yes on the leave screen.
	watchPost(b, (*bot).watchCount, "c", watchIn{Room: "r", OK: true}, &v)
	if v.CountAsk != 0 {
		t.Fatalf("asked again after answering")
	}
	var left map[string]any
	watchPost(b, (*bot).watchLeave, "d", watchIn{Room: "r"}, &left)
	if left["ask"] != true || left["ep"] != float64(4) {
		t.Fatalf("leave answered %v", left)
	}
	watchPost(b, (*bot).watchCount, "d", watchIn{Room: "r", OK: true}, nil)
	want := []string{"note tc:[otakase:5@0]", "note td:[otakase:5@0]", "progress tc:4", "progress td:4"}
	if l := wait(4); fmt.Sprint(l) != fmt.Sprint(want) {
		t.Fatalf("saved %v, want %v", l, want)
	}
}
