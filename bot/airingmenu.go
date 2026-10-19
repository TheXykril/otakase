package main

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// The airing board's menu and button. Picking a show answers only the
// member who picked it with the show's card: cover, score, genres, a short
// synopsis, when it airs, and buttons to get a DM when it airs, start a
// watch party, open it on AniList and MyAnimeList, or play it in Otakase. "My shows today"
// lists the episodes airing today from the member's linked AniList.

const airingShowQuery = `query ($id: Int) {
  Media(id: $id) {
    id idMal siteUrl isAdult averageScore genres
    description(asHtml: false)
    coverImage { extraLarge }
    title { romaji english }
  }
}`

type airingShowInfo struct {
	ID           int      `json:"id"`
	IDMal        int      `json:"idMal"`
	SiteURL      string   `json:"siteUrl"`
	IsAdult      bool     `json:"isAdult"`
	AverageScore int      `json:"averageScore"`
	Genres       []string `json:"genres"`
	Description  string   `json:"description"`
	CoverImage   struct {
		ExtraLarge string `json:"extraLarge"`
	} `json:"coverImage"`
	Title struct{ Romaji, English string }
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// synopsis turns AniList's description into a few plain lines.
func synopsis(s string, n int) string {
	s = strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n").Replace(s)
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.NewReplacer("&quot;", `"`, "&amp;", "&", "&#039;", "'", "&lt;", "<", "&gt;", ">").Replace(s)
	var paras []string
	for _, p := range strings.Split(s, "\n") {
		if p = strings.TrimSpace(p); p != "" {
			paras = append(paras, p)
		}
	}
	return clip(strings.Join(paras, "\n"), n)
}

// todaysAiring finds a show on today's board data.
func (b *bot) todaysAiring(id int) (airing, bool) {
	var found airing
	ok := false
	b.store.view(func(d *storeData) {
		if d.Airing == nil {
			return
		}
		for _, a := range d.Airing.All {
			if a.ID == id {
				found, ok = a, true
				return
			}
		}
	})
	return found, ok
}

// airingCard is a show's card from the menu.
func airingCard(info airingShowInfo, a airing, today bool) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	title := info.Title.English
	if title == "" {
		title = info.Title.Romaji
	}
	var lines []string
	if today {
		lines = append(lines, fmt.Sprintf("Episode %d · <t:%d:t> (<t:%d:R>)", a.Episode, a.At, a.At))
	}
	var meta []string
	if info.AverageScore > 0 {
		meta = append(meta, fmt.Sprintf("**%d%%**", info.AverageScore))
	}
	if len(info.Genres) > 0 {
		g := info.Genres
		if len(g) > 4 {
			g = g[:4]
		}
		meta = append(meta, strings.Join(g, ", "))
	}
	if len(meta) > 0 {
		lines = append(lines, strings.Join(meta, " · "))
	}
	if s := synopsis(info.Description, 400); s != "" {
		lines = append(lines, "", s)
	}
	e := &discordgo.MessageEmbed{Title: clip(title, 256), URL: info.SiteURL, Description: strings.Join(lines, "\n"), Color: shu}
	if info.CoverImage.ExtraLarge != "" {
		e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: info.CoverImage.ExtraLarge}
	}
	id := strconv.Itoa(info.ID)
	row := discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "DM me when it airs", Style: discordgo.PrimaryButton, CustomID: "airing-follow:" + id},
		discordgo.Button{Label: "Watch party", Style: discordgo.SecondaryButton, CustomID: "airing-party:" + id},
		discordgo.Button{Label: "AniList", Style: discordgo.LinkButton, URL: info.SiteURL},
	}}
	if info.IDMal > 0 {
		row.Components = append(row.Components, discordgo.Button{Label: "MyAnimeList", Style: discordgo.LinkButton, URL: fmt.Sprintf("https://myanimelist.net/anime/%d", info.IDMal)})
	}
	if play := playButton(info.ID, 0); play != nil {
		row.Components = append(row.Components, play)
	}
	return e, withQueueButton([]discordgo.MessageComponent{row}, info.ID)
}

// pickAiring answers a pick from the board's menu.
func (b *bot) pickAiring(i *discordgo.InteractionCreate) {
	vals := i.MessageComponentData().Values
	if len(vals) == 0 {
		return
	}
	id, _ := strconv.Atoi(vals[0])
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		var r struct {
			Data struct {
				Media *airingShowInfo `json:"Media"`
			} `json:"data"`
		}
		if err := anilistPost("", airingShowQuery, map[string]any{"id": id}, &r); err != nil || r.Data.Media == nil || r.Data.Media.IsAdult {
			if err != nil {
				log.Printf("airing card: %v", err)
			}
			return errorEmbed("Couldn't load that show from AniList. Try again."), nil
		}
		a, ok := b.todaysAiring(id)
		return airingCard(*r.Data.Media, a, ok)
	})
}

// followAiring follows a show from its card.
func (b *bot) followAiring(i *discordgo.InteractionCreate, id string) {
	user := i.Member.User.ID
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		l, ok, err := findFollowShow("anilist:" + id)
		if err != nil || !ok {
			if err != nil {
				log.Printf("follow lookup: %v", err)
			}
			return errorEmbed("Couldn't look up that show on AniList. Try again."), nil
		}
		added := false
		b.store.update(func(d *storeData) { added = addFollow(d, user, l.Show) })
		if !added {
			return errorEmbed("You follow %d shows already. `/unfollow` one first.", maxFollows), nil
		}
		return followAckEmbed(l), nil
	})
}

// partyAiring starts a watch party for a show from its card, in 15
// minutes with everyone talking. Only the host sees the post; others find
// the event and its voice channel in the server.
func (b *bot) partyAiring(i *discordgo.InteractionCreate, id string) {
	if ev := b.hostedParty(i.Member.User.ID); ev != nil {
		b.respond(i, true, errorEmbed("You already have a watch party planned: %s\nCancel it first with the **Cancel** button on its post.", eventLink(ev)))
		return
	}
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		return b.planParty(i.Member, "anilist:"+id, 15, 0, partyTalk, false)
	})
	b.announceParty(i.Member.User.ID, i.ChannelID, true)
}

// myAiring lists today's episodes from the member's AniList (watching,
// planning, paused or rewatching).
func (b *bot) myAiring(i *discordgo.InteractionCreate) {
	var link anilistLink
	var linked bool
	var all []airing
	b.store.view(func(d *storeData) {
		link, linked = d.AniList[i.Member.User.ID]
		if d.Airing != nil {
			all = d.Airing.All
		}
	})
	if !linked {
		b.respond(i, true, errorEmbed("Link your AniList first with `/link-anilist`, then this shows what airs today from your list."))
		return
	}
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		ids, err := anilistListIDs(link.ID)
		if err != nil {
			log.Printf("my shows today: %v", err)
			return errorEmbed("Couldn't read your AniList list. It may be private."), nil
		}
		return myAiringEmbed(all, ids), nil
	})
}

func myAiringEmbed(all []airing, ids map[int]bool) *discordgo.MessageEmbed {
	var mine []airing
	for _, a := range all {
		if ids[a.ID] {
			mine = append(mine, a)
		}
	}
	if len(mine) == 0 {
		return &discordgo.MessageEmbed{Description: "Nothing from your AniList airs today.", Color: shu}
	}
	sort.SliceStable(mine, func(i, j int) bool { return mine[i].At < mine[j].At })
	var lines []string
	for _, a := range mine {
		lines = append(lines, fmt.Sprintf("<t:%d:t> [%s](%s) *%s* · <t:%d:R>", a.At, mdEscape(clip(a.Title, 60)), a.URL, epLabel(a), a.At))
	}
	return &discordgo.MessageEmbed{Title: "Your shows today", Description: clip(strings.Join(lines, "\n"), 4000), Color: shu,
		Footer: &discordgo.MessageEmbedFooter{Text: "From your AniList: watching, planning, paused and rewatching"}}
}

// anilistListIDs is every show on a member's list they watch, plan,
// paused or rewatch.
func anilistListIDs(userID int) (map[int]bool, error) {
	var r struct {
		Data struct {
			MediaListCollection struct {
				Lists []struct {
					Entries []struct {
						MediaID int `json:"mediaId"`
					} `json:"entries"`
				} `json:"lists"`
			} `json:"MediaListCollection"`
		} `json:"data"`
	}
	err := anilistPost("", `query ($id: Int) {
  MediaListCollection(userId: $id, type: ANIME, status_in: [CURRENT, PLANNING, PAUSED, REPEATING]) {
    lists { entries { mediaId } }
  }
}`, map[string]any{"id": userID}, &r)
	if err != nil {
		return nil, err
	}
	ids := map[int]bool{}
	for _, l := range r.Data.MediaListCollection.Lists {
		for _, e := range l.Entries {
			ids[e.MediaID] = true
		}
	}
	return ids, nil
}
