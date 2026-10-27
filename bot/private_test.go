package main

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func commandWith(opts ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: discordgo.InteractionApplicationCommand,
		Data: discordgo.ApplicationCommandInteractionData{Name: "faq", Options: opts}}}
}

func TestOnlyAsker(t *testing.T) {
	share := func(v bool) *discordgo.ApplicationCommandInteractionDataOption {
		return &discordgo.ApplicationCommandInteractionDataOption{Name: "share", Type: discordgo.ApplicationCommandOptionBoolean, Value: v}
	}
	if !onlyAsker(commandWith()) {
		t.Fatal("no share option should answer only the asker")
	}
	if !onlyAsker(commandWith(share(false))) {
		t.Fatal("share off should answer only the asker")
	}
	if onlyAsker(commandWith(share(true))) {
		t.Fatal("share on should post for everyone")
	}
	if answerFlags(commandWith(share(true))) != 0 || answerFlags(commandWith()) != discordgo.MessageFlagsEphemeral {
		t.Fatal("answerFlags doesn't follow share")
	}
	button := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: discordgo.InteractionMessageComponent}}
	if !onlyAsker(button) {
		t.Fatal("buttons should answer only the presser")
	}
}

func TestAddShareOptions(t *testing.T) {
	cmds := []*discordgo.ApplicationCommand{
		{Name: "faq", Options: []*discordgo.ApplicationCommandOption{{Name: "topic", Required: true}}},
		{Name: "thank"},
	}
	addShareOptions(cmds)
	addShareOptions(cmds) // registering again must not add it twice
	if n := len(cmds[0].Options); n != 2 || cmds[0].Options[1].Name != "share" || cmds[0].Options[1].Required {
		t.Fatalf("faq options = %d, want topic then an optional share", n)
	}
	if len(cmds[1].Options) != 0 {
		t.Fatal("thank is public by design and gets no share option")
	}
}
