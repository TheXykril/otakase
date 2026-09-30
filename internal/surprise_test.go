package internal

import (
	"strings"
	"testing"
)

func TestSurprisePickAvoidsTheLastPick(t *testing.T) {
	entries := []Entry{{Media: Media{ID: 1}}, {Media: Media{ID: 2}}, {Media: Media{ID: 3}}}
	first := func(int) int { return 0 }

	if pick, ok := surprisePick(entries, 0, first); !ok || pick.Media.ID != 1 {
		t.Fatalf("pick = %+v, %v", pick, ok)
	}
	// Reroll never shows the same show twice running.
	if pick, _ := surprisePick(entries, 1, first); pick.Media.ID != 2 {
		t.Fatalf("reroll repeated the pick: %+v", pick)
	}
	// With one show there is nothing else to offer.
	if pick, _ := surprisePick(entries[:1], 1, first); pick.Media.ID != 1 {
		t.Fatalf("single show = %+v", pick)
	}
	if _, ok := surprisePick(nil, 0, first); ok {
		t.Fatal("empty list picked something")
	}

	// Every show is reachable.
	seen := map[int]bool{}
	for i := 0; i < 3; i++ {
		pick, _ := surprisePick(entries, 0, func(int) int { return i })
		seen[pick.Media.ID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("reached %v", seen)
	}
}

func TestSurpriseLabel(t *testing.T) {
	entry := Entry{Media: Media{Title: AnimeTitle{Romaji: "Sousou no Frieren"}, Format: "TV", Episodes: 28}}
	if got := surpriseLabel(entry, &Config{}); got != "▶ Start Sousou no Frieren (TV, 28 episodes)" {
		t.Fatalf("label = %q", got)
	}
	film := Entry{Media: Media{Title: AnimeTitle{Romaji: "Kimi no Na wa."}, Format: "MOVIE", Episodes: 1}}
	if got := surpriseLabel(film, &Config{}); !strings.HasSuffix(got, "(MOVIE, 1 episode)") {
		t.Fatalf("label = %q", got)
	}
	unknown := Entry{Media: Media{Title: AnimeTitle{Romaji: "X"}, Format: "TV_SHORT"}}
	if got := surpriseLabel(unknown, &Config{}); got != "▶ Start X (TV SHORT)" {
		t.Fatalf("label = %q", got)
	}
}

func TestInjectMenuKeysSince(t *testing.T) {
	m := map[string]string{"MenuOrder": "CURRENT,ALL,CONTINUE_LAST,TRACKER"}
	if added := injectMenuKeysSince(m, "26.1.0", "26.1.0"); len(added) != 0 {
		t.Fatalf("same version added %v", added)
	}
	added := injectMenuKeysSince(m, "2.2.2", "26.1.0")
	if len(added) != 1 || added[0] != "SURPRISE" {
		t.Fatalf("added %v", added)
	}
	// Placed where the default order has it: after Continue Last Session.
	if m["MenuOrder"] != "CURRENT,ALL,CONTINUE_LAST,SURPRISE,TRACKER" {
		t.Fatalf("MenuOrder = %q", m["MenuOrder"])
	}
	if again := injectMenuKeysSince(m, "2.2.2", "26.1.0"); len(again) != 0 {
		t.Fatalf("added twice: %v", again)
	}

	// Without its neighbour it goes at the end.
	m = map[string]string{"MenuOrder": "ALL, CURRENT"}
	injectMenuKeysSince(m, "2.2.2", "26.1.0")
	if m["MenuOrder"] != "ALL,CURRENT,SURPRISE" {
		t.Fatalf("MenuOrder = %q", m["MenuOrder"])
	}

	// No saved MenuOrder means the default, which already has it.
	m = map[string]string{"Player": "mpv"}
	if added := injectMenuKeysSince(m, "2.2.2", "26.1.0"); len(added) != 0 || m["MenuOrder"] != "" {
		t.Fatalf("touched a config without MenuOrder: %v %v", added, m)
	}
}
