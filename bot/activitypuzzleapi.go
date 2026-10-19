package main

import (
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Routes for the puzzle games played alone, and the zoomed cover picture
// shared with the party version. A win is a game point under the
// Activity's daily cap.

type pzEntry struct {
	p      actPuzzle
	game   string
	paid   bool
	points int
	moves  int
	won    bool
	done   bool
	at     time.Time // done at, for the party's finish order
}

type pzBoards struct {
	mu sync.Mutex
	m  map[string]*pzEntry // user + "/" + game
}

// pzOut is what the page gets.
type pzOut struct {
	Game   string `json:"game"`
	Board  any    `json:"board"`
	Done   bool   `json:"done"`
	Won    bool   `json:"won"`
	Points int    `json:"points,omitempty"`
	Moves  int    `json:"moves"`
}

func (e *pzEntry) out() pzOut {
	done, won := e.p.result()
	return pzOut{Game: e.game, Board: e.p.view(), Done: done, Won: won, Points: e.points, Moves: e.moves}
}

// apply makes a move and notes when the board ends. Hold the board's lock.
func (e *pzEntry) apply(m pzMove, now time.Time) error {
	if e.done {
		return nil
	}
	if err := e.p.move(m); err != nil {
		return err
	}
	e.moves++
	e.done, e.won = e.p.result()
	if e.done {
		e.at = now
	}
	return nil
}

func init() {
	apiHandle("POST /activity/api/pz/{game}/start", (*bot).pzStart)
	apiHandle("POST /activity/api/pz/{game}/move", (*bot).pzMoveSolo)
	apiHandle("GET /activity/api/pz/pic", (*bot).pzPic)
}

func (b *bot) pzStart(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	game := r.PathValue("game")
	if _, ok := pzGames[game]; !ok {
		apiError(w, http.StatusNotFound, "no such game")
		return
	}
	if b.gameOff(game) {
		apiError(w, http.StatusForbidden, "staff turned this game off")
		return
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if !readJSON(w, r, 1<<10, &in) {
		return
	}
	p, err := b.newPuzzle(game, in.Mode)
	if err != nil {
		apiError(w, http.StatusBadGateway, "couldn't reach AniList, try again in a bit")
		return
	}
	e := &pzEntry{p: p, game: game}
	b.pz.mu.Lock()
	if b.pz.m == nil {
		b.pz.m = map[string]*pzEntry{}
	}
	b.pz.m[user+"/"+game] = e
	out := e.out()
	b.pz.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

// pzReadMove reads a move, looking up the show it names.
func (b *bot) pzReadMove(w http.ResponseWriter, r *http.Request, in any, m *pzMove) bool {
	if !readJSON(w, r, 4<<10, in) {
		return false
	}
	if m.ID > 0 {
		s, ok := b.dailyShowByID(m.ID)
		if !ok {
			apiError(w, http.StatusBadRequest, "pick a show from the list")
			return false
		}
		m.show = &s
	}
	return true
}

func (b *bot) pzMoveSolo(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	game := r.PathValue("game")
	var m pzMove
	if !b.pzReadMove(w, r, &m, &m) {
		return
	}
	b.pz.mu.Lock()
	e := b.pz.m[user+"/"+game]
	if e == nil {
		b.pz.mu.Unlock()
		apiError(w, http.StatusNotFound, "start a round first")
		return
	}
	err := e.apply(m, time.Now())
	pay := e.won && !e.paid
	e.paid = e.paid || e.won
	b.pz.mu.Unlock()
	var perr errPz
	if errors.As(err, &perr) {
		apiError(w, http.StatusBadRequest, perr.Error())
		return
	}
	if pay {
		n := b.actPoint(user, game)
		b.pz.mu.Lock()
		e.points = n
		b.pz.mu.Unlock()
	}
	b.pz.mu.Lock()
	out := e.out()
	b.pz.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

// pzPic is the zoomed cover of the member's coverle board: alone, or in
// the party room named by room. A step past what the board shows is drawn
// at the board's step.
func (b *bot) pzPic(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	q := r.URL.Query()
	var c *coverle
	if id := q.Get("room"); id != "" {
		b.together.mu.Lock()
		if room := b.together.m[id]; room != nil && room.game == "coverle" && room.pzBase != nil {
			if e := b.togetherBoard(room, user); e != nil {
				if cv, ok := e.p.(*coverle); ok {
					cp := *cv
					c = &cp
				}
			}
		}
		b.together.mu.Unlock()
	} else {
		b.pz.mu.Lock()
		if e := b.pz.m[user+"/coverle"]; e != nil {
			if cv, ok := e.p.(*coverle); ok {
				cp := *cv
				c = &cp
			}
		}
		b.pz.mu.Unlock()
	}
	step, err := strconv.Atoi(q.Get("step"))
	if c == nil || err != nil || step < 0 {
		http.NotFound(w, r)
		return
	}
	step = min(step, c.step())
	png, err := zoomPic(c.img, c.cx, c.cy, coverleZoom[step])
	if err != nil {
		http.Error(w, "couldn't draw", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}
