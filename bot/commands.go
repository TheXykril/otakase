package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

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
	}
	if b.cfg.GitHubClientID != "" && b.cfg.ContributorRole != "" {
		cmds = append(cmds, &discordgo.ApplicationCommand{
			Name: "link-github", Description: "Get the Contributor role for a merged pull request"})
	}
	_, err := b.s.ApplicationCommandBulkOverwrite(b.s.State.User.ID, b.cfg.GuildID, cmds)
	return err
}

func (b *bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
		}
	case discordgo.InteractionMessageComponent:
		if i.MessageComponentData().CustomID == "solved" {
			b.markSolved(i)
		}
	}
}

func helpEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: "Otakase bot", Color: shu, Description: strings.Join([]string{
		"`/install` how to install on your system",
		"`/faq` answers to common questions",
		"`/latest` the newest release",
		"`/anime` look up a show on AniList",
		"`/link-github` Contributor role if you have a merged pull request",
		"",
		"In the support forum, press **Mark solved** when your problem is fixed.",
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
