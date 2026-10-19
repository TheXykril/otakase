package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Join verification: while it is on, new members get the "Unverified" role
// and can only see the verify channel. Its card has an "I'm human" button
// that opens a small check (press the picture that matches a word). Passing
// removes the role; three wrong answers or 30 minutes without passing get
// the member kicked, with a DM saying they can rejoin later.
//
// /verification sets the mode: off, on, or auto (the default), which turns
// on by itself when the raid guard sees a burst of joins and off again
// after an hour without one. Turning it off lets everyone still waiting in.
//
// What it changes in the server, all tied to the Unverified role so it is
// easy to undo (deleting the role removes every overwrite below with it):
//   - creates the "Unverified" role (no permissions) if there isn't one;
//   - creates a "verify" text channel if there isn't one, hidden from
//     @everyone, visible but read-only for Unverified, visible for staff;
//   - adds one permission overwrite to every other channel and category:
//     Unverified is denied View Channel. Nothing else on those channels is
//     touched, and categories with synced channels stay in sync.
// Overwrites are added when verification turns on and checked again every
// 10 minutes while it's on, for channels made since. They stay in place
// while it's off; with nobody holding the role they do nothing.

const (
	verifyCalm     = time.Hour        // auto mode turns off after this long without a burst of joins
	verifyDeadline = 30 * time.Minute // time to pass the check
	verifyTries    = 3
	verifyRoleName = "Unverified"
	verifyChanName = "証・verify"
)

type verifyData struct {
	Mode    string                   `json:"mode,omitempty"`   // "on", "off", "" = auto
	Raid    time.Time                `json:"raid,omitzero"`    // last burst of joins, for auto
	Active  bool                     `json:"active,omitempty"` // on at the last check
	Role    string                   `json:"role,omitempty"`
	Channel string                   `json:"channel,omitempty"`
	Pending map[string]verifyPending `json:"pending,omitempty"` // user id → waiting member
}

type verifyPending struct {
	Joined time.Time `json:"joined"`
	Tries  int       `json:"tries,omitempty"`
	Answer string    `json:"answer,omitempty"`
}

func verifyOf(d *storeData) *verifyData {
	if d.Verify == nil {
		d.Verify = &verifyData{}
	}
	if d.Verify.Pending == nil {
		d.Verify.Pending = map[string]verifyPending{}
	}
	return d.Verify
}

// verifyActive reports whether new members have to pass the check.
func verifyActive(v *verifyData, now time.Time) bool {
	switch v.Mode {
	case "on":
		return true
	case "off":
		return false
	}
	return !v.Raid.IsZero() && now.Sub(v.Raid) < verifyCalm
}

func modeName(mode string) string {
	if mode == "" {
		return "auto"
	}
	return mode
}

// verifyRuntime is what verification keeps in memory: recent joins, for
// spotting a burst and catching the members who came just before it, and
// counts for the staff summary.
type verifyRuntime struct {
	mu      sync.Mutex
	joins   []joinRec
	passed  int
	failed  []string
	expired []string

	setupMu sync.Mutex
	ready   bool
	owMu    sync.Mutex
}

type joinRec struct {
	user string
	at   time.Time
}

// join records a join and reports whether joins are coming as fast as the
// raid guard's limit.
func (r *verifyRuntime) join(user string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	recent := r.joins[:0]
	for _, j := range r.joins {
		if now.Sub(j.at) < 5*time.Minute {
			recent = append(recent, j)
		}
	}
	r.joins = append(recent, joinRec{user, now})
	n := 0
	for _, j := range r.joins {
		if now.Sub(j.at) < raidWindow {
			n++
		}
	}
	return n >= raidJoins
}

// recent lists members who joined within d.
func (r *verifyRuntime) recent(now time.Time, d time.Duration) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []string
	for _, j := range r.joins {
		if now.Sub(j.at) < d {
			ids = append(ids, j.user)
		}
	}
	return ids
}

// ── The check ──

var verifyPics = []struct{ Emoji, Word string }{
	{"🐱", "cat"}, {"🐶", "dog"}, {"🍎", "apple"}, {"🚗", "car"}, {"🌙", "moon"}, {"🐟", "fish"},
	{"🌳", "tree"}, {"🍕", "pizza"}, {"⚽", "ball"}, {"🏠", "house"}, {"⭐", "star"}, {"🎸", "guitar"},
}

func verifyEmoji(word string) string {
	for _, p := range verifyPics {
		if p.Word == word {
			return p.Emoji
		}
	}
	return ""
}

// newChallenge picks the word to match and four shuffled choices with it.
func newChallenge(r *rand.Rand) (answer string, choices []string) {
	perm := r.Perm(len(verifyPics))[:4]
	for _, n := range perm {
		choices = append(choices, verifyPics[n].Word)
	}
	return choices[r.IntN(4)], choices
}

func challengeMessage(answer string, choices []string, note string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	var row discordgo.ActionsRow
	for _, w := range choices {
		row.Components = append(row.Components, discordgo.Button{Style: discordgo.SecondaryButton,
			Emoji: &discordgo.ComponentEmoji{Name: verifyEmoji(w)}, CustomID: "verify-pick:" + w})
	}
	desc := fmt.Sprintf("Press the picture that shows: **%s**", answer)
	if note != "" {
		desc = note + "\n" + desc
	}
	return &discordgo.MessageEmbed{Title: "Quick check", Description: desc, Color: shu}, []discordgo.MessageComponent{row}
}

// ── Command ──

func verificationCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{Name: "verification", Description: "Make new members pass a quick check before they see the server",
		DefaultMemberPermissions: &permMod,
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "mode", Description: "auto turns on during a raid and off after an hour of calm", Required: true,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "auto (on during raids)", Value: "auto"}, {Name: "on", Value: "on"}, {Name: "off", Value: "off"},
				}},
		}}
}

func (b *bot) verification(i *discordgo.InteractionCreate) {
	mode := optionMap(i)["mode"].StringValue()
	if mode == "auto" {
		mode = ""
	}
	mod := i.Member.User.ID
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		text, err := b.setVerifyMode(mode, fmt.Sprintf("<@%s>", mod))
		if err != nil {
			return errorEmbed("Couldn't set it up: %v. The bot needs **Manage Roles** and **Manage Channels**.", err), nil
		}
		return &discordgo.MessageEmbed{Color: shu, Description: text}, nil
	})
}

// setVerifyMode switches join verification to mode ("" is auto), sets it up
// or lets everyone waiting in, and tells the mod log who changed it. The
// text says what happened, in Discord markdown.
func (b *bot) setVerifyMode(mode, who string) (string, error) {
	now := time.Now()
	var active bool
	b.store.update(func(d *storeData) {
		v := verifyOf(d)
		v.Mode = mode
		active = verifyActive(v, now)
	})
	var text string
	if active {
		role, ch, err := b.verifySetup()
		if err != nil {
			log.Printf("verification setup: %v", err)
			return "", err
		}
		go b.verifyOverwrites(role, ch)
		text = fmt.Sprintf("Join verification is **on**. New members get the Unverified role and only see <#%s> until they pass the check.", ch)
		b.store.update(func(d *storeData) { verifyOf(d).Active = true })
	} else {
		released := b.verifyRelease()
		text = "Join verification is **" + modeName(mode) + "**."
		if mode == "" {
			text += " It turns on by itself when lots of members join at once, and off again after an hour of calm."
		}
		if released > 0 {
			text += fmt.Sprintf(" Let in %d member(s) who were still waiting.", released)
		}
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276,
		Description: fmt.Sprintf("%s set join verification to **%s**.", who, modeName(mode))}, nil, false)
	return text, nil
}

// ── Setup ──

// verifySetup makes sure the role, the verify channel and its card exist,
// and starts adding the channel overwrites. It returns the role and channel.
func (b *bot) verifySetup() (role, channel string, err error) {
	r := &b.verify
	r.setupMu.Lock()
	defer r.setupMu.Unlock()
	b.store.view(func(d *storeData) {
		if d.Verify != nil {
			role, channel = d.Verify.Role, d.Verify.Channel
		}
	})
	if r.ready && role != "" && channel != "" {
		return role, channel, nil
	}
	roles, err := b.s.GuildRoles(b.cfg.GuildID)
	if err != nil {
		return "", "", err
	}
	found := false
	for _, x := range roles {
		if x.ID == role || (!found && strings.EqualFold(x.Name, verifyRoleName)) {
			role, found = x.ID, true
		}
	}
	if !found {
		none, no := int64(0), false
		x, err := b.s.GuildRoleCreate(b.cfg.GuildID, &discordgo.RoleParams{Name: verifyRoleName, Permissions: &none, Hoist: &no, Mentionable: &no},
			discordgo.WithAuditLogReason("Join verification"))
		if err != nil {
			return "", "", err
		}
		role = x.ID
	}
	chans, err := b.s.GuildChannels(b.cfg.GuildID)
	if err != nil {
		return "", "", err
	}
	found = false
	for _, c := range chans {
		if c.ID == channel || (!found && c.Type == discordgo.ChannelTypeGuildText && strings.EqualFold(channelBase(c.Name), "verify")) {
			channel, found = c.ID, true
		}
	}
	if !found {
		c, err := b.s.GuildChannelCreateComplex(b.cfg.GuildID, discordgo.GuildChannelCreateData{
			Name: verifyChanName, Type: discordgo.ChannelTypeGuildText, Position: 0,
			Topic:                "New here? Press I'm human below to get into the server.",
			PermissionOverwrites: verifyChannelOverwrites(b.cfg.GuildID, b.s.State.User.ID, role, b.cfg.StaffRoles),
		}, discordgo.WithAuditLogReason("Join verification"))
		if err != nil {
			return "", "", err
		}
		channel = c.ID
	}
	b.verifyCard(channel)
	b.store.update(func(d *storeData) {
		v := verifyOf(d)
		v.Role, v.Channel = role, channel
	})
	r.ready = true
	go b.verifyOverwrites(role, channel)
	return role, channel, nil
}

const (
	verifyReadOnly = discordgo.PermissionSendMessages | discordgo.PermissionAddReactions | discordgo.PermissionCreatePublicThreads |
		discordgo.PermissionCreatePrivateThreads | discordgo.PermissionSendMessagesInThreads
	verifyBotAllow = discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionEmbedLinks |
		discordgo.PermissionReadMessageHistory
)

// verifyChannelOverwrites hides the verify channel from @everyone and shows
// it, read-only, to Unverified; the bot and staff can see it too.
func verifyChannelOverwrites(guildID, botID, role string, staff []string) []*discordgo.PermissionOverwrite {
	o := []*discordgo.PermissionOverwrite{
		{ID: guildID, Type: discordgo.PermissionOverwriteTypeRole, Deny: discordgo.PermissionViewChannel},
		{ID: role, Type: discordgo.PermissionOverwriteTypeRole, Allow: discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory, Deny: verifyReadOnly},
		{ID: botID, Type: discordgo.PermissionOverwriteTypeMember, Allow: verifyBotAllow},
	}
	for _, r := range staff {
		o = append(o, &discordgo.PermissionOverwrite{ID: r, Type: discordgo.PermissionOverwriteTypeRole,
			Allow: discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory})
	}
	return o
}

// needsVerifyDeny reports whether a channel still lets Unverified see it.
func needsVerifyDeny(c *discordgo.Channel, role, verifyChannel string) bool {
	if c.ID == verifyChannel || c.IsThread() {
		return false
	}
	for _, o := range c.PermissionOverwrites {
		if o.ID == role && o.Deny&discordgo.PermissionViewChannel != 0 {
			return false
		}
	}
	return true
}

// verifyOverwrites denies Unverified View Channel on every channel but the
// verify one, keeping any other bits an existing overwrite for the role has.
func (b *bot) verifyOverwrites(role, verifyChannel string) {
	r := &b.verify
	if !r.owMu.TryLock() {
		return // already running
	}
	defer r.owMu.Unlock()
	chans, err := b.s.GuildChannels(b.cfg.GuildID)
	if err != nil {
		b.jobFailed("Join verification", err)
		return
	}
	changed := 0
	var failed []string
	for _, c := range chans {
		if !needsVerifyDeny(c, role, verifyChannel) {
			continue
		}
		var allow, deny int64
		for _, o := range c.PermissionOverwrites {
			if o.ID == role {
				allow, deny = o.Allow, o.Deny
			}
		}
		if err := b.s.ChannelPermissionSet(c.ID, role, discordgo.PermissionOverwriteTypeRole, allow&^discordgo.PermissionViewChannel,
			deny|discordgo.PermissionViewChannel, discordgo.WithAuditLogReason("Join verification: hide from Unverified")); err != nil {
			log.Printf("verification: hiding %s: %v", c.Name, err)
			failed = append(failed, c.Name)
			continue
		}
		changed++
	}
	if changed > 0 {
		log.Printf("verification: hid %d channel(s) from Unverified", changed)
	}
	if len(failed) > 0 {
		b.jobFailed("Join verification", fmt.Errorf("couldn't hide these channels from Unverified (the bot can't see or manage them): %s",
			strings.Join(failed, ", ")))
		return
	}
	b.jobOK("Join verification")
}

// verifyCard posts the "I'm human" card unless the bot's card is there.
func (b *bot) verifyCard(channel string) {
	if msgs, err := b.s.ChannelMessages(channel, 20, "", "", ""); err == nil {
		for _, m := range msgs {
			if m.Author != nil && m.Author.ID == b.s.State.User.ID && len(m.Components) > 0 {
				return
			}
		}
	}
	if _, err := b.s.ChannelMessageSendComplex(channel, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{Title: "Welcome to Otakase", Color: shu,
			Description: "Before you can see the rest of the server, press **I'm human** and pick the matching picture. It takes a few seconds.\n\n" +
				fmt.Sprintf("You have %d minutes and %d tries.", int(verifyDeadline.Minutes()), verifyTries)}},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "I'm human", Style: discordgo.SuccessButton, CustomID: "verify-start"},
		}}},
	}); err != nil {
		log.Printf("verify card: %v", err)
	}
}

// ── Joins ──

// verifyJoin runs for every member who joins: it notices a burst of joins
// in auto mode and holds new members at the check while verification is on.
func (b *bot) verifyJoin(userID string, now time.Time) {
	busy := b.verify.join(userID, now)
	var active, started bool
	var mode string
	b.store.update(func(d *storeData) {
		v := verifyOf(d)
		if busy && v.Mode == "" {
			v.Raid = now
		}
		active = verifyActive(v, now)
		started = active && !v.Active
		v.Active = active || v.Active
		mode = v.Mode
	})
	if !active {
		return
	}
	role, channel, err := b.verifySetup()
	if err != nil {
		b.jobFailed("Join verification", err)
		return
	}
	users := []string{userID}
	if started {
		// Hold the members who joined just before the burst was noticed too.
		users = b.verify.recent(now, 2*raidWindow)
		if mode == "" {
			b.modLog(&discordgo.MessageEmbed{Title: "Join verification is on", Color: shu,
				Description: fmt.Sprintf("Lots of members joined at once, so new members now get the Unverified role and have to pass a check in <#%s> within %d minutes. "+
					"It turns off after an hour of calm, or use `/verification`.", channel, int(verifyDeadline.Minutes()))}, nil, false)
		}
	}
	for _, u := range users {
		b.holdMember(u, role, now)
	}
}

func (b *bot) holdMember(userID, role string, now time.Time) {
	held := false
	b.store.update(func(d *storeData) {
		v := verifyOf(d)
		if _, ok := v.Pending[userID]; ok {
			held = true
			return
		}
		v.Pending[userID] = verifyPending{Joined: now.UTC()}
	})
	if held {
		return
	}
	if err := b.s.GuildMemberRoleAdd(b.cfg.GuildID, userID, role, discordgo.WithAuditLogReason("Join verification")); err != nil {
		log.Printf("verify role: %v", err)
		b.store.update(func(d *storeData) { delete(verifyOf(d).Pending, userID) })
	}
}

// verifyRelease removes Unverified from everyone still waiting (and anyone
// else who has it) and reports how many were let in.
func (b *bot) verifyRelease() int {
	var role string
	var users []string
	b.store.update(func(d *storeData) {
		v := verifyOf(d)
		role = v.Role
		for u := range v.Pending {
			users = append(users, u)
		}
		v.Pending = map[string]verifyPending{}
		v.Active = false
	})
	if role == "" {
		return 0
	}
	seen := map[string]bool{}
	for _, u := range users {
		seen[u] = true
	}
	// Members can hold the role without a record (a lost data file, or
	// staff gave it by hand), so look through the member list too.
	after := ""
	for page := 0; page < 20; page++ {
		ms, err := b.s.GuildMembers(b.cfg.GuildID, after, 1000)
		if err != nil {
			log.Printf("verify release: %v", err)
			break
		}
		for _, m := range ms {
			if contains(m.Roles, role) && !seen[m.User.ID] {
				users = append(users, m.User.ID)
				seen[m.User.ID] = true
			}
		}
		if len(ms) < 1000 {
			break
		}
		after = ms[len(ms)-1].User.ID
	}
	n := 0
	for _, u := range users {
		if err := b.s.GuildMemberRoleRemove(b.cfg.GuildID, u, role, discordgo.WithAuditLogReason("Join verification turned off")); err == nil {
			n++
		}
	}
	return n
}

// ── Buttons ──

func (b *bot) verifyButton(i *discordgo.InteractionCreate, id string) {
	user := i.Member.User.ID
	now := time.Now().UTC()
	var role string
	var p verifyPending
	waiting := false
	b.store.view(func(d *storeData) {
		if d.Verify != nil {
			role = d.Verify.Role
			p, waiting = d.Verify.Pending[user]
		}
	})
	if !waiting && role != "" && contains(i.Member.Roles, role) {
		// Has the role but no record: start one now.
		p, waiting = verifyPending{Joined: now}, true
	}
	if !waiting {
		msg := &discordgo.MessageEmbed{Color: shu, Description: "You're already verified. Enjoy the server!"}
		if id == "verify-start" {
			b.respond(i, true, msg)
		} else {
			b.updateMessage(i, msg, nil)
		}
		return
	}
	rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	if id == "verify-start" {
		answer, choices := newChallenge(rng)
		b.store.update(func(d *storeData) {
			p.Answer = answer
			verifyOf(d).Pending[user] = p
		})
		e, comps := challengeMessage(answer, choices, "")
		b.respond(i, true, e, comps...)
		return
	}
	pick := strings.TrimPrefix(id, "verify-pick:")
	if p.Answer != "" && pick == p.Answer {
		b.store.update(func(d *storeData) { delete(verifyOf(d).Pending, user) })
		if err := b.s.GuildMemberRoleRemove(b.cfg.GuildID, user, role, discordgo.WithAuditLogReason("Passed join verification")); err != nil {
			log.Printf("verify pass: %v", err)
			b.updateMessage(i, errorEmbed("That's right, but the bot couldn't let you in. Please ask a mod."), nil)
			return
		}
		b.verify.mu.Lock()
		b.verify.passed++
		b.verify.mu.Unlock()
		b.updateMessage(i, &discordgo.MessageEmbed{Color: shu, Description: "You're in. Welcome to Otakase! The rest of the server shows up in a moment."}, nil)
		return
	}
	p.Tries++
	if p.Tries >= verifyTries {
		b.store.update(func(d *storeData) { delete(verifyOf(d).Pending, user) })
		b.updateMessage(i, errorEmbed("That wasn't right three times, so you'll be removed from the server. You can rejoin later and try again."), nil)
		b.verifyKick(user, "Failed join verification")
		b.verify.mu.Lock()
		b.verify.failed = append(b.verify.failed, user)
		b.verify.mu.Unlock()
		return
	}
	answer, choices := newChallenge(rng)
	p.Answer = answer
	b.store.update(func(d *storeData) { verifyOf(d).Pending[user] = p })
	left := verifyTries - p.Tries
	e, comps := challengeMessage(answer, choices, fmt.Sprintf("Not quite. %d %s left.", left, map[bool]string{true: "try", false: "tries"}[left == 1]))
	b.updateMessage(i, e, comps)
}

func (b *bot) updateMessage(i *discordgo.InteractionCreate, e *discordgo.MessageEmbed, comps []discordgo.MessageComponent) {
	if comps == nil {
		comps = []discordgo.MessageComponent{}
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps},
	}); err != nil {
		log.Printf("verify update: %v", err)
	}
}

// verifyKick DMs the member that they can rejoin, then kicks them.
func (b *bot) verifyKick(userID, reason string) {
	b.dm(userID, &discordgo.MessageEmbed{Color: shu, Description: "You were removed from the Otakase server because the join check wasn't passed. " +
		"No worries: you can rejoin later with an invite link and try again."})
	if err := b.s.GuildMemberDeleteWithReason(b.cfg.GuildID, userID, reason); err != nil {
		log.Printf("verify kick: %v", err)
	}
}

// ── Loop ──

// verifyExpired lists the members who ran out of time.
func verifyExpired(pending map[string]verifyPending, now time.Time) []string {
	var ids []string
	for u, p := range pending {
		if now.Sub(p.Joined) > verifyDeadline {
			ids = append(ids, u)
		}
	}
	return ids
}

// verifyLoop kicks members who didn't pass in time, turns auto mode off
// after an hour of calm, keeps the overwrites up to date and sends staff a
// short summary every 10 minutes when something happened.
func (b *bot) verifyLoop() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	n := 0
	for range tick.C {
		n++
		now := time.Now()
		var wasActive, active bool
		var mode string
		var expired []string
		b.store.view(func(d *storeData) {
			if v := d.Verify; v != nil {
				wasActive, active, mode = v.Active, verifyActive(v, now), v.Mode
				if active {
					expired = verifyExpired(v.Pending, now)
				}
			}
		})
		if len(expired) > 0 {
			b.store.update(func(d *storeData) {
				for _, u := range expired {
					delete(verifyOf(d).Pending, u)
				}
			})
		}
		if wasActive && !active {
			released := b.verifyRelease()
			desc := "Join verification is off again after an hour of calm."
			if mode == "off" {
				desc = "Join verification is off."
			}
			if released > 0 {
				desc += fmt.Sprintf(" Let in %d member(s) who were still waiting.", released)
			}
			b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: desc}, nil, false)
		}
		for _, u := range expired {
			b.verifyKick(u, "Didn't pass join verification in time")
		}
		b.verify.mu.Lock()
		b.verify.expired = append(b.verify.expired, expired...)
		b.verify.mu.Unlock()
		if n%10 != 0 {
			continue
		}
		if active {
			// Check again that the role and channel still exist, and hide
			// channels made since.
			b.verify.setupMu.Lock()
			b.verify.ready = false
			b.verify.setupMu.Unlock()
			if role, ch, err := b.verifySetup(); err != nil {
				b.jobFailed("Join verification", err)
			} else {
				b.verifyOverwrites(role, ch)
			}
		}
		if e := b.verify.summary(); e != nil {
			b.modLog(e, nil, false)
		}
	}
}

// summary empties the counts into a staff note, nil when nothing happened.
func (r *verifyRuntime) summary() *discordgo.MessageEmbed {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.passed == 0 && len(r.failed) == 0 && len(r.expired) == 0 {
		return nil
	}
	e := verifySummary(r.passed, r.failed, r.expired)
	r.passed, r.failed, r.expired = 0, nil, nil
	return e
}

func verifySummary(passed int, failed, expired []string) *discordgo.MessageEmbed {
	mentions := func(ids []string) string {
		if len(ids) > 20 {
			return strings.Join(mentionList(ids[:20]), " ") + fmt.Sprintf(" and %d more", len(ids)-20)
		}
		return strings.Join(mentionList(ids), " ")
	}
	desc := fmt.Sprintf("%d passed the check.", passed)
	if len(failed) > 0 {
		desc += fmt.Sprintf("\n%d removed after %d wrong answers: %s", len(failed), verifyTries, mentions(failed))
	}
	if len(expired) > 0 {
		desc += fmt.Sprintf("\n%d removed for not verifying within %d minutes: %s", len(expired), int(verifyDeadline.Minutes()), mentions(expired))
	}
	return &discordgo.MessageEmbed{Title: "Join verification", Color: 0x8A8276, Description: desc}
}

func mentionList(ids []string) []string {
	out := make([]string, len(ids))
	for n, id := range ids {
		out[n] = "<@" + id + ">"
	}
	return out
}
