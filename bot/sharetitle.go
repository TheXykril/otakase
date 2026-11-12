package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// /share-title: /share-anime for films and series. The suggestions come
// from IMDb's search. The DM links the IMDb
// page; a linked app gets the title under Community → From Discord and
// opens it from Trakt.

func init() {
	registerCommand(&discordgo.ApplicationCommand{Name: "share-title", Description: "Recommend a movie or series to another member, sent to their DMs and their Otakase", Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Who to recommend it to", Required: true},
		{Type: discordgo.ApplicationCommandOptionString, Name: "title", Description: "The movie or series (pick from the suggestions)", Required: true, Autocomplete: true},
		{Type: discordgo.ApplicationCommandOptionString, Name: "note", Description: "A few words on why they'd like it", MaxLength: shareNoteMax},
	}}, (*bot).shareTitle)
	registerAutocomplete("share-title", (*bot).suggestFilmTitles)
	registerHelp("`/share-title` recommend a movie or series to another member, in their DMs and their Otakase")
}

// pickedFilmRe is a suggestion's value: the IMDb id and whether it is a
// series.
var pickedFilmRe = regexp.MustCompile(`^(tt\d{5,10}):(movie|show)$`)

func filmChoice(s watchShow) *discordgo.ApplicationCommandOptionChoice {
	kind, label := "movie", "Movie"
	if s.series() {
		kind, label = "show", "Series"
	}
	name := s.Title
	if s.Year > 0 {
		name += fmt.Sprintf(" (%d)", s.Year)
	}
	return &discordgo.ApplicationCommandOptionChoice{Name: clip(name+" · "+label, 100), Value: watchTitleIMDb(s.ID) + ":" + kind}
}

func (b *bot) suggestFilmTitles(i *discordgo.InteractionCreate) {
	var q string
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "title" && o.Focused {
			q = strings.TrimSpace(o.StringValue())
		}
	}
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	if len(q) >= 2 && len(q) <= 100 && !pickedFilmRe.MatchString(q) {
		found, err := watchTitleSearch(q)
		if err != nil {
			log.Printf("film suggestions: %v", err)
		}
		for _, s := range found {
			if len(choices) == 25 {
				break
			}
			choices = append(choices, filmChoice(s))
		}
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	}); err != nil {
		log.Printf("film suggestions: %v", err)
	}
}

// shareTitleDM is the recommendation in the member's DMs.
func shareTitleDM(from string, show watchShow, note string) *discordgo.MessageSend {
	desc := fmt.Sprintf("**%s** recommends this to you.", mdEscape(from))
	if note != "" {
		desc += "\n> " + strings.ReplaceAll(quiet(note), "\n", "\n> ")
	}
	title := show.Title
	if show.Year > 0 {
		title += fmt.Sprintf(" (%d)", show.Year)
	}
	e := &discordgo.MessageEmbed{Title: clip(title, 200), URL: "https://www.imdb.com/title/" + watchTitleIMDb(show.ID) + "/", Color: shu, Description: desc,
		Footer: &discordgo.MessageEmbedFooter{Text: "Sent with /share-title in the Otakase Discord"}}
	if show.Cover != "" {
		e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: show.Cover}
	}
	return &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e}, AllowedMentions: &discordgo.MessageAllowedMentions{}}
}

// shareTitleItem is the recommendation in the member's app.
func shareTitleItem(from string, show watchShow, note string) inboxItem {
	kind := "movie"
	if show.series() {
		kind = "show"
	}
	return inboxItem{Kind: "queue", IMDb: watchTitleIMDb(show.ID), Key: kind, Title: clip(show.Title, 100), Text: note, From: clip(from, 64)}
}

func (b *bot) shareTitle(i *discordgo.InteractionCreate) {
	sender := i.Member.User.ID
	d := i.ApplicationCommandData()
	var target *discordgo.User
	var picked, note string
	for _, o := range d.Options {
		switch o.Name {
		case "member":
			if d.Resolved != nil {
				target = d.Resolved.Users[fmt.Sprint(o.Value)]
			}
		case "title":
			picked = strings.TrimSpace(o.StringValue())
		case "note":
			note = clip(cleanText(o.StringValue(), false), shareNoteMax)
		}
	}
	inServer := false
	if target != nil && d.Resolved != nil {
		_, inServer = d.Resolved.Members[target.ID]
	}
	if p := shareProblem(sender, target, inServer); p != "" {
		b.respond(i, true, errorEmbed("%s", strings.ReplaceAll(p, "Bots don't watch anime.", "Bots don't watch movies.")))
		return
	}
	if !shareLimit.allow(sender, appNow()) {
		b.respond(i, true, errorEmbed("You shared a lot this hour. Try again later."))
		return
	}
	from := displayName(i.Member)
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		var show watchShow
		var err error
		if m := pickedFilmRe.FindStringSubmatch(picked); m != nil {
			show, err = watchTitleByID(watchTitleID(m[1]))
		} else {
			// Typed without picking: take the best match.
			var found []watchShow
			found, err = watchTitleSearch(picked)
			if err == nil && len(found) == 0 {
				return errorEmbed("Nothing found. Pick a title from the suggestions."), nil
			}
			if err == nil {
				show = found[0]
			}
		}
		if err != nil {
			log.Printf("share-title lookup: %v", err)
			return errorEmbed("Couldn't look that title up. Try again."), nil
		}
		dmed := false
		if ch, err := b.s.UserChannelCreate(target.ID); err == nil {
			_, err = b.s.ChannelMessageSendComplex(ch.ID, shareTitleDM(from, show, note))
			dmed = err == nil
			if err != nil {
				log.Printf("share-title DM %s: %v", target.ID, err)
			}
		}
		apps := 0
		if b.hasApp(target.ID) {
			apps = b.sendToApps(target.ID, shareTitleItem(from, show, note))
		}
		return shareResult(target.ID, show.Title, dmed, apps), nil
	})
}
