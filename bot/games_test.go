package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testGamesBot(t *testing.T) *bot {
	t.Helper()
	return &bot{store: openStore(filepath.Join(t.TempDir(), "s.json"))}
}

func TestGamePointsWeeks(t *testing.T) {
	b := testGamesBot(t)
	b.store.update(func(d *storeData) {
		addGamePoints(d, "a", "riddle", 3, "2026-W41")
		addGamePoints(d, "b", "daily", 5, "2026-W41")
		// a plays again next week: last week's points must stay countable.
		addGamePoints(d, "a", "scramble", 1, "2026-W42")
		d.Guess = map[string]guessScore{"a": {Total: 4, Week: 4, WeekOf: "2026-W41"}}
	})
	var last, now, all map[string]int
	b.store.view(func(d *storeData) {
		last, now, all = weekPoints(d, "2026-W41"), weekPoints(d, "2026-W42"), allPoints(d)
	})
	if last["a"] != 7 || last["b"] != 5 {
		t.Errorf("last week = %v, want a 7 (3 + 4 guess), b 5", last)
	}
	if now["a"] != 1 || now["b"] != 0 {
		t.Errorf("this week = %v", now)
	}
	if all["a"] != 8 || all["b"] != 5 {
		t.Errorf("all time = %v", all)
	}
	if w := weekWinners(last); len(w) != 1 || w[0] != "a" {
		t.Errorf("winners = %v", w)
	}
	if w := weekWinners(map[string]int{"x": 2, "y": 2, "z": 1}); len(w) != 2 {
		t.Errorf("a tie should share the win: %v", w)
	}
	if w := weekWinners(nil); len(w) != 0 {
		t.Errorf("nobody played: %v", w)
	}
	var games map[string]int
	b.store.view(func(d *storeData) { games = d.Games["a"].Games })
	if games["riddle"] != 3 || games["scramble"] != 1 {
		t.Errorf("per game = %v", games)
	}
}

func TestHigherLowerCap(t *testing.T) {
	b := testGamesBot(t)
	for range 8 {
		b.hlPoint("a")
	}
	var s gameScore
	b.store.view(func(d *storeData) { s = d.Games["a"] })
	if s.Total != hlDailyCap || s.HLToday != hlDailyCap {
		t.Errorf("points = %d (today %d), want the cap %d", s.Total, s.HLToday, hlDailyCap)
	}
	if b.hlBest("a", 4) != 4 || b.hlBest("a", 2) != 4 {
		t.Error("best run should stay the highest")
	}
}

func TestHigherLowerPick(t *testing.T) {
	a := guessShow{ID: 1, Score: 80}
	pool := []guessShow{a, {ID: 2, Score: 80}, {ID: 3, Score: 0}, {ID: 4, Score: 70}, {ID: 5, Score: 90}}
	seen := map[int]bool{}
	for k := range 4 {
		s, ok := hlNext(pool, a, "score", []int{5}, func(n int) int { return k % n })
		if !ok {
			t.Fatal("no show picked")
		}
		seen[s.ID] = true
	}
	if len(seen) != 1 || !seen[4] {
		t.Errorf("only show 4 has a different, known score and wasn't used: got %v", seen)
	}
	if !hlRight(a, guessShow{Score: 90}, "score", true) || hlRight(a, guessShow{Score: 90}, "score", false) {
		t.Error("90 after 80 is higher")
	}
	if !hlRight(guessShow{Popular: 10}, guessShow{Popular: 5}, "popularity", false) {
		t.Error("5 after 10 is lower")
	}
}

func TestDailyCompare(t *testing.T) {
	answer := guessShow{ID: 1, Year: 2016, Format: "TV", Episodes: 24, Score: 85, Studio: "MAPPA", Source: "MANGA",
		Genres: []string{"Action", "Drama"}, Rank: 40}
	g := guessShow{ID: 2, Year: 2015, Format: "MOVIE", Episodes: 1, Score: 85, Studio: "MAPPA", Source: "ORIGINAL",
		Genres: []string{"Drama", "Romance"}, Rank: 100}
	cells := map[string]dailyCell{}
	for _, c := range dailyCompare(g, answer) {
		cells[c.Key] = c
	}
	want := map[string][2]string{
		"year":       {"near", "up"},
		"format":     {"no", ""},
		"episodes":   {"no", "up"},
		"score":      {"yes", ""},
		"studio":     {"yes", ""},
		"source":     {"no", ""},
		"genres":     {"near", ""},
		"popularity": {"no", "up"}, // the answer is more popular (#40 vs #100)
	}
	if len(cells) != len(want) {
		t.Fatalf("got %d cells", len(cells))
	}
	for k, w := range want {
		if c := cells[k]; c.Hit != w[0] || c.Arrow != w[1] {
			t.Errorf("%s = %s %q, want %s %q", k, c.Hit, c.Arrow, w[0], w[1])
		}
	}
	for _, c := range dailyCompare(answer, answer) {
		if c.Hit != "yes" {
			t.Errorf("the answer itself: %s is %s", c.Key, c.Hit)
		}
	}
	if c := dailyNumber("episodes", "Episodes", "0", 0, 12, 3, false); c.Text != "?" || c.Hit != "no" {
		t.Errorf("unknown episodes = %+v", c)
	}
}

func TestDailyPointsAndNumbers(t *testing.T) {
	for n, want := range map[int]int{1: 5, 2: 5, 3: 4, 4: 4, 5: 3, 6: 3, 7: 2, 8: 2} {
		if got := dailyPoints(n); got != want {
			t.Errorf("dailyPoints(%d) = %d, want %d", n, got, want)
		}
	}
	if dailyNum("2026-10-13") != 1 || dailyNum("2026-10-20") != 8 {
		t.Error("puzzle numbers start at 1 on 2026-10-13")
	}
	pool := []guessShow{{ID: 1, Rank: 1}, {ID: 2, Rank: 500}, {ID: 3, Rank: 2}}
	for k := range 3 {
		s, _ := dailyPick(pool, []int{1}, func(n int) int { return k % n })
		if s.ID != 3 {
			t.Errorf("pick = %d, want 3 (top 300, not used lately)", s.ID)
		}
	}
	pl := dailyPlay{Solved: true, Guesses: []dailyGuess{
		{Cells: []dailyCell{{Hit: "no"}, {Hit: "near"}}},
		{Cells: []dailyCell{{Hit: "yes"}, {Hit: "yes"}}},
	}}
	if got := dailyGrid(pl); got != "⬛🟨\n🟩🟩" {
		t.Errorf("grid = %q", got)
	}
	if dailyScore(pl) != "2/8" || dailyScore(dailyPlay{}) != "X/8" {
		t.Error("score text")
	}
}

func TestDailyTry(t *testing.T) {
	b := testGamesBot(t)
	answer := guessShow{ID: 1, Name: "Frieren", Titles: []string{"Frieren"}, Rank: 3, Year: 2023}
	other := guessShow{ID: 2, Name: "Naruto", Titles: []string{"Naruto"}, Rank: 9, Year: 2002}
	b.guess.pool, b.guess.fetched = []guessShow{answer, other}, time.Now()
	day := dailyDay(time.Now())
	b.store.update(func(d *storeData) { d.Daily = &dailyPuzzle{Day: day, Num: dailyNum(day), Show: answer} })

	if _, _, _, err := b.dailyTry("u", 99); err == nil {
		t.Error("a show outside the pool should be refused")
	}
	_, pl, done, err := b.dailyTry("u", 2)
	if err != nil || done || len(pl.Guesses) != 1 {
		t.Fatalf("first try: %v %v %+v", err, done, pl)
	}
	if _, _, _, err := b.dailyTry("u", 2); err == nil {
		t.Error("the same show twice should be refused")
	}
	_, pl, done, err = b.dailyTry("u", 1)
	if err != nil || !done || !pl.Solved {
		t.Fatalf("solve: %v %v %+v", err, done, pl)
	}
	if _, _, _, err := b.dailyTry("u", 2); err == nil {
		t.Error("no tries after the end")
	}
	var st dailyStats
	var pts int
	b.store.view(func(d *storeData) { st, pts = d.DailyStats["u"], d.Games["u"].Total })
	if st.Played != 1 || st.Won != 1 || st.Streak != 1 || pts != dailyPoints(2) {
		t.Errorf("stats %+v, points %d", st, pts)
	}
	if got := dailyMatches(b.guess.pool, "frie", 5); len(got) != 1 || got[0].ID != 1 {
		t.Errorf("matches = %v", got)
	}
}

func TestBuiltinRiddles(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range builtinRiddles {
		if ids[r.ID] {
			t.Errorf("riddle id %s twice", r.ID)
		}
		ids[r.ID] = true
		if !riddleEmojis(r.Emojis) {
			t.Errorf("%s: %q isn't emojis only", r.Name, r.Emojis)
		}
		if !guessMatches(r.Name, r.Titles) {
			t.Errorf("%s: its own name doesn't count as right", r.Name)
		}
	}
	if riddleEmojis("🍥 naruto") || riddleEmojis("🍥") || riddleEmojis("🍥 9") {
		t.Error("letters, digits or a single emoji should be refused")
	}
	b := testGamesBot(t)
	b.store.update(func(d *storeData) { d.Riddles = []riddle{{ID: "m1", Name: "X", Emojis: "🍥🍥"}} })
	if _, ok := b.store.removeRiddle("s1"); !ok {
		t.Error("built-in riddle should turn off")
	}
	if _, ok := b.store.removeRiddle("m1"); !ok {
		t.Error("added riddle should go")
	}
	var left []riddle
	b.store.view(func(d *storeData) { left = riddles(d) })
	if len(left) != len(builtinRiddles)-1 {
		t.Errorf("%d riddles left", len(left))
	}
}

func TestScramble(t *testing.T) {
	for _, ok := range []string{"Naruto", "One Piece", "Death Note"} {
		if !scrambleable(ok) {
			t.Errorf("%q should scramble", ok)
		}
	}
	for _, no := range []string{"Re:Zero", "Fullmetal Alchemist Brotherhood Extra", "K", "Steins;Gate"} {
		if scrambleable(no) {
			t.Errorf("%q shouldn't scramble", no)
		}
	}
	seq := 0
	intn := func(n int) int { seq++; return seq % n }
	got := scrambleTitle("One Piece", intn)
	words := strings.Split(got, "   ")
	if len(words) != 2 || strings.ReplaceAll(words[0], " ", "") == "ONE" {
		t.Errorf("scramble = %q", got)
	}
	if sortRunes(strings.ReplaceAll(words[1], " ", "")) != sortRunes("PIECE") {
		t.Errorf("letters changed: %q", words[1])
	}
	if scrambleHint("One Piece") != "O _ _   P _ _ _ _" {
		t.Errorf("hint = %q", scrambleHint("One Piece"))
	}
}

func sortRunes(s string) string {
	r := []rune(s)
	for i := range r {
		for j := i + 1; j < len(r); j++ {
			if r[j] < r[i] {
				r[i], r[j] = r[j], r[i]
			}
		}
	}
	return string(r)
}

func TestBingo(t *testing.T) {
	card := bingoCard("2026-10")
	again := bingoCard("2026-10")
	if len(card) != 9 || card[4] != nil {
		t.Fatalf("card should be 9 squares with a free middle")
	}
	genres := 0
	for k, s := range card {
		if k == 4 {
			continue
		}
		if s != again[k] {
			t.Error("the same month should give the same card")
		}
		if s.Genre {
			genres++
		}
	}
	if genres > 3 {
		t.Errorf("%d genre squares", genres)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	movie := &bingoSquares[0]
	test := []*bingoSquare{movie, movie, movie, nil, nil, nil, nil, nil, nil}
	e := listEntry{Status: "COMPLETED", UpdatedAt: now.Unix(), CompletedAt: fuzzyDate{2026, 10, 2}}
	e.Media.Format = "MOVIE"
	e.Media.Title.English = "Your Name"
	old := e
	old.CompletedAt, old.UpdatedAt = fuzzyDate{2026, 9, 2}, now.AddDate(0, -1, 0).Unix()
	adult := e
	adult.Media.IsAdult = true
	fill := bingoFill(test, []listEntry{old, adult, e}, now)
	if fill[0] != "Your Name" || fill[1] != "Your Name" || fill[4] != "free" || fill[3] != "" {
		t.Errorf("fill = %q", fill)
	}
	lines, full := bingoScore(fill)
	if bitCount(lines) != 1 || full {
		t.Errorf("one row done: lines %b, full %v", lines, full)
	}

	b := testGamesBot(t)
	if got := b.bingoAward("u", fill, now); got != bingoLine {
		t.Errorf("first award = %d", got)
	}
	if got := b.bingoAward("u", fill, now); got != 0 {
		t.Errorf("same lines again = %d", got)
	}
	all := []string{"a", "a", "a", "a", "free", "a", "a", "a", "a"}
	if got := b.bingoAward("u", all, now); got != 7*bingoLine+bingoFull {
		t.Errorf("full card = %d", got)
	}
	if got := b.bingoAward("u", all, now.AddDate(0, 1, 0)); got != 8*bingoLine+bingoFull {
		t.Errorf("a new month starts over: %d", got)
	}
}

func TestActivitySessions(t *testing.T) {
	var a activitySessions
	tok := a.add("u1")
	r := httptest.NewRequest("GET", "/activity/api/daily", nil)
	if a.user(r) != "" {
		t.Error("no token, no user")
	}
	r.Header.Set("Authorization", "Bearer "+tok)
	if a.user(r) != "u1" {
		t.Error("token should give its user")
	}
	r.Header.Set("Authorization", "Bearer nope")
	if a.user(r) != "" {
		t.Error("a wrong token gives nobody")
	}
	b := testGamesBot(t)
	w := httptest.NewRecorder()
	b.activityDaily(w, httptest.NewRequest("GET", "/activity/api/daily", nil))
	if w.Code != 401 {
		t.Errorf("signed out = %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] == "" {
		t.Error("error message missing")
	}
	for _, f := range []string{"index.html", "app.js", "sdk.js"} {
		if _, err := activityFiles.ReadFile("assets/activity/" + f); err != nil {
			t.Errorf("%s not embedded: %v", f, err)
		}
	}
}

func TestSoloRounds(t *testing.T) {
	b := testGamesBot(t)
	r := &soloRound{game: "riddle", name: "Naruto", titles: []string{"Naruto"}, puzzle: "🍥🦊", clues: []string{"Title: N _ _ _ _ _"}}
	v := r.view()
	if len(v.Clues) != 0 || !v.More || v.Answer != "" || v.Left != 3 {
		t.Errorf("start view = %+v", v)
	}
	b.soloAnswer("u", r, "bleach", false)
	if v = r.view(); len(v.Clues) != 1 || v.More || v.Left != 2 || v.Done {
		t.Errorf("after a wrong try = %+v", v)
	}
	b.soloAnswer("u", r, "naruto", false)
	if v = r.view(); !v.Done || !v.Right || v.Points != 1 || v.Answer != "Naruto" {
		t.Errorf("after the right try = %+v", v)
	}
	own := &soloRound{game: "riddle", titles: []string{"Naruto"}, by: "u", clues: []string{"x"}}
	b.soloAnswer("u", own, "Naruto", false)
	if !own.right || own.points != 0 {
		t.Error("your own riddle wins no point")
	}
	lose := &soloRound{game: "scramble", titles: []string{"Naruto"}, clues: []string{"x"}}
	for range 3 {
		b.soloAnswer("u", lose, "no", false)
	}
	if !lose.done || lose.right {
		t.Error("three wrong tries end a scramble")
	}
	for range 15 {
		b.actPoint("v", "guess")
	}
	var s gameScore
	b.store.view(func(d *storeData) { s = d.Games["v"] })
	if s.Total != actDailyCap {
		t.Errorf("Activity points = %d, want the cap %d", s.Total, actDailyCap)
	}
	if stripMarkdown("Studio **MAPPA** `x`") != "Studio MAPPA x" {
		t.Error("markdown left in a clue")
	}
}

func TestActivityFonts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /activity/fonts/{name}", apiRoutes["GET /activity/fonts/{name}"])
	for path, want := range map[string]int{
		"/activity/fonts/Inter-SemiBold.otf": http.StatusOK,
		"/activity/fonts/OtakaseSymbols.ttf": http.StatusNotFound,
		"/activity/fonts/Inter-Nope.otf":     http.StatusNotFound,
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("%s = %d, want %d", path, w.Code, want)
		}
	}
}
