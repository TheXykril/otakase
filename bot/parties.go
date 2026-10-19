package main

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Watch party RSVPs: the party post has I'm in and Can't make it buttons,
// lists who's going, and the going members get a DM shortly before the
// start. Parties are kept in the store so a restart keeps the RSVPs.

// watchParty is a planned party, stored by its event id.
type watchParty struct {
	Show    string    `json:"show"`
	URL     string    `json:"url,omitempty"`
	Image   string    `json:"image,omitempty"`
	Link    string    `json:"link"` // the server event
	Host    string    `json:"host"`
	Channel string    `json:"channel"` // its voice channel
	Start   time.Time `json:"start"`
	Created time.Time `json:"created"`
	Going   []string  `json:"going,omitempty"`
	Out     []string  `json:"out,omitempty"` // can't make it
	// Announced: the role ping went out. Reminded: the DMs went out.
	Announced bool `json:"announced,omitempty"`
	Reminded  bool `json:"reminded,omitempty"`
	// AniListID is the show, when it was found on AniList. Sync: members
	// can follow the host's player from their own Otakase (partysync.go).
	AniListID int  `json:"anilist_id,omitempty"`
	Sync      bool `json:"sync,omitempty"`
	// AppReminded: the going members' apps got their reminder (partyapp.go).
	AppReminded bool `json:"app_reminded,omitempty"`
}

const (
	partyReminderLead = 5 * time.Minute // DM going members this long before the start
	partyGoingShown   = 20              // going members listed on the post
	partyPingRoleName = "Watch party pings"
)

// partyReminderAt is when going members get their DM: a few minutes before
// the start, or at the start for a party planned at short notice.
func partyReminderAt(created, start time.Time) time.Time {
	if start.Sub(created) >= partyReminderLead {
		return start.Add(-partyReminderLead)
	}
	return start
}

// toggleRSVP adds or removes user from the going (or can't make it) list
// and reports whether they're on it now. Being on one list takes them off
// the other.
func toggleRSVP(p *watchParty, user string, going bool) bool {
	on, other := &p.Going, &p.Out
	if !going {
		on, other = &p.Out, &p.Going
	}
	*other = slices.DeleteFunc(*other, func(id string) bool { return id == user })
	if slices.Contains(*on, user) {
		*on = slices.DeleteFunc(*on, func(id string) bool { return id == user })
		return false
	}
	*on = append(*on, user)
	return true
}

// goingList mentions up to max going members, then says how many more.
func goingList(ids []string, max int) string {
	if len(ids) == 0 {
		return "Nobody yet. Press **I'm in**."
	}
	var parts []string
	for k, id := range ids {
		if k == max {
			parts = append(parts, fmt.Sprintf("+%d more", len(ids)-max))
			break
		}
		parts = append(parts, "<@"+id+">")
	}
	return strings.Join(parts, " ")
}

func voiceLink(guild, channel string) string {
	return fmt.Sprintf("https://discord.com/channels/%s/%s", guild, channel)
}

// partyPost is the party's announcement with its RSVP buttons.
func partyPost(eventID string, p watchParty) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	e := &discordgo.MessageEmbed{
		Title: "Watch party: " + p.Show,
		URL:   p.Link,
		Description: fmt.Sprintf("<@%s> is hosting in <#%s>, starting <t:%d:R>. Press **I'm in** to get a DM when it's about to start.",
			p.Host, p.Channel, p.Start.Unix()),
		Color:  shu,
		Fields: []*discordgo.MessageEmbedField{{Name: fmt.Sprintf("Going (%d)", len(p.Going)), Value: goingList(p.Going, partyGoingShown)}},
	}
	if len(p.Out) > 0 {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Can't make it", Value: fmt.Sprint(len(p.Out))})
	}
	if p.Sync {
		e.Description += "\n\nEveryone watches in their own Otakase, synced to the host. Press **Join in Otakase** " +
			"(link the app first with `otakase -discord-link`)."
	}
	if p.Image != "" {
		e.Image = &discordgo.MessageEmbedImage{URL: p.Image}
	}
	buttons := []discordgo.MessageComponent{
		discordgo.Button{Label: "I'm in", Style: discordgo.SuccessButton, CustomID: "party-in:" + eventID},
		discordgo.Button{Label: "Can't make it", Style: discordgo.SecondaryButton, CustomID: "party-out:" + eventID},
		discordgo.Button{Label: "Open event", Style: discordgo.LinkButton, URL: p.Link},
	}
	if p.Sync {
		buttons = append(buttons, discordgo.Button{Label: "Join in Otakase", Style: discordgo.LinkButton, URL: partyJoinURL(eventID)})
	}
	buttons = append(buttons, discordgo.Button{Label: "Cancel", Style: discordgo.DangerButton, CustomID: "party-cancel:" + eventID})
	return e, []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
}

// partyRSVP handles the I'm in and Can't make it buttons.
func (b *bot) partyRSVP(i *discordgo.InteractionCreate, eventID string, going bool) {
	user := i.Member.User.ID
	var p watchParty
	var ok, on bool
	b.store.update(func(d *storeData) {
		if p, ok = d.Parties[eventID]; ok {
			on = toggleRSVP(&p, user, going)
			d.Parties[eventID] = p
		}
	})
	if !ok {
		b.respond(i, true, errorEmbed("This watch party is already over or cancelled."))
		return
	}
	e, comps := partyPost(eventID, p)
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps},
	}); err != nil {
		log.Printf("party rsvp: %v", err)
		return
	}
	var text string
	switch {
	case going && on && p.Reminded:
		text = fmt.Sprintf("You're in. It's on now: join <#%s>.", p.Channel)
	case going && on:
		text = "You're in. You'll get a DM when it's about to start."
	case going:
		text = "Took you off the going list."
	case on:
		text = "Got it, you can't make it this time."
	default:
		text = "Took you off the can't make it list."
	}
	if _, err := b.s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{{Color: shu, Description: text}}, Flags: discordgo.MessageFlagsEphemeral}); err != nil {
		log.Printf("party rsvp reply: %v", err)
	}
}

// partyPingRole finds the "Watch party pings" role, or "" when there's none.
func (b *bot) partyPingRole() string {
	roles, err := b.s.GuildRoles(b.cfg.GuildID)
	if err != nil {
		log.Printf("listing roles: %v", err)
		return ""
	}
	for _, r := range roles {
		if strings.EqualFold(strings.TrimSpace(r.Name), partyPingRoleName) {
			return r.ID
		}
	}
	return ""
}

// announceParty pings the watch party role once for the party host just
// planned, in channel. With post set it also posts the party with its
// buttons there, for when the host's own copy is only visible to them.
func (b *bot) announceParty(host, channel string, post bool) {
	var id string
	var p watchParty
	b.store.update(func(d *storeData) {
		for k, x := range d.Parties {
			if x.Host == host && !x.Announced {
				x.Announced = true
				d.Parties[k] = x
				id, p = k, x
			}
		}
	})
	if id == "" {
		return // planning failed
	}
	msg := &discordgo.MessageSend{AllowedMentions: &discordgo.MessageAllowedMentions{}}
	if role := b.partyPingRole(); role != "" {
		msg.Content = fmt.Sprintf("<@&%s> New watch party: **%s**, starting <t:%d:R>.", role, p.Show, p.Start.Unix())
		msg.AllowedMentions.Roles = []string{role}
	}
	if post {
		e, comps := partyPost(id, p)
		msg.Embeds, msg.Components = []*discordgo.MessageEmbed{e}, comps
	}
	if msg.Content == "" && !post {
		return
	}
	if _, err := b.s.ChannelMessageSendComplex(channel, msg); err != nil {
		log.Printf("party announcement: %v", err)
	}
}

// partyLoop sends the reminder DMs, also for parties from before a restart.
func (b *bot) partyLoop() {
	for {
		b.remindParties(time.Now())
		time.Sleep(20 * time.Second)
	}
}

func (b *bot) remindParties(now time.Time) {
	type due struct {
		id string
		p  watchParty
	}
	var send []due
	var gone []string
	b.store.update(func(d *storeData) {
		for id, p := range d.Parties {
			if now.Sub(p.Start) > 24*time.Hour {
				delete(d.Parties, id) // its event was removed some other way
				gone = append(gone, id)
				continue
			}
			if p.Reminded || now.Before(partyReminderAt(p.Created, p.Start)) {
				continue
			}
			p.Reminded = true
			d.Parties[id] = p
			if now.Sub(p.Start) < partyNoShowWait { // too late after a long outage
				send = append(send, due{id, p})
			}
		}
	})
	for _, id := range gone {
		syncHub.end(id)
	}
	for _, x := range send {
		title := "Watch party starting now"
		if x.p.Start.After(now.Add(time.Minute)) {
			title = "Watch party starting soon"
		}
		e := &discordgo.MessageEmbed{Title: title, Color: shu,
			Description: fmt.Sprintf("**%s** starts <t:%d:t> (<t:%d:R>), hosted by <@%s>. Join the voice channel to watch along.",
				x.p.Show, x.p.Start.Unix(), x.p.Start.Unix(), x.p.Host)}
		if x.p.Image != "" {
			e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: x.p.Image}
		}
		comps := linkButtons("Join voice channel", voiceLink(b.cfg.GuildID, x.p.Channel), "Open event", x.p.Link)
		for _, user := range x.p.Going {
			b.dmParty(user, e, comps)
		}
	}
}

func (b *bot) dmParty(user string, e *discordgo.MessageEmbed, comps []discordgo.MessageComponent) {
	ch, err := b.s.UserChannelCreate(user)
	if err != nil {
		log.Printf("party DM %s: %v", user, err)
		return
	}
	if _, err := b.s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}); err != nil {
		log.Printf("party DM %s: %v", user, err) // usually DMs turned off
	}
}

// partyCancelled forgets a cancelled party and tells the going members,
// except whoever cancelled it.
func (b *bot) partyCancelled(eventID, by string) {
	var p watchParty
	var ok bool
	b.store.update(func(d *storeData) {
		if p, ok = d.Parties[eventID]; ok {
			delete(d.Parties, eventID)
		}
	})
	syncHub.end(eventID)
	if !ok {
		return
	}
	e := &discordgo.MessageEmbed{Title: "Watch party cancelled", Color: 0x8A8276,
		Description: fmt.Sprintf("The **%s** watch party planned for <t:%d:f> was cancelled.", p.Show, p.Start.Unix())}
	for _, user := range p.Going {
		if user != by {
			b.dmParty(user, e, nil)
		}
	}
}

// forgetParty drops a finished party's RSVPs.
func (b *bot) forgetParty(eventID string) {
	b.store.update(func(d *storeData) { delete(d.Parties, eventID) })
	syncHub.end(eventID)
}
