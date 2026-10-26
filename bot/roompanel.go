package main

import (
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Room controls: every voice room gets a panel in its chat with buttons
// for the owner to rename, lock, limit, hide, invite, kick and hand the
// room over.
// When the owner leaves, the room goes to whoever has been in it longest.

// roomState is what the panel shows about a room.
type roomState struct {
	Owner  string
	Locked bool // @everyone can't connect
	Hidden bool // @everyone can't see it
	Limit  int
	Adult  bool // age-restricted (18+): stays hidden and invite only
}

// roomStateOf reads a room's state from its channel.
func roomStateOf(c *discordgo.Channel, guildID, owner string) roomState {
	st := roomState{Owner: owner, Limit: c.UserLimit, Adult: c.NSFW}
	if o := findOverwrite(c, guildID); o != nil {
		st.Locked = o.Deny&discordgo.PermissionVoiceConnect != 0
		st.Hidden = o.Deny&discordgo.PermissionViewChannel != 0
	}
	return st
}

// findOverwrite returns a copy of the channel's overwrite for id, or nil.
func findOverwrite(c *discordgo.Channel, id string) *discordgo.PermissionOverwrite {
	for _, o := range c.PermissionOverwrites {
		if o.ID == id {
			x := *o
			return &x
		}
	}
	return nil
}

// roomPanel is the controls message for a room.
func roomPanel(st roomState) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	access, seen, limit := "Open", "Visible", "No limit"
	if st.Locked {
		access = "Locked"
	}
	if st.Hidden {
		seen = "Hidden"
	}
	if st.Limit > 0 {
		limit = fmt.Sprintf("%d people", st.Limit)
	}
	if st.Adult {
		access, seen = "Invite only", "Hidden, 18+"
	}
	e := &discordgo.MessageEmbed{Title: "Room controls", Color: shu,
		Description: "Only the room owner (or staff) can use these buttons. The room is deleted once everyone leaves.",
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Owner", Value: "<@" + st.Owner + ">", Inline: true},
			{Name: "Access", Value: access, Inline: true},
			{Name: "Visibility", Value: seen, Inline: true},
			{Name: "Limit", Value: limit, Inline: true},
		}}
	lock, hide := "Lock", "Hide"
	if st.Locked {
		lock = "Unlock"
	}
	if st.Hidden {
		hide = "Show"
	}
	return e, []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Rename", Style: discordgo.SecondaryButton, CustomID: "rc-rename"},
			discordgo.Button{Label: lock, Style: discordgo.SecondaryButton, CustomID: "rc-lock", Disabled: st.Adult},
			discordgo.Button{Label: "Limit", Style: discordgo.SecondaryButton, CustomID: "rc-limit"},
			discordgo.Button{Label: hide, Style: discordgo.SecondaryButton, CustomID: "rc-hide", Disabled: st.Adult},
		}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Invite", Style: discordgo.PrimaryButton, CustomID: "rc-invite"},
			discordgo.Button{Label: "Kick", Style: discordgo.DangerButton, CustomID: "rc-kick"},
			discordgo.Button{Label: "Transfer owner", Style: discordgo.SecondaryButton, CustomID: "rc-owner"},
		}},
	}
}

// parseRoomLimit reads the Limit form: a whole number from 0 (no limit) to 99.
func parseRoomLimit(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 || n > 99 {
		return 0, false
	}
	return n, true
}

// roomJoins remembers when each member joined each voice channel, to find
// who has been in a room longest.
type roomJoins struct {
	mu sync.Mutex
	at map[string]map[string]time.Time // channel → member → joined
}

var joins = &roomJoins{}

func (j *roomJoins) join(channel, user string, now time.Time) {
	if channel == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.at == nil {
		j.at = map[string]map[string]time.Time{}
	}
	if j.at[channel] == nil {
		j.at[channel] = map[string]time.Time{}
	}
	if _, ok := j.at[channel][user]; !ok {
		j.at[channel][user] = now
	}
}

func (j *roomJoins) leave(channel, user string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.at[channel], user)
	if len(j.at[channel]) == 0 {
		delete(j.at, channel)
	}
}

func (j *roomJoins) times(channel string) map[string]time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	m := map[string]time.Time{}
	for k, v := range j.at[channel] {
		m[k] = v
	}
	return m
}

// longestPresent picks who of present has been there longest. Members
// with no join time were there before a restart, so they count as first.
func longestPresent(present []string, joined map[string]time.Time) string {
	best := ""
	var bestAt time.Time
	for _, id := range present {
		at := joined[id]
		if best == "" || at.Before(bestAt) || (at.Equal(bestAt) && id < best) {
			best, bestAt = id, at
		}
	}
	return best
}

// isRoomStaff reports whether a member can manage any room.
func (b *bot) isRoomStaff(m *discordgo.Member) bool {
	if m.Permissions&discordgo.PermissionAdministrator != 0 {
		return true
	}
	for _, r := range m.Roles {
		if slices.Contains(b.cfg.StaffRoles, r) {
			return true
		}
	}
	return false
}

// roomMembers lists the people (not bots) in a voice channel.
func (b *bot) roomMembers(channelID string) []string {
	g, err := b.s.State.Guild(b.cfg.GuildID)
	if err != nil {
		return nil
	}
	b.s.State.RLock()
	defer b.s.State.RUnlock()
	var ids []string
	for _, vs := range g.VoiceStates {
		if vs.ChannelID == channelID && (vs.Member == nil || vs.Member.User == nil || !vs.Member.User.Bot) {
			ids = append(ids, vs.UserID)
		}
	}
	return ids
}

// postRoomPanel puts the controls in a new room's chat.
func (b *bot) postRoomPanel(channelID string) {
	var r room
	var ok bool
	b.store.view(func(d *storeData) { r, ok = d.Rooms[channelID] })
	if !ok {
		return
	}
	c, err := b.s.Channel(channelID)
	if err != nil {
		log.Printf("room panel: %v", err)
		return
	}
	e, comps := roomPanel(roomStateOf(c, b.cfg.GuildID, r.Owner))
	m, err := b.s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: fmt.Sprintf("<@%s>, this is your room.", r.Owner), Embeds: []*discordgo.MessageEmbed{e}, Components: comps,
		AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{r.Owner}},
	})
	if err != nil {
		log.Printf("room panel: %v", err)
		return
	}
	b.store.update(func(d *storeData) {
		if x, ok := d.Rooms[channelID]; ok {
			x.Panel = m.ID
			d.Rooms[channelID] = x
		}
	})
}

// refreshRoomPanel shows a room's current state on its panel.
func (b *bot) refreshRoomPanel(channelID string) {
	var r room
	var ok bool
	b.store.view(func(d *storeData) { r, ok = d.Rooms[channelID] })
	if !ok {
		return
	}
	if r.Panel == "" {
		b.postRoomPanel(channelID)
		return
	}
	c, err := b.s.Channel(channelID)
	if err != nil {
		return
	}
	e, comps := roomPanel(roomStateOf(c, b.cfg.GuildID, r.Owner))
	edit := discordgo.NewMessageEdit(channelID, r.Panel).SetEmbed(e)
	edit.Components = &comps
	if _, err := b.s.ChannelMessageEditComplex(edit); err != nil {
		log.Printf("room panel update: %v", err)
	}
}

// roomControl handles the panel's buttons and the kick and transfer menus.
func (b *bot) roomControl(i *discordgo.InteractionCreate, id string) {
	ch := i.ChannelID
	r, ok := b.roomFor(i)
	if !ok {
		return
	}
	switch id {
	case "rc-rename":
		name := ""
		if c, err := b.s.State.Channel(ch); err == nil {
			name = c.Name
		}
		b.roomForm(i, "rc-rename-form", "Rename room", discordgo.TextInput{
			CustomID: "value", Label: "Name", Style: discordgo.TextInputShort, Required: true, MaxLength: 60, Value: name})
	case "rc-limit":
		b.roomForm(i, "rc-limit-form", "Room limit", discordgo.TextInput{
			CustomID: "value", Label: "Max people (0 = no limit, up to 99)", Style: discordgo.TextInputShort, Required: true, MaxLength: 2})
	case "rc-lock", "rc-hide":
		b.roomAct(i, false, func() string { return b.toggleRoom(ch, r, id == "rc-lock") })
	case "rc-invite":
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Who should be able to see and join the room?"},
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{MenuType: discordgo.UserSelectMenu, CustomID: "rc-invite-pick", Placeholder: "Pick someone to invite"},
			}})
	case "rc-invite-pick":
		vals := i.MessageComponentData().Values
		if len(vals) == 0 {
			return
		}
		b.roomAct(i, true, func() string {
			if vals[0] == r.Owner || vals[0] == b.s.State.User.ID {
				return "Pick another member."
			}
			if u := i.MessageComponentData().Resolved.Users[vals[0]]; u != nil && u.Bot {
				return "Pick another member."
			}
			if err := b.inviteToRoom(ch, vals[0]); err != nil {
				log.Printf("room invite: %v", err)
				return "Couldn't add them."
			}
			b.roomSay(ch, fmt.Sprintf("<@%s>, <@%s> invited you to this room.", vals[0], i.Member.User.ID), vals[0])
			return fmt.Sprintf("Invited <@%s>.", vals[0])
		})
	case "rc-kick", "rc-owner":
		b.pickRoomMember(i, r, id == "rc-kick")
	case "rc-kick-pick", "rc-owner-pick":
		vals := i.MessageComponentData().Values
		if len(vals) == 0 {
			return
		}
		b.roomAct(i, true, func() string {
			if id == "rc-kick-pick" {
				return b.kickFromRoom(ch, vals[0])
			}
			if err := b.transferRoom(ch, vals[0]); err != nil {
				log.Printf("room transfer: %v", err)
				return "Couldn't hand the room over."
			}
			b.roomSay(ch, fmt.Sprintf("<@%s> handed this room to <@%s>.", i.Member.User.ID, vals[0]), vals[0])
			return fmt.Sprintf("<@%s> owns the room now.", vals[0])
		})
	}
}

// roomFor finds the room an interaction is about and checks the member
// may change it, answering them when not.
func (b *bot) roomFor(i *discordgo.InteractionCreate) (room, bool) {
	var r room
	var ok bool
	b.store.view(func(d *storeData) { r, ok = d.Rooms[i.ChannelID] })
	if !ok {
		b.respond(i, true, errorEmbed("This room is gone."))
		return r, false
	}
	if i.Member.User.ID != r.Owner && !b.isRoomStaff(i.Member) {
		b.respond(i, true, errorEmbed("Only the room owner can do that."))
		return r, false
	}
	return r, true
}

func (b *bot) roomForm(i *discordgo.InteractionCreate, id, title string, input discordgo.TextInput) {
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{CustomID: id, Title: title, Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{input}},
		}},
	}); err != nil {
		log.Printf("room form: %v", err)
	}
}

// roomControlForm handles the Rename and Limit forms.
func (b *bot) roomControlForm(i *discordgo.InteractionCreate, id string) {
	ch := i.ChannelID
	if _, ok := b.roomFor(i); !ok {
		return
	}
	v := modalValues(i)["value"]
	edit := map[string]any{} // a map, so a limit of 0 is still sent
	done := ""
	if id == "rc-rename-form" {
		if v == "" {
			b.respond(i, true, errorEmbed("The name can't be empty."))
			return
		}
		edit["name"], done = v, "Renamed the room."
	} else {
		n, ok := parseRoomLimit(v)
		if !ok {
			b.respond(i, true, errorEmbed("The limit is a number from 0 (no limit) to 99."))
			return
		}
		edit["user_limit"], done = n, "Changed the limit."
	}
	b.roomAct(i, false, func() string {
		if _, err := b.s.RequestWithBucketID("PATCH", discordgo.EndpointChannel(ch), edit, discordgo.EndpointChannel(ch)); err != nil {
			log.Printf("room control: %v", err)
			return "Couldn't change the room. Discord allows two renames every 10 minutes."
		}
		return done
	})
}

// roomAct acknowledges at once (channel edits can be slow), runs fn and
// answers with its text only to the member, then refreshes the panel.
// update replaces the menu message the interaction came from.
func (b *bot) roomAct(i *discordgo.InteractionCreate, update bool, fn func() string) {
	resp := &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}
	if update {
		resp = &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredMessageUpdate}
	}
	if err := b.s.InteractionRespond(i.Interaction, resp); err != nil {
		log.Printf("room control defer: %v", err)
		return
	}
	text := fn()
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{{Color: shu, Description: text}}, Components: &[]discordgo.MessageComponent{}}); err != nil {
		log.Printf("room control reply: %v", err)
	}
	b.refreshRoomPanel(i.ChannelID)
}

// toggleRoom locks/unlocks (connect) or hides/shows (view) a room for
// @everyone. The owner, the people inside, the bot and staff keep access.
func (b *bot) toggleRoom(ch string, r room, lock bool) string {
	c, err := b.s.Channel(ch)
	if err != nil {
		return "Couldn't read the room."
	}
	if c.NSFW {
		return "An 18+ room always stays hidden and invite only. Use `/room privacy` to change that."
	}
	bit := int64(discordgo.PermissionViewChannel)
	if lock {
		bit = discordgo.PermissionVoiceConnect
	}
	every := findOverwrite(c, b.cfg.GuildID)
	if every == nil {
		every = &discordgo.PermissionOverwrite{ID: b.cfg.GuildID, Type: discordgo.PermissionOverwriteTypeRole}
	}
	on := every.Deny&bit == 0
	if on {
		// Let the people who should keep access in first.
		b.allowInRoom(c, b.s.State.User.ID, roomOwnerAllow)
		b.allowInRoom(c, r.Owner, roomGuest)
		for _, id := range b.roomMembers(ch) {
			b.allowInRoom(c, id, roomGuest)
		}
		for _, role := range b.cfg.StaffRoles {
			o := findOverwrite(c, role)
			if o == nil {
				o = &discordgo.PermissionOverwrite{ID: role}
			}
			_ = b.s.ChannelPermissionSet(ch, role, discordgo.PermissionOverwriteTypeRole, o.Allow|roomGuest, o.Deny&^roomGuest)
		}
		every.Allow, every.Deny = every.Allow&^bit, every.Deny|bit
	} else {
		every.Deny &^= bit
	}
	if err := b.s.ChannelPermissionSet(ch, b.cfg.GuildID, discordgo.PermissionOverwriteTypeRole, every.Allow, every.Deny); err != nil {
		log.Printf("room lock: %v", err)
		return "Couldn't change the room."
	}
	switch {
	case lock && on:
		return "Locked. Only the people inside now (and anyone you `/room-invite`) can join."
	case lock:
		return "Unlocked. Anyone can join."
	case on:
		return "Hidden. Only the people inside now (and anyone you `/room-invite`) can see it."
	default:
		return "The room is visible again."
	}
}

// allowInRoom adds allow bits to a member's overwrite in c, keeping the rest.
func (b *bot) allowInRoom(c *discordgo.Channel, user string, allow int64) {
	o := findOverwrite(c, user)
	if o == nil {
		o = &discordgo.PermissionOverwrite{ID: user}
	}
	if err := b.s.ChannelPermissionSet(c.ID, user, discordgo.PermissionOverwriteTypeMember, o.Allow|allow, o.Deny&^allow); err != nil {
		log.Printf("room access: %v", err)
	}
}

// pickRoomMember answers Kick and Transfer owner with a menu of the people
// in the room.
func (b *bot) pickRoomMember(i *discordgo.InteractionCreate, r room, kick bool) {
	var opts []discordgo.SelectMenuOption
	for _, id := range b.roomMembers(i.ChannelID) {
		if id == r.Owner || len(opts) == 25 {
			continue
		}
		label := id
		if m, err := b.s.State.Member(b.cfg.GuildID, id); err == nil && m.User != nil {
			label = displayName(m)
		}
		opts = append(opts, discordgo.SelectMenuOption{Label: clip(label, 100), Value: id})
	}
	if len(opts) == 0 {
		b.respond(i, true, errorEmbed("Nobody else is in the room."))
		return
	}
	id, text, ph := "rc-owner-pick", "Who should own the room?", "Pick the new owner"
	if kick {
		id, text, ph = "rc-kick-pick", "Who should leave the room? If it's locked they can't come back.", "Pick someone to kick"
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: text}, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: id, Placeholder: ph, Options: opts},
	}})
}

// kickFromRoom disconnects a member and, when the room is locked, keeps
// them out.
func (b *bot) kickFromRoom(ch, user string) string {
	var r room
	b.store.view(func(d *storeData) { r = d.Rooms[ch] })
	if user == r.Owner || user == b.s.State.User.ID {
		return "You can't kick the room owner."
	}
	if vs, err := b.s.State.VoiceState(b.cfg.GuildID, user); err == nil && vs.ChannelID == ch {
		if err := b.s.GuildMemberMove(b.cfg.GuildID, user, nil); err != nil {
			log.Printf("room kick: %v", err)
			return "Couldn't disconnect them. The bot needs **Move Members**."
		}
	}
	c, err := b.s.Channel(ch)
	if err != nil {
		return fmt.Sprintf("Removed <@%s>.", user)
	}
	if roomStateOf(c, b.cfg.GuildID, r.Owner).Locked {
		o := findOverwrite(c, user)
		if o == nil {
			o = &discordgo.PermissionOverwrite{ID: user}
		}
		if err := b.s.ChannelPermissionSet(ch, user, discordgo.PermissionOverwriteTypeMember,
			o.Allow&^discordgo.PermissionVoiceConnect, o.Deny|discordgo.PermissionVoiceConnect); err != nil {
			log.Printf("room kick: %v", err)
		}
		return fmt.Sprintf("Removed <@%s>. They can't rejoin while the room is locked.", user)
	}
	return fmt.Sprintf("Removed <@%s>. Lock the room to keep them out.", user)
}

// transferRoom makes user the room's owner and moves the owner's
// permissions to them.
func (b *bot) transferRoom(ch, user string) error {
	var old string
	var r room
	var ok bool
	b.store.update(func(d *storeData) {
		if r, ok = d.Rooms[ch]; ok {
			old, r.Owner = r.Owner, user
			d.Rooms[ch] = r
		}
	})
	if !ok {
		return fmt.Errorf("room %s is gone", ch)
	}
	c, err := b.s.Channel(ch)
	if err != nil {
		return err
	}
	allow := roomOwnerAllow
	if r.Event != "" {
		allow |= partyVoice
	}
	o := findOverwrite(c, user)
	if o == nil {
		o = &discordgo.PermissionOverwrite{ID: user}
	}
	if err := b.s.ChannelPermissionSet(ch, user, discordgo.PermissionOverwriteTypeMember, o.Allow|allow, o.Deny&^allow); err != nil {
		return err
	}
	if p := findOverwrite(c, old); p != nil && old != user {
		manage := int64(discordgo.PermissionManageChannels | discordgo.PermissionVoiceMoveMembers)
		if err := b.s.ChannelPermissionSet(ch, old, discordgo.PermissionOverwriteTypeMember, p.Allow&^manage, p.Deny); err != nil {
			log.Printf("room transfer: %v", err)
		}
	}
	b.refreshRoomPanel(ch)
	return nil
}

// roomSay posts a note in a room's chat.
func (b *bot) roomSay(ch, text string, ping ...string) {
	if _, err := b.s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{Color: shu, Description: text}}, Content: mentions(ping),
		AllowedMentions: &discordgo.MessageAllowedMentions{Users: ping},
	}); err != nil {
		log.Printf("room note: %v", err)
	}
}

func mentions(ids []string) string {
	var s []string
	for _, id := range ids {
		s = append(s, "<@"+id+">")
	}
	return strings.Join(s, " ")
}

// trackRoomPresence notes who joins and leaves voice channels and hands a
// room to whoever has been in it longest when its owner leaves.
func (b *bot) trackRoomPresence(v *discordgo.VoiceStateUpdate) {
	before := ""
	if v.BeforeUpdate != nil {
		before = v.BeforeUpdate.ChannelID
	}
	if before == v.ChannelID {
		return
	}
	joins.leave(before, v.UserID)
	joins.join(v.ChannelID, v.UserID, time.Now())
	if before == "" {
		return
	}
	var r room
	var ok bool
	b.store.view(func(d *storeData) { r, ok = d.Rooms[before] })
	if !ok || r.Owner != v.UserID {
		return
	}
	next := longestPresent(b.roomMembers(before), joins.times(before))
	if next == "" {
		return // empty; it's cleaned up
	}
	if err := b.transferRoom(before, next); err != nil {
		log.Printf("room auto transfer: %v", err)
		return
	}
	b.roomSay(before, fmt.Sprintf("<@%s> left, so <@%s> owns this room now.", v.UserID, next), next)
}
