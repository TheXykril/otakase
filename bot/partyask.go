package main

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Joining someone from /whos-watching: under the list, each of the first
// few members has Open same ep (Play in Otakase at their episode) and Ask
// to party. Asking someone who runs Watch with me answers with their join
// link at once; otherwise the bot DMs them (and tells their apps) that the
// asker wants to watch along, with a Send my link button for once they
// turned Watch with me on.

const whosWatchingButtons = 5 // rows, one per member

var (
	askLimit       = newRateLimit(5, 10*time.Minute) // asks per member
	askPairLimit   = newRateLimit(1, 10*time.Minute) // the same member asked by the same member
	partyLinkLimit = newRateLimit(10, 10*time.Minute)
)

func init() {
	registerComponent("party-ask:", (*bot).partyAsk)
	registerComponent("party-share:", (*bot).partyShare)
	registerDMComponent("party-share:", (*bot).partyShare)
	registerHelp("`/whos-watching` has **Open same ep** and **Ask to party** buttons; with **Watch with me** on in Otakase your Discord status gets a join button")
}

// whosWatchingAnswer is /whos-watching: the list, and its buttons.
func (b *bot) whosWatchingAnswer(i *discordgo.InteractionCreate) {
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	list := watchingList(appNow())
	e := whosWatchingView(list, appShowInfo)
	rows := whosWatchingRows(list, appShowInfo, b.dashName)
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e},
		Components: &rows, AllowedMentions: &discordgo.MessageAllowedMentions{}}); err != nil {
		log.Printf("edit: %v", err)
	}
}

// whosWatchingRows are the buttons for the first members the list shows:
// the same ones it leaves out (adult titles, nothing to show) are left out
// here, and so are shows that aren't on AniList.
func whosWatchingRows(list []nowWatching, lookup func(int) (airingShowInfo, bool), name func(string) string) []discordgo.MessageComponent {
	rows := []discordgo.MessageComponent{}
	for _, e := range list {
		if len(rows) == whosWatchingButtons {
			break
		}
		id := e.Show.AniListID
		if id <= 0 {
			continue
		}
		if info, ok := lookup(id); !ok || info.IsAdult {
			continue
		}
		who := clip(name(e.User), 40)
		open := "Open same show · " + who
		if e.Show.Episode > 0 {
			open = fmt.Sprintf("Open ep %d · %s", e.Show.Episode, who)
		}
		rows = append(rows, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: open, Style: discordgo.LinkButton, URL: playURL(id, e.Show.Episode)},
			discordgo.Button{Label: clip("Ask "+who+" to party", 80), Style: discordgo.SecondaryButton, CustomID: "party-ask:" + e.User},
		}})
	}
	return rows
}

// joinRow is the Join in Otakase button for a Watch with me party.
func joinRow(p liveParty) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Join in Otakase", Style: discordgo.LinkButton, URL: partyJoinURL(p.ID)},
	}}}
}

// partyAsk is the Ask to party button: arg is the member watching.
func (b *bot) partyAsk(i *discordgo.InteractionCreate, watcher string) {
	asker := interactionUser(i)
	if asker == nil {
		return
	}
	if asker.ID == watcher {
		b.respond(i, true, errorEmbed("That's you. Turn on **Watch with me** in Otakase (Community menu) and your Discord status gets a join button."))
		return
	}
	who := b.dashName(watcher)
	if p, ok := livePartyOf(watcher); ok {
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf(
			"**%s** is already hosting with Watch with me. Join and your Otakase follows their player.", mdEscape(who))}, joinRow(p)...)
		return
	}
	if !askLimit.allow(asker.ID, appNow()) || !askPairLimit.allow(asker.ID+":"+watcher, appNow()) {
		b.respond(i, true, errorEmbed("You asked a moment ago. Give them a few minutes."))
		return
	}
	name := clip(b.dashName(asker.ID), 40)
	e := &discordgo.MessageEmbed{Title: "Watch together?", Color: shu, Description: fmt.Sprintf(
		"**%s** saw you in `/whos-watching` and wants to watch along with you.\n\n"+
			"Turn on **Watch with me** in Otakase (Community menu) while an episode plays, then press **Send my link**: "+
			"they get your join link and their Otakase follows your player.", mdEscape(name))}
	comps := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Send my link", Style: discordgo.PrimaryButton, CustomID: "party-share:" + asker.ID},
	}}}
	dmed := false
	if ch, err := b.s.UserChannelCreate(watcher); err == nil {
		_, err = b.s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e}, Components: comps,
			AllowedMentions: &discordgo.MessageAllowedMentions{}})
		dmed = err == nil
	}
	apps := b.sendToApps(watcher, inboxItem{Kind: "notice", Title: "Watch together?", From: name, Expires: appNow().Add(time.Hour),
		Text: name + " wants to watch along. Turn on Watch with me in Community, then press Send my link in the bot's DM."})
	if !dmed && apps == 0 {
		b.respond(i, true, errorEmbed("Couldn't reach **%s**: their DMs are closed.", mdEscape(who)))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf(
		"Asked **%s**. If they say yes, the bot DMs you their join link.", mdEscape(who))})
}

// partyShare is Send my link, in the DM the asker's request came in: arg
// is the asker.
func (b *bot) partyShare(i *discordgo.InteractionCreate, asker string) {
	u := interactionUser(i)
	if u == nil {
		return
	}
	p, ok := livePartyOf(u.ID)
	if !ok {
		b.respond(i, true, errorEmbed("Watch with me isn't running. Turn it on in Otakase (Community → Watch with me) while an episode plays, then press this again."))
		return
	}
	if !partyLinkLimit.allow(u.ID, appNow()) {
		b.respond(i, true, errorEmbed("You sent a few already. Try again in a few minutes."))
		return
	}
	name := clip(b.dashName(u.ID), 40)
	show := ""
	if info, ok := appShowInfo(p.AniListID); ok && !info.IsAdult && p.Show != "" {
		show = fmt.Sprintf(" They're on **%s**", showText(clip(p.Show, 80)))
		if p.Episode > 0 {
			show += fmt.Sprintf(", episode %d", p.Episode)
		}
		show += "."
	}
	e := &discordgo.MessageEmbed{Title: "Join the party", Color: shu, Description: fmt.Sprintf(
		"**%s** said yes.%s Press **Join in Otakase** and your player follows theirs.", mdEscape(name), show)}
	ch, err := b.s.UserChannelCreate(asker)
	if err == nil {
		_, err = b.s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e}, Components: joinRow(p),
			AllowedMentions: &discordgo.MessageAllowedMentions{}})
	}
	if err != nil {
		log.Printf("party share DM: %v", err)
		b.respond(i, true, errorEmbed("Couldn't DM them: their DMs are closed."))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Sent them your join link."})
}
