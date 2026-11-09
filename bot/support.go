package main

import (
	"fmt"
	"log"
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
		discordgo.Button{Label: "Report a bug", Style: discordgo.SecondaryButton, CustomID: "bug"},
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

// bugIssueBody is the bug report filed from a support post. It ends with
// the post's link, which bugfollow.go uses to find the post again.
func bugIssueBody(question, details, postURL string) string {
	body := "**Describe the bug**\n" + clip(strings.TrimSpace(question), 1500)
	if details = strings.TrimSpace(details); details != "" {
		body += "\n\n**Version and system**\n" + clip(details, 300)
	}
	return body + "\n\n_From the Discord support post: " + postURL + "_"
}

// reportBug answers /bug or the "Report a bug" button inside a support post
// with a short form; sending it files the bug report (see fileBugReport).
func (b *bot) reportBug(i *discordgo.InteractionCreate) {
	th, err := b.s.Channel(i.ChannelID)
	if err != nil || th.ParentID != b.cfg.SupportForum {
		b.respond(i, true, errorEmbed("Use this inside a post in the support forum."))
		return
	}
	var r bugReport
	b.store.view(func(d *storeData) { r = d.BugReports[th.ID] })
	if r.Issue > 0 {
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf(
			"This post is already reported as bug **#%d**. You'll get a message here when it's fixed.", r.Issue)})
		return
	}
	if b.cfg.GitHubToken == "" {
		b.respond(i, true, errorEmbed("Bug reports aren't set up on this server yet. A moderator will pass it on."))
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{CustomID: "bug-form:" + th.ID, Title: "Report a bug", Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{
				CustomID: "details", Label: "App version and device", Style: discordgo.TextInputShort, Required: true, MaxLength: 200,
				Placeholder: "e.g. 26.2.0 on a Pixel 8 (About & updates shows the version)"}}},
		}},
	}); err != nil {
		log.Printf("bug form: %v", err)
	}
}

// fileBugReport files the bug report from a sent /bug form.
func (b *bot) fileBugReport(i *discordgo.InteractionCreate, threadID string) {
	th, err := b.s.Channel(threadID)
	if err != nil || th.ParentID != b.cfg.SupportForum {
		b.respond(i, true, errorEmbed("Use this inside a post in the support forum."))
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	reply := func(e *discordgo.MessageEmbed) {
		if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e}}); err != nil {
			log.Printf("edit: %v", err)
		}
	}
	b.rememberBugReport(th)
	var r bugReport
	b.store.view(func(d *storeData) { r = d.BugReports[th.ID] })
	if r.Issue > 0 {
		reply(&discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("This post is already reported as bug **#%d**.", r.Issue)})
		return
	}
	question := ""
	// A forum post's first message has the same id as the post.
	if m, err := b.s.ChannelMessage(th.ID, th.ID); err == nil {
		question = m.Content
	}
	post := fmt.Sprintf("https://discord.com/channels/%s/%s", b.cfg.GuildID, th.ID)
	is, status, err := createIssue(b.cfg.GitHubToken, clip(th.Name, 120), bugIssueBody(question, modalValues(i)["details"], post), []string{"bug"})
	if err != nil {
		log.Printf("bug report: %v (%d)", err, status)
		reply(errorEmbed("Couldn't file the bug report right now. Try again in a minute."))
		return
	}
	b.store.update(func(d *storeData) {
		if cur, ok := d.BugReports[th.ID]; ok {
			cur.Issue = is.Number
			d.BugReports[th.ID] = cur
		}
	})
	if _, err := b.s.ChannelMessageSendComplex(th.ID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{Color: shu, Description: fmt.Sprintf(
			"Reported as bug **#%d**. The bot posts here when a release fixes it.", is.Number)}},
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Printf("bug report note: %v", err)
	}
	reply(&discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Filed as bug **#%d**. Thanks!", is.Number)})
}

func init() {
	registerModal("bug-form:", func(b *bot, i *discordgo.InteractionCreate, threadID string) { b.fileBugReport(i, threadID) })
}
