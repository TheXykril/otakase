package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestKoiKoiDeck(t *testing.T) {
	n := map[byte]int{}
	for c := range 48 {
		n[kkKind(c)]++
	}
	if n['B'] != 5 || n['A'] != 9 || n['R'] != 10 || n['P'] != 24 {
		t.Errorf("kinds %v", n)
	}
	for _, c := range []int{kkCurtain, kkMoon, kkRainMan} {
		if kkKind(c) != 'B' {
			t.Errorf("%d should be a bright", c)
		}
	}
	for _, c := range append(append([]int{kkSake, kkBoar, kkDeer, kkButterfly}, kkPoem...), kkBlue...) {
		if k := kkKind(c); k != 'A' && k != 'R' {
			t.Errorf("%d is %c", c, k)
		}
	}
}

func TestKoiKoiScore(t *testing.T) {
	cases := []struct {
		got  []int
		want int
	}{
		{[]int{0, 8, 28}, 5},                               // three brights
		{[]int{0, 8, 28, 40}, 7},                           // rainy four
		{[]int{0, 8, 28, 44}, 8},                           // four brights
		{[]int{0, 8, 28, 40, 44}, 10},                      // five
		{[]int{0, 8, 40}, 0},                               // three with the rain man isn't a combo
		{[]int{kkBoar, kkDeer, kkButterfly}, 5},            // boar, deer, butterfly
		{append(slices.Clone(kkPoem), kkBlue...), 12},      // both ribbon sets and 6 ribbons
		{[]int{kkCurtain, kkSake}, 5},                      // flower viewing
		{[]int{kkMoon, kkCurtain, kkSake}, 10},             // both viewings
		{[]int{2, 3, 6, 7, 10, 11, 14, 15, 18, 19}, 1},     // ten plains
		{[]int{2, 3, 6, 7, 10, 11, 14, 15, 18, kkSake}, 1}, // the cup is a plain too
	}
	for _, c := range cases {
		if ys, got := kkScore(c.got); got != c.want {
			t.Errorf("%v: %d %v, want %d", c.got, got, ys, c.want)
		}
	}
}

func TestKoiKoiTables(t *testing.T) {
	pool := pzPool()
	for i := range pool {
		pool[i].Cover = "https://s4.anilist.co/x.jpg"
	}
	name := func(id string) string { return id }
	people := []string{"a", "b", "c", "d", "e"}
	g, ok := newKoiKoi(people, 4, pool, name, seeded())
	if !ok || len(g.tables) != 2 || len(g.shows) != 12 {
		t.Fatalf("tables %v", ok)
	}
	humans := 0
	for _, tb := range g.tables {
		if len(tb.seats) != 4 {
			t.Errorf("seats %d", len(tb.seats))
		}
		for _, s := range tb.seats {
			if !s.bot {
				humans++
			}
		}
	}
	if humans != 5 {
		t.Errorf("seated %d people", humans)
	}
	for size, hand := range map[int]int{2: 8, 3: 7, 4: 5} {
		g, _ := newKoiKoi([]string{"a"}, size, pool, name, seeded())
		tb := g.tables[0]
		tb.deal(time.Now(), seeded())
		cards := slices.Clone(tb.deck)
		cards = append(cards, tb.field...)
		for _, s := range tb.seats {
			if len(s.hand) != hand {
				t.Errorf("size %d: hand %d", size, len(s.hand))
			}
			cards = append(cards, s.hand...)
		}
		slices.Sort(cards)
		if len(slices.Compact(cards)) != 48 {
			t.Errorf("size %d: deck has %d cards", size, len(cards))
		}
		if len(tb.deck) < size*hand {
			t.Errorf("size %d: deck runs out", size)
		}
	}
}

// A whole game of bots always ends, with every card accounted for.
func TestKoiKoiBots(t *testing.T) {
	pool := pzPool()
	for i := range pool {
		pool[i].Cover = "c"
	}
	for size := 2; size <= 4; size++ {
		for seed := range 20 {
			intn := seeded()
			for range seed {
				intn(5)
			}
			g, _ := newKoiKoi(nil, size, pool, nil, intn)
			g.tables = append(g.tables, &kkTable{})
			tb := g.tables[0]
			for i := range size {
				tb.seats = append(tb.seats, &kkSeat{id: "bot:" + string(rune('a'+i)), bot: true})
			}
			now := time.Unix(0, 0)
			tb.deal(now, intn)
			for step := 0; tb.stage != "finished"; step++ {
				if step > 1000 {
					t.Fatalf("size %d seed %d: stuck in %s", size, seed, tb.stage)
				}
				if tb.stage == "play" {
					n := len(tb.deck) + len(tb.field)
					for _, s := range tb.seats {
						n += len(s.hand) + len(s.got)
					}
					if n != 48 {
						t.Fatalf("size %d seed %d: %d cards", size, seed, n)
					}
				}
				now = now.Add(10 * time.Second)
				tb.step(now, func(string) bool { return true }, intn)
			}
			if len(tb.results) != kkHands {
				t.Errorf("%d hands", len(tb.results))
			}
		}
	}
}

func TestKoiKoiPlay(t *testing.T) {
	now := time.Unix(0, 0)
	a, c := &kkSeat{id: "a", name: "a"}, &kkSeat{id: "c", name: "c"}
	tb := &kkTable{seats: []*kkSeat{a, c}, stage: "play"}
	// Pine bright and pine plain on the field, the pine ribbon in hand.
	tb.field = []int{0, 2, 20}
	a.hand = []int{1, 21}
	c.hand = []int{30, 31}
	tb.deck = []int{45, 46, 47, 40}
	if err := tb.play(1, -1, now); err == nil {
		t.Fatal("two matches need a pick")
	}
	if err := tb.play(1, 2, now); err == nil {
		t.Fatal("a pick that isn't the same month should fail")
	}
	if err := tb.play(1, 0, now); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(a.got, 0) || !slices.Contains(a.got, 1) || tb.turn != 1 || len(tb.field) != 3 {
		t.Fatalf("got %v field %v turn %d", a.got, tb.field, tb.turn)
	}
	if err := tb.play(31, -1, now); err != nil {
		t.Fatal(err)
	}
	// Someone with a combo decides; stopping scores it, doubled by a koi-koi.
	tb.stage, tb.turn = "decide", 0
	a.got = []int{0, 8, 28}
	c.koi = true
	if err := tb.decide(false, now); err != nil {
		t.Fatal(err)
	}
	if tb.result == nil || tb.result.Points != 10 || !tb.result.Koi || a.total != 10 || tb.stage != "done" {
		t.Errorf("result %+v total %d", tb.result, a.total)
	}
}

func TestKoiKoiParty(t *testing.T) {
	b, now := togetherBot(t)
	b.guess.pool = pzPool()
	for i := range b.guess.pool {
		b.guess.pool[i].Cover = "c"
	}
	r := b.together.room("r")
	for i, u := range []string{"a", "b", "c"} {
		r.touch(u, now.Add(time.Duration(i)*time.Second))
	}
	r.game, r.stat = "koikoi", "2"
	if err := b.socialStart(r, "koikoi", b.guess.pool, *now); err != nil {
		t.Fatal(err)
	}
	if len(r.kk.tables) != 2 {
		t.Fatalf("3 people at tables of 2: %d tables", len(r.kk.tables))
	}
	v := b.kkView(r, "a", *now)
	if v.Me < 0 || len(v.Mine) != 8 || len(v.Seats) != 2 || len(v.Shows) != 12 {
		t.Fatalf("view %+v", v)
	}
	if err := b.kkAct(r, "zz", togetherIn{Kind: "play"}, *now); err == nil {
		t.Error("a watcher can't play")
	}
	// Everyone leaves the tables to the bots: the game still ends.
	delete(r.members, "a")
	delete(r.members, "b")
	delete(r.members, "c")
	r.members["w"] = &togetherMember{}
	for i := 0; r.phase != "over"; i++ {
		if i > 2000 {
			t.Fatal("never ended")
		}
		*now = now.Add(10 * time.Second)
		b.kkStep(r, *now)
	}
	for _, w := range r.winners {
		if w == "" || strings.HasPrefix(w, "bot:") {
			t.Errorf("winner %q", w)
		}
	}
}
