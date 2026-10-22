package main

// The Shinigami Notebook giveaway. Staff start one with /giveaway start;
// members press Join until joining closes. Then, every few minutes, a page
// of the notebook is written: random names on it are out. The last one
// left wins the prize (a /redeem code minted for them, unless staff named
// another prize).
//
// It stays random, but members can tip the odds with hanko (hanko.go) in
// the Shop. Items are for this giveaway only, two per member, each costing
// more than the last:
//
//	Plot Armor       survives being written once
//	Fake Name        half as likely to be picked, the whole giveaway
//	Shinigami Eyes   shows the next page before it's written; your own name on it is crossed out
//	Apple for Ryuk   safe on the next page, whatever it says
//	Revival Ticket   written out, back on the page after
//	Decoy            written: a random other survivor takes your place
//
// Each page may also bring a twist: a filler episode (nobody is written),
// a double page, a revival arc (someone out comes back), plot armor for a
// random name on the page, or a pair of Shinigami Eyes for a random
// survivor.
//
// The next page is drawn as soon as the last one is written, so the Eyes
// have something to see. Revivals and bought items work on the page as it
// is written.

import (
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	gaMaxItems     = 2
	gaLogLines     = 12
	gaShownLog     = 8
	gaTwistPercent = 35
	gaDefaultJoin  = 30 // minutes
	gaDefaultEvery = 10 // minutes
	gaMaxPlayers   = 500
)

type giveaway struct {
	ID        string               `json:"id"` // its message
	Channel   string               `json:"channel"`
	Thread    string               `json:"thread,omitempty"` // where each page is posted
	Host      string               `json:"host"`
	Prize     string               `json:"prize,omitempty"` // "" = a /redeem code
	Code      string               `json:"code,omitempty"`  // the winner's code
	Started   time.Time            `json:"started"`
	Closed    bool                 `json:"closed,omitempty"` // joining closed, pages started
	JoinUntil time.Time            `json:"join_until"`
	Every     int                  `json:"every"` // minutes between pages
	NextPage  time.Time            `json:"next_page"`
	Round     int                  `json:"round"` // pages written
	Players   map[string]*gaPlayer `json:"players"`
	Page      []string             `json:"page,omitempty"` // drawn for the next write
	Log       []string             `json:"log,omitempty"`
	Winner    string               `json:"winner,omitempty"`
	Done      bool                 `json:"done,omitempty"`
	Cancelled bool                 `json:"cancelled,omitempty"`
}

type gaPlayer struct {
	Joined time.Time      `json:"joined"`
	Out    int            `json:"out,omitempty"`  // the page they were written on
	Back   bool           `json:"back,omitempty"` // revival ticket: back on the next page
	Items  map[string]int `json:"items,omitempty"`
	Alias  bool           `json:"alias,omitempty"` // fake name bought
	Bought int            `json:"bought,omitempty"`
}

type gaItem struct {
	Key, Name, Desc string
	Price           int
}

var gaItems = []gaItem{
	{"armor", "Plot Armor", "Survive being written once", 90},
	{"alias", "Fake Name", "Half as likely to be picked, all giveaway", 50},
	{"eyes", "Shinigami Eyes", "See the next page; your name on it is crossed out", 40},
	{"apple", "Apple for Ryuk", "Safe on the next page, whatever it says", 25},
	{"revive", "Revival Ticket", "Written out? Back on the page after", 70},
	{"decoy", "Decoy", "Written? A random survivor takes your place", 45},
}

func gaItemByKey(k string) (gaItem, bool) {
	for _, it := range gaItems {
		if it.Key == k {
			return it, true
		}
	}
	return gaItem{}, false
}

// gaPrice is what the member's next item costs: each one more than the last.
func gaPrice(it gaItem, p *gaPlayer) int { return it.Price * (1 + p.Bought) }

var gaRand = func(n int) int { return rand.IntN(n) } // for tests

var (
	gaWritten = []string{
		"%s's name was written. Forty seconds later, gone.",
		"The pen stops on %s.",
		"%s fills a line. Ryuk laughs.",
		"%s is written in neat handwriting. Out.",
		"%s looks up. Too late, it's written.",
	}
	gaSpared = map[string]string{
		"armor":  "%s was written, but plot armor holds.",
		"apple":  "Ryuk is busy with %s's apple and smudges the name.",
		"twist":  "Plot armor out of nowhere: %s survives the page.",
		"eyes":   "%s saw it coming and crossed their name out.",
		"last":   "The ink runs dry before %s's name is done.",
		"revive": "%s's name fades from the page. They're back.",
	}
)

func init() {
	registerCommand(&discordgo.ApplicationCommand{Name: "giveaway", Description: "The Shinigami Notebook giveaway: last name standing wins",
		DefaultMemberPermissions: &permMod,
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "start", Description: "Start a giveaway in this channel",
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "prize", Description: "The prize (leave out for a /redeem code with a watch ticket)", MaxLength: 100},
					{Type: discordgo.ApplicationCommandOptionInteger, Name: "join_minutes", Description: fmt.Sprintf("How long joining is open (default %d)", gaDefaultJoin),
						MinValue: ptr(1.0), MaxValue: 7 * 24 * 60},
					{Type: discordgo.ApplicationCommandOptionInteger, Name: "page_minutes", Description: fmt.Sprintf("Minutes between pages (default %d)", gaDefaultEvery),
						MinValue: ptr(1.0), MaxValue: 24 * 60},
				}},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "next", Description: "Close joining or write the next page now"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "end", Description: "Call off the giveaway in this channel"},
		}}, (*bot).giveawayCommand)
	registerComponent("ga-join:", (*bot).gaJoinButton)
	registerComponent("ga-shop:", (*bot).gaShopButton)
	registerComponent("ga-buy:", (*bot).gaBuyMenu)
	registerComponent("ga-eyes:", (*bot).gaEyesButton)
	registerComponent("ga-claim:", (*bot).gaClaimButton)
	registerJob("Giveaways", (*bot).giveawayLoop)
	registerHelp("Giveaways: press **Join** on a Shinigami Notebook post; spend hanko in its **Shop** to survive the pages")
}

// ── The game ──

func (g *giveaway) alive() []string {
	var out []string
	for u, p := range g.Players {
		if p.Out == 0 {
			out = append(out, u)
		}
	}
	slices.Sort(out)
	return out
}

func (g *giveaway) addLog(lines ...string) {
	g.Log = append(g.Log, lines...)
	if len(g.Log) > gaLogLines {
		g.Log = g.Log[len(g.Log)-gaLogLines:]
	}
}

// gaPageSize is how many names a page takes from n survivors.
func gaPageSize(n int) int { return max(1, n/3) }

// drawNames picks n of from at random, Fake Names half as likely.
func (g *giveaway) drawNames(from []string, n int, rnd func(int) int) []string {
	pool := slices.Clone(from)
	var out []string
	for len(out) < n && len(pool) > 0 {
		total := 0
		for _, u := range pool {
			total += g.weight(u)
		}
		r := rnd(total)
		for k, u := range pool {
			if r -= g.weight(u); r < 0 {
				out = append(out, u)
				pool = slices.Delete(pool, k, k+1)
				break
			}
		}
	}
	return out
}

func (g *giveaway) weight(u string) int {
	if p := g.Players[u]; p != nil && p.Alias {
		return 1
	}
	return 2
}

// drawPage draws the next page from the survivors.
func (g *giveaway) drawPage(rnd func(int) int) {
	alive := g.alive()
	g.Page = g.drawNames(alive, gaPageSize(len(alive)), rnd)
}

// closeJoining starts the pages; false when too few joined.
func (g *giveaway) closeJoining(now time.Time, rnd func(int) int) bool {
	g.JoinUntil, g.Closed = now, true
	if len(g.Players) < 2 {
		g.Done, g.Cancelled = true, true
		g.addLog("Not enough names for the notebook. The giveaway is off.")
		return false
	}
	g.drawPage(rnd)
	g.NextPage = now.Add(time.Duration(g.Every) * time.Minute)
	g.addLog(fmt.Sprintf("Joining is closed with %s. The first page is written <t:%d:R>.", dashPlural(len(g.Players), "name"), g.NextPage.Unix()))
	return true
}

func gaName(u string) string { return "<@" + u + ">" }

// writePage writes the next page and returns its lines. It ends the
// giveaway when one survivor is left.
func (g *giveaway) writePage(now time.Time, rnd func(int) int) []string {
	g.Round++
	var lines []string
	say := func(format, u string) { lines = append(lines, fmt.Sprintf(format, gaName(u))) }
	// Revival tickets from the last page.
	for _, u := range sortedKeys(g.Players) {
		if p := g.Players[u]; p.Back {
			p.Back, p.Out = false, 0
			say(gaSpared["revive"], u)
		}
	}
	alive := g.alive()
	var page []string
	for _, u := range g.Page {
		if slices.Contains(alive, u) {
			page = append(page, u)
		}
	}
	twist := ""
	if rnd(100) < gaTwistPercent {
		twist = []string{"filler", "double", "revival", "armor", "eyes"}[rnd(5)]
	}
	switch twist {
	case "filler":
		lines = append(lines, "**Filler episode.** Ryuk forgot the notebook at home. Nobody is written.")
		page = nil
	case "double":
		lines = append(lines, "**Double page.** The pen is in a hurry.")
		var rest []string
		for _, u := range alive {
			if !slices.Contains(page, u) {
				rest = append(rest, u)
			}
		}
		page = append(page, g.drawNames(rest, gaPageSize(len(alive)), rnd)...)
	case "revival":
		var out []string
		for _, u := range sortedKeys(g.Players) {
			if p := g.Players[u]; p.Out > 0 && !p.Back {
				out = append(out, u)
			}
		}
		if len(out) > 0 {
			u := out[rnd(len(out))]
			g.Players[u].Out = 0
			say("**Revival arc.** %s walks back in like nothing happened.", u)
			alive = g.alive()
		}
	case "eyes":
		u := alive[rnd(len(alive))]
		p := g.Players[u]
		if p.Items == nil {
			p.Items = map[string]int{}
		}
		p.Items["eyes"]++
		say("**Shinigami Eyes** for %s. They can see the next page now.", u)
	}
	// Apples are eaten on every page, picked or not.
	for _, u := range alive {
		p := g.Players[u]
		if p.Items["apple"] == 0 {
			continue
		}
		p.Items["apple"]--
		if k := slices.Index(page, u); k >= 0 {
			page = slices.Delete(page, k, k+1)
			say(gaSpared["apple"], u)
		}
	}
	if twist == "armor" && len(page) > 0 {
		k := rnd(len(page))
		say(gaSpared["twist"], page[k])
		page = slices.Delete(page, k, k+1)
	}
	var written []string
	for _, u := range page {
		if p := g.Players[u]; p.Items["decoy"] > 0 {
			var free []string
			for _, v := range alive {
				if !slices.Contains(page, v) && !slices.Contains(written, v) && g.Players[v].Items["decoy"] == 0 {
					free = append(free, v)
				}
			}
			if len(free) > 0 {
				p.Items["decoy"]--
				v := free[rnd(len(free))]
				lines = append(lines, fmt.Sprintf("%s's decoy works: %s's name is on the line instead.", gaName(u), gaName(v)))
				u = v
			}
		}
		if p := g.Players[u]; p.Items["armor"] > 0 {
			p.Items["armor"]--
			say(gaSpared["armor"], u)
			continue
		}
		written = append(written, u)
	}
	// Somebody always survives a page.
	for len(written) >= len(alive) && len(written) > 0 {
		k := rnd(len(written))
		say(gaSpared["last"], written[k])
		written = slices.Delete(written, k, k+1)
	}
	for _, u := range written {
		p := g.Players[u]
		p.Out = g.Round
		say(gaWritten[rnd(len(gaWritten))], u)
		if p.Items["revive"] > 0 {
			p.Items["revive"]--
			p.Back = true
			say("%s holds a Revival Ticket.", u)
		}
	}
	if len(written) == 0 && twist != "filler" && len(lines) == 0 {
		lines = append(lines, "The page stays blank.")
	}
	alive = g.alive()
	back := false
	for _, p := range g.Players {
		back = back || p.Back
	}
	if len(alive) == 1 && !back {
		g.Done, g.Winner, g.Page = true, alive[0], nil
		lines = append(lines, fmt.Sprintf("**%s is the last name standing.**", gaName(g.Winner)))
	} else {
		g.drawPage(rnd)
		g.NextPage = now.Add(time.Duration(g.Every) * time.Minute)
	}
	header := fmt.Sprintf("**Page %d**", g.Round)
	g.addLog(header + ": " + strings.Join(lines, " "))
	return append([]string{header}, lines...)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// gaJoin adds user to the giveaway.
func gaJoin(g *giveaway, user string, now time.Time) error {
	switch {
	case g.Done || !now.Before(g.JoinUntil):
		return errors.New("Joining is closed.")
	case g.Players[user] != nil:
		return errors.New("Your name is already in the notebook.")
	case len(g.Players) >= gaMaxPlayers:
		return errors.New("The notebook is full.")
	}
	g.Players[user] = &gaPlayer{Joined: now}
	return nil
}

// gaBuy buys an item for user with their hanko.
func gaBuy(d *storeData, g *giveaway, user, key string) (string, error) {
	it, ok := gaItemByKey(key)
	p := g.Players[user]
	switch {
	case !ok:
		return "", errors.New("There's no such item.")
	case g.Done:
		return "", errors.New("This giveaway is over.")
	case p == nil:
		return "", errors.New("Join the giveaway first.")
	case p.Out > 0 && !p.Back:
		return "", errors.New("Your name is already written.")
	case p.Bought >= gaMaxItems:
		return "", fmt.Errorf("You have %d items already, the most for one giveaway.", gaMaxItems)
	case key == "alias" && p.Alias:
		return "", errors.New("You already go by a fake name.")
	}
	price := gaPrice(it, p)
	if !spendHanko(d, user, price) {
		return "", fmt.Errorf("%s costs %d hanko and you have %d.", it.Name, price, hankoBalance(d, user))
	}
	p.Bought++
	if key == "alias" {
		p.Alias = true
	} else {
		if p.Items == nil {
			p.Items = map[string]int{}
		}
		p.Items[key]++
	}
	return fmt.Sprintf("Bought **%s** for %d hanko. %s.", it.Name, price, it.Desc), nil
}

// gaEyes uses a pair of Shinigami Eyes: the next page, with user crossed
// out of it.
func gaEyes(g *giveaway, user string) ([]string, bool, error) {
	p := g.Players[user]
	switch {
	case g.Done || !g.Closed:
		return nil, false, errors.New("There's no page to look at yet.")
	case p == nil || p.Items["eyes"] == 0:
		return nil, false, errors.New("You don't have Shinigami Eyes. They're in the Shop.")
	}
	p.Items["eyes"]--
	page := slices.Clone(g.Page)
	k := slices.Index(g.Page, user)
	if k >= 0 {
		g.Page = slices.Delete(g.Page, k, k+1)
	}
	return page, k >= 0, nil
}

// ── Discord ──

func (g *giveaway) embed(now time.Time) *discordgo.MessageEmbed {
	prize := g.Prize
	if prize == "" {
		prize = "a /redeem code with a watch ticket"
	}
	alive := g.alive()
	var lines []string
	lines = append(lines, "**Prize:** "+prize)
	switch {
	case g.Cancelled:
		lines = append(lines, "**Called off.**")
	case g.Done:
		lines = append(lines, fmt.Sprintf("**Winner:** %s after %s.", gaName(g.Winner), dashPlural(g.Round, "page")))
	case now.Before(g.JoinUntil):
		lines = append(lines, fmt.Sprintf("Press **Join** to put your name in the notebook. Joining closes <t:%d:R>; then a page is written every %s and random names on it are out. The last name standing wins.",
			g.JoinUntil.Unix(), dashPlural(g.Every, "minute")))
	default:
		lines = append(lines, fmt.Sprintf("Page %d is written <t:%d:R>.", g.Round+1, g.NextPage.Unix()))
	}
	if !g.Done {
		lines = append(lines, "Spend hanko in the **Shop** to survive the pages (`/hanko` to see yours).")
	}
	if n := len(g.Log); n > 0 {
		lines = append(lines, "")
		lines = append(lines, g.Log[max(0, n-gaShownLog):]...)
	}
	return &discordgo.MessageEmbed{Title: "死 Shinigami Notebook", Color: shu, Description: clip(strings.Join(lines, "\n"), 4000),
		Footer: &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("%d of %s still standing", len(alive), dashPlural(len(g.Players), "name"))}}
}

func (g *giveaway) buttons(now time.Time) []discordgo.MessageComponent {
	var row []discordgo.MessageComponent
	switch {
	case g.Cancelled:
		return []discordgo.MessageComponent{}
	case g.Done:
		row = append(row, discordgo.Button{Label: "Claim prize", Style: discordgo.SuccessButton, CustomID: "ga-claim:" + g.ID})
	default:
		if now.Before(g.JoinUntil) {
			row = append(row, discordgo.Button{Label: "Join", Style: discordgo.DangerButton, CustomID: "ga-join:" + g.ID})
		}
		row = append(row,
			discordgo.Button{Label: "Shop", Style: discordgo.SecondaryButton, CustomID: "ga-shop:" + g.ID},
			discordgo.Button{Label: "Use Shinigami Eyes", Style: discordgo.SecondaryButton, CustomID: "ga-eyes:" + g.ID})
	}
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: row}}
}

func (b *bot) giveawayCommand(i *discordgo.InteractionCreate) {
	if !b.staffOnly(i) {
		return
	}
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return
	}
	sub := data.Options[0]
	opts := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
	for _, o := range sub.Options {
		opts[o.Name] = o
	}
	switch sub.Name {
	case "start":
		b.giveawayStart(i, opts)
	case "next":
		id := b.channelGiveaway(i.ChannelID)
		if id == "" {
			b.respond(i, true, errorEmbed("No giveaway running in this channel."))
			return
		}
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Writing the next page."})
		b.giveawayStep(id, appNow(), true)
	case "end":
		id := b.channelGiveaway(i.ChannelID)
		if id == "" {
			b.respond(i, true, errorEmbed("No giveaway running in this channel."))
			return
		}
		var g *giveaway
		b.store.update(func(d *storeData) {
			if g = d.Giveaways[id]; g != nil {
				g.Done, g.Cancelled = true, true
				g.addLog("Staff called the giveaway off.")
			}
		})
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Called off."})
		b.giveawayRefresh(id)
	}
}

// channelGiveaway is the running giveaway posted in channel, the newest one.
func (b *bot) channelGiveaway(channel string) string {
	id := ""
	var at time.Time
	b.store.view(func(d *storeData) {
		for k, g := range d.Giveaways {
			if !g.Done && (g.Channel == channel || g.Thread == channel) && g.Started.After(at) {
				id, at = k, g.Started
			}
		}
	})
	return id
}

func (b *bot) giveawayStart(i *discordgo.InteractionCreate, opts map[string]*discordgo.ApplicationCommandInteractionDataOption) {
	now := appNow()
	g := &giveaway{Channel: i.ChannelID, Host: i.Member.User.ID, Started: now, Every: gaDefaultEvery, Players: map[string]*gaPlayer{}}
	join := gaDefaultJoin
	if o, ok := opts["join_minutes"]; ok {
		join = int(o.IntValue())
	}
	if o, ok := opts["page_minutes"]; ok {
		g.Every = int(o.IntValue())
	}
	if o, ok := opts["prize"]; ok {
		g.Prize = clip(cleanText(o.StringValue(), false), 100)
	}
	g.JoinUntil = now.Add(time.Duration(join) * time.Minute)
	msg, err := b.s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{g.embed(now)},
		Components: g.buttons(now), AllowedMentions: &discordgo.MessageAllowedMentions{}})
	if err != nil {
		log.Printf("giveaway post: %v", err)
		b.respond(i, true, errorEmbed("I couldn't post in this channel."))
		return
	}
	g.ID = msg.ID
	if th, err := b.s.MessageThreadStart(i.ChannelID, msg.ID, "Shinigami Notebook", 1440); err == nil {
		g.Thread = th.ID
	} else {
		log.Printf("giveaway thread: %v", err)
	}
	b.store.update(func(d *storeData) {
		if d.Giveaways == nil {
			d.Giveaways = map[string]*giveaway{}
		}
		d.Giveaways[g.ID] = g
	})
	b.staffNote(&discordgo.MessageEmbed{Title: "Giveaways", Color: shu, Description: fmt.Sprintf("<@%s> started a Shinigami Notebook giveaway in <#%s>.", g.Host, g.Channel)})
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Giveaway started. `/giveaway next` closes joining or writes a page early, `/giveaway end` calls it off."})
}

func (b *bot) giveawayLoop() {
	for {
		now := appNow()
		var due []string
		b.store.view(func(d *storeData) {
			for id, g := range d.Giveaways {
				if !g.Done && (!g.Closed && !now.Before(g.JoinUntil) || g.Closed && !now.Before(g.NextPage)) {
					due = append(due, id)
				}
				// Finished ones are kept a month for their Claim button.
				if g.Done && now.Sub(g.Started) > 30*24*time.Hour {
					delete(d.Giveaways, id)
				}
			}
		})
		for _, id := range due {
			b.giveawayStep(id, now, false)
		}
		time.Sleep(15 * time.Second)
	}
}

// giveawayStep closes joining or writes the next page, when due or forced.
func (b *bot) giveawayStep(id string, now time.Time, force bool) {
	var lines []string
	var g giveaway
	b.store.update(func(d *storeData) {
		cur := d.Giveaways[id]
		if cur == nil || cur.Done {
			return
		}
		switch {
		case !cur.Closed:
			if !force && now.Before(cur.JoinUntil) {
				return
			}
			cur.closeJoining(now, gaRand)
			lines = []string{cur.Log[len(cur.Log)-1]}
		case force || !now.Before(cur.NextPage):
			lines = cur.writePage(now, gaRand)
		}
		if cur.Done && cur.Winner != "" && cur.Prize == "" {
			note := "Shinigami Notebook giveaway"
			cur.Code = mintCodes(d, cur.Host, note, 1, now)[0]
		}
		g = *cur
	})
	if len(lines) == 0 || b.s == nil {
		return
	}
	b.giveawayRefresh(id)
	where := g.Thread
	if where == "" {
		where = g.Channel
	}
	if _, err := b.s.ChannelMessageSendComplex(where, &discordgo.MessageSend{Content: clip(strings.Join(lines, "\n"), 2000),
		AllowedMentions: gaPings(g.Winner)}); err != nil {
		log.Printf("giveaway page: %v", err)
	}
	if g.Winner != "" {
		b.dm(g.Winner, &discordgo.MessageEmbed{Title: "You won the Shinigami Notebook", Color: shu,
			Description: fmt.Sprintf("Last name standing. Press **Claim prize** on the giveaway post in <#%s>.", g.Channel)})
	}
}

// gaPings lets a page ping only the winner.
func gaPings(winner string) *discordgo.MessageAllowedMentions {
	if winner == "" {
		return &discordgo.MessageAllowedMentions{}
	}
	return &discordgo.MessageAllowedMentions{Users: []string{winner}}
}

// giveawayRefresh edits the giveaway's post to match.
func (b *bot) giveawayRefresh(id string) {
	if b.s == nil {
		return
	}
	var e *discordgo.MessageEmbed
	var comps []discordgo.MessageComponent
	var channel string
	now := appNow()
	b.store.view(func(d *storeData) {
		if g := d.Giveaways[id]; g != nil {
			e, comps, channel = g.embed(now), g.buttons(now), g.Channel
		}
	})
	if e == nil {
		return
	}
	if _, err := b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: id, Channel: channel,
		Embeds: &[]*discordgo.MessageEmbed{e}, Components: &comps, AllowedMentions: &discordgo.MessageAllowedMentions{}}); err != nil {
		log.Printf("giveaway edit: %v", err)
	}
}

func (b *bot) gaJoinButton(i *discordgo.InteractionCreate, id string) {
	user := interactionUser(i).ID
	var err error
	b.store.update(func(d *storeData) {
		g := d.Giveaways[id]
		if g == nil {
			err = errors.New("This giveaway is gone.")
			return
		}
		if _, banned := benched(d, user, time.Now()); banned {
			err = errors.New("You're on a break from games.")
			return
		}
		err = gaJoin(g, user, appNow())
	})
	if err != nil {
		b.respond(i, true, errorEmbed("%s", err.Error()))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Your name is in the notebook. Good luck. The **Shop** has ways to survive the pages."})
	b.giveawayRefresh(id)
}

// shopView is the Shop as the member sees it.
func (b *bot) shopView(id, user, msg string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	var bal int
	var p gaPlayer
	var in bool
	b.store.view(func(d *storeData) {
		bal = hankoBalance(d, user)
		if g := d.Giveaways[id]; g != nil && g.Players[user] != nil {
			p, in = *g.Players[user], true
		}
	})
	var lines []string
	if msg != "" {
		lines = append(lines, msg, "")
	}
	lines = append(lines, fmt.Sprintf("You have **%d** hanko 判. Up to %d items each giveaway, each one costs more than the last.", bal, gaMaxItems), "")
	var opts []discordgo.SelectMenuOption
	for _, it := range gaItems {
		price := gaPrice(it, &p)
		have := p.Items[it.Key]
		if it.Key == "alias" && p.Alias {
			have = 1
		}
		line := fmt.Sprintf("**%s** · %d · %s", it.Name, price, it.Desc)
		if have > 0 {
			line += fmt.Sprintf(" (you have %d)", have)
		}
		lines = append(lines, line)
		opts = append(opts, discordgo.SelectMenuOption{Label: fmt.Sprintf("%s · %d hanko", it.Name, price), Value: it.Key, Description: it.Desc})
	}
	if !in {
		lines = append(lines, "", "Join the giveaway to buy.")
	}
	e := &discordgo.MessageEmbed{Title: "Shop", Color: shu, Description: strings.Join(lines, "\n")}
	if !in || p.Bought >= gaMaxItems {
		return e, []discordgo.MessageComponent{}
	}
	return e, []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "ga-buy:" + id, Placeholder: "Buy an item", Options: opts}}}}
}

func (b *bot) gaShopButton(i *discordgo.InteractionCreate, id string) {
	e, comps := b.shopView(id, interactionUser(i).ID, "")
	b.respond(i, true, e, comps...)
}

func (b *bot) gaBuyMenu(i *discordgo.InteractionCreate, id string) {
	user := interactionUser(i).ID
	vals := i.MessageComponentData().Values
	if len(vals) == 0 {
		return
	}
	var msg string
	var err error
	b.store.update(func(d *storeData) {
		g := d.Giveaways[id]
		if g == nil {
			err = errors.New("This giveaway is gone.")
			return
		}
		msg, err = gaBuy(d, g, user, vals[0])
	})
	if err != nil {
		msg = err.Error()
	}
	e, comps := b.shopView(id, user, msg)
	if rerr := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}}); rerr != nil {
		log.Printf("giveaway shop: %v", rerr)
	}
}

func (b *bot) gaEyesButton(i *discordgo.InteractionCreate, id string) {
	user := interactionUser(i).ID
	var page []string
	var crossed bool
	var err error
	b.store.update(func(d *storeData) {
		g := d.Giveaways[id]
		if g == nil {
			err = errors.New("This giveaway is gone.")
			return
		}
		page, crossed, err = gaEyes(g, user)
	})
	if err != nil {
		b.respond(i, true, errorEmbed("%s", err.Error()))
		return
	}
	names := make([]string, len(page))
	for k, u := range page {
		names[k] = gaName(u)
		if u == user {
			names[k] = "~~" + names[k] + "~~"
		}
	}
	text := "The next page reads: " + strings.Join(names, ", ") + "."
	if len(page) == 0 {
		text = "The next page is blank."
	}
	if crossed {
		text += "\n\nYour name was on it. You crossed it out."
	} else {
		text += "\n\nYou're not on it. Yet."
	}
	b.respond(i, true, &discordgo.MessageEmbed{Title: "Shinigami Eyes", Color: shu, Description: text})
}

func (b *bot) gaClaimButton(i *discordgo.InteractionCreate, id string) {
	user := interactionUser(i).ID
	var g giveaway
	b.store.view(func(d *storeData) {
		if cur := d.Giveaways[id]; cur != nil {
			g = *cur
		}
	})
	switch {
	case g.Winner == "":
		b.respond(i, true, errorEmbed("This giveaway is gone."))
	case g.Winner != user:
		b.respond(i, true, errorEmbed("Only %s can claim this one.", gaName(g.Winner)))
	case g.Code != "":
		b.respond(i, true, &discordgo.MessageEmbed{Title: "Your prize", Color: shu,
			Description: "`" + formatCheatCode(g.Code) + "`\n\nRedeem it with `/redeem` for a watch ticket, or give it to someone. It works once."})
	default:
		b.respondQuiet(i, true, &discordgo.MessageEmbed{Title: "Your prize", Color: shu,
			Description: fmt.Sprintf("**%s**. <@%s> ran this giveaway and will get it to you.", g.Prize, g.Host)})
	}
}
