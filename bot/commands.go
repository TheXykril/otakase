package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var zero = 0.0

func (b *bot) registerCommands() error {
	osChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: "Debian / Ubuntu / other Linux", Value: "linux"},
		{Name: "Arch / Manjaro", Value: "arch"},
		{Name: "Windows", Value: "windows"},
		{Name: "macOS", Value: "mac"},
	}
	var faqChoices []*discordgo.ApplicationCommandOptionChoice
	for _, f := range faqs {
		faqChoices = append(faqChoices, &discordgo.ApplicationCommandOptionChoice{Name: f.Question, Value: f.Key})
	}
	cmds := []*discordgo.ApplicationCommand{
		{Name: "help", Description: "What this bot can do"},
		{Name: "install", Description: "How to install Otakase", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "system", Description: "Your system", Required: true, Choices: osChoices},
		}},
		{Name: "faq", Description: "Answer to a common question", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "topic", Description: "Question", Required: true, Choices: faqChoices},
		}},
		{Name: "latest", Description: "The latest Otakase release"},
		{Name: "anime", Description: "Look up an anime on AniList", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "title", Description: "Anime title", Required: true},
		}},
		{Name: "provider-status", Description: "Which anime sources work right now"},
		{Name: "bug", Description: "Turn this support post into a GitHub issue"},
		{Name: "watchparty", Description: "Plan a watch party in a voice channel", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "anime", Description: "What you'll watch (pick from the suggestions)", Required: true, Autocomplete: true},
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "starts_in", Description: "Minutes from now (default 15)", MinValue: &zero, MaxValue: 10080},
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "limit", Description: "Max people in the voice channel (0 = no limit)", MinValue: &zero, MaxValue: 99},
			{Type: discordgo.ApplicationCommandOptionString, Name: "voice", Description: "Who can talk and screen share (default: everyone talks, host shares)", Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Everyone talks, only the host screen shares", Value: partyTalk},
				{Name: "Everyone talks and can screen share", Value: partyShare},
				{Name: "Only the host talks and screen shares", Value: partyHostOnly},
			}},
		}},
	}
	cmds = append(cmds, modCommands()...)
	cmds = append(cmds, roomCommands()...)
	cmds = append(cmds, welcomeCommand())
	cmds = append(cmds, suggestionCommands()...)
	if b.cfg.GitHubClientID != "" && b.cfg.ContributorRole != "" {
		cmds = append(cmds, &discordgo.ApplicationCommand{
			Name: "link-github", Description: "Get the Contributor role for a merged pull request"})
	}
	_, err := b.s.ApplicationCommandBulkOverwrite(b.s.State.User.ID, b.cfg.GuildID, cmds)
	return err
}

func (b *bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Member == nil {
		return // DMs
	}
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		d := i.ApplicationCommandData()
		opt := func(name string) string {
			for _, o := range d.Options {
				if o.Name == name {
					return o.StringValue()
				}
			}
			return ""
		}
		switch d.Name {
		case "help":
			b.respond(i, false, helpEmbed())
		case "install":
			g := installGuides[opt("system")]
			b.respond(i, false, &discordgo.MessageEmbed{Title: "Install on " + g.Title, Description: g.Body, Color: shu,
				Footer: &discordgo.MessageEmbedFooter{Text: "Otakase needs mpv · more on the website"}})
		case "faq":
			f, _ := faqByKey(opt("topic"))
			b.respond(i, false, &discordgo.MessageEmbed{Title: f.Question, Description: f.Answer, Color: shu})
		case "latest":
			b.deferThen(i, false, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
				return latestRelease(b.cfg.GitHubToken)
			})
		case "anime":
			b.deferThen(i, false, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) { return searchAnime(opt("title")) })
		case "link-github":
			b.startGitHubLink(i)
		case "provider-status":
			b.deferThen(i, false, b.providerStatus)
		case "bug":
			b.reportBug(i)
		case "watchparty":
			b.watchparty(i)
		case "Report to mods":
			b.reportMessage(i)
		case "warn":
			b.warn(i)
		case "warnings":
			b.warnings(i)
		case "purge":
			b.purge(i)
		case "slowmode":
			b.slowmode(i)
		case "room":
			b.roomCommand(i)
		case "post-welcome":
			b.postWelcome(i)
		case "room-invite":
			b.roomInvite(i)
		case "suggest":
			b.suggestForm(i)
		case "suggestion-status":
			b.setSuggestionStatus(i)
		}
	case discordgo.InteractionApplicationCommandAutocomplete:
		if i.ApplicationCommandData().Name == "watchparty" {
			b.suggestTitles(i)
		}
	case discordgo.InteractionModalSubmit:
		if i.ModalSubmitData().CustomID == "suggest-form" {
			b.postSuggestion(i)
		}
	case discordgo.InteractionMessageComponent:
		id := i.MessageComponentData().CustomID
		if eventID, ok := strings.CutPrefix(id, "party-cancel:"); ok {
			b.cancelParty(i, eventID)
			return
		}
		if strings.HasPrefix(id, "mod-") {
			b.modAction(i, id)
			return
		}
		switch id {
		case "solved":
			b.markSolved(i)
		case "bug":
			b.reportBug(i)
		case "sug-up", "sug-down":
			b.voteSuggestion(i, id == "sug-up")
		}
	}
}

func helpEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: "Otakase bot", Color: shu, Description: strings.Join([]string{
		"`/install` how to install on your system",
		"`/faq` answers to common questions",
		"`/latest` the newest release",
		"`/anime` look up a show on AniList",
		"`/provider-status` which anime sources work right now",
		"`/watchparty` plan a watch party in a voice channel",
		"`/room` your own voice channel, public or private (or join **Create room**)",
		"`/suggest` suggest an idea; vote on ideas with 👍 and 👎",
		"`/bug` turn a support post into a GitHub issue",
		"Type `#123` to show a GitHub issue or pull request",
		"`/link-github` Contributor role if you have a merged pull request",
		"Right-click a message → **Apps → Report to mods** to flag it",
		"",
		"In the support forum, press **Mark solved** when your problem is fixed.",
		"Episodes airing today are posted in the anime channel each morning.",
	}, "\n")}
}

func (b *bot) respond(i *discordgo.InteractionCreate, ephemeral bool, e *discordgo.MessageEmbed, comps ...discordgo.MessageComponent) {
	data := &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}
	if ephemeral {
		data.Flags = discordgo.MessageFlagsEphemeral
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource, Data: data}); err != nil {
		log.Printf("respond: %v", err)
	}
}

// deferThen acknowledges at once (lookups can take longer than Discord's
// three seconds) and fills the answer in when fn returns.
func (b *bot) deferThen(i *discordgo.InteractionCreate, ephemeral bool, fn func() (*discordgo.MessageEmbed, []discordgo.MessageComponent)) {
	var flags discordgo.MessageFlags
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: flags}}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	e, comps := fn()
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{e}, Components: &comps}); err != nil {
		log.Printf("edit: %v", err)
	}
}

func linkButtons(pairs ...string) []discordgo.MessageComponent {
	var row discordgo.ActionsRow
	for k := 0; k+1 < len(pairs); k += 2 {
		row.Components = append(row.Components, discordgo.Button{Label: pairs[k], URL: pairs[k+1], Style: discordgo.LinkButton})
	}
	return []discordgo.MessageComponent{row}
}

func errorEmbed(format string, a ...any) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Description: fmt.Sprintf(format, a...), Color: 0x8A8276}
}
