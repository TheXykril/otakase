package main

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Suggestions: /suggest opens a form, the bot posts the idea in the
// suggestions channel with vote buttons and a thread to talk about it.
// Staff set its status with /suggestion-status inside that thread.

type suggestion struct {
	Channel string    `json:"channel"`
	Author  string    `json:"author"`
	Name    string    `json:"name"` // author's display name when posted
	Avatar  string    `json:"avatar,omitempty"`
	Title   string    `json:"title"`
	Details string    `json:"details,omitempty"`
	Created time.Time `json:"created"`
	Up      []string  `json:"up,omitempty"`
	Down    []string  `json:"down,omitempty"`
	Status  string    `json:"status,omitempty"` // "" = open
	Note    string    `json:"note,omitempty"`
	By      string    `json:"by,omitempty"` // who set the status
}

// suggestionCooldown is how long a member waits between suggestions.
const suggestionCooldown = 5 * time.Minute

type suggestionStatus struct {
	Key, Label string
	Color      int
	Closed     bool // no more votes
}

var suggestionStatuses = []suggestionStatus{
	{"", "Open", shu, false},
	{"planned", "Planned", 0x3B82C4, false},
	{"progress", "In progress", 0xD9A33A, false},
	{"done", "Done", 0x3E9B5F, true},
	{"declined", "Declined", 0x8A8276, true},
}

func statusByKey(key string) suggestionStatus {
	for _, s := range suggestionStatuses {
		if s.Key == key {
			return s
		}
	}
	return suggestionStatuses[0]
}

func suggestionCommands() []*discordgo.ApplicationCommand {
	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, s := range suggestionStatuses {
		key := s.Key
		if key == "" {
			key = "open"
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: s.Label, Value: key})
	}
	return []*discordgo.ApplicationCommand{
		{Name: "suggest", Description: "Suggest an idea for Otakase; everyone can vote on it"},
		{Name: "suggestion-status", Description: "Set the status of the suggestion this thread is about", DefaultMemberPermissions: &permPurge,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "status", Description: "New status", Required: true, Choices: choices},
				{Type: discordgo.ApplicationCommandOptionString, Name: "note", Description: "Short note shown on the suggestion", MaxLength: 300},
			}},
	}
}

// vote records userID's vote. Pressing the same button again takes the vote
// back; pressing the other one moves it.
func (s *suggestion) vote(userID string, up bool) {
	had := slices.Contains(s.Up, userID)
	if !up {
		had = slices.Contains(s.Down, userID)
	}
	s.Up = slices.DeleteFunc(s.Up, func(id string) bool { return id == userID })
	s.Down = slices.DeleteFunc(s.Down, func(id string) bool { return id == userID })
	if had {
		return
	}
	if up {
		s.Up = append(s.Up, userID)
	} else {
		s.Down = append(s.Down, userID)
	}
}

func (s *suggestion) embed() *discordgo.MessageEmbed {
	st := statusByKey(s.Status)
	e := &discordgo.MessageEmbed{Title: s.Title, Description: s.Details, Color: st.Color, Timestamp: s.Created.Format(time.RFC3339),
		Author: &discordgo.MessageEmbedAuthor{Name: "Suggestion by " + s.Name, IconURL: s.Avatar}}
	status := "**" + st.Label + "**"
	if s.By != "" {
		status += fmt.Sprintf(" · set by <@%s>", s.By)
	}
	if s.Note != "" {
		status += "\n" + s.Note
	}
	e.Fields = []*discordgo.MessageEmbedField{{Name: "Status", Value: status}}
	if st.Closed {
		e.Footer = &discordgo.MessageEmbedFooter{Text: "Voting closed"}
	} else {
		e.Footer = &discordgo.MessageEmbedFooter{Text: "Vote with the buttons · talk about it in the thread"}
	}
	return e
}

func (s *suggestion) components() []discordgo.MessageComponent {
	closed := statusByKey(s.Status).Closed
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: fmt.Sprint(len(s.Up)), Emoji: &discordgo.ComponentEmoji{Name: "👍"},
			Style: discordgo.SuccessButton, CustomID: "sug-up", Disabled: closed},
		discordgo.Button{Label: fmt.Sprint(len(s.Down)), Emoji: &discordgo.ComponentEmoji{Name: "👎"},
			Style: discordgo.DangerButton, CustomID: "sug-down", Disabled: closed},
	}}}
}

// suggestForm opens the /suggest form.
func (b *bot) suggestForm(i *discordgo.InteractionCreate) {
	if b.cfg.SuggestionsChannel == "" {
		b.respond(i, true, errorEmbed("Suggestions aren't set up on this server yet."))
		return
	}
	if wait := b.suggestionWait(i.Member.User.ID, time.Now()); wait > 0 {
		b.respond(i, true, errorEmbed("You can post another suggestion in %d min.", int(wait.Minutes())+1))
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{CustomID: "suggest-form", Title: "Suggest an idea", Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{
				CustomID: "title", Label: "Idea", Style: discordgo.TextInputShort, Required: true, MinLength: 5, MaxLength: 100,
				Placeholder: "e.g. Download a whole season at once"}}},
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{
				CustomID: "details", Label: "Details (optional)", Style: discordgo.TextInputParagraph, MaxLength: 1500,
				Placeholder: "How it would work, and why it would help"}}},
		}},
	}); err != nil {
		log.Printf("suggest form: %v", err)
	}
}

// suggestionWait is how long userID has to wait before suggesting again.
func (b *bot) suggestionWait(userID string, now time.Time) time.Duration {
	var last time.Time
	b.store.view(func(d *storeData) {
		for _, s := range d.Suggestions {
			if s.Author == userID && s.Created.After(last) {
				last = s.Created
			}
		}
	})
	return max(0, last.Add(suggestionCooldown).Sub(now))
}

func modalValues(i *discordgo.InteractionCreate) map[string]string {
	v := map[string]string{}
	for _, c := range i.ModalSubmitData().Components {
		row, ok := c.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, f := range row.Components {
			if t, ok := f.(*discordgo.TextInput); ok {
				v[t.CustomID] = strings.TrimSpace(t.Value)
			}
		}
	}
	return v
}

// postSuggestion posts a submitted /suggest form.
func (b *bot) postSuggestion(i *discordgo.InteractionCreate) {
	v := modalValues(i)
	if v["title"] == "" {
		b.respond(i, true, errorEmbed("The idea can't be empty."))
		return
	}
	u := i.Member.User
	s := &suggestion{Channel: b.cfg.SuggestionsChannel, Author: u.ID, Name: displayName(i.Member), Avatar: u.AvatarURL("64"),
		Title: v["title"], Details: v["details"], Created: time.Now().UTC()}
	msg, err := b.s.ChannelMessageSendComplex(s.Channel, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{s.embed()}, Components: s.components(),
		AllowedMentions: &discordgo.MessageAllowedMentions{}})
	if err != nil {
		log.Printf("post suggestion: %v", err)
		b.respond(i, true, errorEmbed("Couldn't post the suggestion, please try again later."))
		return
	}
	b.store.update(func(d *storeData) { d.Suggestions[msg.ID] = *s })
	name := s.Title
	if r := []rune(name); len(r) > 90 {
		name = string(r[:90]) + "…"
	}
	if _, err := b.s.MessageThreadStart(s.Channel, msg.ID, name, 10080); err != nil {
		log.Printf("suggestion thread: %v", err)
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu,
		Description: "Thanks! Your suggestion is up: " + messageLink(b.cfg.GuildID, s.Channel, msg.ID)})
}

// voteSuggestion handles the 👍 and 👎 buttons.
func (b *bot) voteSuggestion(i *discordgo.InteractionCreate, up bool) {
	var s suggestion
	var found bool
	b.store.update(func(d *storeData) {
		s, found = d.Suggestions[i.Message.ID]
		if !found || statusByKey(s.Status).Closed {
			return
		}
		s.vote(i.Member.User.ID, up)
		d.Suggestions[i.Message.ID] = s
	})
	if !found {
		b.respond(i, true, errorEmbed("This suggestion is no longer tracked, so votes can't be counted."))
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{s.embed()}, Components: s.components()},
	}); err != nil {
		log.Printf("vote: %v", err)
	}
}

// setSuggestionStatus handles /suggestion-status, run in a suggestion's
// thread (a thread started from a message has that message's id).
func (b *bot) setSuggestionStatus(i *discordgo.InteractionCreate) {
	opts := optionMap(i)
	key := opts["status"].StringValue()
	if key == "open" {
		key = ""
	}
	note := ""
	if o, ok := opts["note"]; ok {
		note = strings.TrimSpace(o.StringValue())
	}
	var s suggestion
	var found bool
	b.store.update(func(d *storeData) {
		s, found = d.Suggestions[i.ChannelID]
		if !found {
			return
		}
		s.Status, s.Note, s.By = key, note, i.Member.User.ID
		if key == "" {
			s.By = ""
		}
		d.Suggestions[i.ChannelID] = s
	})
	if !found {
		b.respond(i, true, errorEmbed("Use this inside a suggestion's thread."))
		return
	}
	if _, err := b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: s.Channel, ID: i.ChannelID,
		Embeds: &[]*discordgo.MessageEmbed{s.embed()}, Components: ptr(s.components())}); err != nil {
		log.Printf("suggestion status: %v", err)
		b.respond(i, true, errorEmbed("Couldn't update the suggestion."))
		return
	}
	st := statusByKey(key)
	text := fmt.Sprintf("<@%s> marked this suggestion **%s**.", i.Member.User.ID, st.Label)
	if note != "" {
		text += "\n" + note
	}
	b.respond(i, false, &discordgo.MessageEmbed{Description: text, Color: st.Color})
	if st.Closed {
		archived := true
		if _, err := b.s.ChannelEditComplex(i.ChannelID, &discordgo.ChannelEdit{Archived: &archived}); err != nil {
			log.Printf("archive suggestion thread: %v", err)
		}
	}
}

func ptr[T any](v T) *T { return &v }
