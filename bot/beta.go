package main

import (
	"log"
	"slices"

	"github.com/bwmarrin/discordgo"
)

// Beta opt-in from Discord. The Tester role means development builds: when
// a member gains it (from staff, /beta on, or the bot noticing a dev build)
// their linked apps get the DevBuilds setting turned on, and off again when
// they lose it. /beta gives or takes the role yourself.
//
// The change is seen in the member update event by comparing the roles
// before and after; a member the bot hadn't cached has no before, so
// nothing is sent for them. /beta caches the member first so it always
// counts.

func init() {
	registerCommand(&discordgo.ApplicationCommand{Name: "beta", Description: "Get Otakase development builds (the Tester role), or go back to stable", Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "builds", Description: "On for development builds, off for stable releases", Required: true, Choices: []*discordgo.ApplicationCommandOptionChoice{
			{Name: "on", Value: "on"},
			{Name: "off", Value: "off"},
		}},
	}}, (*bot).betaCommand)
	registerHelp("`/beta on` development builds of Otakase and the Tester role, `/beta off` back to stable")
	onStart(func(b *bot) { b.s.AddHandler(b.onTesterChange) })
}

// testerChange says whether a member update gave (+1) or took (-1) the role.
func testerChange(before, after []string, role string) int {
	had, has := slices.Contains(before, role), slices.Contains(after, role)
	switch {
	case has && !had:
		return 1
	case had && !has:
		return -1
	}
	return 0
}

// betaSetting is the inbox item that switches an app's DevBuilds.
func betaSetting(on bool) inboxItem {
	if on {
		return inboxItem{Kind: "setting", Key: "DevBuilds", Value: "true",
			Text: "You have the Tester role in the Otakase Discord, so Otakase now updates to development builds. /beta off in Discord goes back to stable."}
	}
	return inboxItem{Kind: "setting", Key: "DevBuilds", Value: "false",
		Text: "You no longer have the Tester role in the Otakase Discord, so Otakase updates to stable releases again."}
}

func (b *bot) onTesterChange(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
	if m.Member == nil || m.User == nil || m.User.Bot || m.GuildID != b.cfg.GuildID || m.BeforeUpdate == nil {
		return
	}
	role := b.testerRoleID()
	if role == "" {
		return
	}
	change := testerChange(m.BeforeUpdate.Roles, m.Roles, role)
	if change == 0 || !b.hasApp(m.User.ID) {
		return
	}
	n := b.sendToApps(m.User.ID, betaSetting(change > 0))
	log.Printf("dev builds %v for %s on %d apps", change > 0, m.User.ID, n)
}

func (b *bot) betaCommand(i *discordgo.InteractionCreate) {
	on := len(i.ApplicationCommandData().Options) > 0 && i.ApplicationCommandData().Options[0].StringValue() == "on"
	user := i.Member.User.ID
	role := b.testerRoleID()
	if role == "" {
		b.respond(i, true, errorEmbed("There's no Tester role in this server yet. Ask a moderator."))
		return
	}
	if slices.Contains(i.Member.Roles, role) == on {
		state := "You don't have the Tester role."
		if on {
			state = "You already have the Tester role."
		}
		b.respond(i, true, errorEmbed("%s", state))
		return
	}
	// Cached, so the member update has a before to compare with.
	if _, err := b.s.State.Member(b.cfg.GuildID, user); err != nil {
		m := *i.Member
		m.GuildID = b.cfg.GuildID
		_ = b.s.State.MemberAdd(&m)
	}
	// The member chose: the dev build check must not give the role back.
	b.store.update(func(d *storeData) {
		st := d.AppStats[user]
		st.Tester = true
		d.AppStats[user] = st
	})
	var err error
	if on {
		err = b.s.GuildMemberRoleAdd(b.cfg.GuildID, user, role)
	} else {
		err = b.s.GuildMemberRoleRemove(b.cfg.GuildID, user, role)
	}
	if err != nil {
		log.Printf("beta role: %v", err)
		b.respond(i, true, errorEmbed("Couldn't change your role. Ask a moderator."))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: betaReply(on, b.hasApp(user))})
}

// betaReply is the answer to /beta.
func betaReply(on, linked bool) string {
	msg := "You're back on stable releases and no longer a Tester."
	if on {
		msg = "You're a Tester now: thanks for trying new things first! Report what breaks in the support forum."
	}
	switch {
	case linked && on:
		msg += "\nYour linked Otakase switches to development builds by itself; the next update brings one."
	case linked:
		msg += "\nYour linked Otakase switches back to stable releases by itself."
	case on:
		msg += "\nIn Otakase, turn on **DevBuilds** in the settings (or link it with `/link-app` and it follows this role)."
	default:
		msg += "\nIn Otakase, turn off **DevBuilds** in the settings."
	}
	return msg
}
