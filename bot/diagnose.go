package main

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// /diagnose tells members how to get `otakase -diag` output for their post.
// The log reader and version check read the pasted block like any log.

func diagnoseEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title: "Share your diagnostics",
		Color: shu,
		Description: strings.Join([]string{
			"`otakase -diag` prints your version, system, mpv, where your config and log are, and the end of the log from your last run. Tokens and passwords are hidden.",
			"",
			"Run otakase once and let the problem happen, then run the command below and paste the whole block (with the ``` lines) in your support post.",
		}, "\n"),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Linux", Value: "```sh\notakase -diag\n```Copy it straight away with `otakase -diag | wl-copy` (Wayland) or `otakase -diag | xclip -sel clip` (X11)."},
			{Name: "macOS", Value: "```sh\notakase -diag | pbcopy\n```Then paste it in your post."},
			{Name: "Windows", Value: "In PowerShell or Command Prompt:\n```powershell\notakase -diag | clip\n```Then paste it in your post."},
		},
		Footer: &discordgo.MessageEmbedFooter{Text: "Says the flag is unknown? Update first with otakase -u"},
	}
}

func (b *bot) diagnose(i *discordgo.InteractionCreate) {
	b.respond(i, false, diagnoseEmbed())
}
