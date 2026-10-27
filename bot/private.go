package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// Lookups answer only the member who asked. The share option posts the
// answer for everyone instead, e.g. a helper showing /faq to someone.

func init() {
	registerHelp("Lookups like `/faq`, `/rank` or `/profile` answer only you; turn on their **share** option to post the answer for everyone")
}

// sharedAnswers are the commands that get the share option.
var sharedAnswers = map[string]bool{
	"help": true, "install": true, "faq": true, "diagnose": true, "latest": true, "changelog": true, "anime": true,
	"helpers": true, "levels": true, "rank": true, "leaderboard": true,
	"profile": true, "compare": true, "server-top": true, "server-watch-top": true,
	"watch-time": true, "whos-watching": true, "badges": true, "wrap": true, "featured": true,
}

func shareOption() *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionBoolean, Name: "share",
		Description: "Post it for everyone here (default: only you see it)"}
}

// addShareOptions gives the commands in sharedAnswers their share option.
func addShareOptions(cmds []*discordgo.ApplicationCommand) {
	for _, c := range cmds {
		if sharedAnswers[c.Name] && !hasOption(c, "share") {
			c.Options = append(c.Options, shareOption())
		}
	}
}

func hasOption(c *discordgo.ApplicationCommand, name string) bool {
	for _, o := range c.Options {
		if o.Name == name {
			return true
		}
	}
	return false
}

// onlyAsker reports whether the answer to a command is only for the member
// who used it: true unless they turned share on.
func onlyAsker(i *discordgo.InteractionCreate) bool {
	if i.Type != discordgo.InteractionApplicationCommand {
		return true
	}
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "share" && o.Type == discordgo.ApplicationCommandOptionBoolean {
			return !o.BoolValue()
		}
	}
	return true
}

// answerFlags are the flags for a command's (deferred) answer.
func answerFlags(i *discordgo.InteractionCreate) discordgo.MessageFlags {
	if onlyAsker(i) {
		return discordgo.MessageFlagsEphemeral
	}
	return 0
}

// dmInvite tells a member someone invited them to a channel, with a link
// that opens it. It reports whether the DM went through.
func (b *bot) dmInvite(user, from, channel, what string) bool {
	ch, err := b.s.UserChannelCreate(user)
	if err != nil {
		return false
	}
	e := &discordgo.MessageEmbed{Color: shu, Title: "You're invited",
		Description: fmt.Sprintf("<@%s> invited you to %s <#%s>.", from, what, channel)}
	var comps []discordgo.MessageComponent
	if b.cfg.GuildID != "" {
		comps = linkButtons("Open channel", fmt.Sprintf("https://discord.com/channels/%s/%s", b.cfg.GuildID, channel))
	}
	_, err = b.s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e}, Components: comps,
		AllowedMentions: &discordgo.MessageAllowedMentions{}})
	return err == nil
}

// dmText DMs a member a short note; false when their DMs are closed.
func (b *bot) dmText(user, text string) bool {
	ch, err := b.s.UserChannelCreate(user)
	if err != nil {
		return false
	}
	_, err = b.s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{Color: shu, Description: text}},
		AllowedMentions: &discordgo.MessageAllowedMentions{}})
	return err == nil
}
