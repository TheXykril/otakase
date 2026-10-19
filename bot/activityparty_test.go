package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// togetherBot is a games bot with a fixed clock and a small show pool.
func togetherBot(t *testing.T) (*bot, *time.Time) {
	t.Helper()
	b := testGamesBot(t)
	for i := 1; i <= 12; i++ {
		name := "Show " + string(rune('A'+i))
		b.guess.pool = append(b.guess.pool, guessShow{ID: i, Name: name, Titles: []string{name}, Score: 50 + i*3, Popular: 1000 * i})
	}
	b.guess.fetched = time.Now()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	old := togetherNow
	togetherNow = func() time.Time { return now }
	t.Cleanup(func() { togetherNow = old })
	return b, &now
}

// togetherPost sends body as user and decodes the answer into out.
func togetherPost(b *bot, fn func(*bot, http.ResponseWriter, *http.Request), user string, body any, out any) int {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/x", bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+b.acts.add(user))
	w := httptest.NewRecorder()
	fn(b, w, r)
	if out != nil {
		json.NewDecoder(w.Body).Decode(out)
	}
	return w.Code
}

func TestTogetherLobbyAndHost(t *testing.T) {
	b, now := togetherBot(t)
	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "room1"}, &v)
	*now = now.Add(time.Second)
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "room1"}, &v)
	if len(v.Members) != 2 || v.Host != "a" || v.Members[0].ID != "a" {
		t.Fatalf("members %+v host %q", v.Members, v.Host)
	}
	if code := togetherPost(b, (*bot).togetherStart, "b", togetherIn{Room: "room1", Game: "hl"}, nil); code != http.StatusForbidden {
		t.Errorf("only the host starts: %d", code)
	}
	if code := togetherPost(b, (*bot).togetherJoin, "c", togetherIn{Room: "../x y"}, nil); code != http.StatusBadRequest {
		t.Errorf("bad room id: %d", code)
	}
	// a goes quiet: b becomes the host.
	*now = now.Add(togetherIdle + time.Second)
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "room1"}, &v)
	if len(v.Members) != 1 || v.Host != "b" {
		t.Errorf("after a left: %+v host %q", v.Members, v.Host)
	}
}

func TestTogetherHigherLower(t *testing.T) {
	b, now := togetherBot(t)
	for _, u := range []string{"a", "b", "c"} {
		togetherPost(b, (*bot).togetherJoin, u, togetherIn{Room: "r"}, nil)
	}
	var v togetherView
	if code := togetherPost(b, (*bot).togetherStart, "a", togetherIn{Room: "r", Game: "hl"}, &v); code != 200 || v.Phase != "hl" {
		t.Fatalf("start: %d %+v", code, v)
	}
	room := b.together.m["r"]
	room.running = true // the test drives the clock itself
	higher := hlValue(room.b, "score") > hlValue(room.a, "score")
	togetherPost(b, (*bot).togetherVote, "a", togetherIn{Room: "r", Higher: higher}, nil)
	togetherPost(b, (*bot).togetherVote, "b", togetherIn{Room: "r", Higher: higher}, nil)
	togetherPost(b, (*bot).togetherVote, "b", togetherIn{Room: "r", Higher: !higher}, nil) // second vote ignored
	togetherPost(b, (*bot).togetherVote, "c", togetherIn{Room: "r", Higher: !higher}, nil)
	b.together.mu.Lock()
	b.togetherStep(room, *now) // everyone voted: reveal
	if !room.revealed || len(room.alive) != 2 || len(room.out) != 1 || room.out[0] != "c" {
		t.Fatalf("reveal: alive %v out %v", room.alive, room.out)
	}
	hv := b.togetherHLView(room, "c", *now)
	if hv.In || len(hv.Players) != 3 || !hv.Players[2].Out {
		t.Errorf("c's view: %+v", hv)
	}
	*now = now.Add(togetherReveal)
	b.togetherStep(room, *now) // next pair
	if room.revealed || room.hlRound != 2 {
		t.Fatalf("next pair: revealed %v round %d", room.revealed, room.hlRound)
	}
	higher = hlValue(room.b, "score") > hlValue(room.a, "score")
	room.votes["a"] = higher
	*now = now.Add(togetherVoteTime) // b never votes
	b.togetherStep(room, *now)
	if len(room.winners) != 1 || room.winners[0] != "a" {
		t.Fatalf("winner: %v", room.winners)
	}
	*now = now.Add(togetherReveal)
	b.togetherStep(room, *now)
	b.together.mu.Unlock()
	if room.phase != "over" {
		t.Errorf("phase %q", room.phase)
	}
	var pts int
	b.store.view(func(d *storeData) { pts = d.Games["a"].Total })
	if pts != 1 {
		t.Errorf("winner points = %d", pts)
	}
}

func TestTogetherRace(t *testing.T) {
	b, now := togetherBot(t)
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, nil)
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "r"}, nil)
	b.together.mu.Lock()
	room := b.together.m["r"]
	room.running = true
	room.phase, room.game, room.round, room.scores = "race", "riddle", 1, map[string]int{}
	room.cur = &soloRound{game: "riddle", name: "Show B", titles: []string{"Show B"}, puzzle: "🅱️", clues: []string{"Title: S _ _ _   B"}}
	room.start = *now
	room.ends = now.Add(togetherRoundTime(room.cur))
	b.together.mu.Unlock()

	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	if len(v.Race.Clues) != 0 || v.Race.Answer != "" {
		t.Fatalf("no clue or answer yet: %+v", v.Race)
	}
	*now = now.Add(togetherClueEvery)
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	if len(v.Race.Clues) != 1 {
		t.Errorf("clue after %v: %v", togetherClueEvery, v.Race.Clues)
	}
	var ans struct {
		Right bool
	}
	togetherPost(b, (*bot).togetherAnswer, "b", togetherIn{Room: "r", Text: "Naruto"}, &ans)
	if ans.Right {
		t.Error("wrong answer counted")
	}
	togetherPost(b, (*bot).togetherAnswer, "a", togetherIn{Room: "r", Text: "show b"}, &ans)
	if !ans.Right || room.winner != "a" || room.scores["a"] != 1 {
		t.Fatalf("right answer: %v winner %q", ans.Right, room.winner)
	}
	if code := togetherPost(b, (*bot).togetherAnswer, "b", togetherIn{Room: "r", Text: "show b"}, nil); code != 409 {
		t.Errorf("answer after the round: %d", code)
	}
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "r"}, &v)
	if v.Race.Winner == "" || v.Race.Answer != "Show B" || len(v.Race.Feed) != 2 || v.Race.Feed[1].Text != "got it" {
		t.Errorf("after the win: %+v", v.Race)
	}
	// Last round over: the match ends with a as winner.
	b.together.mu.Lock()
	room.round = togetherRounds
	*now = now.Add(togetherBetween)
	b.togetherStep(room, *now)
	b.together.mu.Unlock()
	if room.phase != "over" || len(room.winners) != 1 || room.winners[0] != "a" {
		t.Errorf("match end: %q %v", room.phase, room.winners)
	}
}

func TestTogetherLongPoll(t *testing.T) {
	b, _ := togetherBot(t)
	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	done := make(chan togetherView)
	go func() {
		r := httptest.NewRequest("GET", "/x?room=r&v="+itoa(v.Ver), nil)
		r.Header.Set("Authorization", "Bearer "+b.acts.add("a"))
		w := httptest.NewRecorder()
		b.togetherState(w, r)
		var got togetherView
		json.NewDecoder(w.Body).Decode(&got)
		done <- got
	}()
	select {
	case <-done:
		t.Fatal("answered before anything changed")
	case <-time.After(100 * time.Millisecond):
	}
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "r"}, nil)
	select {
	case got := <-done:
		if len(got.Members) != 2 {
			t.Errorf("woke with %+v", got.Members)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("not woken by a join")
	}
}

func TestTogetherDaily(t *testing.T) {
	b, _ := togetherBot(t)
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, nil)
	day := dailyDay(time.Now())
	b.store.update(func(d *storeData) {
		d.DailyPlays = map[string]dailyPlay{"a": {Day: day, Guesses: []dailyGuess{{ID: 3, Name: "Secret", Cells: []dailyCell{{Hit: "yes"}, {Hit: "no"}}}}}}
	})
	r := httptest.NewRequest("GET", "/x?room=r", nil)
	r.Header.Set("Authorization", "Bearer "+b.acts.add("a"))
	w := httptest.NewRecorder()
	b.togetherDaily(w, r)
	if bytes.Contains(w.Body.Bytes(), []byte("Secret")) {
		t.Error("titles must not show")
	}
	var rows []togetherDailyRow
	json.NewDecoder(w.Body).Decode(&rows)
	if len(rows) != 1 || len(rows[0].Rows) != 1 || rows[0].Rows[0][0] != "yes" || !rows[0].Me {
		t.Errorf("rows %+v", rows)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
