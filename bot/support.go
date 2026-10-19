package main

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// onThreadCreate greets a new support forum post with a checklist, links to
// the wiki sections that match the question and a "Mark solved" button.
func (b *bot) onThreadCreate(s *discordgo.Session, t *discordgo.ThreadCreate) {
	if !t.NewlyCreated || t.ParentID != b.cfg.SupportForum || t.OwnerID == s.State.User.ID {
		return
	}
	// The starter message lands just after the thread; posting first would
	// put the checklist above the question.
	time.Sleep(2 * time.Second)
	text := ""
	// A forum post's first message has the same id as the post.
	if m, err := s.ChannelMessage(t.ID, t.ID); err == nil {
		text = m.Content
	}
	b.greetSupportPost(t.ID, t.Name, text)
}

// greetSupportPost sends the checklist, the matching wiki sections and the
// buttons to a support post.
func (b *bot) greetSupportPost(id, title, text string) {
	embeds := []*discordgo.MessageEmbed{{Description: b.text("support_checklist"), Color: shu}}
	if e := b.wikiEmbed(title, text); e != nil {
		embeds = append(embeds, e)
	}
	msg, err := b.s.ChannelMessageSendComplex(id, &discordgo.MessageSend{
		Embeds:     embeds,
		Components: append(supportButtons(), answerRows(len(embeds) > 1)...),
	})
	if err != nil {
		log.Printf("support greeting: %v", err)
	} else if len(embeds) > 1 {
		b.trackAnswer(msg, wikiAnswerKeys(embeds[1]), title+"\n"+text)
	}
}

// supportButtons sit under the greeting on a new support post.
func supportButtons() []discordgo.MessageComponent {
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Mark solved", Style: discordgo.SuccessButton, CustomID: "solved"},
		discordgo.Button{Label: "Report on GitHub", Style: discordgo.SecondaryButton, CustomID: "bug"},
		discordgo.Button{Label: "Troubleshooting", Style: discordgo.LinkButton, URL: wikiURL + "/Troubleshooting"},
	}}}
}

// markSolved tags the post Solved, then locks and closes it, and asks who
// helped. Only the poster and people who can manage threads may do it.
func (b *bot) markSolved(i *discordgo.InteractionCreate) {
	s := b.s
	th, err := s.Channel(i.ChannelID)
	if err != nil || th.ParentID != b.cfg.SupportForum {
		return
	}
	user := i.Member.User.ID
	owner := b.postOwner(th)
	if user != owner && i.Member.Permissions&discordgo.PermissionManageThreads == 0 {
		b.respond(i, true, errorEmbed("Only the person who posted this can mark it solved."))
		return
	}
	forum, err := s.Channel(th.ParentID)
	if err != nil {
		log.Printf("solved: %v", err)
		return
	}
	tags := th.AppliedTags
	for _, t := range forum.AvailableTags {
		if strings.EqualFold(t.Name, "Solved") && !contains(tags, t.ID) {
			tags = append(tags, t.ID)
		}
	}
	if len(tags) > 5 { // Discord allows five tags per post
		tags = tags[len(tags)-5:]
	}
	b.noteSolved(th.ID)
	b.respond(i, false, &discordgo.MessageEmbed{Description: "Marked solved by <@" + user + ">. Glad it's working!", Color: shu})
	b.askWhoHelped(i, th, owner)
	b.closeSolved(th.ID, &tags)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// bugReportURL opens a new GitHub issue filled in from a support post.
func bugReportURL(title, question, postURL string) string {
	body := "**Describe the bug**\n" + clip(strings.TrimSpace(question), 1500) +
		"\n\n**Version and system**\n\n\n_From the Discord support post: " + postURL + "_"
	q := url.Values{"title": {clip(title, 120)}, "body": {body}, "labels": {"bug"}}
	return repoURL + "/issues/new?" + q.Encode()
}

// reportBug answers /bug or the "Report on GitHub" button inside a support
// post with a link that opens a pre-filled GitHub issue.
func (b *bot) reportBug(i *discordgo.InteractionCreate) {
	s := b.s
	th, err := s.Channel(i.ChannelID)
	if err != nil || th.ParentID != b.cfg.SupportForum {
		b.respond(i, true, errorEmbed("Use this inside a post in the support forum."))
		return
	}
	question := ""
	// A forum post's first message has the same id as the post.
	if m, err := s.ChannelMessage(th.ID, th.ID); err == nil {
		question = m.Content
	}
	post := fmt.Sprintf("https://discord.com/channels/%s/%s", b.cfg.GuildID, th.ID)
	b.rememberBugReport(th)
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu,
		Description: "This opens a GitHub issue filled in from this post. Add your version (`otakase -v`) and system, then submit it."},
		linkButtons("Open GitHub issue", bugReportURL(th.Name, question, post))...)
}
