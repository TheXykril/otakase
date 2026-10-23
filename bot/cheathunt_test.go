package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// cheatTestPool is enough shows for every kind of puzzle.
func cheatTestPool() []guessShow {
	studios := []string{"Kyoto Animation", "Madhouse", "Bones", "A-1 Pictures", "Toei Animation", "Sunrise", "Shaft",
		"Trigger", "Wit Studio", "MAPPA", "Production I.G", "Pierrot", "Ufotable", "Lerche", "David Production",
		"CloverWorks", "J.C.Staff", "White Fox", "P.A. Works", "Doga Kobo", "Studio Deen", "Gainax", "Nippon Animation",
		"OLM", "Brain's Base", "Kinema Citrus", "Yostar Pictures", "Haoliners", "Encourage Films", "Silver Link"}
	seasons := []string{"WINTER", "SPRING", "SUMMER", "FALL"}
	var pool []guessShow
	for k := range 120 {
		pool = append(pool, guessShow{ID: 1000 + k, MalID: 5000 + k, Name: fmt.Sprintf("Show %d", k),
			Studio: studios[k%len(studios)], Year: 1980 + k%45, Season: seasons[k%4], Episodes: 12 + k%40,
			Leads: []string{fmt.Sprintf("Hero Number%d", k), fmt.Sprintf("Friend Number%d", k)}})
	}
	return pool
}

func TestCiphersRoundTrip(t *testing.T) {
	plain := "WHISPER SILENT KITSUNE TO THE BOT"
	if got := caesar(caesar(plain, 7), 19); got != plain {
		t.Errorf("caesar: %q", got)
	}
	if got := atbash(atbash(plain)); got != plain {
		t.Errorf("atbash: %q", got)
	}
	if got := vigenere("ATTACK", "LEMON"); got != "LXFOPV" {
		t.Errorf("vigenere: %q", got)
	}
	if got := a1z26("AB Z"); got != "1-2 / 26" {
		t.Errorf("a1z26: %q", got)
	}
}

func TestHuntNorm(t *testing.T) {
	if huntNorm("A-1 Pictures") != huntNorm("a1pictures") || huntNorm(" Spring 2014 ") != "spring2014" {
		t.Error("norm")
	}
	if a := studioAnswers("Studio Deen"); len(a) != 2 || a[1] != "deen" {
		t.Errorf("studio answers %v", a)
	}
}

func TestEveryKindBuilds(t *testing.T) {
	pool := cheatTestPool()
	for seed := range 40 {
		intn := rand.New(rand.NewPCG(uint64(seed), 9)).IntN
		for _, k := range huntKinds {
			p, err := buildHunt(k.name, k.first, pool, intn)
			if err != nil {
				t.Fatalf("%s seed %d: %v", k.name, seed, err)
			}
			if len(p.Steps) != 3 || len(p.Steps[0].Answers) == 0 || p.Steps[1].Whisper == "" || len(p.Steps[2].Answers) == 0 {
				t.Fatalf("%s: bad steps %+v", k.name, p.Steps)
			}
			for _, s := range p.Steps {
				if strings.TrimSpace(s.Text) == "" {
					t.Fatalf("%s: empty text", k.name)
				}
			}
		}
	}
}

func TestPuzzlesVary(t *testing.T) {
	pool := cheatTestPool()
	texts := map[string]bool{}
	for seed := range 20 {
		p, err := newHuntPuzzle(pool, rand.New(rand.NewPCG(uint64(seed), 1)).IntN)
		if err != nil {
			t.Fatal(err)
		}
		texts[p.Steps[0].Text+p.Steps[1].Text] = true
	}
	if len(texts) < 19 {
		t.Errorf("only %d different puzzles in 20", len(texts))
	}
}

func TestGridHasWordOnce(t *testing.T) {
	intn := rand.New(rand.NewPCG(3, 4)).IntN
	for range 30 {
		s, word, err := huntGrid(nil, intn)
		if err != nil {
			t.Fatal(err)
		}
		var grid [][]byte
		for _, l := range strings.Split(s.Text, "\n") {
			if row := strings.ReplaceAll(strings.TrimSpace(l), " ", ""); len(row) == huntGridSize && strings.ToUpper(row) == row {
				grid = append(grid, []byte(row))
			}
		}
		if len(grid) != huntGridSize || countWord(grid, strings.ToUpper(word)) != 1 {
			t.Fatalf("word %q not found once in\n%s", word, s.Text)
		}
	}
}

func TestTimelineOrder(t *testing.T) {
	pool := cheatTestPool()
	byMal := map[string]guessShow{}
	for _, s := range pool {
		byMal[malURL(s.MalID)] = s
	}
	s, word, err := huntTimeline(pool, rand.New(rand.NewPCG(5, 6)).IntN)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		letter byte
		year   int
	}
	var rows []row
	for _, l := range strings.Split(s.Text, "\n") {
		if strings.HasPrefix(l, "[") {
			rows = append(rows, row{l[1], byMal[l[4:]].Year})
		}
	}
	for a := range rows {
		for b := range rows {
			if rows[a].year < rows[b].year+2 && rows[a].year > rows[b].year-2 && a != b {
				t.Fatalf("years too close: %+v", rows)
			}
		}
	}
	var got []byte
	for len(rows) > 0 {
		k := 0
		for j := range rows {
			if rows[j].year < rows[k].year {
				k = j
			}
		}
		got = append(got, rows[k].letter)
		rows = append(rows[:k], rows[k+1:]...)
	}
	if strings.ToLower(string(got)) != word {
		t.Errorf("letters read %q, answer %q", got, word)
	}
}

func cheatTestBot(t *testing.T) *bot {
	b := appTestBot(t)
	cheatStartLimit = newRateLimit(30, time.Hour)
	cheatAnswerLimit = newRateLimit(10, time.Hour)
	cheatRedeemFails = newRateLimit(5, 10*time.Minute)
	cheatPool = func(*bot) ([]guessShow, error) { return cheatTestPool(), nil }
	cheatRand = func() func(int) int { return rand.New(rand.NewPCG(7, 8)).IntN }
	t.Cleanup(func() {
		cheatPool = func(b *bot) ([]guessShow, error) { return b.guess.shows() }
		cheatRand = func() func(int) int { return rand.IntN }
	})
	return b
}

func TestCheatHuntFlow(t *testing.T) {
	b := cheatTestBot(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	appNow = func() time.Time { return now }
	tok := testToken(b, "u1")

	if w := apiJSON(t, b, "/v1/cheat", `{}`, ""); w.Code != 401 {
		t.Fatalf("unlinked: %d", w.Code)
	}
	m := decode(t, apiJSON(t, b, "/v1/cheat", `{}`, tok))
	if m["status"] != "puzzle" || m["step"] != 1.0 || m["where"] != "app" {
		t.Fatalf("start: %v", m)
	}
	// The same hunt comes back.
	if again := decode(t, apiJSON(t, b, "/v1/cheat", `{}`, tok)); again["text"] != m["text"] {
		t.Fatal("hunt changed on reopen")
	}

	var h cheatHunt
	b.store.view(func(d *storeData) { h = d.CheatHunts["u1"] })
	if m := decode(t, apiJSON(t, b, "/v1/cheat/answer", `{"answer":"definitely wrong"}`, tok)); m["correct"] != false || m["message"] != "Nothing happens." {
		t.Fatalf("wrong answer: %v", m)
	}
	m = decode(t, apiJSON(t, b, "/v1/cheat/answer", fmt.Sprintf(`{"answer":%q}`, strings.ToUpper(h.Puzzle.Steps[0].Answers[0])), tok))
	if m["correct"] != true || m["where"] != "discord" {
		t.Fatalf("step 1: %v", m)
	}
	// The whisper is for Discord, not the app.
	m = decode(t, apiJSON(t, b, "/v1/cheat/answer", fmt.Sprintf(`{"answer":%q}`, h.Puzzle.Steps[1].Whisper), tok))
	if m["correct"] != false || m["step"] != 2.0 || !strings.Contains(m["message"].(string), "Somewhere else") {
		t.Fatalf("whisper in app: %v", m)
	}
	if _, ok := b.cheatWhisper("u2", h.Puzzle.Steps[1].Whisper, now); ok {
		t.Fatal("someone else's whisper counted")
	}
	next, ok := b.cheatWhisper("u1", h.Puzzle.Steps[1].Whisper, now)
	if !ok || next != h.Puzzle.Steps[2].Text {
		t.Fatalf("whisper: %v %q", ok, next)
	}
	m = decode(t, apiJSON(t, b, "/v1/cheat/answer", fmt.Sprintf(`{"answer":%q}`, h.Puzzle.Steps[2].Answers[0]), tok))
	code, _ := m["code"].(string)
	if m["correct"] != true || m["status"] != "found" || len(code) != 14 {
		t.Fatalf("solve: %v", m)
	}
	// One a week: reopening shows the code, not a new hunt.
	if m := decode(t, apiJSON(t, b, "/v1/cheat", `{}`, tok)); m["status"] != "found" || m["code"] != code {
		t.Fatalf("after solve: %v", m)
	}

	// Anyone can redeem it, once.
	x, err := b.redeemCheat("u2", strings.ToLower(strings.ReplaceAll(code, "-", " ")), now)
	if err != nil || x.Finder != "u1" || x.RedeemedBy != "u2" {
		t.Fatalf("redeem: %+v %v", x, err)
	}
	if _, err := b.redeemCheat("u3", code, now); err != errLinkNotFound {
		t.Fatalf("second redeem: %v", err)
	}
	if _, err := b.redeemCheat("u3", "ZZZZ-ZZZZ-ZZZZ", now); err != errLinkWrong {
		t.Fatalf("made up code: %v", err)
	}
	if m := decode(t, apiJSON(t, b, "/v1/cheat", `{}`, tok)); m["redeemed"] != true {
		t.Fatalf("redeemed flag: %v", m)
	}
	b.store.view(func(d *storeData) {
		if d.WatchTickets["u2"] != 1 || d.WatchTickets["u1"] != 0 {
			t.Fatalf("tickets %v", d.WatchTickets)
		}
	})

	// Next week a new hunt opens.
	now = nextWeek(now).Add(time.Hour)
	if m := decode(t, apiJSON(t, b, "/v1/cheat", `{}`, tok)); m["status"] != "puzzle" {
		t.Fatalf("next week: %v", m)
	}
}

func TestCheatHuntLost(t *testing.T) {
	b := cheatTestBot(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	if _, err := b.cheatStart("u1", now); err != nil {
		t.Fatal(err)
	}
	for range cheatMaxWrong {
		b.cheatAnswer("u1", "nope", now)
	}
	v, ok := func() (cheatView, bool) {
		var v cheatView
		var ok bool
		b.store.view(func(d *storeData) { v, ok = cheatState(d, "u1", now) })
		return v, ok
	}()
	if !ok || v.Status != "wait" {
		t.Fatalf("lost: %+v", v)
	}
	v, _ = b.cheatStart("u1", now.Add(cheatLostPause+time.Minute))
	if v.Status != "puzzle" || v.WrongLeft != cheatMaxWrong {
		t.Fatalf("new hunt after the pause: %+v", v)
	}
}

func TestNextWeek(t *testing.T) {
	for _, day := range []int{19, 22, 25} { // Monday, Thursday, Sunday
		got := nextWeek(time.Date(2026, 10, day, 15, 0, 0, 0, time.UTC))
		if want := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
			t.Errorf("Oct %d: %v", day, got)
		}
	}
}
