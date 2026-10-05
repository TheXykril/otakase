package main

import (
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// onThreadCreate greets a new support forum post with a checklist and a
// "Mark solved" button.
func (b *bot) onThreadCreate(s *discordgo.Session, t *discordgo.ThreadCreate) {
	if !t.NewlyCreated || t.ParentID != b.cfg.SupportForum {
		return
	}
	// The starter message lands just after the thread; posting first would
	// put the checklist above the question.
	time.Sleep(2 * time.Second)
	_, err := s.ChannelMessageSendComplex(t.ID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{Description: supportChecklist, Color: shu}},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Mark solved", Style: discordgo.SuccessButton, CustomID: "solved"},
			discordgo.Button{Label: "Troubleshooting", Style: discordgo.LinkButton, URL: wikiURL + "/Troubleshooting"},
		}}},
	})
	if err != nil {
		log.Printf("support greeting: %v", err)
	}
}

// markSolved tags the post Solved and closes it. Only the poster and people
// who can manage threads may do it.
func (b *bot) markSolved(i *discordgo.InteractionCreate) {
	s := b.s
	th, err := s.Channel(i.ChannelID)
	if err != nil || th.ParentID != b.cfg.SupportForum {
		return
	}
	user := i.Member.User.ID
	if user != th.OwnerID && i.Member.Permissions&discordgo.PermissionManageThreads == 0 {
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
	b.respond(i, false, &discordgo.MessageEmbed{Description: "Marked solved by <@" + user + ">. Glad it's working!", Color: shu})
	archived := true
	if _, err := s.ChannelEditComplex(th.ID, &discordgo.ChannelEdit{AppliedTags: &tags, Archived: &archived}); err != nil {
		log.Printf("solved edit: %v", err)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
