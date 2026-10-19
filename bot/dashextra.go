package main

import (
	"fmt"
	"html/template"
	"log"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Dashboard pages for voice rooms and posting as the bot, and the
// controls on the suggestions, moderation and games pages.

// dashChannel is a channel's name from the gateway cache, else a short id.
func (b *bot) dashChannel(id string) string {
	if b.s != nil && b.s.State != nil {
		if c, err := b.s.State.Channel(id); err == nil {
			return "#" + c.Name
		}
	}
	if len(id) > 6 {
		return "channel …" + id[len(id)-6:]
	}
	return "channel " + id
}

// Voice rooms.

func (b *bot) dashRooms() template.HTML {
	type roomRow struct {
		ID string
		room
	}
	var list []roomRow
	b.store.view(func(d *storeData) {
		for id, r := range d.Rooms {
			list = append(list, roomRow{id, r})
		}
	})
	sort.Slice(list, func(i, j int) bool { return list[i].Created.Before(list[j].Created) })
	now := time.Now()
	var cards []template.HTML
	parties := 0
	for _, r := range list {
		kind := "room"
		if r.Event != "" {
			kind = "watch party"
			parties++
		}
		people := "—"
		if b.s != nil && b.s.State != nil {
			if g, err := b.s.State.Guild(b.cfg.GuildID); err == nil {
				n := 0
				for _, vs := range g.VoiceStates {
					if vs.ChannelID == r.ID {
						n++
					}
				}
				people = fmt.Sprint(n)
			}
		}
		cards = append(cards, dashExec("setting", struct {
			Name, Desc, Now string
			Controls        template.HTML
		}{b.dashChannel(r.ID), fmt.Sprintf("%s · %s in it · open %s", b.dashName(r.Owner), people, dashAgo(now.Sub(r.Created))), kind,
			dashControls(
				dashControl{Section: "rooms", Do: "room-rename", Name: r.ID, Line: "New name", TextMax: 100, Button: "Rename", Quiet: true},
				dashControl{Section: "rooms", Do: "room-close", Name: r.ID, Confirm: "sure", Button: "Close", Quiet: true},
			)}))
	}
	return dashJoin(
		dashStats(dashStat{"Rooms open", fmt.Sprint(len(list))}, dashStat{"Watch party rooms", fmt.Sprint(parties)}),
		dashRoomCards(cards),
		dashNote("Rooms close by themselves a minute after the last person leaves."),
	)
}

func dashRoomCards(cards []template.HTML) template.HTML {
	if len(cards) == 0 {
		return dashNote("No voice rooms open. Members make one by joining Create room or with /room.")
	}
	return dashJoin(cards...)
}

// Post as the bot.

// postChannels are the text channels the bot can post in, in server order.
func (b *bot) postChannels() []*discordgo.Channel {
	if b.s == nil || b.s.State == nil {
		return nil
	}
	g, err := b.s.State.Guild(b.cfg.GuildID)
	if err != nil {
		return nil
	}
	var out []*discordgo.Channel
	for _, c := range g.Channels {
		if c.Type != discordgo.ChannelTypeGuildText && c.Type != discordgo.ChannelTypeGuildNews {
			continue
		}
		if b.s.State.User != nil {
			if p, err := b.s.State.UserChannelPermissions(b.s.State.User.ID, c.ID); err == nil && p&discordgo.PermissionSendMessages == 0 {
				continue
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

func (b *bot) dashPostPage() template.HTML {
	chans := b.postChannels()
	if len(chans) == 0 {
		return dashNote("The bot isn't connected to Discord, so there's nowhere to post.")
	}
	var choices []dashChoice
	for _, c := range chans {
		choices = append(choices, dashChoice{Value: c.ID, Label: "#" + c.Name})
	}
	return dashJoin(
		dashNote("Post a message in a channel as the bot. Discord markdown works; @everyone, @here and other mentions don't ping anyone."),
		dashControls(dashControl{Section: "post", Do: "say", Choices: choices, Text: "What the bot says", Button: "Post"}),
	)
}

func (b *bot) dashSay(channel, text, who string) error {
	if text == "" {
		return fmt.Errorf("Write something to post.")
	}
	if len([]rune(text)) > 2000 {
		return fmt.Errorf("Discord messages are 2000 characters at most.")
	}
	ok := false
	for _, c := range b.postChannels() {
		ok = ok || c.ID == channel
	}
	if !ok {
		return fmt.Errorf("The bot can't post in that channel.")
	}
	m, err := b.s.ChannelMessageSendComplex(channel, &discordgo.MessageSend{Content: text, AllowedMentions: &discordgo.MessageAllowedMentions{}})
	if err != nil {
		log.Printf("dashboard post: %v", err)
		return fmt.Errorf("Discord said no: %v.", err)
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s posted as the bot in %s:\n%s",
		who, messageLink(b.cfg.GuildID, channel, m.ID), clip(text, 1500))}, nil, false)
	setFlash(false, "Posted in %s.", b.dashChannel(channel))
	return nil
}

// Suggestions.

func (b *bot) dashSuggestionStatus(id, key, who string) error {
	if key == "open" {
		key = ""
	}
	valid := false
	for _, st := range suggestionStatuses {
		valid = valid || st.Key == key
	}
	if !valid {
		return fmt.Errorf("Unknown status.")
	}
	if b.s == nil || b.s.State == nil || b.s.State.User == nil {
		return fmt.Errorf("The bot isn't connected to Discord.")
	}
	found, err := b.changeSuggestionStatus(id, key, "", b.s.State.User.ID)
	if !found {
		return fmt.Errorf("That suggestion is gone.")
	}
	if err != nil {
		log.Printf("dashboard suggestion status: %v", err)
		return fmt.Errorf("Saved, but couldn't update the card in Discord: %v.", err)
	}
	st := statusByKey(key)
	if _, err := b.s.ChannelMessageSendComplex(id, &discordgo.MessageSend{AllowedMentions: &discordgo.MessageAllowedMentions{},
		Embeds: []*discordgo.MessageEmbed{{Description: fmt.Sprintf("Staff marked this suggestion **%s**.", st.Label), Color: st.Color}}}); err != nil {
		log.Printf("dashboard suggestion note: %v", err)
	}
	b.archiveIfClosed(id, key)
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s marked suggestion <#%s> **%s**.", who, id, st.Label)}, nil, false)
	setFlash(false, "Suggestion marked %s.", st.Label)
	return nil
}

// Moderation.

func (b *bot) dashClearWarnings(user, who string) error {
	n := 0
	b.store.update(func(d *storeData) {
		n = len(d.Warnings[user])
		delete(d.Warnings, user)
	})
	if n == 0 {
		return fmt.Errorf("They have no warnings.")
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s cleared %s for <@%s>.", who, dashPlural(n, "warning"), user)}, nil, false)
	setFlash(false, "Cleared %s for %s.", dashPlural(n, "warning"), b.dashName(user))
	return nil
}
