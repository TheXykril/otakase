package main

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// /diagnose tells members how to share what the app knows about their
// problem in their support post.

func diagnoseEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title: "Share your diagnostics",
		Color: shu,
		Description: strings.Join([]string{
			"The easiest way is **More → Report a problem** in the app. It posts to the support forum for you, " +
				"with your version, device and the app's details attached. It needs the app linked to Discord (**More → Discord**).",
			"",
			"Otherwise, add to your post:",
			"- Your version, from **About & updates** (in More or Settings)",
			"- Your device: Android phone, Android TV, Windows, macOS or Linux",
			"- A screenshot of **Diagnostics** (in More or Settings), which shows which sources answer",
		}, "\n"),
		Footer: &discordgo.MessageEmbedFooter{Text: "Tokens and passwords are never included"},
	}
}

func (b *bot) diagnose(i *discordgo.InteractionCreate) {
	b.respond(i, onlyAsker(i), diagnoseEmbed())
}
