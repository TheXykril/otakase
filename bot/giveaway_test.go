package main

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
)

func gaTest(n int) *giveaway {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	g := &giveaway{ID: "m1", Every: 10, Started: now, JoinUntil: now.Add(time.Hour), Players: map[string]*gaPlayer{}}
	for k := range n {
		if err := gaJoin(g, fmt.Sprintf("u%d", k), now); err != nil {
			panic(err)
		}
	}
	return g
}

func TestHankoEarnSpend(t *testing.T) {
	var d storeData
	earnHanko(&d, "u1", 30)
	hankoWatch(&d, "u1", hankoWatchSecs*2+10)
	if w := d.Hanko["u1"]; w.Balance != 30+2*hankoPerWatch || w.WatchSecs != 10 {
		t.Fatalf("wallet %+v", w)
	}
	if spendHanko(&d, "u1", 1000) || !spendHanko(&d, "u1", 30) || d.Hanko["u1"].Earned != 40 {
		t.Errorf("spend %+v", d.Hanko["u1"])
	}
	if got := adjustHanko(&d, "u1", -500); got != 0 {
		t.Errorf("take below zero: %d", got)
	}
	addGamePoints(&d, "u2", "daily", 7, "2026-W43")
	if d.Hanko["u2"].Balance != 7 {
		t.Errorf("game points give hanko: %+v", d.Hanko["u2"])
	}
}

func TestMintedCodes(t *testing.T) {
	b := cheatTestBot(t)
	var codes []string
	b.store.update(func(d *storeData) { codes = mintCodes(d, "staff", "Oct giveaway", 3, appNow()) })
	if len(codes) != 3 {
		t.Fatal(codes)
	}
	x, err := b.redeemCheat("u1", formatCheatCode(codes[0]), appNow())
	if err != nil || x.Note != "Oct giveaway" {
		t.Fatalf("redeem %+v %v", x, err)
	}
	if _, err := b.redeemCheat("u2", codes[0], appNow()); err != errLinkNotFound {
		t.Errorf("second redeem: %v", err)
	}
	b.store.view(func(d *storeData) {
		if d.WatchTickets["u1"] != 1 || len(unusedMinted(d)) != 2 {
			t.Errorf("tickets %v unused %v", d.WatchTickets, unusedMinted(d))
		}
		if _, _, found := weekCode(d, "staff", appNow()); found {
			t.Error("minting must not count as finding a code")
		}
	})
}

func TestGiveawayShop(t *testing.T) {
	var d storeData
	g := gaTest(2)
	earnHanko(&d, "u0", 1000)
	if _, err := gaBuy(&d, g, "u0", "apple"); err != nil {
		t.Fatal(err)
	}
	// The second item costs twice its price.
	if _, err := gaBuy(&d, g, "u0", "armor"); err != nil || d.Hanko["u0"].Balance != 1000-25-180 {
		t.Fatalf("%v %d", err, d.Hanko["u0"].Balance)
	}
	if _, err := gaBuy(&d, g, "u0", "decoy"); err == nil {
		t.Error("third item")
	}
	if _, err := gaBuy(&d, g, "u1", "apple"); err == nil {
		t.Error("bought without hanko")
	}
	if _, err := gaBuy(&d, g, "u9", "apple"); err == nil {
		t.Error("bought without joining")
	}
}

func TestGiveawayEndsWithOneWinner(t *testing.T) {
	for seed := range 200 {
		rnd := rand.New(rand.NewPCG(uint64(seed), 3)).IntN
		g := gaTest(2 + seed%40)
		for _, u := range []string{"u0", "u1"} {
			g.Players[u].Items = map[string]int{"armor": 1, "decoy": 1, "revive": 1, "apple": 1}
		}
		g.Players["u1"].Alias = true
		now := g.JoinUntil
		if !g.closeJoining(now, rnd) {
			t.Fatal("closed")
		}
		for !g.Done {
			if g.Round > 500 {
				t.Fatalf("seed %d: no winner after 500 pages", seed)
			}
			now = now.Add(10 * time.Minute)
			g.writePage(now, rnd)
			if len(g.alive()) == 0 {
				t.Fatalf("seed %d: everyone out", seed)
			}
		}
		if len(g.alive()) != 1 || g.Winner != g.alive()[0] {
			t.Fatalf("seed %d: winner %q alive %v", seed, g.Winner, g.alive())
		}
	}
}

func TestGiveawayItems(t *testing.T) {
	never := func(n int) int { return n - 1 } // no twist, last pick
	g := gaTest(3)
	g.closeJoining(g.JoinUntil, never)
	g.Page = []string{"u0"}
	g.Players["u0"].Items = map[string]int{"armor": 1}
	g.writePage(g.JoinUntil, never)
	if g.Players["u0"].Out != 0 || g.Players["u0"].Items["armor"] != 0 {
		t.Errorf("armor %+v", g.Players["u0"])
	}
	g.Page = []string{"u0"}
	g.Players["u0"].Items["decoy"] = 1
	g.writePage(g.JoinUntil, never)
	if g.Players["u0"].Out != 0 || len(g.alive()) != 2 {
		t.Errorf("decoy: %+v alive %v", g.Players["u0"], g.alive())
	}
	g.Page = []string{"u0"}
	g.Players["u0"].Items["revive"] = 1
	g.writePage(g.JoinUntil, never)
	if !g.Players["u0"].Back || g.Done {
		t.Fatalf("revive: %+v done %v", g.Players["u0"], g.Done)
	}
	g.Page = nil
	g.writePage(g.JoinUntil, never)
	if g.Players["u0"].Out != 0 {
		t.Errorf("not back: %+v", g.Players["u0"])
	}
}

func TestGiveawayEyes(t *testing.T) {
	g := gaTest(4)
	if _, _, err := gaEyes(g, "u0"); err == nil {
		t.Error("eyes before joining closed")
	}
	g.closeJoining(g.JoinUntil, func(int) int { return 0 })
	g.Page = []string{"u0", "u1"}
	g.Players["u0"].Items = map[string]int{"eyes": 1}
	page, crossed, err := gaEyes(g, "u0")
	if err != nil || !crossed || len(page) != 2 || len(g.Page) != 1 {
		t.Errorf("%v %v %v %v", page, crossed, err, g.Page)
	}
	if _, _, err := gaEyes(g, "u0"); err == nil {
		t.Error("used twice")
	}
}

func TestGiveawayTooFew(t *testing.T) {
	g := gaTest(1)
	if g.closeJoining(g.JoinUntil, rand.IntN) || !g.Cancelled {
		t.Error("one name must call it off")
	}
}
