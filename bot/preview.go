package main

import (
	"bytes"
	"log"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /preview shows staff what the bot sends, only to them: nothing is saved
// and nothing is posted anywhere else. Buttons and menus that would act
// (follow a show, mark solved) come out disabled; links still work.

func previewCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{Name: "preview", Description: "See a bot message only you can see, without posting it", DefaultMemberPermissions: &permMod,
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "what", Description: "Which message", Required: true, Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Today's airing board", Value: "airing"},
				{Name: "Welcome DM", Value: "welcome"},
				{Name: "New episode DM", Value: "episode"},
				{Name: "Support post greeting", Value: "support"},
				{Name: "Release card (/latest)", Value: "release"},
			}},
		}}
}

// preview is a message to show the caller.
type preview struct {
	note       string // above it, in the first reply
	embeds     []*discordgo.MessageEmbed
	components []discordgo.MessageComponent
	files      []*discordgo.File
	v2         bool
}

func (b *bot) previewCmd(i *discordgo.InteractionCreate) {
	if !b.staffOnly(i) {
		return
	}
	what := ""
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "what" {
			what = o.StringValue()
		}
	}
	// Building some of these takes longer than Discord's three seconds.
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); err != nil {
		log.Printf("preview: %v", err)
		return
	}
	p := b.buildPreview(what, time.Now().UTC())
	note := p.note + "\n-# Only you can see this. Nothing was posted or saved."
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &note}); err != nil {
		log.Printf("preview: %v", err)
	}
	flags := discordgo.MessageFlagsEphemeral
	if p.v2 {
		flags |= discordgo.MessageFlagsIsComponentsV2
	}
	comps := p.components
	if comps == nil {
		comps = []discordgo.MessageComponent{}
	}
	if _, err := b.s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Flags: flags, Embeds: p.embeds, Components: comps, Files: p.files,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Printf("preview %s: %v", what, err)
		msg := "Couldn't show the preview: " + clip(err.Error(), 300)
		_, _ = b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg})
	}
}

func (b *bot) buildPreview(what string, now time.Time) preview {
	switch what {
	case "airing":
		return b.airingPreview(now)
	case "welcome":
		m := b.welcomeDM()
		return preview{note: "The DM new members get" + b.onOff("welcome_dm") + ":", components: m.Components, files: m.Files, v2: true}
	case "episode":
		a := sampleEpisode(b.recentAiring(now), now)
		return preview{note: "A new episode DM for a sample show:", components: disableActions([]discordgo.MessageComponent{episodeCard(a)}), v2: true}
	case "support":
		embeds := []*discordgo.MessageEmbed{{Description: b.text("support_checklist"), Color: shu}}
		note := "The greeting on a new support post. For a sample post about mpv not starting it adds these wiki links:"
		if e := b.wikiEmbed(sampleSupportTitle, sampleSupportQuestion); e != nil {
			embeds = append(embeds, e)
		} else {
			note = "The greeting on a new support post (the wiki isn't loaded yet, so no wiki links):"
		}
		return preview{note: note, embeds: embeds, components: disableActions(supportButtons())}
	case "release":
		e, comps := latestRelease(b.cfg.GitHubToken)
		return preview{note: "The release card `/latest` shows:", embeds: []*discordgo.MessageEmbed{e}, components: comps}
	}
	return preview{note: "Pick what to preview.", embeds: []*discordgo.MessageEmbed{errorEmbed("Unknown preview.")}}
}

const (
	sampleSupportTitle    = "mpv doesn't start"
	sampleSupportQuestion = "When I pick an episode nothing happens and it says mpv not found."
)

// onOff is " (now on)" or " (now off)" for a bool setting.
func (b *bot) onOff(name string) string {
	if b.settingBool(name) {
		return " (now on)"
	}
	return " (now off)"
}

// airingPreview builds the board for the current airing day, from the
// saved board when it is today's, else fresh from AniList.
func (b *bot) airingPreview(now time.Time) preview {
	hour := b.settingInt("airing_hour_utc")
	day := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC)
	if now.Before(day) {
		day = day.Add(-24 * time.Hour)
	}
	var all []airing
	b.store.view(func(d *storeData) {
		if d.Airing != nil && d.Airing.Day.Equal(day) {
			all = append(all, d.Airing.All...)
		}
	})
	if all == nil {
		var err error
		if all, err = fetchAiring(day, day.Add(24*time.Hour)); err != nil {
			return preview{note: "Today's airing board:", embeds: []*discordgo.MessageEmbed{errorEmbed("Couldn't fetch the schedule from AniList: %v", err)}}
		}
	}
	shows := boardShows(all)
	p := preview{note: "Today's airing board" + b.onOff("daily_airing") + ", as it looks right now:", v2: true}
	picture := ""
	if png, err := boardPicture(day, all, shows); err != nil {
		log.Printf("preview picture: %v", err)
	} else {
		p.files = []*discordgo.File{{Name: "airing.png", ContentType: "image/png", Reader: bytes.NewReader(png)}}
		picture = "attachment://airing.png"
	}
	p.components = disableActions(boardMessage(shows, now, picture, b.boardMarks()))
	return p
}

// recentAiring lists shows that aired in the last day, for a sample.
func (b *bot) recentAiring(now time.Time) []airing {
	list, err := fetchAiring(now.Add(-24*time.Hour), now)
	if err != nil {
		log.Printf("preview sample: %v", err)
	}
	return list
}

// sampleEpisode picks the most popular recent episode, or a fixed sample
// when AniList had nothing.
func sampleEpisode(recent []airing, now time.Time) followAiring {
	if len(recent) == 0 {
		return followAiring{MediaID: 154587, At: now.Add(-time.Hour).Unix(), Episode: 5, Episodes: 28, MalID: 52991,
			Show: followedShow{ID: 154587, Title: "Frieren: Beyond Journey's End", URL: "https://anilist.co/anime/154587"}}
	}
	list := append([]airing(nil), recent...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Popularity > list[j].Popularity })
	a := list[0]
	return followAiring{MediaID: a.ID, At: a.At, Episode: a.Episode, Episodes: a.Episodes, Cover: a.Cover,
		Show: followedShow{ID: a.ID, Title: a.Title, URL: a.URL}}
}

// disableActions returns the components with every button and menu that
// would do something disabled. Link buttons keep working.
func disableActions(comps []discordgo.MessageComponent) []discordgo.MessageComponent {
	out := make([]discordgo.MessageComponent, 0, len(comps))
	for _, c := range comps {
		out = append(out, disableAction(c))
	}
	return out
}

func disableAction(c discordgo.MessageComponent) discordgo.MessageComponent {
	switch v := c.(type) {
	case discordgo.Button:
		if v.Style != discordgo.LinkButton {
			v.Disabled = true
		}
		return v
	case discordgo.SelectMenu:
		v.Disabled = true
		return v
	case discordgo.ActionsRow:
		v.Components = disableActions(v.Components)
		return v
	case discordgo.Container:
		v.Components = disableActions(v.Components)
		return v
	case discordgo.Section:
		if v.Accessory != nil {
			v.Accessory = disableAction(v.Accessory)
		}
		return v
	}
	return c
}
