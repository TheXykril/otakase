package main

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// /diagnose tells members how to send their app's details with a report.

func diagnoseEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title: "Share your diagnostics",
		Color: shu,
		Description: strings.Join([]string{
			"Open **Report a problem** in the app (under **More**), say what went wrong and leave the app details on. " +
				"It posts to the support forum with your version, device, playback settings and how the sources are doing, and replies come to you in Discord. Tokens and passwords are never sent.",
			"",
			"The app has to be linked first: **More → Discord** gives you a `/link-app` code.",
			"",
			"Not linked? Post in the support forum yourself with your version (**About & updates**) and device. **Diagnostics** in the app shows which sources answer.",
		}, "\n"),
	}
}

func (b *bot) diagnose(i *discordgo.InteractionCreate) {
	b.respond(i, onlyAsker(i), diagnoseEmbed())
}
