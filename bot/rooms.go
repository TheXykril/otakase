package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Voice rooms: joining the "Create room" voice channel, or /room, makes a
// voice channel for the member. It's deleted once everyone has left.

const (
	roomGrace     = 2 * time.Minute // a new room nobody has joined yet waits this long
	roomEmptyWait = time.Minute     // a used room is removed after being empty this long
)

// emptyRooms remembers since when each room has been empty.
type emptyRooms struct {
	mu    sync.Mutex
	since map[string]time.Time
}

// mark records that a room is empty now and returns how long it has been.
func (e *emptyRooms) mark(id string, now time.Time) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.since == nil {
		e.since = map[string]time.Time{}
	}
	if _, ok := e.since[id]; !ok {
		e.since[id] = now
	}
	return now.Sub(e.since[id])
}

func (e *emptyRooms) clear(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.since, id)
}

var (
	roomOwnerAllow = int64(discordgo.PermissionViewChannel | discordgo.PermissionVoiceConnect |
		discordgo.PermissionManageChannels | discordgo.PermissionVoiceMoveMembers)
	roomHidden = int64(discordgo.PermissionViewChannel | discordgo.PermissionVoiceConnect)
	roomGuest  = int64(discordgo.PermissionViewChannel | discordgo.PermissionVoiceConnect)
)

func roomCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{Name: "room", Description: "Make your own voice channel, or change the one you have", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "privacy", Description: "Who can join (default public)", Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Public: anyone can join", Value: "public"},
				{Name: "Private: only people you invite", Value: "private"},
			}},
			{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Channel name", MaxLength: 60},
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "limit", Description: "Max people (0 = no limit)", MinValue: &zero, MaxValue: 99},
		}},
		{Name: "room-invite", Description: "Let someone into your private room", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Who", Required: true},
		}},
	}
}

// isCreateChannel reports whether a voice channel is the join-to-create one.
func isCreateChannel(name string) bool {
	n := strings.ToLower(channelBase(name))
	return strings.Contains(n, "create room") || strings.Contains(n, "create-room") || strings.Contains(n, "join to create")
}

// roomOverwrites lets the owner manage the room. A private one is hidden
// from everyone else except the bot and the staff roles.
func roomOverwrites(guildID, botID, owner string, staff []string, private bool) []*discordgo.PermissionOverwrite {
	o := []*discordgo.PermissionOverwrite{{ID: owner, Type: discordgo.PermissionOverwriteTypeMember, Allow: roomOwnerAllow}}
	if private {
		o = append(o,
			&discordgo.PermissionOverwrite{ID: guildID, Type: discordgo.PermissionOverwriteTypeRole, Deny: roomHidden},
			&discordgo.PermissionOverwrite{ID: botID, Type: discordgo.PermissionOverwriteTypeMember, Allow: roomOwnerAllow})
		for _, r := range staff {
			o = append(o, &discordgo.PermissionOverwrite{ID: r, Type: discordgo.PermissionOverwriteTypeRole, Allow: roomGuest})
		}
	}
	return o
}

func (b *bot) overwrites(owner string, private bool) []*discordgo.PermissionOverwrite {
	return roomOverwrites(b.cfg.GuildID, b.s.State.User.ID, owner, b.cfg.StaffRoles, private)
}

// ownedRoom returns the room channel a member owns, if any.
func (b *bot) ownedRoom(userID string) string {
	var id string
	b.store.view(func(d *storeData) {
		for ch, r := range d.Rooms {
			if r.Owner == userID && r.Event == "" {
				id = ch
			}
		}
	})
	return id
}

func (b *bot) createRoom(m *discordgo.Member, name string, private bool, limit int) (*discordgo.Channel, error) {
	if name == "" {
		name = displayName(m) + "'s room"
	}
	parent := ""
	if c, err := b.s.State.Channel(b.cfg.CreateRoom); err == nil {
		parent = c.ParentID
	} else if c, err := b.s.State.Channel(b.cfg.LoungeChannel); err == nil {
		parent = c.ParentID
	}
	ch, err := b.s.GuildChannelCreateComplex(b.cfg.GuildID, discordgo.GuildChannelCreateData{
		Name: name, Type: discordgo.ChannelTypeGuildVoice, ParentID: parent, UserLimit: limit,
		PermissionOverwrites: b.overwrites(m.User.ID, private),
	}, discordgo.WithAuditLogReason("Voice room for "+m.User.Username))
	if err != nil {
		return nil, err
	}
	b.store.update(func(d *storeData) { d.Rooms[ch.ID] = room{Owner: m.User.ID, Created: time.Now().UTC()} })
	return ch, nil
}

func displayName(m *discordgo.Member) string {
	if m.Nick != "" {
		return m.Nick
	}
	if m.User.GlobalName != "" {
		return m.User.GlobalName
	}
	return m.User.Username
}

func (b *bot) onVoiceState(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	if v.GuildID != b.cfg.GuildID || v.Member == nil || v.Member.User == nil || v.Member.User.Bot {
		return
	}
	if v.BeforeUpdate != nil && v.BeforeUpdate.ChannelID != v.ChannelID {
		b.sweepRoom(v.BeforeUpdate.ChannelID, false)
	}
	b.markRoomUsed(v.ChannelID)
	b.empty.clear(v.ChannelID)
	if v.ChannelID == "" || v.ChannelID != b.cfg.CreateRoom {
		return
	}
	ch := b.ownedRoom(v.UserID)
	if ch == "" {
		c, err := b.createRoom(v.Member, "", false, 0)
		if err != nil {
			log.Printf("create room: %v", err)
			return
		}
		ch = c.ID
	}
	if err := s.GuildMemberMove(b.cfg.GuildID, v.UserID, &ch); err != nil {
		log.Printf("move to room: %v", err)
	}
}

// sweepRoom deletes a room once nobody is in it. New rooms get a moment for
// their owner to join, unless force is set.
func (b *bot) sweepRoom(channelID string, force bool) {
	var r room
	var ok bool
	b.store.view(func(d *storeData) { r, ok = d.Rooms[channelID] })
	if !ok {
		return
	}
	if b.voiceCount(channelID) > 0 {
		b.empty.clear(channelID)
		return
	}
	if !force {
		if time.Now().Before(r.KeepUntil) {
			return
		}
		if !r.Used {
			if time.Since(r.Created) < roomGrace {
				return
			}
		} else if left := roomEmptyWait - b.empty.mark(channelID, time.Now()); left > 0 {
			time.AfterFunc(left+time.Second, func() { b.sweepRoom(channelID, false) })
			return
		}
	}
	b.empty.clear(channelID)
	if _, err := b.s.ChannelDelete(channelID, discordgo.WithAuditLogReason("Voice room empty")); err != nil {
		if rest, isRest := err.(*discordgo.RESTError); !isRest || rest.Response == nil || rest.Response.StatusCode != 404 {
			log.Printf("delete room: %v", err)
			return
		}
	}
	b.store.update(func(d *storeData) { delete(d.Rooms, channelID) })
}

// markRoomUsed notes that someone joined a room, so it goes as soon as it's
// empty again.
func (b *bot) markRoomUsed(channelID string) {
	var r room
	var ok bool
	b.store.view(func(d *storeData) { r, ok = d.Rooms[channelID] })
	if !ok || r.Used {
		return
	}
	b.store.update(func(d *storeData) {
		r.Used = true
		d.Rooms[channelID] = r
	})
}

func (b *bot) voiceCount(channelID string) int {
	g, err := b.s.State.Guild(b.cfg.GuildID)
	if err != nil {
		return 1 // unknown; keep the room
	}
	b.s.State.RLock()
	defer b.s.State.RUnlock()
	n := 0
	for _, vs := range g.VoiceStates {
		if vs.ChannelID == channelID {
			n++
		}
	}
	return n
}

// roomLoop clears rooms left empty, also ones from before a restart.
func (b *bot) roomLoop() {
	for {
		time.Sleep(time.Minute)
		var ids []string
		b.store.view(func(d *storeData) {
			for id := range d.Rooms {
				ids = append(ids, id)
			}
		})
		for _, id := range ids {
			b.sweepRoom(id, false)
		}
	}
}

func (b *bot) roomCommand(i *discordgo.InteractionCreate) {
	o := optionMap(i)
	name, limit, privacy := "", -1, ""
	if v, ok := o["name"]; ok {
		name = strings.TrimSpace(v.StringValue())
	}
	if v, ok := o["limit"]; ok {
		limit = int(v.IntValue())
	}
	if v, ok := o["privacy"]; ok {
		privacy = v.StringValue()
	}
	owner := i.Member.User.ID
	if ch := b.ownedRoom(owner); ch != "" {
		// A map, so a limit of 0 (no limit) is still sent.
		edit := map[string]any{}
		if name != "" {
			edit["name"] = name
		}
		if limit >= 0 {
			edit["user_limit"] = limit
		}
		if privacy != "" {
			ow := b.overwrites(owner, privacy == "private")
			// Keep members already invited.
			if c, err := b.s.State.Channel(ch); err == nil {
				for _, p := range c.PermissionOverwrites {
					if p.Type == discordgo.PermissionOverwriteTypeMember && p.ID != owner && p.ID != b.s.State.User.ID {
						ow = append(ow, p)
					}
				}
			}
			edit["permission_overwrites"] = ow
		}
		if len(edit) == 0 {
			b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Your room is <#%s>. Pass a privacy, name or limit to change it.", ch)})
			return
		}
		if _, err := b.s.RequestWithBucketID("PATCH", discordgo.EndpointChannel(ch), edit, discordgo.EndpointChannel(ch)); err != nil {
			log.Printf("edit room: %v", err)
			b.respond(i, true, errorEmbed("Couldn't change your room."))
			return
		}
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Updated <#%s>.", ch)})
		return
	}
	c, err := b.createRoom(i.Member, name, privacy == "private", max(limit, 0))
	if err != nil {
		log.Printf("create room: %v", err)
		b.respond(i, true, errorEmbed("Couldn't make the room. The bot needs **Manage Channels** and **Move Members**."))
		return
	}
	text := fmt.Sprintf("Made <#%s>. ", c.ID)
	if vs, err := b.s.State.VoiceState(b.cfg.GuildID, owner); err == nil && vs.ChannelID != "" {
		_ = b.s.GuildMemberMove(b.cfg.GuildID, owner, &c.ID)
		text += "Moved you in."
	} else {
		text += "Join it within 2 minutes or it's removed."
	}
	if privacy == "private" {
		text += " Only you can see it; use `/room-invite` to let people in."
	}
	text += " It's deleted once everyone leaves."
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: text})
}

func (b *bot) roomInvite(i *discordgo.InteractionCreate) {
	ch := b.ownedRoom(i.Member.User.ID)
	if ch == "" {
		b.respond(i, true, errorEmbed("You don't have a room. Make one with `/room`."))
		return
	}
	user := optUser(i, optionMap(i)["member"])
	if user.Bot || user.ID == i.Member.User.ID {
		b.respond(i, true, errorEmbed("Pick another member."))
		return
	}
	if err := b.s.ChannelPermissionSet(ch, user.ID, discordgo.PermissionOverwriteTypeMember, roomGuest, 0); err != nil {
		log.Printf("room invite: %v", err)
		b.respond(i, true, errorEmbed("Couldn't add them."))
		return
	}
	b.respond(i, false, &discordgo.MessageEmbed{Color: shu,
		Description: fmt.Sprintf("<@%s>, <@%s> invited you to <#%s>.", user.ID, i.Member.User.ID, ch)})
}
