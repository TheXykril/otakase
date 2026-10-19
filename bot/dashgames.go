package main

import (
	"fmt"
	"html/template"
	"sort"
	"time"
)

// The dashboard's Games page: this week's podium and boards with bars, how
// many points each game gave, the daily puzzle, what is running now with
// its controls, chat levels and level roles. The riddle list has its own
// page.

// dashRank is one line of a ranked board.
type dashRank struct {
	Rank, Name, Sub, Value string
	Pct                    int // bar length, against the board's top
}

func init() {
	template.Must(dashPartsTmpl.New("board").Parse(
		`{{if .Rows}}<ol class="rank">{{range .Rows}}<li style="--w:{{.Pct}}%">{{with .Rank}}<span class="r">{{.}}</span>{{end}}` +
			`<span class="who">{{.Name}}{{with .Sub}}<small>{{.}}</small>{{end}}</span><b>{{.Value}}</b></li>{{end}}</ol>` +
			`{{else}}<p class="note">{{.Empty}}</p>{{end}}`))
	template.Must(dashPartsTmpl.New("podium").Parse(
		`<div class="podium">{{range .}}<div class="p{{.Rank}}"><span class="m">{{.Rank}}</span><div class="name">{{.Name}}</div><div class="step">{{.Value}}</div></div>{{end}}</div>`))
	template.Must(dashPartsTmpl.New("cols").Parse(`<div class="cols">{{range .}}<section>{{.}}</section>{{end}}</div>`))
}

// dashBoard is a ranked list with a bar behind each line.
func dashBoard(rows []dashRank, empty string) template.HTML {
	return dashExec("board", struct {
		Rows  []dashRank
		Empty string
	}{rows, empty})
}

// dashCols puts parts side by side on wide screens, one under another on
// phones.
func dashCols(parts ...template.HTML) template.HTML { return dashExec("cols", parts) }

// dashRanks turns ranked points into board lines, top n.
func (b *bot) dashRanks(list []guessRank, n int, unit string) []dashRank {
	var out []dashRank
	for k, r := range list {
		if k == n {
			break
		}
		out = append(out, dashRank{Rank: fmt.Sprint(k + 1), Name: b.dashName(r.User), Value: fmt.Sprint(r.Points) + unit, Pct: dashPct(r.Points, list[0].Points)})
	}
	return out
}

func dashPct(v, top int) int {
	if top <= 0 || v <= 0 {
		return 0
	}
	return max(4, v*100/top)
}

// dashPodiumOf is the top three in podium order: second, first, third.
func (b *bot) dashPodiumOf(list []guessRank) template.HTML {
	if len(list) == 0 {
		return ""
	}
	top := b.dashRanks(list, 3, "")
	order := []int{1, 0, 2}
	var out []dashRank
	for _, k := range order {
		if k < len(top) {
			out = append(out, top[k])
		}
	}
	return dashExec("podium", out)
}

// gamesSummary is what the Games page reads from the store.
type gamesSummary struct {
	week, all, last, xp, guessWeek, guessAll []guessRank
	byGame                                   map[string]int
	weekTotal                                int
	daily                                    dailyPuzzle
	played, solved                           int
	riddles                                  int
}

func (b *bot) gamesSummary(now time.Time) gamesSummary {
	var g gamesSummary
	wk := weekKey(now.UTC())
	g.byGame = map[string]int{}
	b.store.view(func(d *storeData) {
		weekly := weekPoints(d, wk)
		for _, p := range weekly {
			g.weekTotal += p
		}
		g.week, g.all = ranked(weekly, 0), ranked(allPoints(d), 0)
		g.last = ranked(weekPoints(d, weekKey(now.UTC().AddDate(0, 0, -7))), 1)
		g.xp = xpRanked(d.XP)
		g.guessWeek, g.guessAll = guessLeaders(d.Guess, wk, true), guessLeaders(d.Guess, wk, false)
		for _, s := range d.Games {
			for game, n := range s.Games {
				g.byGame[game] += n
			}
		}
		for _, s := range d.Guess {
			g.byGame["guess"] += s.Total
		}
		if d.Daily != nil {
			g.daily = *d.Daily
		}
		for _, p := range d.DailyPlays {
			if p.Day == g.daily.Day && len(p.Guesses) > 0 {
				g.played++
				if p.Solved {
					g.solved++
				}
			}
		}
		g.riddles = len(riddles(d))
	})
	return g
}

func (b *bot) dashGames() template.HTML {
	now := time.Now()
	g := b.gamesSummary(now)

	champion := "nobody yet"
	if len(g.last) > 0 {
		champion = b.dashName(g.last[0].User)
	}
	daily := dashStat{"Daily puzzle, not picked yet", "—"}
	if g.daily.Day == dailyDay(now) {
		daily = dashStat{fmt.Sprintf("Daily #%d solved", g.daily.Num), fmt.Sprintf("%d of %d", g.solved, g.played)}
	}
	activity := "off"
	if b.activityReady() {
		activity = "on"
	}

	// Running now, with its controls.
	live := b.dashPartiesPlaying()

	// Points by game, most first.
	var games []dashRank
	top := 0
	for _, n := range g.byGame {
		top = max(top, n)
	}
	keys := make([]string, 0, len(gameNames))
	for k := range gameNames {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if g.byGame[keys[i]] != g.byGame[keys[j]] {
			return g.byGame[keys[i]] > g.byGame[keys[j]]
		}
		return gameNames[keys[i]] < gameNames[keys[j]]
	})
	for _, k := range keys {
		games = append(games, dashRank{Name: gameNames[k], Value: fmt.Sprint(g.byGame[k]), Pct: dashPct(g.byGame[k], top)})
	}

	// Chat levels with how far into the next level each member is.
	var levels []dashRank
	for k, r := range g.xp {
		if k == 10 {
			break
		}
		lvl, into, need := levelFor(r.Points)
		levels = append(levels, dashRank{Rank: fmt.Sprint(k + 1), Name: b.dashName(r.User),
			Sub: fmt.Sprintf("%d XP", r.Points), Value: fmt.Sprintf("Lv %d", lvl), Pct: dashPct(into, need)})
	}

	return dashJoin(
		dashStats(
			dashStat{"Players this week", fmt.Sprint(len(g.week))},
			dashStat{"Points this week", fmt.Sprint(g.weekTotal)},
			dashStat{"Last week's " + championRole, champion},
			daily,
			dashStat{"Running now", fmt.Sprint(len(live))},
			dashStat{"Activity", activity},
		),
		dashHeading("This week"),
		dashNote("Every game counts. Each Monday last week's winner gets the "+championRole+" role."),
		b.dashPodiumOf(g.week),
		dashCols(
			dashJoin(dashHeading("Game points, this week"), dashBoard(b.dashRanks(g.week, 10, ""), "No game points this week yet.")),
			dashJoin(dashHeading("Game points, all time"), dashBoard(b.dashRanks(g.all, 10, ""), "No game points yet.")),
		),
		dashCols(
			dashJoin(dashHeading("Points by game, all time"), dashBoard(games, "")),
			dashJoin(dashHeading("Playing together now"), dashBoard(live, "Nobody is playing together."),
				dashControls(dashControl{Section: "games", Do: "guess-reset-week", Button: "Reset this week's guess points", Confirm: "sure", Quiet: true})),
		),
		dashCols(
			dashJoin(dashHeading("Guess the anime, this week"), dashBoard(b.dashRanks(g.guessWeek, 10, ""), "No guesses this week yet.")),
			dashJoin(dashHeading("Guess the anime, all time"), dashBoard(b.dashRanks(g.guessAll, 10, ""), "No guesses yet.")),
		),
		dashHeading("Chat levels"),
		dashNote("The bar is how far each member is into their next level."),
		dashBoard(levels, "Nobody has XP yet."),
		b.dashLevelRoles(),
		dashText(`<p class="note">`+dashPlural(g.riddles, "emoji riddle")+` in rotation, listed on the <a href="?s=riddles">Riddles</a> page.</p>`),
	)
}

// dashRiddles lists the emoji riddles, with a button to take one out.
func (b *bot) dashRiddles() template.HTML {
	var rds []riddle
	b.store.view(func(d *storeData) { rds = riddles(d) })
	added := 0
	var rows [][]template.HTML
	for _, r := range rds {
		by := "built-in"
		if r.By != "" {
			by = b.dashName(r.By)
			added++
		}
		rows = append(rows, []template.HTML{dashText(template.HTMLEscapeString(r.Emojis)),
			dashText(template.HTMLEscapeString(r.Name)), dashText(template.HTMLEscapeString(by)),
			dashControls(dashControl{Section: "riddles", Do: "riddle-remove", Name: r.ID, Button: "Remove", Quiet: true})})
	}
	return dashJoin(
		dashStats(dashStat{"Riddles", fmt.Sprint(len(rds))}, dashStat{"Added by members", fmt.Sprint(added)}),
		dashNote("Members add riddles with /riddle-add. Removing a built-in riddle turns it off."),
		dashHTMLTable([]string{"Riddle", "Answer", "Added by", ""}, rows, "No riddles."),
	)
}

// dashPartiesPlaying lists the Activity rooms playing a game together.
func (b *bot) dashPartiesPlaying() []dashRank {
	type room struct {
		game    string
		players int
	}
	var rooms []room
	b.together.mu.Lock()
	for _, r := range b.together.m {
		if r.phase != "lobby" && r.phase != "over" && len(r.members) > 0 {
			rooms = append(rooms, room{r.game, len(r.members)})
		}
	}
	b.together.mu.Unlock()
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].players > rooms[j].players })
	var out []dashRank
	for _, r := range rooms {
		name := gameNames[r.game]
		if r.game == "hl" {
			name = gameNames["higher-lower"]
		}
		out = append(out, dashRank{Name: name, Sub: "Play together", Value: dashPlural(r.players, "player")})
	}
	return out
}
