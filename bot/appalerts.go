package main

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Support alerts in the app: when someone else answers a support post the
// member made from the app (or with /support), and when a bug they
// reported ships fixed in a release, their linked apps get a notice that
// opens the post or the release. A busy post alerts at most once every ten
// minutes.

const replyAlertEvery = 10 * time.Minute

var replyAlertLimit = newRateLimit(1, replyAlertEvery)

func init() {
	onStart(func(b *bot) { b.s.AddHandler(b.onSupportReply) })
}

// supportPostOwner is the member a support post made from the app or with
// /support belongs to, "" for any other channel.
func (b *bot) supportPostOwner(thread string) string {
	owner := ""
	b.store.view(func(d *storeData) {
		if u, ok := d.FormPosts[thread]; ok {
			owner = u
		}
		if r, ok := d.AppReports[thread]; ok && r.User != "" {
			owner = r.User
		}
	})
	return owner
}

// replyAuthor is the name a reply's author goes by in the server.
func replyAuthor(m *discordgo.Message) string {
	if m.Member != nil && m.Member.Nick != "" {
		return m.Member.Nick
	}
	if m.Author.GlobalName != "" {
		return m.Author.GlobalName
	}
	return m.Author.Username
}

// replyNotice is the inbox item for a reply in a member's support post.
func replyNotice(guild, postName string, m *discordgo.Message) inboxItem {
	text := cleanText(replyAuthor(m), false) + " replied"
	if postName != "" {
		text += fmt.Sprintf(" in “%s”", clip(cleanText(postName, false), 80))
	}
	if c := clip(cleanText(m.Content, false), 120); c != "" {
		text += ": " + c
	}
	return inboxItem{Kind: "notice", Title: "Reply to your support post", Text: text,
		URL: fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guild, m.ChannelID, m.ID)}
}

func (b *bot) onSupportReply(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot || m.GuildID != b.cfg.GuildID {
		return
	}
	owner := b.supportPostOwner(m.ChannelID)
	if owner == "" || owner == m.Author.ID || !b.hasApp(owner) || !replyAlertLimit.allow(m.ChannelID, appNow()) {
		return
	}
	name := ""
	if th, err := s.State.Channel(m.ChannelID); err == nil {
		name = th.Name
	}
	n := b.sendToApps(owner, replyNotice(b.cfg.GuildID, name, m.Message))
	log.Printf("support reply alert for %s on %d apps", owner, n)
}

// fixedNotice is the inbox item for a reported bug that shipped fixed.
func fixedNotice(is ghIssueFull, rel ghRelease) inboxItem {
	v := rel.version()
	return inboxItem{Kind: "notice", Title: "Fixed in v" + v,
		Text: fmt.Sprintf("Your report “%s” (#%d) is fixed. Update with otakase -u.", clip(cleanText(is.Title, false), 80), is.Number),
		URL:  rel.HTMLURL}
}

// noticeBugFixed tells the poster's apps that their bug is fixed. Posts
// made from the app or with /support belong to the bot, so the member who
// asked is looked up for those.
func (b *bot) noticeBugFixed(thread, poster string, is ghIssueFull, rel ghRelease) {
	if owner := b.supportPostOwner(thread); owner != "" {
		poster = owner
	}
	if poster == "" {
		return
	}
	if n := b.sendToApps(poster, fixedNotice(is, rel)); n > 0 {
		log.Printf("bug fixed notice for %s on %d apps", poster, n)
	}
}
