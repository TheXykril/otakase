package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The daily puzzle: one popular show a day (UTC) for everyone, 8 tries.
// Each try is a show from the guess the anime pool, and the answer says
// how close each of its details is: year, format, episodes, score, studio,
// source, genres and popularity. Green is right, yellow is close, and an
// arrow says whether the answer is higher or lower. Played in chat with
// /daily (only you see your board) or in the Activity (activity.go); both
// share the same tries. Finishing posts a spoiler-free grid in the
// channel and gives game points: 5 for one or two tries, down to 2.

const dailyTries = 8

// dailyEpoch is the day of puzzle #1.
var dailyEpoch = time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)

type dailyPuzzle struct {
	Day    string    `json:"day"`
	Num    int       `json:"num"`
	Show   guessShow `json:"show"`
	Recent []int     `json:"recent,omitempty"` // answers lately, not picked again for a while
}

// dailyCell is how close one detail of a try is to the answer.
type dailyCell struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Text  string `json:"text"`
	Hit   string `json:"hit"`             // yes, near or no
	Arrow string `json:"arrow,omitempty"` // up: the answer's is higher, down: lower
}

type dailyGuess struct {
	ID    int         `json:"id"`
	Name  string      `json:"name"`
	Cells []dailyCell `json:"cells"`
}

// dailyPlay is a member's tries at a day's puzzle.
type dailyPlay struct {
	Day     string       `json:"day"`
	Guesses []dailyGuess `json:"guesses"`
	Solved  bool         `json:"solved"`
	Done    bool         `json:"done"`
}

type dailyStats struct {
	Played  int    `json:"played"`
	Won     int    `json:"won"`
	Streak  int    `json:"streak"`
	Best    int    `json:"best"`
	LastWon string `json:"last_won,omitempty"` // day of the last solve
}

func dailyDay(t time.Time) string { return t.UTC().Format(time.DateOnly) }

func dailyNum(day string) int {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return 0
	}
	return int(t.Sub(dailyEpoch).Hours()/24) + 1
}

// dailyPoints is the game points for solving in n tries.
func dailyPoints(n int) int { return max(2, 5-(n-1)/2) }

var dailySources = map[string]string{"ORIGINAL": "Original", "MANGA": "Manga", "LIGHT_NOVEL": "Light novel",
	"WEB_NOVEL": "Web novel", "NOVEL": "Novel", "VISUAL_NOVEL": "Visual novel", "VIDEO_GAME": "Video game",
	"GAME": "Game", "WEB_MANGA": "Web manga", "ONE_SHOT": "One-shot", "LIVE_ACTION": "Live action",
	"MULTIMEDIA_PROJECT": "Multimedia", "OTHER": "Other"}

var dailyFormats = map[string]string{"TV": "TV", "TV_SHORT": "TV short", "MOVIE": "Movie",
	"SPECIAL": "Special", "OVA": "OVA", "ONA": "ONA", "MUSIC": "Music"}

// dailyNumber compares a number: right when equal, close within near, and
// an arrow toward the answer. Unknown (0) is never right.
func dailyNumber(key, label, text string, g, a, near int, lowerIsUp bool) dailyCell {
	c := dailyCell{Key: key, Label: label, Text: text, Hit: "no"}
	if g == 0 || a == 0 {
		if g == 0 {
			c.Text = "?"
		}
		return c
	}
	d := a - g
	switch {
	case d == 0:
		c.Hit = "yes"
		return c
	case d >= -near && d <= near:
		c.Hit = "near"
	}
	up := d > 0
	if lowerIsUp {
		up = !up
	}
	c.Arrow = map[bool]string{true: "up", false: "down"}[up]
	return c
}

func dailyMatch(key, label, g, a string) dailyCell {
	c := dailyCell{Key: key, Label: label, Text: g, Hit: "no"}
	if g == "" {
		c.Text = "?"
	} else if g == a {
		c.Hit = "yes"
	}
	return c
}

// dailyCompare says how close each detail of g is to the answer a.
func dailyCompare(g, a guessShow) []dailyCell {
	cells := []dailyCell{
		dailyNumber("year", "Year", fmt.Sprint(g.Year), g.Year, a.Year, 2, false),
		dailyMatch("format", "Format", dailyFormats[g.Format], dailyFormats[a.Format]),
		dailyNumber("episodes", "Episodes", fmt.Sprint(g.Episodes), g.Episodes, a.Episodes, 3, false),
		dailyNumber("score", "Score", fmt.Sprintf("%d%%", g.Score), g.Score, a.Score, 5, false),
		dailyMatch("studio", "Studio", g.Studio, a.Studio),
		dailyMatch("source", "Source", dailySources[g.Source], dailySources[a.Source]),
	}
	// Genres: right when the same, close when any is shared.
	gc := dailyCell{Key: "genres", Label: "Genres", Text: strings.Join(g.Genres[:min(len(g.Genres), 3)], ", "), Hit: "no"}
	shared := 0
	for _, x := range g.Genres {
		if slices.Contains(a.Genres, x) {
			shared++
		}
	}
	if gc.Text == "" {
		gc.Text = "?"
	}
	if shared > 0 {
		gc.Hit = "near"
		if shared == len(g.Genres) && shared == len(a.Genres) {
			gc.Hit = "yes"
		}
	}
	// Popularity rank: a smaller number is more popular, so up means the
	// answer is more popular.
	pc := dailyNumber("popularity", "Popularity", fmt.Sprintf("#%d", g.Rank), g.Rank, a.Rank, 30, true)
	return append(cells, gc, pc)
}

// dailyPick chooses a day's answer from the 300 most popular shows, not
// one used in the last 120 days.
func dailyPick(pool []guessShow, recent []int, intn func(int) int) (guessShow, bool) {
	var choices []guessShow
	for _, s := range pool {
		if s.Rank > 0 && s.Rank <= 300 && !slices.Contains(recent, s.ID) {
			choices = append(choices, s)
		}
	}
	if len(choices) == 0 {
		for _, s := range pool {
			if !slices.Contains(recent, s.ID) {
				choices = append(choices, s)
			}
		}
	}
	if len(choices) == 0 {
		return guessShow{}, false
	}
	return choices[intn(len(choices))], true
}

// dailyToday is today's puzzle, picked on the first ask of the day.
func (b *bot) dailyToday() (dailyPuzzle, error) {
	day := dailyDay(time.Now())
	var p dailyPuzzle
	b.store.view(func(d *storeData) {
		if d.Daily != nil {
			p = *d.Daily
		}
	})
	if p.Day == day {
		return p, nil
	}
	pool, err := b.guess.shows()
	if err != nil {
		return dailyPuzzle{}, err
	}
	b.store.update(func(d *storeData) {
		if d.Daily != nil && d.Daily.Day == day {
			p = *d.Daily // picked while fetching
			return
		}
		var recent []int
		if d.Daily != nil {
			recent = d.Daily.Recent
		}
		show, ok := dailyPick(pool, recent, rand.IntN)
		if !ok {
			err = fmt.Errorf("no shows")
			return
		}
		recent = append(recent, show.ID)
		if len(recent) > 120 {
			recent = recent[len(recent)-120:]
		}
		p = dailyPuzzle{Day: day, Num: dailyNum(day), Show: show, Recent: recent}
		d.Daily = &p
		// Yesterday's tries are done with.
		for u, pl := range d.DailyPlays {
			if pl.Day != day {
				delete(d.DailyPlays, u)
			}
		}
	})
	return p, err
}

// dailyPlayOf is a member's play at today's puzzle.
func (b *bot) dailyPlayOf(user, day string) dailyPlay {
	var pl dailyPlay
	b.store.view(func(d *storeData) { pl = d.DailyPlays[user] })
	if pl.Day != day {
		pl = dailyPlay{Day: day}
	}
	return pl
}

// dailyShowByID finds a show in the pool.
func (b *bot) dailyShowByID(id int) (guessShow, bool) {
	pool, err := b.guess.shows()
	if err != nil {
		return guessShow{}, false
	}
	for _, s := range pool {
		if s.ID == id {
			return s, true
		}
	}
	return guessShow{}, false
}

// errDaily is a message for the player.
type errDaily string

func (e errDaily) Error() string { return string(e) }

// dailyTry adds a try for a member. finished is true when this try ended
// their game, solved or not.
func (b *bot) dailyTry(user string, showID int) (p dailyPuzzle, pl dailyPlay, finished bool, err error) {
	if p, err = b.dailyToday(); err != nil {
		return p, pl, false, errDaily("Couldn't reach AniList for today's show. Try again in a bit.")
	}
	g, ok := b.dailyShowByID(showID)
	if !ok && showID == p.Show.ID {
		g, ok = p.Show, true
	}
	if !ok {
		return p, pl, false, errDaily("Pick a show from the suggestions.")
	}
	week := weekKey(time.Now().UTC())
	b.store.update(func(d *storeData) {
		pl = d.DailyPlays[user]
		if pl.Day != p.Day {
			pl = dailyPlay{Day: p.Day}
		}
		if pl.Done {
			err = errDaily("You already played today's puzzle. A new one comes at midnight UTC.")
			return
		}
		for _, x := range pl.Guesses {
			if x.ID == g.ID {
				err = errDaily("You already tried " + g.Name + ".")
				return
			}
		}
		pl.Guesses = append(pl.Guesses, dailyGuess{ID: g.ID, Name: g.Name, Cells: dailyCompare(g, p.Show)})
		pl.Solved = g.ID == p.Show.ID
		pl.Done = pl.Solved || len(pl.Guesses) >= dailyTries
		if d.DailyPlays == nil {
			d.DailyPlays = map[string]dailyPlay{}
		}
		d.DailyPlays[user] = pl
		if !pl.Done {
			return
		}
		finished = true
		if d.DailyStats == nil {
			d.DailyStats = map[string]dailyStats{}
		}
		st := d.DailyStats[user]
		st.Played++
		if pl.Solved {
			st.Won++
			yesterday := dailyDay(time.Now().AddDate(0, 0, -1))
			if st.LastWon == yesterday {
				st.Streak++
			} else {
				st.Streak = 1
			}
			st.LastWon = p.Day
			st.Best = max(st.Best, st.Streak)
			addGamePoints(d, user, "daily", dailyPoints(len(pl.Guesses)), week)
		} else {
			st.Streak = 0
		}
		d.DailyStats[user] = st
	})
	return p, pl, finished, err
}

var dailySquares = map[string]string{"yes": "🟩", "near": "🟨", "no": "⬛"}

// dailyGrid is the spoiler-free grid of a play, one row per try.
func dailyGrid(pl dailyPlay) string {
	var rows []string
	for _, g := range pl.Guesses {
		var row strings.Builder
		for _, c := range g.Cells {
			row.WriteString(dailySquares[c.Hit])
		}
		rows = append(rows, row.String())
	}
	return strings.Join(rows, "\n")
}

func dailyScore(pl dailyPlay) string {
	if pl.Solved {
		return fmt.Sprintf("%d/%d", len(pl.Guesses), dailyTries)
	}
	return fmt.Sprintf("X/%d", dailyTries)
}

var dailyArrows = map[string]string{"up": "⬆", "down": "⬇"}

// dailyRow is one try on the chat board.
func dailyRow(k int, g dailyGuess) string {
	var cells []string
	for _, c := range g.Cells {
		cells = append(cells, dailySquares[c.Hit]+" "+c.Text+dailyArrows[c.Arrow])
	}
	return fmt.Sprintf("**%d. %s**\n%s", k+1, clip(g.Name, 80), strings.Join(cells, " · "))
}

// dailyBoardMessage is a member's board in chat.
func (b *bot) dailyBoardMessage(p dailyPuzzle, pl dailyPlay, note string) *discordgo.InteractionResponseData {
	var rows []string
	for k, g := range pl.Guesses {
		rows = append(rows, dailyRow(k, g))
	}
	desc := "One show a day for everyone, played in the Otakase Activity; each try shows how close it is.\n" +
		"-# 🟩 right · 🟨 close · ⬆⬇ the answer's is higher or lower · Year · Format · Episodes · Score · Studio · Source · Genres · Popularity"
	if len(rows) > 0 {
		desc = strings.Join(rows, "\n\n")
	}
	if note != "" {
		desc = note + "\n\n" + desc
	}
	e := &discordgo.MessageEmbed{Title: fmt.Sprintf("Daily puzzle #%d", p.Num), Color: shu, Description: desc,
		Footer: &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("%d of %d tries · new puzzle at midnight UTC", len(pl.Guesses), dailyTries)}}
	if pl.Done {
		e.Title = fmt.Sprintf("Daily puzzle #%d · %s", p.Num, dailyScore(pl))
		e.URL = p.Show.URL
		e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: p.Show.Cover}
		e.Description = "The answer: **" + p.Show.Name + "**\n\n" + e.Description
	}
	data := &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Flags: discordgo.MessageFlagsEphemeral,
		Components: []discordgo.MessageComponent{}}
	if b.activityReady() {
		data.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Play in the Activity", Style: discordgo.PrimaryButton, CustomID: "daily-launch", Emoji: &discordgo.ComponentEmoji{Name: "🎮"}},
		}}}
	}
	return data
}

// dailyCommand opens the Activity, or shows today's board when the
// Activity isn't set up.
func (b *bot) dailyCommand(i *discordgo.InteractionCreate) {
	b.dailyBoard(i, true)
}

// dailyBoard shows the member's board for today; launch opens the
// Activity instead when it is set up.
func (b *bot) dailyBoard(i *discordgo.InteractionCreate, launch bool) {
	if launch && b.activityReady() && b.launchActivity(i) {
		return
	}
	p, err := b.dailyToday()
	if err != nil {
		log.Printf("daily: %v", err)
		b.respond(i, true, errorEmbed("Couldn't reach AniList for today's show. Try again in a bit."))
		return
	}
	data := b.dailyBoardMessage(p, b.dailyPlayOf(i.Member.User.ID, p.Day), "")
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource, Data: data}); err != nil {
		log.Printf("daily board: %v", err)
	}
}

// postDailyShare posts a finished play's grid in the channel, no spoilers.
func (b *bot) postDailyShare(channel, user string, p dailyPuzzle, pl dailyPlay) {
	if pl := b.place(channel); pl.Support || pl.Staff {
		return
	}
	var st dailyStats
	b.store.view(func(d *storeData) { st = d.DailyStats[user] })
	line := fmt.Sprintf("<@%s> solved today's puzzle in **%d/%d**", user, len(pl.Guesses), dailyTries)
	if !pl.Solved {
		line = fmt.Sprintf("<@%s> didn't get today's puzzle", user)
	}
	if st.Streak > 1 {
		line += fmt.Sprintf(" · %d day streak", st.Streak)
	}
	e := &discordgo.MessageEmbed{Title: fmt.Sprintf("Daily puzzle #%d · %s", p.Num, dailyScore(pl)), Color: shu,
		Description: line + "\n\n" + dailyGrid(pl),
		Footer:      &discordgo.MessageEmbedFooter{Text: "/games to play · one show a day for everyone"}}
	if _, err := b.s.ChannelMessageSendComplex(channel, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e},
		AllowedMentions: &discordgo.MessageAllowedMentions{}}); err != nil {
		log.Printf("daily share: %v", err)
	}
}

// dailyMatches are the pool shows whose titles contain q, most popular
// first, at most n.
func dailyMatches(pool []guessShow, q string, n int) []guessShow {
	q = compact(q)
	var out []guessShow
	for _, s := range pool {
		for _, t := range s.Titles {
			if q == "" || strings.Contains(compact(t), q) {
				out = append(out, s)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out[:min(len(out), n)]
}
