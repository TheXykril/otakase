package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Each day at AiringHour (UTC) the bot posts a live board of the episodes
// airing in the next 24 hours to the anime channel: one card with a picture
// of the most popular shows, then the 20 most popular grouped by the hour
// they air. Times are Discord timestamps, so everyone sees their own
// time zone. Through the day the bot edits the board: the current hour is
// marked airing now and the next one up next, and aired hours move to small
// lines at the bottom. Yesterday's board is deleted when today's goes up. A menu under it
// opens a show's card (link, DM when it airs, watch party) and "My shows
// today" lists what airs from the member's AniList.

const airingQuery = `query ($from: Int, $to: Int, $page: Int) {
  Page(page: $page, perPage: 50) {
    pageInfo { hasNextPage }
    airingSchedules(airingAt_greater: $from, airingAt_lesser: $to, sort: TIME) {
      airingAt episode
      media { id siteUrl isAdult popularity format countryOfOrigin title { romaji english } episodes bannerImage coverImage { extraLarge } }
    }
  }
}`

// boardSize is how many shows the board lists, most popular first.
const boardSize = 20

type airing struct {
	ID         int    `json:"id"`
	At         int64  `json:"at"`
	Episode    int    `json:"ep"`
	Episodes   int    `json:"eps,omitempty"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Popularity int    `json:"pop"`
	Cover      string `json:"cover,omitempty"`
	Banner     string `json:"banner,omitempty"`
}

// airingBoard is today's board message and what it shows.
type airingBoard struct {
	Channel string    `json:"channel"`
	Message string    `json:"message"`
	Day     time.Time `json:"day"`
	Shows   []airing  `json:"shows"` // on the board, by time
	All     []airing  `json:"all"`   // everything airing that day, for My shows
	State   int       `json:"state"` // boardState the message shows
}

func fetchAiring(from, to time.Time) ([]airing, error) {
	var list []airing
	for page := 1; page <= 4; page++ {
		body, _ := json.Marshal(map[string]any{"query": airingQuery,
			"variables": map[string]any{"from": from.Unix(), "to": to.Unix(), "page": page}})
		req, _ := http.NewRequest("POST", "https://graphql.anilist.co", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		var r struct {
			Data struct {
				Page struct {
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
					AiringSchedules []struct {
						AiringAt int64 `json:"airingAt"`
						Episode  int   `json:"episode"`
						Media    struct {
							ID              int    `json:"id"`
							SiteURL         string `json:"siteUrl"`
							IsAdult         bool   `json:"isAdult"`
							Popularity      int    `json:"popularity"`
							Format          string `json:"format"`
							CountryOfOrigin string `json:"countryOfOrigin"`
							Episodes        *int   `json:"episodes"`
							Title           struct{ Romaji, English string }
							BannerImage     string `json:"bannerImage"`
							CoverImage      struct {
								ExtraLarge string `json:"extraLarge"`
							} `json:"coverImage"`
						} `json:"media"`
					} `json:"airingSchedules"`
				} `json:"Page"`
			} `json:"data"`
		}
		if err := getJSON(req, &r); err != nil {
			return nil, err
		}
		for _, a := range r.Data.Page.AiringSchedules {
			m := a.Media
			if m.IsAdult || m.CountryOfOrigin != "JP" {
				continue
			}
			title := m.Title.English
			if title == "" {
				title = m.Title.Romaji
			}
			eps := 0
			if m.Episodes != nil {
				eps = *m.Episodes
			}
			list = append(list, airing{ID: m.ID, At: a.AiringAt, Episode: a.Episode, Episodes: eps, Title: title, URL: m.SiteURL,
				Popularity: m.Popularity, Cover: m.CoverImage.ExtraLarge, Banner: m.BannerImage})
		}
		if !r.Data.Page.PageInfo.HasNextPage {
			break
		}
	}
	return list, nil
}

// boardShows keeps the most popular shows, listed by time.
func boardShows(all []airing) []airing {
	list := append([]airing(nil), all...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Popularity > list[j].Popularity })
	if len(list) > boardSize {
		list = list[:boardSize]
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].At < list[j].At })
	return list
}

// airingGroup is the shows starting in one UTC hour.
type airingGroup struct {
	At    int64 // start of the hour
	Shows []airing
}

// airingGroups groups shows (sorted by time) by the hour they air.
func airingGroups(shows []airing) []airingGroup {
	var groups []airingGroup
	for _, s := range shows {
		h := s.At - s.At%3600
		if n := len(groups); n > 0 && groups[n-1].At == h {
			groups[n-1].Shows = append(groups[n-1].Shows, s)
		} else {
			groups = append(groups, airingGroup{At: h, Shows: []airing{s}})
		}
	}
	return groups
}

// boardState changes when an hour starts or ends, which is when the board
// needs editing.
func boardState(shows []airing, now time.Time) int {
	started, aired := 0, 0
	for _, g := range airingGroups(shows) {
		if g.At <= now.Unix() {
			started++
		}
		if g.At+3600 <= now.Unix() {
			aired++
		}
	}
	return started*1000 + aired
}

// epLabel is "ep 3", with "new" or "finale" when it is one.
func epLabel(a airing) string {
	ep := fmt.Sprintf("ep %d", a.Episode)
	switch {
	case a.Episodes > 0 && a.Episode == a.Episodes:
		ep += " · finale"
	case a.Episode == 1:
		ep += " · new"
	}
	return ep
}

// boardMessage is the board: one Shu card (components v2) with the
// picture on top, then the hours to come in one column: the current hour is
// marked airing now and the next up next, each with a live countdown. Aired
// hours move to small lines at the bottom. The show menu and My shows today
// close the card. picture is the picture's URL, empty to leave it out; marks
// are the Shu emojis, plain text when empty.
func boardMessage(shows []airing, now time.Time, picture string, marks boardMarks) []discordgo.MessageComponent {
	color := shu
	var parts []discordgo.MessageComponent
	if picture != "" {
		parts = append(parts, discordgo.MediaGallery{Items: []discordgo.MediaGalleryItem{{Media: discordgo.UnfurledMediaItem{URL: picture}}}})
	}
	if len(shows) == 0 {
		parts = append(parts, discordgo.TextDisplay{Content: "Nothing airing today."})
		return []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: parts}}
	}
	// The labels are Shu word emojis when uploaded, else bold text.
	label := func(m, text string) string {
		if m == "" {
			return "**" + text + "**"
		}
		return m
	}
	groups := airingGroups(shows)
	var aired []string
	var live []string
	next := true
	for _, g := range groups {
		if g.At+3600 <= now.Unix() {
			var names []string
			for _, s := range g.Shows {
				names = append(names, fmt.Sprintf("%s %s", mdEscape(clip(s.Title, 50)), epLabel(s)))
			}
			aired = append(aired, fmt.Sprintf("-# <t:%d:t> · %s", g.At, strings.Join(names, " · ")))
			continue
		}
		status := fmt.Sprintf("-# <t:%d:R>", g.At)
		switch {
		case g.At <= now.Unix():
			status = label(marks.Live, "AIRING NOW")
		case next:
			status = fmt.Sprintf("%s · <t:%d:R>", label(marks.Next, "UP NEXT"), g.At)
			next = false
		}
		lines := []string{fmt.Sprintf("### <t:%d:t>", g.At), status}
		for _, s := range g.Shows {
			lines = append(lines, fmt.Sprintf("%s *%s*", mdEscape(clip(s.Title, 60)), epLabel(s)))
		}
		live = append(live, strings.Join(lines, "\n"))
	}
	// Blank space under the picture and above the menu.
	large, off := discordgo.SeparatorSpacingSizeLarge, false
	gap := discordgo.Separator{Divider: &off, Spacing: &large}
	if picture != "" {
		parts = append(parts, gap)
	}
	// A message holds 40 components: lines between hours only when they fit.
	lines := 1+len(parts)+2*len(live)+2+4 <= 40
	small := discordgo.SeparatorSpacingSizeSmall
	divider := true
	for k, l := range live {
		if lines && k > 0 {
			parts = append(parts, discordgo.Separator{Divider: &divider, Spacing: &small})
		}
		parts = append(parts, discordgo.TextDisplay{Content: clip(l, 1500)})
	}
	if len(aired) > 0 {
		if len(live) > 0 {
			parts = append(parts, discordgo.Separator{Divider: &divider, Spacing: &large})
		}
		head := "-# **AIRED TODAY**"
		if marks.Aired != "" {
			head = "-# " + marks.Aired + " **AIRED TODAY**"
		}
		parts = append(parts, discordgo.TextDisplay{Content: clip(head+"\n"+strings.Join(aired, "\n"), 2000)})
	}
	// One option per show: Discord rejects the whole post when two options
	// share a value, as happens when a show airs two episodes in one day.
	var opts []discordgo.SelectMenuOption
	seen := map[int]int{}
	for _, s := range shows {
		if i, ok := seen[s.ID]; ok {
			first := strings.TrimPrefix(strings.TrimPrefix(opts[i].Description, "Episodes "), "Episode ")
			first, _, _ = strings.Cut(first, "–")
			opts[i].Description = "Episodes " + first + "–" + strconv.Itoa(s.Episode)
			continue
		}
		if len(opts) == 25 {
			continue
		}
		seen[s.ID] = len(opts)
		opts = append(opts, discordgo.SelectMenuOption{Label: clip(s.Title, 100), Value: strconv.Itoa(s.ID), Description: "Episode " + strconv.Itoa(s.Episode)})
	}
	parts = append(parts, gap,
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.SelectMenu{
			MenuType: discordgo.StringSelectMenu, CustomID: "airing-pick", Placeholder: "Pick a show for its link or a DM when it airs…", Options: opts}}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "My shows today", Style: discordgo.SecondaryButton, CustomID: "airing-mine"}}},
	)
	return []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: parts}}
}

// boardPicture draws the picture for the day: the most popular show as the
// spotlight and the next six, in the order they air.
func boardPicture(day time.Time, all, shows []airing) ([]byte, error) {
	pop := append([]airing(nil), shows...)
	sort.SliceStable(pop, func(i, j int) bool { return pop[i].Popularity > pop[j].Popularity })
	if len(pop) > boardCovers+1 {
		pop = pop[:boardCovers+1]
	}
	if len(pop) > 1 {
		rest := pop[1:]
		sort.SliceStable(rest, func(i, j int) bool { return rest[i].At < rest[j].At })
	}
	urls := make([]string, 0, len(pop)+1)
	for _, s := range pop {
		urls = append(urls, s.Cover)
	}
	if len(pop) > 0 {
		urls = append(urls, pop[0].Banner)
	}
	imgs := fetchImages(urls)
	var list []boardShow
	for k, s := range pop {
		list = append(list, boardShow{Title: s.Title, Episode: s.Episode, Cover: imgs[k]})
	}
	if len(list) > 0 {
		list[0].Banner = imgs[len(imgs)-1]
	}
	return renderAiringBoard(day.Format("Monday 2 Jan"), len(all), list)
}

// boardPNGCache keeps the day's picture, so edits don't draw it again.
type boardPNGCache struct {
	mu  sync.Mutex
	day time.Time
	png []byte
}

// boardImage is the picture for the day, drawn once; nil when drawing
// failed.
func (b *bot) boardImage(day time.Time, all, shows []airing) []byte {
	c := &b.boardPNG
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.png != nil && c.day.Equal(day) {
		return c.png
	}
	png, err := boardPicture(day, all, shows)
	if err != nil {
		log.Printf("airing picture: %v", err) // the list goes up alone
		return nil
	}
	c.day, c.png = day, png
	return png
}

func boardFile(png []byte) *discordgo.File {
	return &discordgo.File{Name: "airing.png", ContentType: "image/png", Reader: bytes.NewReader(png)}
}

func (b *bot) postAiring(now time.Time) error {
	day := time.Date(now.Year(), now.Month(), now.Day(), b.settingInt("airing_hour_utc"), 0, 0, 0, time.UTC)
	var old *airingBoard
	b.store.view(func(d *storeData) { old = d.Airing })
	if old != nil && old.Day.Equal(day) && old.Message != "" {
		return nil
	}
	all, err := fetchAiring(day, day.Add(24*time.Hour))
	if err != nil {
		return fmt.Errorf("fetching the schedule: %w", err)
	}
	shows := boardShows(all)
	msg := &discordgo.MessageSend{Flags: discordgo.MessageFlagsIsComponentsV2}
	picture := ""
	if png := b.boardImage(day, all, shows); png != nil {
		msg.Files = []*discordgo.File{boardFile(png)}
		picture = "attachment://airing.png"
	}
	msg.Components = boardMessage(shows, now, picture, b.boardMarks())
	m, err := b.s.ChannelMessageSendComplex(b.cfg.AiringChannel, msg)
	if err != nil {
		return fmt.Errorf("posting: %w", err)
	}
	b.store.update(func(d *storeData) {
		d.Airing = &airingBoard{Channel: m.ChannelID, Message: m.ID, Day: day, Shows: shows, All: all, State: boardState(shows, now)}
	})
	b.dropOldBoards(m.ID, old)
	return nil
}

// dropOldBoards deletes yesterday's board, and posts from before the live
// board (an embed titled "Airing today · ...").
func (b *bot) dropOldBoards(keep string, old *airingBoard) {
	if old != nil && old.Message != "" && old.Message != keep {
		if err := b.s.ChannelMessageDelete(old.Channel, old.Message); err != nil {
			log.Printf("airing: deleting yesterday's board: %v", err)
		}
	}
	msgs, err := b.s.ChannelMessages(b.cfg.AiringChannel, 50, "", "", "")
	if err != nil {
		return
	}
	for _, m := range msgs {
		if m.ID == keep || m.Author == nil || m.Author.ID != b.s.State.User.ID {
			continue
		}
		for _, e := range m.Embeds {
			if strings.HasPrefix(e.Title, "Airing today · ") {
				b.s.ChannelMessageDelete(m.ChannelID, m.ID)
				break
			}
		}
	}
}

// refreshAiring edits today's board when an hour has started or ended.
func (b *bot) refreshAiring(now time.Time) error {
	var board airingBoard
	ok := false
	b.store.view(func(d *storeData) {
		if d.Airing != nil {
			board, ok = *d.Airing, true
		}
	})
	state := boardState(board.Shows, now)
	if !ok || board.Message == "" || state == board.State {
		return nil
	}
	m, err := b.s.ChannelMessage(board.Channel, board.Message)
	if rest, isRest := err.(*discordgo.RESTError); isRest && rest.Response != nil && rest.Response.StatusCode == 404 {
		b.boardGone(board.Message) // deleted while the bot was away: post it again
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the board: %w", err)
	}
	// The picture goes up again with every edit: an edit that only points
	// at the posted file's link loses the picture.
	edit := &discordgo.MessageEdit{ID: board.Message, Channel: board.Channel, Flags: discordgo.MessageFlagsIsComponentsV2,
		Attachments: &[]*discordgo.MessageAttachment{}}
	picture := ""
	if png := b.boardImage(board.Day, board.All, board.Shows); png != nil {
		// The new file replaces the old: id 0 is the first file sent.
		edit.Files = []*discordgo.File{boardFile(png)}
		edit.Attachments = &[]*discordgo.MessageAttachment{{ID: "0", Filename: "airing.png"}}
		picture = "attachment://airing.png"
	} else if len(m.Attachments) > 0 {
		// Drawing failed: keep the posted file.
		edit.Attachments = &[]*discordgo.MessageAttachment{{ID: m.Attachments[0].ID, Filename: m.Attachments[0].Filename}}
		picture = "attachment://" + m.Attachments[0].Filename
	}
	comps := boardMessage(board.Shows, now, picture, b.boardMarks())
	// No embeds: they would stop the switch of an older board to a card.
	embeds := []*discordgo.MessageEmbed{}
	edit.Components, edit.Embeds = &comps, &embeds
	if _, err := b.s.ChannelMessageEditComplex(edit); err != nil {
		return fmt.Errorf("editing the board: %w", err)
	}
	b.store.update(func(d *storeData) {
		if d.Airing != nil && d.Airing.Message == board.Message {
			d.Airing.State = state
		}
	})
	return nil
}

func (b *bot) airingLoop() {
	if b.cfg.AiringChannel == "" {
		return
	}
	for {
		now := time.Now().UTC()
		// postAiring does nothing once today's board is up, so a failed
		// post is retried on the next pass and a deleted board is posted
		// again.
		if b.settingBool("daily_airing") && now.Hour() >= b.settingInt("airing_hour_utc") {
			if err := b.postAiring(now); err != nil {
				b.jobFailed("Daily airing post", err)
			} else {
				b.jobOK("Daily airing post")
			}
		}
		if err := b.refreshAiring(now); err != nil {
			log.Printf("airing board: %v", err)
		}
		time.Sleep(5 * time.Minute)
	}
}

// boardGone forgets a deleted board so the next pass posts today's again:
// staff delete the board to have it posted fresh.
func (b *bot) boardGone(id string) {
	ours := false
	b.store.view(func(d *storeData) { ours = d.Airing != nil && d.Airing.Message == id })
	if !ours {
		return // not the board: don't rewrite the data file
	}
	b.store.update(func(d *storeData) {
		if d.Airing != nil && d.Airing.Message == id {
			d.Airing.Message = ""
		}
	})
}

func (b *bot) onMessageDelete(s *discordgo.Session, m *discordgo.MessageDelete) {
	b.boardGone(m.ID)
}
