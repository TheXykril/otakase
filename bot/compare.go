package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// /compare member: a picture in the /profile card's style with both
// avatars, how many shows they have in common, how much their scores agree,
// the three shows both rated highest and up to three the other member loved
// that the caller hasn't seen.

// comparison is what /compare found.
type comparison struct {
	Common    int          // shows both watched (not just planned)
	Agreement int          // 0-100, -1 when no show was scored by both
	Shared    []sharedPick // both rated highest
	Loved     []listEntry  // theirs, not on your list
}

type sharedPick struct {
	Media     mediaBrief
	You, Them float64
}

// lovedScore is the least score (out of 100) that counts as loved.
const lovedScore = 70

// compareLists compares your list with theirs.
func compareLists(you, them []listEntry) comparison {
	mine := map[int]listEntry{}
	for _, e := range you {
		mine[e.MediaID] = e
	}
	c := comparison{Agreement: -1}
	var diff float64
	scored := 0
	for _, t := range them {
		y, ok := mine[t.MediaID]
		if !ok {
			if t.Score >= lovedScore && !t.Media.IsAdult && t.seen() {
				c.Loved = append(c.Loved, t)
			}
			continue
		}
		if !y.seen() || !t.seen() {
			continue
		}
		c.Common++
		if y.Score > 0 && t.Score > 0 {
			diff += math.Abs(y.Score - t.Score)
			scored++
			if !t.Media.IsAdult {
				c.Shared = append(c.Shared, sharedPick{Media: t.Media, You: y.Score, Them: t.Score})
			}
		}
	}
	if scored > 0 {
		c.Agreement = int(math.Round(100 - diff/float64(scored)))
	}
	sort.SliceStable(c.Shared, func(i, j int) bool {
		a, b := c.Shared[i], c.Shared[j]
		if lo, hi := math.Min(a.You, a.Them), math.Min(b.You, b.Them); lo != hi {
			return lo > hi
		}
		if a.You+a.Them != b.You+b.Them {
			return a.You+a.Them > b.You+b.Them
		}
		return a.Media.Popularity > b.Media.Popularity
	})
	sort.SliceStable(c.Loved, func(i, j int) bool {
		if c.Loved[i].Score != c.Loved[j].Score {
			return c.Loved[i].Score > c.Loved[j].Score
		}
		return c.Loved[i].Media.Popularity > c.Loved[j].Media.Popularity
	})
	c.Shared = c.Shared[:min(len(c.Shared), 3)]
	c.Loved = c.Loved[:min(len(c.Loved), 3)]
	return c
}

// tenScore is a 100 point score out of 10: "9", "8.5".
func tenScore(s float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", s/10), ".0")
}

// compareUser is the part of an AniList user the card shows.
type compareUser struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	SiteURL     string `json:"siteUrl"`
	BannerImage string `json:"bannerImage"`
	Avatar      struct {
		Large string `json:"large"`
	} `json:"avatar"`
}

func compareUserInfo(id int) (compareUser, error) {
	var r struct {
		Data struct{ User *compareUser } `json:"data"`
	}
	if err := anilistSocialPost("", `query ($id: Int) { User(id: $id) { id name siteUrl bannerImage avatar { large } } }`, map[string]any{"id": id}, &r); err != nil {
		return compareUser{}, err
	}
	if r.Data.User == nil {
		return compareUser{}, fmt.Errorf("no user")
	}
	return *r.Data.User, nil
}

// compare is /compare.
func (b *bot) compare(i *discordgo.InteractionCreate) {
	me := i.Member.User
	var them *discordgo.User
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "member" {
			them = o.UserValue(b.s)
		}
	}
	if them == nil {
		return
	}
	if them.ID == me.ID {
		b.respond(i, true, errorEmbed("Pick someone else to compare with."))
		return
	}
	var myLink, theirLink anilistLink
	var mineOK, theirsOK bool
	b.store.view(func(d *storeData) {
		myLink, mineOK = d.AniList[me.ID]
		theirLink, theirsOK = d.AniList[them.ID]
	})
	switch {
	case !mineOK:
		b.respond(i, true, errorEmbed("Link your AniList first with `/link-anilist`."))
		return
	case !theirsOK:
		b.respond(i, true, errorEmbed("<@%s> hasn't linked AniList, so there's nothing to compare.", them.ID))
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	fail := func(e *discordgo.MessageEmbed) {
		if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e}}); err != nil {
			log.Printf("edit: %v", err)
		}
	}
	mine, err := anilistLists.get(myLink.ID)
	if err == nil {
		var theirs []listEntry
		theirs, err = anilistLists.get(theirLink.ID)
		if errors.Is(err, errPrivateList) {
			fail(errorEmbed("<@%s>'s AniList list is private, so there's nothing to compare.", them.ID))
			return
		}
		if err == nil {
			b.sendComparison(i, me.ID, them.ID, myLink, theirLink, compareLists(mine, theirs))
			return
		}
	} else if errors.Is(err, errPrivateList) {
		fail(errorEmbed("Your AniList list is private, so there's nothing to compare. Make it public in AniList's settings."))
		return
	}
	log.Printf("compare: %v", err)
	fail(errorEmbed("Couldn't read AniList right now. Try again in a minute."))
}

func (b *bot) sendComparison(i *discordgo.InteractionCreate, meID, themID string, myLink, theirLink anilistLink, c comparison) {
	you, err1 := compareUserInfo(myLink.ID)
	they, err2 := compareUserInfo(theirLink.ID)
	if err1 != nil || err2 != nil {
		log.Printf("compare users: %v %v", err1, err2)
		you.Name, they.Name = myLink.Name, theirLink.Name
	}
	if they.SiteURL == "" {
		they.SiteURL = "https://anilist.co/user/" + theirLink.Name
	}
	buttons := linkButtons(they.Name+"'s AniList", they.SiteURL, "Their anime list", strings.TrimSuffix(they.SiteURL, "/")+"/animelist")
	summary := compareSummary(meID, themID, c)
	png, err := renderCompareCard(compareCardFor(you, they, c))
	if err != nil {
		log.Printf("compare card: %v", err)
		e := &discordgo.MessageEmbed{Title: you.Name + " and " + they.Name, Description: summary, Color: shu}
		b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e}, Components: &buttons})
		return
	}
	color := shu
	box := []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: []discordgo.MessageComponent{
		discordgo.MediaGallery{Items: []discordgo.MediaGalleryItem{{Media: discordgo.UnfurledMediaItem{URL: "attachment://compare.png"}}}},
		discordgo.TextDisplay{Content: "-# " + summary},
		buttons[0],
	}}}
	file := &discordgo.File{Name: "compare.png", ContentType: "image/png", Reader: bytes.NewReader(png)}
	err = b.editComponentsV2(i.Interaction, &discordgo.WebhookEdit{Components: &box, Files: []*discordgo.File{file}, AllowedMentions: &discordgo.MessageAllowedMentions{}})
	if err == nil {
		return
	}
	log.Printf("compare card message: %v", err)
	e := &discordgo.MessageEmbed{Description: summary, Color: shu, Image: &discordgo.MessageEmbedImage{URL: "attachment://compare.png"}}
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e}, Components: &buttons, Files: []*discordgo.File{file}}); err != nil {
		log.Printf("edit: %v", err)
	}
}

// compareSummary is one line under the picture.
func compareSummary(meID, themID string, c comparison) string {
	line := fmt.Sprintf("<@%s> and <@%s> have %s in common", meID, themID, plural(c.Common, "show"))
	if c.Agreement >= 0 {
		line += fmt.Sprintf(" and agree on scores %d%%", c.Agreement)
	}
	return line + "."
}

// compareCardFor downloads the card's images.
func compareCardFor(you, they compareUser, c comparison) compareCard {
	card := compareCard{You: you.Name, Them: they.Name, Common: c.Common, Agreement: c.Agreement}
	urls := []string{you.Avatar.Large, they.Avatar.Large, you.BannerImage}
	for _, s := range c.Shared {
		card.Shared = append(card.Shared, tileShow{Title: s.Media.name(), Chip: tenScore(s.You) + " · " + tenScore(s.Them)})
		urls = append(urls, s.Media.cover())
	}
	for _, e := range c.Loved {
		card.Loved = append(card.Loved, tileShow{Title: e.Media.name(), Chip: tenScore(e.Score) + " / 10"})
		urls = append(urls, e.Media.cover())
	}
	imgs := fetchImages(urls)
	card.YouAvatar, card.ThemAvatar, card.Banner = imgs[0], imgs[1], imgs[2]
	k := 3
	for n := range card.Shared {
		card.Shared[n].Cover = imgs[k]
		k++
	}
	for n := range card.Loved {
		card.Loved[n].Cover = imgs[k]
		k++
	}
	switch {
	case len(card.Shared) > 0:
		card.Hero = card.Shared[0].Cover
	case len(card.Loved) > 0:
		card.Hero = card.Loved[0].Cover
	}
	return card
}
