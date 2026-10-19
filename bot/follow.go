package main

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Members /follow shows and the bot sends them a DM when a new episode of
// one airs. Follows live in the store, keyed by user id.

const maxFollows = 50

// followedShow is one show a member follows.
type followedShow struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
}

func followCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{Name: "follow", Description: "Get a DM when a new episode of a show airs", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "anime", Description: "The show (pick from the suggestions)", Required: true, Autocomplete: true},
		}},
		{Name: "unfollow", Description: "Stop episode DMs for a show", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "anime", Description: "A show you follow", Required: true, Autocomplete: true},
		}},
		{Name: "following", Description: "The shows you get episode DMs for"},
	}
}

const followShowQuery = `query ($id: Int, $q: String) {
  Media(id: $id, search: $q, type: ANIME, isAdult: false) {
    id siteUrl status title { romaji english }
    nextAiringEpisode { airingAt episode }
  }
}`

type followLookup struct {
	Show   followedShow
	Status string
	NextAt int64
	NextEp int
}

// findFollowShow looks up a picked suggestion (anilist:ID) or, failing
// that, the best title match for typed text.
func findFollowShow(q string) (followLookup, bool, error) {
	vars := map[string]any{"q": q}
	if m := pickedShowRe.FindStringSubmatch(q); m != nil {
		id, _ := strconv.Atoi(m[1])
		vars = map[string]any{"id": id}
	}
	var r struct {
		Data struct {
			Media *struct {
				ID                int    `json:"id"`
				SiteURL           string `json:"siteUrl"`
				Status            string `json:"status"`
				Title             struct{ Romaji, English string }
				NextAiringEpisode *struct {
					AiringAt int64 `json:"airingAt"`
					Episode  int   `json:"episode"`
				} `json:"nextAiringEpisode"`
			} `json:"Media"`
		} `json:"data"`
	}
	// AniList answers 404 when nothing matches.
	if err := anilistPost("", followShowQuery, vars, &r); err != nil && !strings.Contains(err.Error(), "404") {
		return followLookup{}, false, err
	}
	m := r.Data.Media
	if m == nil {
		return followLookup{}, false, nil
	}
	l := followLookup{Show: followedShow{ID: m.ID, Title: m.Title.English, URL: m.SiteURL}, Status: m.Status}
	if l.Show.Title == "" {
		l.Show.Title = m.Title.Romaji
	}
	if n := m.NextAiringEpisode; n != nil {
		l.NextAt, l.NextEp = n.AiringAt, n.Episode
	}
	return l, true, nil
}

// addFollow adds a show to a member's list. It reports false when the list
// is full; following a show twice is fine.
func addFollow(d *storeData, user string, show followedShow) bool {
	if d.Follows == nil {
		d.Follows = map[string][]followedShow{}
	}
	for _, f := range d.Follows[user] {
		if f.ID == show.ID {
			return true
		}
	}
	if len(d.Follows[user]) >= maxFollows {
		return false
	}
	d.Follows[user] = append(d.Follows[user], show)
	return true
}

// removeFollow drops a show by its id and returns it.
func removeFollow(d *storeData, user string, id int) (followedShow, bool) {
	list := d.Follows[user]
	for k, f := range list {
		if f.ID == id {
			list = append(list[:k:k], list[k+1:]...)
			if len(list) == 0 {
				delete(d.Follows, user)
			} else {
				d.Follows[user] = list
			}
			return f, true
		}
	}
	return followedShow{}, false
}

// followedIDs lists every show someone follows, once.
func followedIDs(follows map[string][]followedShow) []int {
	seen := map[int]bool{}
	var ids []int
	for _, list := range follows {
		for _, f := range list {
			if !seen[f.ID] {
				seen[f.ID] = true
				ids = append(ids, f.ID)
			}
		}
	}
	sort.Ints(ids)
	return ids
}

func followAckEmbed(l followLookup) *discordgo.MessageEmbed {
	desc := fmt.Sprintf("Following [%s](%s). I'll DM you when a new episode airs.", l.Show.Title, l.Show.URL)
	switch {
	case l.NextAt > 0:
		desc += fmt.Sprintf("\nNext: episode %d <t:%d:R>.", l.NextEp, l.NextAt)
	case l.Status == "FINISHED" || l.Status == "CANCELLED":
		desc += "\nThis show has finished airing, so there may be nothing new to tell you about."
	default:
		desc += "\nNo episode date is announced yet."
	}
	return &discordgo.MessageEmbed{Description: desc, Color: shu,
		Footer: &discordgo.MessageEmbedFooter{Text: "Keep DMs from server members on to get them · /unfollow to stop"}}
}

func followingEmbed(list []followedShow) *discordgo.MessageEmbed {
	if len(list) == 0 {
		return &discordgo.MessageEmbed{Description: "You don't follow any shows. Use `/follow` to get a DM when a new episode airs.", Color: shu}
	}
	var lines []string
	for _, f := range list {
		lines = append(lines, fmt.Sprintf("[%s](%s)", clip(f.Title, 80), f.URL))
	}
	return &discordgo.MessageEmbed{Title: "Shows you follow", Description: clip(strings.Join(lines, "\n"), 4000), Color: shu,
		Footer: &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("%d of %d · /unfollow to stop one", len(list), maxFollows)}}
}

// episodeCard is one aired episode in a DM: a Shu card with the show's
// cover, then AniList and MyAnimeList links and a button to stop DMs for it.
func episodeCard(a followAiring) discordgo.MessageComponent {
	color := shu
	label, line := "NEW EPISODE", fmt.Sprintf("Episode %d is out.", a.Episode)
	if a.Episodes > 0 {
		line = fmt.Sprintf("Episode %d of %d is out.", a.Episode, a.Episodes)
	}
	switch {
	case a.Episodes > 0 && a.Episode == a.Episodes:
		label, line = "FINALE", fmt.Sprintf("The finale, episode %d, is out.", a.Episode)
	case a.Episode == 1:
		label, line = "FIRST EPISODE", "Episode 1 is out."
	}
	meta := fmt.Sprintf("-# Aired <t:%d:t>", a.At)
	if a.NextAt > 0 {
		meta += fmt.Sprintf(" · episode %d <t:%d:R>", a.NextEp, a.NextAt)
	}
	content := fmt.Sprintf("-# **%s**\n### %s\n%s\n%s\n-# Watch it with `otakase`", label, mdEscape(clip(a.Show.Title, 120)), line, meta)
	var top discordgo.MessageComponent = discordgo.TextDisplay{Content: content}
	if a.Cover != "" {
		top = discordgo.Section{Components: []discordgo.MessageComponent{discordgo.TextDisplay{Content: content}},
			Accessory: discordgo.Thumbnail{Media: discordgo.UnfurledMediaItem{URL: a.Cover}}}
	}
	anilist := a.Show.URL
	if anilist == "" {
		anilist = fmt.Sprintf("https://anilist.co/anime/%d", a.MediaID)
	}
	buttons := []discordgo.MessageComponent{discordgo.Button{Label: "AniList", Style: discordgo.LinkButton, URL: anilist}}
	if a.MalID > 0 {
		buttons = append(buttons, discordgo.Button{Label: "MyAnimeList", Style: discordgo.LinkButton, URL: fmt.Sprintf("https://myanimelist.net/anime/%d", a.MalID)})
	}
	buttons = append(buttons, discordgo.Button{Label: "Stop DMs for this show", Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("follow-stop:%d", a.MediaID)})
	buttons = withPlayButton(buttons, a.MediaID, a.Episode)
	return discordgo.Container{AccentColor: &color, Components: []discordgo.MessageComponent{top, discordgo.ActionsRow{Components: buttons}}}
}

// episodeCardsPerMessage keeps a DM under Discord's 40 components: a card
// is at most 9.
const episodeCardsPerMessage = 4

func (b *bot) follow(i *discordgo.InteractionCreate, q string) {
	user := i.Member.User.ID
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		l, ok, err := findFollowShow(q)
		if err != nil {
			log.Printf("follow lookup: %v", err)
			return errorEmbed("Couldn't look up that show on AniList. Try again."), nil
		}
		if !ok {
			return errorEmbed("No show found. Pick one from the suggestions."), nil
		}
		added := false
		b.store.update(func(d *storeData) { added = addFollow(d, user, l.Show) })
		if !added {
			return errorEmbed("You follow %d shows already. `/unfollow` one first.", maxFollows), nil
		}
		return followAckEmbed(l), nil
	})
}

func (b *bot) unfollow(i *discordgo.InteractionCreate, q string) {
	user := i.Member.User.ID
	var show followedShow
	found := false
	if m := pickedShowRe.FindStringSubmatch(q); m != nil {
		id, _ := strconv.Atoi(m[1])
		b.store.update(func(d *storeData) { show, found = removeFollow(d, user, id) })
	} else {
		// Typed text: match a followed title.
		b.store.update(func(d *storeData) {
			for _, f := range d.Follows[user] {
				if strings.EqualFold(f.Title, strings.TrimSpace(q)) {
					show, found = removeFollow(d, user, f.ID)
					return
				}
			}
		})
	}
	if !found {
		b.respond(i, true, errorEmbed("You don't follow that show. `/following` lists the ones you do."))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Description: fmt.Sprintf("Stopped following [%s](%s).", show.Title, show.URL), Color: shu})
}

// stopFollowButton is the Stop DMs button under an episode DM. It works in
// the DM, where the interaction has a user but no member.
func (b *bot) stopFollowButton(i *discordgo.InteractionCreate, id string) {
	user := i.User
	if i.Member != nil {
		user = i.Member.User
	}
	n, _ := strconv.Atoi(id)
	var show followedShow
	found := false
	b.store.update(func(d *storeData) { show, found = removeFollow(d, user.ID, n) })
	if !found {
		b.respond(i, true, &discordgo.MessageEmbed{Description: "You don't get DMs for this show anymore.", Color: shu})
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Description: fmt.Sprintf("Stopped DMs for %s. `/follow` it again any time.", mdEscape(show.Title)), Color: shu})
}

func (b *bot) following(i *discordgo.InteractionCreate) {
	var list []followedShow
	b.store.view(func(d *storeData) { list = append(list, d.Follows[i.Member.User.ID]...) })
	b.respond(i, true, followingEmbed(list))
}

// followedChoices suggests the shows a member follows that match q.
func followedChoices(list []followedShow, q string) []*discordgo.ApplicationCommandOptionChoice {
	q = strings.ToLower(strings.TrimSpace(q))
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	for _, f := range list {
		if q == "" || strings.Contains(strings.ToLower(f.Title), q) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: clip(f.Title, 100), Value: fmt.Sprintf("anilist:%d", f.ID)})
		}
		if len(choices) == 25 {
			break
		}
	}
	return choices
}

func (b *bot) suggestFollowed(i *discordgo.InteractionCreate) {
	var q string
	for _, o := range i.ApplicationCommandData().Options {
		if o.Focused {
			q = o.StringValue()
		}
	}
	var list []followedShow
	b.store.view(func(d *storeData) { list = append(list, d.Follows[i.Member.User.ID]...) })
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: followedChoices(list, q)},
	}); err != nil {
		log.Printf("followed suggestions: %v", err)
	}
}

const followAiringQuery = `query ($ids: [Int], $from: Int, $to: Int, $page: Int) {
  Page(page: $page, perPage: 50) {
    pageInfo { hasNextPage }
    airingSchedules(mediaId_in: $ids, airingAt_greater: $from, airingAt_lesser: $to, sort: TIME) {
      airingAt episode mediaId
      media { episodes idMal coverImage { large } nextAiringEpisode { airingAt episode } }
    }
  }
}`

type followAiring struct {
	MediaID  int
	At       int64
	Episode  int
	Episodes int
	MalID    int
	Cover    string
	NextAt   int64 // the episode after this one, 0 when none is announced
	NextEp   int
	Show     followedShow
}

// fetchFollowAiring lists episodes of the given shows that aired after from
// and up to to.
func fetchFollowAiring(ids []int, from, to time.Time) ([]followAiring, error) {
	var out []followAiring
	for start := 0; start < len(ids); start += 50 {
		chunk := ids[start:min(start+50, len(ids))]
		for page := 1; page <= 4; page++ {
			var r struct {
				Data struct {
					Page struct {
						PageInfo struct {
							HasNextPage bool `json:"hasNextPage"`
						} `json:"pageInfo"`
						AiringSchedules []struct {
							AiringAt int64 `json:"airingAt"`
							Episode  int   `json:"episode"`
							MediaID  int   `json:"mediaId"`
							Media    struct {
								Episodes   *int `json:"episodes"`
								IDMal      int  `json:"idMal"`
								CoverImage struct {
									Large string `json:"large"`
								} `json:"coverImage"`
								NextAiringEpisode *struct {
									AiringAt int64 `json:"airingAt"`
									Episode  int   `json:"episode"`
								} `json:"nextAiringEpisode"`
							} `json:"media"`
						} `json:"airingSchedules"`
					} `json:"Page"`
				} `json:"data"`
			}
			// airingAt_lesser is exclusive, so +1 keeps an episode airing
			// exactly at "to" in this window rather than the next.
			if err := anilistPost("", followAiringQuery, map[string]any{"ids": chunk, "from": from.Unix(), "to": to.Unix() + 1, "page": page}, &r); err != nil {
				return nil, err
			}
			for _, a := range r.Data.Page.AiringSchedules {
				eps := 0
				if a.Media.Episodes != nil {
					eps = *a.Media.Episodes
				}
				f := followAiring{MediaID: a.MediaID, At: a.AiringAt, Episode: a.Episode, Episodes: eps, MalID: a.Media.IDMal, Cover: a.Media.CoverImage.Large}
				if n := a.Media.NextAiringEpisode; n != nil && n.Episode > a.Episode {
					f.NextAt, f.NextEp = n.AiringAt, n.Episode
				}
				out = append(out, f)
			}
			if !r.Data.Page.PageInfo.HasNextPage {
				break
			}
		}
	}
	return out, nil
}

// episodeDMs pairs aired episodes with the members who follow each show.
func episodeDMs(follows map[string][]followedShow, aired []followAiring) map[string][]followAiring {
	dms := map[string][]followAiring{}
	for user, list := range follows {
		for _, f := range list {
			for _, a := range aired {
				if a.MediaID == f.ID {
					a.Show = f
					dms[user] = append(dms[user], a)
				}
			}
		}
	}
	for _, list := range dms {
		sort.Slice(list, func(i, j int) bool { return list[i].At < list[j].At })
	}
	return dms
}

// checkFollows sends DMs for episodes that aired since the last check. The
// check time is saved, so a restart neither repeats nor skips episodes
// (anything older than a day is dropped rather than sent late).
func (b *bot) checkFollows(now time.Time) {
	var follows map[string][]followedShow
	var last time.Time
	b.store.view(func(d *storeData) {
		last = d.FollowsChecked
		follows = map[string][]followedShow{}
		for u, l := range d.Follows {
			follows[u] = append([]followedShow(nil), l...)
		}
	})
	if last.IsZero() || now.Sub(last) > 24*time.Hour {
		last = now.Add(-10 * time.Minute)
	}
	ids := followedIDs(follows)
	if len(ids) > 0 {
		aired, err := fetchFollowAiring(ids, last, now)
		if err != nil {
			b.jobFailed("Follow DMs", err)
			return // try the same window again next time
		}
		b.jobOK("Follow DMs")
		for user, eps := range episodeDMs(follows, aired) {
			b.sendAiringToApps(user, eps)
			b.sendEpisodeDMs(user, eps)
		}
	}
	b.store.update(func(d *storeData) { d.FollowsChecked = now })
}

func (b *bot) sendEpisodeDMs(user string, eps []followAiring) {
	ch, err := b.s.UserChannelCreate(user)
	if err != nil {
		log.Printf("follow DM %s: %v", user, err)
		return
	}
	for k := 0; k < len(eps); k += episodeCardsPerMessage {
		var cards []discordgo.MessageComponent
		for _, a := range eps[k:min(k+episodeCardsPerMessage, len(eps))] {
			cards = append(cards, episodeCard(a))
		}
		if _, err := b.s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{Flags: discordgo.MessageFlagsIsComponentsV2, Components: cards}); err != nil {
			log.Printf("follow DM %s: %v", user, err) // usually DMs turned off
			return
		}
	}
}

func (b *bot) followLoop() {
	for {
		b.checkFollows(time.Now().UTC())
		time.Sleep(5 * time.Minute)
	}
}
