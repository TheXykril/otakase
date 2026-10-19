package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /share-anime: recommend a show to another member. They get a DM with the
// cover, the sender's note and Play in Otakase / Queue in Otakase buttons,
// and when they linked the app, the show waits for them under Community →
// From Discord too, marked with who sent it.

const shareNoteMax = 300

var shareLimit = newRateLimit(6, time.Hour)

func init() {
	registerCommand(&discordgo.ApplicationCommand{Name: "share-anime", Description: "Recommend a show to another member, sent to their DMs and their Otakase", Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Who to recommend it to", Required: true},
		{Type: discordgo.ApplicationCommandOptionString, Name: "anime", Description: "The show (pick from the suggestions)", Required: true, Autocomplete: true},
		{Type: discordgo.ApplicationCommandOptionString, Name: "note", Description: "A few words on why they'd like it", MaxLength: shareNoteMax},
	}}, (*bot).shareAnime)
	registerAutocomplete("share-anime", (*bot).suggestTitles)
	registerHelp("`/share-anime` recommend a show to another member, in their DMs and their Otakase")
}

// shareProblem says why a share can't go to target, "" when it can.
func shareProblem(sender string, target *discordgo.User, inServer bool) string {
	switch {
	case target == nil || !inServer:
		return "That member isn't in the server."
	case target.Bot:
		return "Bots don't watch anime. Pick a member."
	case target.ID == sender:
		return "That's you. `/follow` it to get a DM when a new episode airs."
	}
	return ""
}

// shareDM is the recommendation in the member's DMs.
func shareDM(from string, id int, title, siteURL, cover, note string) *discordgo.MessageSend {
	desc := fmt.Sprintf("**%s** recommends this to you.", mdEscape(from))
	if note != "" {
		desc += "\n> " + strings.ReplaceAll(quiet(note), "\n", "\n> ")
	}
	e := &discordgo.MessageEmbed{Title: clip(title, 200), URL: siteURL, Color: shu, Description: desc,
		Footer: &discordgo.MessageEmbedFooter{Text: "Sent with /share-anime in the Otakase Discord"}}
	if cover != "" {
		e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: cover}
	}
	msg := &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e}, AllowedMentions: &discordgo.MessageAllowedMentions{}}
	if buttons := withPlayButton(nil, id, 0); len(buttons) > 0 {
		msg.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
	}
	return msg
}

func (b *bot) shareAnime(i *discordgo.InteractionCreate) {
	sender := i.Member.User.ID
	d := i.ApplicationCommandData()
	var target *discordgo.User
	var q, note string
	for _, o := range d.Options {
		switch o.Name {
		case "member":
			if d.Resolved != nil {
				target = d.Resolved.Users[fmt.Sprint(o.Value)]
			}
		case "anime":
			q = o.StringValue()
		case "note":
			note = clip(cleanText(o.StringValue(), false), shareNoteMax)
		}
	}
	inServer := false
	if target != nil && d.Resolved != nil {
		_, inServer = d.Resolved.Members[target.ID]
	}
	if p := shareProblem(sender, target, inServer); p != "" {
		b.respond(i, true, errorEmbed("%s", p))
		return
	}
	if !shareLimit.allow(sender, appNow()) {
		b.respond(i, true, errorEmbed("You shared a lot of shows this hour. Try again later."))
		return
	}
	from := displayName(i.Member)
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		l, ok, err := findFollowShow(q)
		if err != nil {
			log.Printf("share lookup: %v", err)
			return errorEmbed("Couldn't look up that show on AniList. Try again."), nil
		}
		if !ok {
			return errorEmbed("No show found. Pick one from the suggestions."), nil
		}
		info, found := appShowInfo(l.Show.ID)
		if found && info.IsAdult {
			return errorEmbed("That one can't be shared from here."), nil
		}
		cover := ""
		if found {
			cover = info.CoverImage.ExtraLarge
		}
		dmed := false
		if ch, err := b.s.UserChannelCreate(target.ID); err == nil {
			_, err = b.s.ChannelMessageSendComplex(ch.ID, shareDM(from, l.Show.ID, l.Show.Title, l.Show.URL, cover, note))
			dmed = err == nil
			if err != nil {
				log.Printf("share DM %s: %v", target.ID, err) // usually DMs turned off
			}
		}
		apps := 0
		if b.hasApp(target.ID) {
			apps = b.sendToApps(target.ID, inboxItem{Kind: "queue", AniListID: l.Show.ID, Title: clip(l.Show.Title, 100),
				Text: note, From: clip(from, 64), URL: playURL(l.Show.ID, 0)})
		}
		return shareResult(target.ID, l.Show.Title, dmed, apps), nil
	})
}

// shareResult tells the sender where the recommendation went.
func shareResult(target, title string, dmed bool, apps int) *discordgo.MessageEmbed {
	var where []string
	if dmed {
		where = append(where, "in their DMs")
	}
	if apps > 0 {
		where = append(where, "in their Otakase ("+dashPlural(apps, "app")+")")
	}
	if len(where) == 0 {
		return errorEmbed("Couldn't reach <@%s>: they don't take DMs from the server and haven't linked Otakase.", target)
	}
	return &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Recommended **%s** to <@%s> %s.", mdEscape(title), target, strings.Join(where, " and "))}
}
