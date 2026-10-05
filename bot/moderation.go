package main

import (
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Moderation: reports, warnings, raid guard, new-account checks, the scam
// filter and /purge and /slowmode.

const (
	warnLimit     = 3 // every third warning times the member out
	newAccountAge = 7 * 24 * time.Hour
	newMemberAge  = 24 * time.Hour
	raidJoins     = 10
	raidWindow    = time.Minute
	invitePause   = 30 * time.Minute
)

var (
	permMod   = int64(discordgo.PermissionModerateMembers)
	permPurge = int64(discordgo.PermissionManageMessages)
	permSlow  = int64(discordgo.PermissionManageChannels)
)

func modCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{Type: discordgo.MessageApplicationCommand, Name: "Report to mods"},
		{Name: "warn", Description: "Warn a member (every third warning is a 1 hour timeout)", DefaultMemberPermissions: &permMod,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Who", Required: true},
				{Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Why (sent to them)", Required: true, MaxLength: 300},
			}},
		{Name: "warnings", Description: "A member's warnings", DefaultMemberPermissions: &permMod,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Who", Required: true},
				{Type: discordgo.ApplicationCommandOptionBoolean, Name: "clear", Description: "Remove all their warnings"},
			}},
		{Name: "purge", Description: "Delete recent messages in this channel", DefaultMemberPermissions: &permPurge,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "count", Description: "How many to look through (1-100)", Required: true, MinValue: &one, MaxValue: 100},
				{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Only this member's messages"},
			}},
		{Name: "slowmode", Description: "Set slowmode (0 turns it off)", DefaultMemberPermissions: &permSlow,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "seconds", Description: "Seconds between messages", Required: true, MinValue: &zero, MaxValue: 21600},
				{Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: "Default this channel",
					ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildForum, discordgo.ChannelTypeGuildVoice}},
			}},
	}
}

var one = 1.0

// modLog posts to mod-chat.
func (b *bot) modLog(e *discordgo.MessageEmbed, comps []discordgo.MessageComponent, pingStaff bool) {
	if b.cfg.ModChannel == "" {
		return
	}
	msg := &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e}, Components: comps,
		AllowedMentions: &discordgo.MessageAllowedMentions{}}
	if pingStaff {
		var pings []string
		for _, r := range b.cfg.StaffRoles {
			pings = append(pings, "<@&"+r+">")
		}
		msg.Content = strings.Join(pings, " ")
		msg.AllowedMentions.Roles = b.cfg.StaffRoles
	}
	if _, err := b.s.ChannelMessageSendComplex(b.cfg.ModChannel, msg); err != nil {
		log.Printf("mod log: %v", err)
	}
}

func (b *bot) timeout(userID string, d time.Duration, reason string) error {
	until := time.Now().Add(d)
	return b.s.GuildMemberTimeout(b.cfg.GuildID, userID, &until, discordgo.WithAuditLogReason(reason))
}

// isStaff reports whether a message's author can manage messages there.
func (b *bot) isStaff(m *discordgo.MessageCreate) bool {
	if m.Member == nil {
		return false
	}
	m.Member.User = m.Author
	p, err := b.s.State.MessagePermissions(m.Message)
	return err == nil && p&(discordgo.PermissionManageMessages|discordgo.PermissionAdministrator) != 0
}

func messageLink(guild, channel, msg string) string {
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guild, channel, msg)
}

// ── Report to mods ──

func (b *bot) reportMessage(i *discordgo.InteractionCreate) {
	d := i.ApplicationCommandData()
	msg := d.Resolved.Messages[d.TargetID]
	if msg == nil || msg.Author == nil {
		b.respond(i, true, errorEmbed("Couldn't read that message."))
		return
	}
	reporter := i.Member.User.ID
	if msg.Author.Bot || msg.Author.ID == reporter {
		b.respond(i, true, errorEmbed("That message can't be reported."))
		return
	}
	if !b.issues.allowKey("report/" + msg.ID) {
		b.respond(i, true, &discordgo.MessageEmbed{Description: "Someone already reported this. The mods are on it.", Color: shu})
		return
	}
	content := msg.Content
	if content == "" {
		content = "(no text)"
	}
	if n := len(msg.Attachments); n > 0 {
		content += fmt.Sprintf("\n+ %d attachment(s)", n)
	}
	e := &discordgo.MessageEmbed{
		Title:       "Message reported",
		URL:         messageLink(b.cfg.GuildID, msg.ChannelID, msg.ID),
		Description: "```\n" + clip(strings.ReplaceAll(content, "```", "'''"), 1500) + "\n```",
		Color:       shu,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Author", Value: "<@" + msg.Author.ID + ">", Inline: true},
			{Name: "Channel", Value: "<#" + msg.ChannelID + ">", Inline: true},
			{Name: "Reported by", Value: "<@" + reporter + ">", Inline: true},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
	b.modLog(e, []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Delete message", Style: discordgo.DangerButton, CustomID: "mod-del:" + msg.ChannelID + ":" + msg.ID},
		discordgo.Button{Label: "Timeout 1h", Style: discordgo.DangerButton, CustomID: "mod-to:" + msg.Author.ID},
		discordgo.Button{Label: "Dismiss", Style: discordgo.SecondaryButton, CustomID: "mod-dismiss"},
	}}}, false)
	b.respond(i, true, &discordgo.MessageEmbed{Description: "Sent to the mods. Thanks for keeping the server nice.", Color: shu})
}

// modAction handles the buttons on a report.
func (b *bot) modAction(i *discordgo.InteractionCreate, id string) {
	if i.Member.Permissions&(discordgo.PermissionManageMessages|discordgo.PermissionModerateMembers|discordgo.PermissionAdministrator) == 0 {
		b.respond(i, true, errorEmbed("Only moderators can do that."))
		return
	}
	mod := i.Member.User.ID
	var outcome string
	switch {
	case strings.HasPrefix(id, "mod-del:"):
		parts := strings.Split(id, ":")
		if len(parts) != 3 {
			return
		}
		if err := b.s.ChannelMessageDelete(parts[1], parts[2], discordgo.WithAuditLogReason("Reported message")); err != nil {
			outcome = "Message was already gone"
		} else {
			outcome = "Message deleted"
		}
	case strings.HasPrefix(id, "mod-to:"):
		if err := b.timeout(strings.TrimPrefix(id, "mod-to:"), time.Hour, "Reported message"); err != nil {
			log.Printf("report timeout: %v", err)
			b.respond(i, true, errorEmbed("Couldn't time them out. The bot needs **Timeout Members** and a role above theirs."))
			return
		}
		outcome = "Timed out for 1 hour"
	default:
		outcome = "Dismissed"
	}
	if len(i.Message.Embeds) == 0 {
		return
	}
	e := i.Message.Embeds[0]
	e.Color = 0x8A8276
	e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Handled", Value: fmt.Sprintf("%s by <@%s>", outcome, mod)})
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: []discordgo.MessageComponent{}},
	}); err != nil {
		log.Printf("mod action: %v", err)
	}
}

// ── Warnings ──

func optionMap(i *discordgo.InteractionCreate) map[string]*discordgo.ApplicationCommandInteractionDataOption {
	m := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
	for _, o := range i.ApplicationCommandData().Options {
		m[o.Name] = o
	}
	return m
}

// optUser returns a user option with its full details from the resolved data.
func optUser(i *discordgo.InteractionCreate, o *discordgo.ApplicationCommandInteractionDataOption) *discordgo.User {
	u := o.UserValue(nil)
	if r := i.ApplicationCommandData().Resolved; r != nil && r.Users[u.ID] != nil {
		return r.Users[u.ID]
	}
	return u
}

func (b *bot) warn(i *discordgo.InteractionCreate) {
	o := optionMap(i)
	user := optUser(i, o["member"])
	reason := o["reason"].StringValue()
	if user.Bot {
		b.respond(i, true, errorEmbed("Bots can't be warned."))
		return
	}
	var count int
	b.store.update(func(d *storeData) {
		d.Warnings[user.ID] = append(d.Warnings[user.ID], warning{At: time.Now().UTC(), By: i.Member.User.ID, Reason: reason})
		count = len(d.Warnings[user.ID])
	})
	extra := ""
	if count%warnLimit == 0 {
		if err := b.timeout(user.ID, time.Hour, fmt.Sprintf("Warning %d: %s", count, reason)); err != nil {
			log.Printf("warn timeout: %v", err)
			extra = " Couldn't time them out (check the bot's role)."
		} else {
			extra = " That's warning " + strconv.Itoa(count) + ", so they're timed out for 1 hour."
		}
	}
	dm := fmt.Sprintf("You got a warning in the Otakase server: **%s**\nThis is warning %d.", reason, count)
	if count%warnLimit == 0 {
		dm += " You're timed out for 1 hour."
	} else if left := warnLimit - count%warnLimit; left == 1 {
		dm += " One more means a 1 hour timeout."
	}
	if ch, err := b.s.UserChannelCreate(user.ID); err == nil {
		_, _ = b.s.ChannelMessageSendEmbed(ch.ID, &discordgo.MessageEmbed{Description: dm, Color: shu})
	}
	b.modLog(&discordgo.MessageEmbed{Title: "Warning " + strconv.Itoa(count), Color: shu,
		Description: fmt.Sprintf("<@%s> warned by <@%s>: %s", user.ID, i.Member.User.ID, reason)}, nil, false)
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu,
		Description: fmt.Sprintf("Warned <@%s> (warning %d).%s", user.ID, count, extra)})
}

func (b *bot) warnings(i *discordgo.InteractionCreate) {
	o := optionMap(i)
	user := optUser(i, o["member"])
	if c, ok := o["clear"]; ok && c.BoolValue() {
		b.store.update(func(d *storeData) { delete(d.Warnings, user.ID) })
		b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276,
			Description: fmt.Sprintf("<@%s> cleared the warnings of <@%s>.", i.Member.User.ID, user.ID)}, nil, false)
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Cleared <@%s>'s warnings.", user.ID)})
		return
	}
	var list []warning
	b.store.view(func(d *storeData) { list = append(list, d.Warnings[user.ID]...) })
	b.respond(i, true, warningsEmbed(user.ID, list))
}

func warningsEmbed(userID string, list []warning) *discordgo.MessageEmbed {
	if len(list) == 0 {
		return &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("<@%s> has no warnings.", userID)}
	}
	var lines []string
	start := max(0, len(list)-15)
	for n, w := range list[start:] {
		lines = append(lines, fmt.Sprintf("**%d.** <t:%d:d> by <@%s>: %s", start+n+1, w.At.Unix(), w.By, clip(w.Reason, 120)))
	}
	return &discordgo.MessageEmbed{Title: fmt.Sprintf("%d warning(s)", len(list)), Color: shu,
		Description: fmt.Sprintf("<@%s>\n", userID) + strings.Join(lines, "\n")}
}

// ── Raid guard and new accounts ──

type raidGuard struct {
	mu      sync.Mutex
	joins   []time.Time
	alerted time.Time
}

// join records a join and reports whether it starts a raid alert.
func (g *raidGuard) join(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	recent := g.joins[:0]
	for _, t := range g.joins {
		if now.Sub(t) < raidWindow {
			recent = append(recent, t)
		}
	}
	g.joins = append(recent, now)
	if len(g.joins) >= raidJoins && now.Sub(g.alerted) > invitePause {
		g.alerted = now
		return true
	}
	return false
}

func accountAge(userID string, now time.Time) time.Duration {
	t, err := discordgo.SnowflakeTimestamp(userID)
	if err != nil {
		return 0
	}
	return now.Sub(t)
}

func (b *bot) checkJoin(m *discordgo.GuildMemberAdd) {
	now := time.Now()
	if b.raid.join(now) {
		b.raidAlert(now)
	}
	if age := accountAge(m.User.ID, now); age < newAccountAge {
		b.modLog(&discordgo.MessageEmbed{Title: "New account joined", Color: shu,
			Description: fmt.Sprintf("<@%s> (%s), account made %s. Links are blocked for their first day.",
				m.User.ID, m.User.Username, ageText(age))}, nil, false)
	}
}

func ageText(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}

// raidAlert pings the staff and pauses invites for half an hour, which needs
// the Manage Server permission.
func (b *bot) raidAlert(now time.Time) {
	until := now.Add(invitePause)
	_, err := b.s.RequestWithBucketID("PUT", discordgo.EndpointGuild(b.cfg.GuildID)+"/incident-actions",
		map[string]any{"invites_disabled_until": until.UTC().Format(time.RFC3339)}, discordgo.EndpointGuild(b.cfg.GuildID)+"/incident-actions")
	desc := fmt.Sprintf("%d or more members joined within a minute. ", raidJoins)
	if err != nil {
		log.Printf("pause invites: %v", err)
		desc += "Couldn't pause invites (the bot needs **Manage Server**). Pause them from the server menu: **Pause Invites**."
	} else {
		desc += fmt.Sprintf("Invites are paused until <t:%d:t>.", until.Unix())
	}
	b.modLog(&discordgo.MessageEmbed{Title: "Possible raid", Description: desc, Color: shu}, nil, true)
}

var linkRe = regexp.MustCompile(`(?i)https?://|discord(app)?\.(gg|com/invite)/|\bwww\.`)

// newcomerLink reports whether a message is a link from an account younger
// than a week in its first day on the server.
func newcomerLink(m *discordgo.MessageCreate, now time.Time) bool {
	if m.Member == nil || m.Member.JoinedAt.IsZero() || now.Sub(m.Member.JoinedAt) > newMemberAge {
		return false
	}
	return accountAge(m.Author.ID, now) < newAccountAge && linkRe.MatchString(m.Content)
}

// ── Scam filter ──

var (
	urlRe   = regexp.MustCompile(`(?i)\bhttps?://[^\s<>()]+`)
	nitroRe = regexp.MustCompile(`(?i)\b(free|gift|claim|giveaway|airdrop)\b.{0,60}\bnitro\b|\bnitro\b.{0,60}\b(free|gift|claim|giveaway)\b`)
	giftRe  = regexp.MustCompile(`(?i)\b(steam|cs2|csgo|skins?)\b.{0,60}\b(gift|free|giveaway|claim)\b|\b(gift|free|giveaway|claim)\b.{0,60}\b(steam|cs2|csgo|skins?)\b`)
	everyRe = regexp.MustCompile(`@(everyone|here)`)
	lureRe  = regexp.MustCompile(`(?i)gift|nitro|promo|free|claim|airdrop|drop|giveaway|trade`)
	// Lookalike spelling: 1/l for i, 0 for o, rn for m, cl for d, 3 for e, 4 for a.
	skeleton = strings.NewReplacer("1", "i", "l", "i", "0", "o", "rn", "m", "cl", "d", "3", "e", "4", "a", "-", "", ".", "")
)

var realDomains = []string{"discord.com", "discord.gg", "discordapp.com", "discordapp.net", "discord.media",
	"discordstatus.com", "discord.new", "steamcommunity.com", "steampowered.com", "steamstatic.com", "steam.tv"}

func realDomain(host string) bool {
	for _, d := range realDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// scamReason says why a message looks like a scam, or "" when it doesn't.
func scamReason(text string) string {
	var hosts []string
	for _, raw := range urlRe.FindAllString(text, -1) {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			hosts = append(hosts, strings.ToLower(u.Hostname()))
		}
	}
	for _, h := range hosts {
		if realDomain(h) {
			continue
		}
		s := skeleton.Replace(h)
		for _, brand := range []string{"discord", "steamcommunity", "steampowered"} {
			if !strings.Contains(s, brand) {
				continue
			}
			// Spelled to look like the brand, or the brand next to a lure.
			if !strings.Contains(strings.NewReplacer("-", "", ".", "").Replace(h), brand) || lureRe.MatchString(h) {
				return "lookalike link (" + h + ")"
			}
		}
	}
	if len(hosts) == 0 {
		return ""
	}
	offsite := false
	for _, h := range hosts {
		if !realDomain(h) {
			offsite = true
		}
	}
	switch {
	case nitroRe.MatchString(text):
		return "free Nitro link"
	case giftRe.MatchString(text) && offsite:
		return "Steam gift link"
	case everyRe.MatchString(text) && offsite:
		return "@everyone with a link"
	}
	return ""
}

// filterMessage runs the scam filter and the newcomer link block. It
// reports whether the message was removed.
func (b *bot) filterMessage(m *discordgo.MessageCreate) bool {
	if b.isStaff(m) {
		return false
	}
	if reason := scamReason(m.Content); reason != "" {
		_ = b.s.ChannelMessageDelete(m.ChannelID, m.ID, discordgo.WithAuditLogReason("Scam: "+reason))
		action := "Timed out for 1 hour."
		if err := b.timeout(m.Author.ID, time.Hour, "Scam: "+reason); err != nil {
			action = "Couldn't time them out."
		}
		b.modLog(&discordgo.MessageEmbed{Title: "Scam removed", Color: shu,
			Description: fmt.Sprintf("<@%s> in <#%s>: %s. %s\n```\n%s\n```", m.Author.ID, m.ChannelID, reason, action,
				clip(strings.ReplaceAll(m.Content, "```", "'''"), 800))}, nil, false)
		return true
	}
	if newcomerLink(m, time.Now()) {
		_ = b.s.ChannelMessageDelete(m.ChannelID, m.ID, discordgo.WithAuditLogReason("Link from a new account"))
		note, err := b.s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Content:         fmt.Sprintf("<@%s> links unlock after your first day here. Your message was removed, sorry about that.", m.Author.ID),
			AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{m.Author.ID}},
		})
		if err == nil {
			time.AfterFunc(20*time.Second, func() { _ = b.s.ChannelMessageDelete(note.ChannelID, note.ID) })
		}
		b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276,
			Description: fmt.Sprintf("Removed a link from new account <@%s> in <#%s>:\n```\n%s\n```", m.Author.ID, m.ChannelID,
				clip(strings.ReplaceAll(m.Content, "```", "'''"), 500))}, nil, false)
		return true
	}
	return false
}

// ── /purge and /slowmode ──

func (b *bot) purge(i *discordgo.InteractionCreate) {
	o := optionMap(i)
	count := int(o["count"].IntValue())
	var only string
	if u, ok := o["member"]; ok {
		only = u.UserValue(nil).ID
	}
	channel := i.ChannelID
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		msgs, err := b.s.ChannelMessages(channel, count, "", "", "")
		if err != nil {
			return errorEmbed("Couldn't read the channel."), nil
		}
		ids := purgeIDs(msgs, only, time.Now())
		switch len(ids) {
		case 0:
		case 1:
			err = b.s.ChannelMessageDelete(channel, ids[0])
		default:
			err = b.s.ChannelMessagesBulkDelete(channel, ids)
		}
		if err != nil {
			log.Printf("purge: %v", err)
			return errorEmbed("Couldn't delete them. The bot needs **Manage Messages** here."), nil
		}
		who := ""
		if only != "" {
			who = " from <@" + only + ">"
		}
		b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276,
			Description: fmt.Sprintf("<@%s> purged %d message(s)%s in <#%s>.", i.Member.User.ID, len(ids), who, channel)}, nil, false)
		return &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Deleted %d message(s)%s.", len(ids), who)}, nil
	})
}

// purgeIDs picks the messages /purge deletes: optionally one member's, and
// none older than Discord's 14 day bulk delete limit.
func purgeIDs(msgs []*discordgo.Message, only string, now time.Time) []string {
	var ids []string
	for _, m := range msgs {
		if only != "" && (m.Author == nil || m.Author.ID != only) {
			continue
		}
		if now.Sub(m.Timestamp) > 14*24*time.Hour-time.Minute {
			continue
		}
		ids = append(ids, m.ID)
	}
	return ids
}

func (b *bot) slowmode(i *discordgo.InteractionCreate) {
	o := optionMap(i)
	secs := int(o["seconds"].IntValue())
	channel := i.ChannelID
	if c, ok := o["channel"]; ok {
		channel = c.ChannelValue(nil).ID
	}
	if _, err := b.s.ChannelEditComplex(channel, &discordgo.ChannelEdit{RateLimitPerUser: &secs},
		discordgo.WithAuditLogReason("Slowmode by "+i.Member.User.Username)); err != nil {
		log.Printf("slowmode: %v", err)
		b.respond(i, true, errorEmbed("Couldn't change it. The bot needs **Manage Channels**."))
		return
	}
	text := fmt.Sprintf("Slowmode in <#%s> is now %d second(s).", channel, secs)
	if secs == 0 {
		text = fmt.Sprintf("Slowmode in <#%s> is off.", channel)
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: text})
}
