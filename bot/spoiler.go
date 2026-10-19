package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Spoiler guard: in the chat channels, a message that names a show airing
// now or just finished together with spoiler words ("dies", "finale",
// "turns out" ...) and isn't inside ||spoiler tags|| gets a short reply that
// deletes itself, or, in hide mode, is reposted by the bot wrapped in
// spoiler tags under the author's name and picture. Staff pick the mode with
// /spoiler-guard. A member is nudged at most once in 10 minutes.

const (
	spoilerOff   = "off"
	spoilerNudge = "nudge"
	spoilerHide  = "hide"

	spoilerCooldown  = 10 * time.Minute
	spoilerNoteAfter = 30 * time.Second
	spoilerHookName  = "Otakase spoilers"
)

type spoilerGuard struct {
	mu      sync.Mutex
	season  []string  // normalised titles of this and last season's shows
	fetched time.Time // when season was fetched
	titles  []string  // season plus the airing board and followed shows
	built   time.Time
	nagged  map[string]time.Time // member → last nudge
	hooks   map[string]*discordgo.Webhook
}

func spoilerCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{Name: "spoiler-guard", Description: "What the bot does with unmarked spoilers in chat",
		DefaultMemberPermissions: &permPurge, Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "mode", Description: "What to do", Required: true,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Off", Value: spoilerOff},
					{Name: "Nudge: a short reminder that deletes itself (default)", Value: spoilerNudge},
					{Name: "Hide: repost the message inside spoiler tags", Value: spoilerHide},
				}},
		}}
}

func (b *bot) spoilerMode() string {
	mode := ""
	b.store.view(func(d *storeData) { mode = d.SpoilerGuard })
	if mode == "" {
		return spoilerNudge
	}
	return mode
}

func (b *bot) setSpoilerGuard(i *discordgo.InteractionCreate) {
	mode := optionMap(i)["mode"].StringValue()
	b.store.update(func(d *storeData) { d.SpoilerGuard = mode })
	text := map[string]string{
		spoilerOff:   "Spoiler guard is off.",
		spoilerNudge: "Spoiler guard will remind people to use spoiler tags.",
		spoilerHide:  "Spoiler guard will hide unmarked spoilers inside spoiler tags.",
	}[mode]
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("<@%s> set the spoiler guard to **%s**.", i.Member.User.ID, mode)}, nil, false)
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: text})
}

// ── Detector ──

var (
	spoilerTagRe   = regexp.MustCompile(`(?s)\|\|.*?\|\||` + "```.*?```|`[^`]*`" + `|<[^>]*>|https?://\S+`)
	spoilerStrong  = regexp.MustCompile(`\b(dies|died|dying|death of|deaths?|killed|kills|killing|murder(s|ed)?|is dead|was dead|are dead|gets killed|turns out|turned out|betray(s|ed|al)?|traitor|plot twist|revealed to be|reveal that|real identity|true identity|confess(es|ed)?|ends up with|end up together|got together|sacrifice[sd]?|lost (his|her|their) (arm|arms|leg|legs|eye|eyes|memory|powers))\b`)
	spoilerMedium  = regexp.MustCompile(`\b(ending|ended|finale|last episode|final episode|last ep|final ep|cliffhanger|reveal|revealed|twist)\b`)
	spoilerEpisode = regexp.MustCompile(`\b(ep|episode|chapter|ch)\s?\d+\b`)
	spoilerEvent   = regexp.MustCompile(`\b(happened|happens|when|scene|moment|that part|wtf|omg|cant believe|crazy|insane|shock(ed|ing)?|cried|crying|tears|ruined|destroyed|broke me)\b`)
	spoilerBenign  = regexp.MustCompile(`\b(when (does|is|will|do|did)|where (can|do|to|is)|what time|out yet|is out|come out|comes out|coming out|air|airs|airing|release|released|release date|delay|delayed|dub|dubbed|subbed|stream(ing)?|trailer|announced|started watching|start watching|recommend|worth (it|watching)|should i watch|any good|opening|op|ed)\b`)
	// Figures of speech that only sound like spoilers.
	spoilerIdioms = regexp.MustCompile(`\b(died laughing|dying laughing|i died|im dead|im dying|killed it|kills it|killing it|killing me|kills me|dead link|ending (song|theme|music|sequence|credits|animation)|death note)\b`)
)

// spoilerTitleOK reports whether a normalised title is distinctive enough
// to look for in chat: long enough, and not a word people use anyway.
func spoilerTitleOK(t string) bool {
	if len([]rune(t)) < 5 {
		return false
	}
	if !strings.Contains(t, " ") {
		if len([]rune(t)) < 6 {
			return false
		}
		switch t {
		case "monster", "another", "summer", "island", "school", "anime", "season", "family", "friends",
			"hunter", "beyond", "beautiful", "trigger", "orange", "erased", "classroom", "shadow", "kingdom":
			return false
		}
	}
	return true
}

// spoilerTitles turns show titles into the normalised forms to look for:
// each title and its part before a subtitle, also written as one word for
// short titles ("dan da dan" and "dandadan"), longest first.
func spoilerTitles(raw []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range raw {
		for _, v := range []string{r, mainTitle(r)} {
			t := normTitle(v)
			if v == "" || !spoilerTitleOK(t) {
				continue
			}
			forms := []string{t}
			if n := strings.Count(t, " "); n >= 1 && n <= 2 {
				forms = append(forms, strings.ReplaceAll(t, " ", ""))
			}
			for _, f := range forms {
				if !seen[f] {
					seen[f] = true
					out = append(out, f)
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// spoilerHit reports whether text names one of titles (normalised, from
// spoilerTitles) with spoiler words outside spoiler tags, and which title.
func spoilerHit(text string, titles []string) (string, bool) {
	clean := " " + normWords(spoilerTagRe.ReplaceAllString(text, " ")) + " "
	if len(clean) < 12 {
		return "", false
	}
	found := ""
	for _, t := range titles {
		if strings.Contains(clean, " "+t+" ") {
			found = t
			break
		}
	}
	if found == "" {
		return "", false
	}
	rest := strings.ReplaceAll(clean, " "+found+" ", " ")
	rest = spoilerIdioms.ReplaceAllString(rest, " ")
	switch {
	case spoilerStrong.MatchString(rest):
		return found, true
	case spoilerBenign.MatchString(rest):
		return "", false
	case spoilerMedium.MatchString(rest):
		return found, true
	case spoilerEpisode.MatchString(rest) && spoilerEvent.MatchString(rest):
		return found, true
	}
	return "", false
}

// ── Titles ──

// seasonOf is the AniList season of t and the one before it.
func seasonAndPrev(t time.Time) (season string, year int, prevSeason string, prevYear int) {
	seasons := []string{"WINTER", "SPRING", "SUMMER", "FALL"}
	k := (int(t.Month()) - 1) / 3
	season, year = seasons[k], t.Year()
	prevSeason, prevYear = seasons[(k+3)%4], year
	if k == 0 {
		prevYear--
	}
	return
}

const seasonTitlesQuery = `query ($season: MediaSeason, $year: Int, $page: Int) {
  Page(page: $page, perPage: 50) {
    media(type: ANIME, season: $season, seasonYear: $year, isAdult: false, sort: POPULARITY_DESC) { title { romaji english } }
  }
}`

// fetchSeasonTitles gets the titles of the most popular shows of this
// season and the last one.
func fetchSeasonTitles(now time.Time) ([]string, error) {
	s, y, ps, py := seasonAndPrev(now)
	var titles []string
	for _, q := range []struct {
		season string
		year   int
	}{{s, y}, {ps, py}} {
		for page := 1; page <= 2; page++ {
			var r struct {
				Data struct {
					Page struct {
						Media []struct {
							Title struct{ Romaji, English string }
						} `json:"media"`
					} `json:"Page"`
				} `json:"data"`
			}
			if err := anilistPost("", seasonTitlesQuery, map[string]any{"season": q.season, "year": q.year, "page": page}, &r); err != nil {
				return nil, err
			}
			for _, m := range r.Data.Page.Media {
				titles = append(titles, m.Title.Romaji, m.Title.English)
			}
		}
	}
	return titles, nil
}

// spoilerTitleLoop refreshes the season's titles twice a day.
func (b *bot) spoilerTitleLoop() {
	for {
		titles, err := fetchSeasonTitles(time.Now().UTC())
		wait := 12 * time.Hour
		if err != nil {
			b.jobFailed("Spoiler guard titles", err)
			wait = 30 * time.Minute
		} else {
			b.jobOK("Spoiler guard titles")
			list := spoilerTitles(titles)
			g := &b.spoilers
			g.mu.Lock()
			g.season, g.fetched, g.built = list, time.Now(), time.Time{}
			g.mu.Unlock()
		}
		time.Sleep(wait)
	}
}

// currentTitles is the season's titles plus today's airing board and the
// shows members follow, rebuilt every 10 minutes.
func (b *bot) currentTitles() []string {
	g := &b.spoilers
	g.mu.Lock()
	if time.Since(g.built) < 10*time.Minute {
		t := g.titles
		g.mu.Unlock()
		return t
	}
	season := g.season
	g.mu.Unlock()
	var raw []string
	b.store.view(func(d *storeData) {
		if d.Airing != nil {
			for _, a := range d.Airing.All {
				raw = append(raw, a.Title)
			}
		}
		for _, list := range d.Follows {
			for _, f := range list {
				raw = append(raw, f.Title)
			}
		}
	})
	titles := spoilerTitles(raw)
	seen := map[string]bool{}
	for _, t := range titles {
		seen[t] = true
	}
	for _, t := range season {
		if !seen[t] {
			titles = append(titles, t)
		}
	}
	sort.SliceStable(titles, func(i, j int) bool { return len(titles[i]) > len(titles[j]) })
	g.mu.Lock()
	g.titles, g.built = titles, time.Now()
	g.mu.Unlock()
	return titles
}

// ── Acting on a message ──

// nagOK reports whether userID wasn't nudged in the last 10 minutes, and
// marks them nudged.
func (g *spoilerGuard) nagOK(userID string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.nagged == nil {
		g.nagged = map[string]time.Time{}
	}
	if now.Sub(g.nagged[userID]) < spoilerCooldown {
		return false
	}
	g.nagged[userID] = now
	if len(g.nagged) > 2000 {
		for id, t := range g.nagged {
			if now.Sub(t) >= spoilerCooldown {
				delete(g.nagged, id)
			}
		}
	}
	return true
}

// checkSpoiler runs the spoiler guard on a chat message.
func (b *bot) checkSpoiler(m *discordgo.MessageCreate) {
	if len(m.Content) < 12 || !strings.ContainsAny(m.Content, " ") {
		return
	}
	mode := b.spoilerMode()
	if mode == spoilerOff {
		return
	}
	p := b.place(m.ChannelID)
	if p.Staff || p.Support || p.Bots || !chatName(p.Name) {
		return
	}
	title, hit := spoilerHit(m.Content, b.currentTitles())
	if !hit || !b.spoilers.nagOK(m.Author.ID, time.Now()) {
		return
	}
	if mode == spoilerHide && b.hideSpoiler(m) {
		b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276,
			Description: fmt.Sprintf("Hid a possible spoiler (%s) from <@%s> in <#%s>.", title, m.Author.ID, m.ChannelID)}, nil, false)
		return
	}
	note, err := b.s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Content:         "Spoiler? Wrap it in ||like this||",
		Reference:       m.Reference(),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
	if err != nil {
		log.Printf("spoiler nudge: %v", err)
		return
	}
	time.AfterFunc(spoilerNoteAfter, func() { _ = b.s.ChannelMessageDelete(note.ChannelID, note.ID) })
}

// hideSpoiler reposts the message inside spoiler tags through a channel
// webhook with the author's name and picture, then deletes it. It reports
// whether that worked.
func (b *bot) hideSpoiler(m *discordgo.MessageCreate) bool {
	c := b.channel(m.ChannelID)
	if c == nil {
		return false
	}
	hookChannel, thread := c.ID, ""
	if c.IsThread() {
		hookChannel, thread = c.ParentID, c.ID
	}
	hook, err := b.spoilerHook(hookChannel)
	if err != nil {
		log.Printf("spoiler webhook: %v", err)
		return false
	}
	name := m.Author.GlobalName
	if name == "" {
		name = m.Author.Username
	}
	if m.Member != nil && m.Member.Nick != "" {
		name = m.Member.Nick
	}
	params := &discordgo.WebhookParams{
		Content:         spoilerRepost(m.Content),
		Username:        clip(name, 80),
		AvatarURL:       m.Author.AvatarURL("128"),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
		Files:           spoilerFiles(m.Attachments),
	}
	if thread != "" {
		_, err = b.s.WebhookThreadExecute(hook.ID, hook.Token, true, thread, params)
	} else {
		_, err = b.s.WebhookExecute(hook.ID, hook.Token, true, params)
	}
	if err != nil {
		log.Printf("spoiler repost: %v", err)
		b.spoilers.mu.Lock()
		delete(b.spoilers.hooks, hookChannel) // maybe deleted: find it again next time
		b.spoilers.mu.Unlock()
		return false
	}
	if err := b.s.ChannelMessageDelete(m.ChannelID, m.ID, discordgo.WithAuditLogReason("Spoiler hidden")); err != nil {
		log.Printf("spoiler delete: %v", err)
	}
	return true
}

// spoilerRepost is the hidden message's text.
func spoilerRepost(content string) string {
	text := strings.TrimSpace(strings.ReplaceAll(content, "||", ""))
	if text == "" {
		return "-# (spoiler hidden by bot)"
	}
	return "||" + clip(text, 1900) + "||\n-# (spoiler hidden by bot)"
}

// spoilerFiles downloads a message's attachments to post them again as
// spoilers (up to 4, 8 MB each).
func spoilerFiles(atts []*discordgo.MessageAttachment) []*discordgo.File {
	var files []*discordgo.File
	for _, a := range atts {
		if len(files) == 4 || a.Size > 8<<20 {
			continue
		}
		resp, err := httpClient.Get(a.URL)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		name := a.Filename
		if !strings.HasPrefix(name, "SPOILER_") {
			name = "SPOILER_" + name
		}
		files = append(files, &discordgo.File{Name: name, ContentType: a.ContentType, Reader: bytes.NewReader(data)})
	}
	return files
}

// spoilerHook finds or makes the bot's webhook in a channel.
func (b *bot) spoilerHook(channelID string) (*discordgo.Webhook, error) {
	g := &b.spoilers
	g.mu.Lock()
	if h := g.hooks[channelID]; h != nil {
		g.mu.Unlock()
		return h, nil
	}
	g.mu.Unlock()
	hooks, err := b.s.ChannelWebhooks(channelID)
	if err != nil {
		return nil, err
	}
	var hook *discordgo.Webhook
	for _, h := range hooks {
		if h.Name == spoilerHookName && h.Token != "" && h.User != nil && h.User.ID == b.s.State.User.ID {
			hook = h
			break
		}
	}
	if hook == nil {
		if hook, err = b.s.WebhookCreate(channelID, spoilerHookName, ""); err != nil {
			return nil, err
		}
	}
	g.mu.Lock()
	if g.hooks == nil {
		g.hooks = map[string]*discordgo.Webhook{}
	}
	g.hooks[channelID] = hook
	g.mu.Unlock()
	return hook, nil
}
