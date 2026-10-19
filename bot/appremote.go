package main

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /remote: control the episode playing in your own Otakase from Discord,
// say from the phone on the couch. The action goes to the member's linked
// apps as a "remote" item that expires in half a minute, so a late app does
// not pause an episode long after. The app's long poll picks it up within a
// second or so and applies it over mpv's IPC.

const remoteKeep = 30 * time.Second

// remoteActions are the actions the app knows, by key, with their labels.
var remoteActions = []struct{ Key, Label string }{
	{"pause", "Pause"},
	{"play", "Play"},
	{"skip", "Skip 85 s"},
	{"next", "Next episode"},
	{"previous", "Previous episode"},
	{"stop", "Stop"},
}

var remoteLimit = newRateLimit(20, time.Minute)

func init() {
	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, a := range remoteActions {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: a.Label, Value: a.Key})
	}
	registerCommand(&discordgo.ApplicationCommand{Name: "remote", Description: "Pause, play or skip the episode playing in your Otakase", Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "action", Description: "What to do", Required: true, Choices: choices},
	}}, (*bot).remoteCommand)
	registerHelp("`/remote` pause, play or skip the episode playing in your linked Otakase")
}

// remoteLabel is an action's label, "" for an unknown one.
func remoteLabel(key string) string {
	for _, a := range remoteActions {
		if a.Key == key {
			return a.Label
		}
	}
	return ""
}

// remoteItem is the inbox item for an action.
func remoteItem(action string, now time.Time) inboxItem {
	return inboxItem{Kind: "remote", Key: action, Title: remoteLabel(action), Expires: now.Add(remoteKeep)}
}

func (b *bot) remoteCommand(i *discordgo.InteractionCreate) {
	user := i.Member.User.ID
	action := ""
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "action" {
			action = o.StringValue()
		}
	}
	label := remoteLabel(action)
	switch {
	case label == "":
		b.respond(i, true, errorEmbed("Pick an action from the list."))
		return
	case !b.hasApp(user):
		b.respond(i, true, errorEmbed("Link the Otakase app first: run `otakase -discord-link` (or Community → Link to Discord in the app)."))
		return
	case !remoteLimit.allow(user, appNow()):
		b.respond(i, true, errorEmbed("That's a lot of button pressing. Wait a moment."))
		return
	}
	n := b.sendToApps(user, remoteItem(action, appNow()))
	log.Printf("remote %s for %s on %d apps", action, user, n)
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Sent **%s** to %s. An app that is playing an episode does it within a few seconds; the others ignore it.",
		label, dashPlural(n, "linked app"))})
}
