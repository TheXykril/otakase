package main

import (
	"hash/fnv"
	"log"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

// Watch bingo: one 3×3 card a month, the same for everyone, with a free
// middle square. Each square is a kind of show (a movie, something from
// before 2000, a sports anime…) and fills itself when a show like that is
// on your AniList as completed this month. Every row, column or diagonal
// is 2 game points, a full card 5 more. The Activity shows the card; the bot also
// checks every linked member's list every few hours, so points come even
// without looking.

const bingoCheckEvery = 6 * time.Hour

type bingoSquare struct {
	Key   string
	Label string
	Genre bool
	Test  func(e listEntry, now time.Time) bool
}

func bingoGenre(g string) bingoSquare {
	return bingoSquare{Key: "genre-" + strings.ToLower(g), Label: "A " + strings.ToLower(g) + " anime", Genre: true,
		Test: func(e listEntry, _ time.Time) bool { return slices.Contains(e.Media.Genres, g) }}
}

var bingoSquares = []bingoSquare{
	{Key: "movie", Label: "A movie", Test: func(e listEntry, _ time.Time) bool { return e.Media.Format == "MOVIE" }},
	{Key: "short", Label: "A short: 6 episodes or fewer", Test: func(e listEntry, _ time.Time) bool {
		return e.Media.Format != "MOVIE" && e.Media.Episodes > 0 && e.Media.Episodes <= 6
	}},
	{Key: "long", Label: "A long one: 24+ episodes", Test: func(e listEntry, _ time.Time) bool { return e.Media.Episodes >= 24 }},
	{Key: "old", Label: "From before 2000", Test: func(e listEntry, _ time.Time) bool {
		return e.Media.StartDate.Year > 0 && e.Media.StartDate.Year < 2000
	}},
	{Key: "2000s", Label: "From the 2000s", Test: func(e listEntry, _ time.Time) bool {
		return e.Media.StartDate.Year >= 2000 && e.Media.StartDate.Year <= 2009
	}},
	{Key: "new", Label: "From this year or last", Test: func(e listEntry, now time.Time) bool {
		return e.Media.StartDate.Year >= now.Year()-1
	}},
	{Key: "original", Label: "An original story", Test: func(e listEntry, _ time.Time) bool { return e.Media.Source == "ORIGINAL" }},
	{Key: "novel", Label: "Based on a light novel", Test: func(e listEntry, _ time.Time) bool { return e.Media.Source == "LIGHT_NOVEL" }},
	{Key: "game", Label: "Based on a game", Test: func(e listEntry, _ time.Time) bool {
		return e.Media.Source == "VIDEO_GAME" || e.Media.Source == "GAME" || e.Media.Source == "VISUAL_NOVEL"
	}},
	{Key: "top", Label: "Scored 85%+ on AniList", Test: func(e listEntry, _ time.Time) bool { return e.Media.Score >= 85 }},
	{Key: "gem", Label: "A hidden gem: under 30k members", Test: func(e listEntry, _ time.Time) bool {
		return e.Media.Popularity > 0 && e.Media.Popularity < 30000
	}},
	{Key: "extra", Label: "An OVA, ONA or special", Test: func(e listEntry, _ time.Time) bool {
		return e.Media.Format == "OVA" || e.Media.Format == "ONA" || e.Media.Format == "SPECIAL"
	}},
	{Key: "loved", Label: "One you rate 90+", Test: func(e listEntry, _ time.Time) bool { return e.Score >= 90 }},
	bingoGenre("Romance"), bingoGenre("Sports"), bingoGenre("Horror"), bingoGenre("Mecha"),
	bingoGenre("Comedy"), bingoGenre("Mystery"), bingoGenre("Sci-Fi"), bingoGenre("Music"),
	bingoGenre("Psychological"), bingoGenre("Slice of Life"), bingoGenre("Supernatural"),
}

// bingoLines are the squares of every row, column and diagonal; 4 is free.
var bingoLines = [][3]int{{0, 1, 2}, {3, 4, 5}, {6, 7, 8}, {0, 3, 6}, {1, 4, 7}, {2, 5, 8}, {0, 4, 8}, {2, 4, 6}}

// bingoProgress is the points a member already got this month.
type bingoProgress struct {
	Month string `json:"month"`
	Lines int    `json:"lines"` // bit k: bingoLines[k] counted
	Full  bool   `json:"full"`
}

func bingoMonth(t time.Time) string { return t.UTC().Format("2006-01") }

// bingoCard is a month's card: 9 squares with nil in the middle, at most
// three genres. The same month always gives the same card.
func bingoCard(month string) []*bingoSquare {
	h := fnv.New64a()
	h.Write([]byte("otakase-bingo-" + month))
	r := rand.New(rand.NewPCG(h.Sum64(), 7))
	order := r.Perm(len(bingoSquares))
	var picked []*bingoSquare
	genres := 0
	for _, k := range order {
		s := &bingoSquares[k]
		if s.Genre && genres == 3 {
			continue
		}
		if s.Genre {
			genres++
		}
		picked = append(picked, s)
		if len(picked) == 8 {
			break
		}
	}
	return append(picked[:4:4], append([]*bingoSquare{nil}, picked[4:]...)...)
}

// bingoFill is the show that fills each square this month ("" = not yet;
// the middle is always filled).
func bingoFill(card []*bingoSquare, entries []listEntry, now time.Time) []string {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	done := finishedSince(start)
	fill := make([]string, len(card))
	fill[4] = "free"
	for _, e := range entries {
		if e.Media.IsAdult || !done(e) {
			continue
		}
		for k, s := range card {
			if s != nil && fill[k] == "" && s.Test(e, now) {
				fill[k] = e.Media.name()
			}
		}
	}
	return fill
}

// bingoScore is the lines done (as bits) and whether the card is full.
func bingoScore(fill []string) (lines int, full bool) {
	full = true
	for _, f := range fill {
		full = full && f != ""
	}
	for k, l := range bingoLines {
		if fill[l[0]] != "" && fill[l[1]] != "" && fill[l[2]] != "" {
			lines |= 1 << k
		}
	}
	return lines, full
}

func bitCount(n int) int {
	c := 0
	for ; n > 0; n &= n - 1 {
		c++
	}
	return c
}

// bingoAward gives the points for new lines and a newly full card, and
// returns how many.
func (b *bot) bingoAward(user string, fill []string, now time.Time) int {
	month := bingoMonth(now)
	lines, full := bingoScore(fill)
	week := weekKey(now.UTC())
	got := 0
	b.store.update(func(d *storeData) {
		if d.Bingo == nil {
			d.Bingo = map[string]bingoProgress{}
		}
		p := d.Bingo[user]
		if p.Month != month {
			p = bingoProgress{Month: month}
		}
		got = bitCount(lines&^p.Lines) * bingoLine
		if full && !p.Full {
			got += bingoFull
		}
		p.Lines |= lines
		p.Full = p.Full || full
		d.Bingo[user] = p
		if got > 0 {
			addGamePoints(d, user, "bingo", got, week)
		}
	})
	return got
}

// bingoLoop checks every linked member's card now and then.
func (b *bot) bingoLoop() {
	for {
		time.Sleep(bingoCheckEvery)
		lists, err := b.memberLists("")
		if err != nil && len(lists) == 0 {
			log.Printf("watch bingo: %v", err)
			continue
		}
		now := time.Now().UTC()
		card := bingoCard(bingoMonth(now))
		for user, entries := range lists {
			b.bingoAward(user, bingoFill(card, entries, now), now)
		}
	}
}
