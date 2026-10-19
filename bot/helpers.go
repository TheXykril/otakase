package main

import (
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Helper points: when a support post is marked solved, the person who marked
// it picks who helped, and each of them gets a point (one per post). Staff and
// the poster can also give one with /thank. At cfg.HelperPoints points a
// member gets the Helper role. /helpers shows the top helpers.

type helperStat struct {
	Points int       `json:"points"`
	Last   time.Time `json:"last"`
	// Support posts they got a point for, newest last.
	Threads []string `json:"threads,omitempty"`
}

// helperThreadsKept caps the posts remembered per helper.
const helperThreadsKept = 500

// helperRoleColor is a lighter Shu, so Helpers stand out without looking
// like staff.
const helperRoleColor = 0xE0674F

// awardHelper gives helper a point for thread. No point for helping
// yourself, and one per helper per post. It returns the new total and
// whether a point was given.
func awardHelper(d *storeData, helper, thread, poster string, now time.Time) (int, bool) {
	if helper == "" || helper == poster {
		return 0, false
	}
	if d.Helpers == nil {
		d.Helpers = map[string]helperStat{}
	}
	h := d.Helpers[helper]
	if slices.Contains(h.Threads, thread) {
		return h.Points, false
	}
	h.Points++
	h.Last = now.UTC()
	h.Threads = append(h.Threads, thread)
	if len(h.Threads) > helperThreadsKept {
		h.Threads = h.Threads[len(h.Threads)-helperThreadsKept:]
	}
	d.Helpers[helper] = h
	return h.Points, true
}

type helperRank struct {
	ID     string
	Points int
}

// rankHelpers sorts helpers by points; on a tie whoever got there first
// ranks higher.
func rankHelpers(h map[string]helperStat) []helperRank {
	ids := make([]string, 0, len(h))
	for id, s := range h {
		if s.Points > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(a, b int) bool {
		x, y := h[ids[a]], h[ids[b]]
		if x.Points != y.Points {
			return x.Points > y.Points
		}
		if !x.Last.Equal(y.Last) {
			return x.Last.Before(y.Last)
		}
		return ids[a] < ids[b]
	})
	out := make([]helperRank, len(ids))
	for k, id := range ids {
		out[k] = helperRank{id, h[id].Points}
	}
	return out
}

// rankOf is id's place (1 = top), or 0 without points.
func rankOf(ranks []helperRank, id string) int {
	for k, r := range ranks {
		if r.ID == id {
			return k + 1
		}
	}
	return 0
}

func pointsWord(n int) string {
	if n == 1 {
		return "1 helper point"
	}
	return fmt.Sprintf("%d helper points", n)
}

// postOwner is the member a support post is about: who filled in /support
// for posts the bot made, else whoever made the post.
func (b *bot) postOwner(th *discordgo.Channel) string {
	owner := th.OwnerID
	b.store.view(func(d *storeData) {
		if u, ok := d.FormPosts[th.ID]; ok {
			owner = u
		}
		if r, ok := d.AppReports[th.ID]; ok && r.User != "" {
			owner = r.User
		}
	})
	return owner
}

// canManagePost reports whether the member can act for a support post.
func canManagePost(m *discordgo.Member, owner string) bool {
	return m.User.ID == owner || m.Permissions&(discordgo.PermissionManageThreads|discordgo.PermissionManageMessages) != 0
}

// supportThread returns the support post an interaction happened in.
func (b *bot) supportThread(channelID string) (*discordgo.Channel, bool) {
	if b.cfg.SupportForum == "" {
		return nil, false
	}
	th, err := b.s.State.Channel(channelID)
	if err != nil {
		th, err = b.s.Channel(channelID)
	}
	if err != nil || th.ParentID != b.cfg.SupportForum {
		return nil, false
	}
	return th, true
}

// threadRepliers lists who wrote in a post, oldest first, leaving out bots
// and the given ids. At most 25, the most a select menu holds.
func (b *bot) threadRepliers(threadID string, skip ...string) []*discordgo.User {
	msgs, err := b.s.ChannelMessages(threadID, 100, "", "", "")
	if err != nil {
		log.Printf("thread repliers: %v", err)
		return nil
	}
	return repliers(msgs, skip...)
}

func repliers(msgs []*discordgo.Message, skip ...string) []*discordgo.User {
	seen := map[string]bool{}
	for _, id := range skip {
		seen[id] = true
	}
	var out []*discordgo.User
	// Messages come newest first.
	for k := len(msgs) - 1; k >= 0; k-- {
		u := msgs[k].Author
		if u == nil || u.Bot || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		out = append(out, u)
		if len(out) == 25 {
			break
		}
	}
	return out
}

// whoHelpedPrompt is the private "Who helped you?" message shown after Mark
// solved: a menu of the people who replied, or any member when nobody did.
func whoHelpedPrompt(threadID string, people []*discordgo.User, names map[string]string) *discordgo.WebhookParams {
	var menu discordgo.SelectMenu
	if len(people) > 0 {
		menu = discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "helped:" + threadID,
			Placeholder: "Pick who helped…", MaxValues: len(people)}
		for _, u := range people {
			label := names[u.ID]
			if label == "" {
				label = u.Username
			}
			menu.Options = append(menu.Options, discordgo.SelectMenuOption{Label: clip(label, 100), Value: u.ID, Description: "@" + u.Username})
		}
	} else {
		menu = discordgo.SelectMenu{MenuType: discordgo.UserSelectMenu, CustomID: "helped:" + threadID,
			Placeholder: "Pick who helped…", MaxValues: 5}
	}
	return &discordgo.WebhookParams{
		Flags: discordgo.MessageFlagsEphemeral,
		Embeds: []*discordgo.MessageEmbed{{Title: "Who helped you?", Color: shu,
			Description: "Pick the people who helped. They get a thank-you here and a helper point."}},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{menu}},
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Nobody / skip", Style: discordgo.SecondaryButton, CustomID: "helped-skip:" + threadID},
			}},
		},
	}
}

// askWhoHelped follows a Mark solved press with the private helper picker.
func (b *bot) askWhoHelped(i *discordgo.InteractionCreate, th *discordgo.Channel, owner string) {
	people := b.threadRepliers(th.ID, owner, i.Member.User.ID)
	names := map[string]string{}
	for _, u := range people {
		if m, err := b.s.State.Member(b.cfg.GuildID, u.ID); err == nil {
			names[u.ID] = displayName(m)
		} else if u.GlobalName != "" {
			names[u.ID] = u.GlobalName
		}
	}
	if _, err := b.s.FollowupMessageCreate(i.Interaction, true, whoHelpedPrompt(th.ID, people, names)); err != nil {
		log.Printf("who helped: %v", err)
	}
}

// closeSolved locks and archives a solved post.
func (b *bot) closeSolved(threadID string, tags *[]string) {
	yes := true
	if _, err := b.s.ChannelEditComplex(threadID, &discordgo.ChannelEdit{AppliedTags: tags, Archived: &yes, Locked: &yes}); err != nil {
		log.Printf("solved edit: %v", err)
	}
}

// pickedHelpers handles the "Who helped you?" menu.
func (b *bot) pickedHelpers(i *discordgo.InteractionCreate, threadID string) {
	data := i.MessageComponentData()
	th, err := b.s.Channel(threadID)
	if err != nil || th.ParentID != b.cfg.SupportForum {
		b.updateEphemeral(i, errorEmbed("That post is gone."))
		return
	}
	owner := b.postOwner(th)
	if !canManagePost(i.Member, owner) {
		b.updateEphemeral(i, errorEmbed("Only the person who posted this can pick who helped."))
		return
	}
	var picked []string
	for _, id := range data.Values {
		if u := data.Resolved.Users[id]; u != nil && u.Bot {
			continue
		}
		if id == owner || id == i.Member.User.ID || slices.Contains(picked, id) {
			continue
		}
		picked = append(picked, id)
	}
	if len(picked) == 0 {
		b.updateEphemeral(i, errorEmbed("No point for helping yourself, and bots don't count. Pick someone else, or press **Nobody / skip**."))
		return
	}
	b.thankHelpers(i.Member.User.ID, owner, th, picked)
	b.updateEphemeral(i, &discordgo.MessageEmbed{Color: shu, Description: "Thanks! I let them know."})
}

// thankHelpers gives each helper a point for the post, thanks them in it and
// hands out the Helper role to anyone who reached the threshold. The post is
// reopened for the message and closed again.
func (b *bot) thankHelpers(by, owner string, th *discordgo.Channel, helpers []string) {
	totals := map[string]int{}
	gained := map[string]bool{}
	b.store.update(func(d *storeData) {
		for _, h := range helpers {
			totals[h], gained[h] = awardHelper(d, h, th.ID, owner, time.Now())
		}
	})
	var mentions []string
	for _, h := range helpers {
		mentions = append(mentions, "<@"+h+">")
	}
	text := fmt.Sprintf("<@%s> says thanks to %s for the help!", by, joinNames(mentions))
	var reached []string
	for _, h := range helpers {
		if gained[h] && totals[h] >= b.cfg.HelperPoints && b.giveHelperRole(h) {
			reached = append(reached, "<@"+h+">")
		}
	}
	if len(reached) > 0 {
		text += fmt.Sprintf("\n%s reached %s and %s now a **Helper**.", joinNames(reached), pointsWord(b.cfg.HelperPoints), map[bool]string{true: "is", false: "are"}[len(reached) == 1])
	}
	locked := th.ThreadMetadata != nil && th.ThreadMetadata.Archived
	if locked {
		no := false
		if _, err := b.s.ChannelEditComplex(th.ID, &discordgo.ChannelEdit{Archived: &no}); err != nil {
			log.Printf("reopen for thanks: %v", err)
		}
	}
	if _, err := b.s.ChannelMessageSendComplex(th.ID, &discordgo.MessageSend{
		Embeds:          []*discordgo.MessageEmbed{{Description: text, Color: shu}},
		AllowedMentions: &discordgo.MessageAllowedMentions{Users: helpers},
	}); err != nil {
		log.Printf("thank helpers: %v", err)
	}
	if locked {
		b.closeSolved(th.ID, nil)
	}
}

func joinNames(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// skipHelpers handles "Nobody / skip".
func (b *bot) skipHelpers(i *discordgo.InteractionCreate) {
	b.updateEphemeral(i, &discordgo.MessageEmbed{Color: shu, Description: "No problem. Glad it's sorted!"})
}

// updateEphemeral replaces the private message a button or menu was on.
func (b *bot) updateEphemeral(i *discordgo.InteractionCreate, e *discordgo.MessageEmbed) {
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: []discordgo.MessageComponent{}},
	}); err != nil {
		log.Printf("update: %v", err)
	}
}

// helperRole remembers the Helper role's id once found or made.
var helperRole struct {
	sync.Mutex
	id string
}

// helperRoleID finds the Helper role by name, making it when missing.
func (b *bot) helperRoleID() (string, error) {
	helperRole.Lock()
	defer helperRole.Unlock()
	if helperRole.id != "" {
		return helperRole.id, nil
	}
	roles, err := b.s.GuildRoles(b.cfg.GuildID)
	if err != nil {
		return "", err
	}
	for _, r := range roles {
		if strings.EqualFold(r.Name, "Helper") {
			helperRole.id = r.ID
			return r.ID, nil
		}
	}
	color, perms, no := helperRoleColor, int64(0), false
	r, err := b.s.GuildRoleCreate(b.cfg.GuildID, &discordgo.RoleParams{Name: "Helper", Color: &color, Permissions: &perms, Mentionable: &no, Hoist: &no})
	if err != nil {
		return "", err
	}
	helperRole.id = r.ID
	return r.ID, nil
}

// giveHelperRole adds the Helper role, reporting whether it is new for them.
func (b *bot) giveHelperRole(userID string) bool {
	role, err := b.helperRoleID()
	if err != nil {
		log.Printf("helper role: %v", err)
		return false
	}
	if m, err := b.s.GuildMember(b.cfg.GuildID, userID); err == nil && slices.Contains(m.Roles, role) {
		return false
	}
	if err := b.s.GuildMemberRoleAdd(b.cfg.GuildID, userID, role, discordgo.WithAuditLogReason("Reached the helper points for the Helper role")); err != nil {
		log.Printf("helper role: %v", err)
		return false
	}
	return true
}

// thank handles /thank inside a support post.
func (b *bot) thank(i *discordgo.InteractionCreate) {
	th, ok := b.supportThread(i.ChannelID)
	if !ok {
		b.respond(i, true, errorEmbed("Use this inside a post in the support forum."))
		return
	}
	owner := b.postOwner(th)
	if !canManagePost(i.Member, owner) {
		b.respond(i, true, errorEmbed("Only the person who posted this (or staff) can thank someone here."))
		return
	}
	user := optUser(i, optionMap(i)["member"])
	switch {
	case user.Bot:
		b.respond(i, true, errorEmbed("Bots don't collect helper points."))
		return
	case user.ID == owner || user.ID == i.Member.User.ID:
		b.respond(i, true, errorEmbed("No point for helping yourself."))
		return
	}
	var total int
	var given bool
	b.store.update(func(d *storeData) { total, given = awardHelper(d, user.ID, th.ID, owner, time.Now()) })
	if !given {
		b.respond(i, true, errorEmbed("<@%s> already got a point for this post.", user.ID))
		return
	}
	text := fmt.Sprintf("<@%s> thanked <@%s> for the help. They now have %s.", i.Member.User.ID, user.ID, pointsWord(total))
	if total >= b.cfg.HelperPoints && b.giveHelperRole(user.ID) {
		text += " That makes them a **Helper**!"
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{Description: text, Color: shu}},
			AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{user.ID}}},
	}); err != nil {
		log.Printf("thank: %v", err)
	}
}

// helpersEmbed is the /helpers board: the top ten and where the caller is.
func helpersEmbed(h map[string]helperStat, caller string, threshold int) *discordgo.MessageEmbed {
	ranks := rankHelpers(h)
	e := &discordgo.MessageEmbed{Title: "Top helpers", Color: shu,
		Footer: &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("A point for each solved support post you helped with · %s for the Helper role", pointsWord(threshold))}}
	var lines []string
	for k, r := range ranks {
		if k == 10 {
			break
		}
		lines = append(lines, fmt.Sprintf("**%d.** <@%s> · %s", k+1, r.ID, pointsWord(r.Points)))
	}
	if len(lines) == 0 {
		lines = append(lines, "Nobody has helper points yet. Help someone in the support forum and they can thank you when it's solved.")
	}
	if n := rankOf(ranks, caller); n > 0 {
		lines = append(lines, "", fmt.Sprintf("You're **#%d** with %s.", n, pointsWord(h[caller].Points)))
	} else if len(ranks) > 0 {
		lines = append(lines, "", "You don't have helper points yet. Answer a question in the support forum to get some.")
	}
	e.Description = strings.Join(lines, "\n")
	return e
}

func (b *bot) helpers(i *discordgo.InteractionCreate) {
	var e *discordgo.MessageEmbed
	b.store.view(func(d *storeData) { e = helpersEmbed(d.Helpers, i.Member.User.ID, b.cfg.HelperPoints) })
	b.respond(i, false, e)
}
