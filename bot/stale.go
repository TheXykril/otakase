package main

import (
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Support posts quiet for a week get a nudge; if nobody answers it within
// three more days, the post is closed. Solved posts are left alone.

const (
	staleAfter  = 7 * 24 * time.Hour
	closeAfter  = 3 * 24 * time.Hour
	nudgeTitle  = "Still need help?"
	staleChecks = time.Hour
)

func (b *bot) staleLoop() {
	if b.cfg.SupportForum == "" {
		return
	}
	for {
		b.checkStale(time.Now())
		time.Sleep(staleChecks)
	}
}

func (b *bot) checkStale(now time.Time) {
	s := b.s
	list, err := s.GuildThreadsActive(b.cfg.GuildID)
	if err != nil {
		log.Printf("stale: %v", err)
		return
	}
	forum, err := s.Channel(b.cfg.SupportForum)
	if err != nil {
		log.Printf("stale: %v", err)
		return
	}
	solved := ""
	for _, t := range forum.AvailableTags {
		if strings.EqualFold(t.Name, "Solved") {
			solved = t.ID
		}
	}
	for _, th := range list.Threads {
		if th.ParentID != b.cfg.SupportForum || (solved != "" && contains(th.AppliedTags, solved)) || th.LastMessageID == "" {
			continue
		}
		last, err := discordgo.SnowflakeTimestamp(th.LastMessageID)
		if err != nil || now.Sub(last) < closeAfter {
			continue
		}
		msgs, err := s.ChannelMessages(th.ID, 1, "", "", "")
		if err != nil || len(msgs) == 0 {
			continue
		}
		if isNudge(msgs[0], s.State.User.ID) {
			if now.Sub(last) >= closeAfter {
				b.closeStale(th)
			}
			continue
		}
		if now.Sub(last) >= staleAfter {
			b.nudge(th)
		}
	}
}

func isNudge(m *discordgo.Message, botID string) bool {
	if m.Author == nil || m.Author.ID != botID {
		return false
	}
	for _, e := range m.Embeds {
		if e.Title == nudgeTitle {
			return true
		}
	}
	return false
}

func (b *bot) nudge(th *discordgo.Channel) {
	_, err := b.s.ChannelMessageSendComplex(th.ID, &discordgo.MessageSend{
		Content: "<@" + th.OwnerID + ">",
		Embeds: []*discordgo.MessageEmbed{{Title: nudgeTitle, Color: shu,
			Description: "This post has been quiet for a week. Reply here if it's still broken, or press **Mark solved** if it's fixed. " +
				"Without a reply it closes in three days; you can always reopen it by posting again."}},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Mark solved", Style: discordgo.SuccessButton, CustomID: "solved"},
		}}},
		AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{th.OwnerID}},
	})
	if err != nil {
		log.Printf("nudge: %v", err)
	}
}

func (b *bot) closeStale(th *discordgo.Channel) {
	_, _ = b.s.ChannelMessageSendEmbed(th.ID, &discordgo.MessageEmbed{Color: 0x8A8276,
		Description: "Closed after no reply. Post here again to reopen it."})
	archived := true
	if _, err := b.s.ChannelEditComplex(th.ID, &discordgo.ChannelEdit{Archived: &archived}); err != nil {
		log.Printf("close stale: %v", err)
	}
}
