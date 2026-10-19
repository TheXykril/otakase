package main

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// supportExtraCommands are /support, /diagnose, /thank, /helpers and
// /answer-stats.
func supportExtraCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{Name: "support", Description: "Ask for help: fill in a short form and get a support post"},
		{Name: "diagnose", Description: "How to share otakase's diagnostics in your support post"},
		{Name: "thank", Description: "Give someone a helper point for helping in this support post", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Who helped", Required: true},
		}},
		{Name: "helpers", Description: "The top helpers in the support forum"},
		{Name: "answer-stats", Description: "Which automatic answers people said didn't help", DefaultMemberPermissions: &permPurge},
	}
}

// supportExtraCommand runs one of supportExtraCommands.
func (b *bot) supportExtraCommand(i *discordgo.InteractionCreate, name string) {
	switch name {
	case "support":
		b.supportForm(i)
	case "diagnose":
		b.diagnose(i)
	case "thank":
		b.thank(i)
	case "helpers":
		b.helpers(i)
	case "answer-stats":
		b.answerStats(i)
	}
}

// supportExtraComponent handles the buttons and menus of these features and
// reports whether it took the interaction.
func (b *bot) supportExtraComponent(i *discordgo.InteractionCreate, id string) bool {
	if th, ok := strings.CutPrefix(id, "helped:"); ok {
		b.pickedHelpers(i, th)
		return true
	}
	if strings.HasPrefix(id, "helped-skip:") {
		b.skipHelpers(i)
		return true
	}
	switch id {
	case "ans-up", "ans-down":
		b.voteAnswer(i, id == "ans-up")
		return true
	}
	return false
}
