package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /support-queue shows staff what needs attention in the support forum.

const (
	queueShown     = 5  // posts listed per section
	queueChecked   = 40 // open posts whose messages are read
	queueStaffWait = 24 * time.Hour
)

func supportQueueCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{Name: "support-queue", Description: "Support posts that need attention (staff)",
		DefaultMemberPermissions: &permPurge}
}

// postActivity sums up the replies in a support post.
type postActivity struct {
	Replied      bool // someone other than the poster and bots answered
	StaffReplied bool
}

type queueMessage struct {
	Author string
	Bot    bool
}

// summarizePost reads a post's messages. isStaff is asked about each
// replier once at most.
func summarizePost(poster string, msgs []queueMessage, isStaff func(string) bool) postActivity {
	var a postActivity
	asked := map[string]bool{}
	for _, m := range msgs {
		if m.Bot || m.Author == poster || m.Author == "" {
			continue
		}
		a.Replied = true
		if !a.StaffReplied && !asked[m.Author] {
			asked[m.Author] = true
			a.StaffReplied = isStaff(m.Author)
		}
	}
	return a
}

// queueSection lists up to five posts with a note each.
func queueSection(ids []string, note func(string) string) string {
	if len(ids) == 0 {
		return "None"
	}
	var lines []string
	for k, id := range ids {
		if k == queueShown {
			lines = append(lines, fmt.Sprintf("…and %d more", len(ids)-k))
			break
		}
		lines = append(lines, "- "+postLink(id)+" · "+note(id))
	}
	return strings.Join(lines, "\n")
}

func (b *bot) supportQueue(i *discordgo.InteractionCreate) {
	if i.Member.Permissions&(discordgo.PermissionManageMessages|discordgo.PermissionModerateMembers|discordgo.PermissionAdministrator) == 0 {
		b.respond(i, true, errorEmbed("Only staff can use this."))
		return
	}
	if b.cfg.SupportForum == "" {
		b.respond(i, true, errorEmbed("There's no support forum set up."))
		return
	}
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		e, err := b.supportQueueEmbed(time.Now())
		if err != nil {
			return errorEmbed("Couldn't read the support forum: %v", err), nil
		}
		return e, nil
	})
}

func (b *bot) supportQueueEmbed(now time.Time) (*discordgo.MessageEmbed, error) {
	forum, err := b.s.Channel(b.cfg.SupportForum)
	if err != nil {
		return nil, err
	}
	solvedTag := supportSolvedTag(forum)
	threads, err := b.supportThreads(1)
	if err != nil {
		return nil, err
	}
	created := func(id string) time.Time {
		t, _ := discordgo.SnowflakeTimestamp(id)
		return t
	}
	age := func(id string) string { return fmt.Sprintf("posted <t:%d:R>", created(id).Unix()) }

	var open []*discordgo.Channel
	var solved []string
	solvedAt := map[string]time.Time{}
	for _, th := range threads {
		isSolved := solvedTag != "" && contains(th.AppliedTags, solvedTag)
		archived := th.ThreadMetadata != nil && th.ThreadMetadata.Archived
		switch {
		case isSolved && th.ThreadMetadata != nil && now.Sub(th.ThreadMetadata.ArchiveTimestamp) < 7*24*time.Hour:
			solved = append(solved, th.ID)
			solvedAt[th.ID] = th.ThreadMetadata.ArchiveTimestamp
		case !isSolved && !archived:
			open = append(open, th)
		}
	}
	// Oldest first: they've waited longest.
	sort.SliceStable(open, func(a, c int) bool { return snowflakeNewer(open[c].ID, open[a].ID) })
	sort.SliceStable(solved, func(a, c int) bool { return solvedAt[solved[a]].After(solvedAt[solved[c]]) })

	staff := map[string]bool{}
	isStaff := func(user string) bool {
		if v, ok := staff[user]; ok {
			return v
		}
		v := b.memberIsStaff(user)
		staff[user] = v
		return v
	}
	var noReply, noStaff []string
	for k, th := range open {
		if k == queueChecked {
			break
		}
		msgs, err := b.s.ChannelMessages(th.ID, 100, "", "", "")
		if err != nil {
			continue
		}
		var qm []queueMessage
		for _, m := range msgs {
			if m.Author != nil {
				qm = append(qm, queueMessage{Author: m.Author.ID, Bot: m.Author.Bot})
			}
		}
		a := summarizePost(th.OwnerID, qm, isStaff)
		switch {
		case !a.Replied:
			noReply = append(noReply, th.ID)
		case !a.StaffReplied && now.Sub(created(th.ID)) >= queueStaffWait:
			noStaff = append(noStaff, th.ID)
		}
	}

	e := &discordgo.MessageEmbed{Title: "Support queue", Color: shu,
		Description: fmt.Sprintf("**%d** open support posts.", len(open)),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "No reply yet (oldest first)", Value: queueSection(noReply, age)},
			{Name: "Other replies, but no staff after 24 hours", Value: queueSection(noStaff, age)},
			{Name: fmt.Sprintf("Solved this week (%d)", len(solved)), Value: queueSection(solved, func(id string) string {
				return fmt.Sprintf("solved <t:%d:R>", solvedAt[id].Unix())
			})},
		}}
	if len(open) > queueChecked {
		e.Footer = &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Replies checked in the %d oldest open posts.", queueChecked)}
	}
	return e, nil
}

// memberIsStaff reports whether a member has a staff role (Moderator or
// Maintainer), or when there are none, can manage messages.
func (b *bot) memberIsStaff(user string) bool {
	m, err := b.s.State.Member(b.cfg.GuildID, user)
	if err != nil {
		if m, err = b.s.GuildMember(b.cfg.GuildID, user); err != nil {
			return false // left the server
		}
	}
	if len(b.cfg.StaffRoles) > 0 {
		for _, r := range m.Roles {
			if contains(b.cfg.StaffRoles, r) {
				return true
			}
		}
		return false
	}
	p, err := b.s.State.UserChannelPermissions(user, b.cfg.SupportForum)
	return err == nil && p&(discordgo.PermissionManageMessages|discordgo.PermissionAdministrator) != 0
}
