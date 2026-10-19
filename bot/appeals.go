package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Appeals: a member the bot warns or times out gets a DM with an Appeal
// button. It opens a short form, the appeal goes to mod-chat with buttons to
// remove the warning, end the timeout or reject it, and the member hears the
// result by DM. Each action can be appealed once. The button ids carry the
// appeal's id and the appeal is kept in the data file, so the buttons keep
// working after a restart.

// appealKeep is how long an action stays appealable.
const appealKeep = 60 * 24 * time.Hour

type appeal struct {
	User    string    `json:"user"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
	WarnAt  time.Time `json:"warn_at,omitzero"`  // the warning it is about, if any
	Timeout bool      `json:"timeout,omitempty"` // they were timed out
	Text    string    `json:"text,omitempty"`    // their appeal, once sent
	Status  string    `json:"status,omitempty"`  // "", "open", "removed", "ended", "rejected"
	By      string    `json:"by,omitempty"`      // who decided
}

// pruneAppeals forgets actions too old to appeal.
func pruneAppeals(m map[string]appeal, now time.Time) {
	for id, a := range m {
		if now.Sub(a.At) > appealKeep {
			delete(m, id)
		}
	}
}

// appealAction names what is being appealed.
func (a appeal) action() string {
	switch {
	case !a.WarnAt.IsZero() && a.Timeout:
		return "a warning and a 1 hour timeout"
	case !a.WarnAt.IsZero():
		return "a warning"
	}
	return "a 1 hour timeout"
}

// appealButtons are the staff's choices for an appeal.
func appealButtons(id string, a appeal) []discordgo.MessageComponent {
	var row discordgo.ActionsRow
	if !a.WarnAt.IsZero() {
		row.Components = append(row.Components, discordgo.Button{Label: "Remove warning", Style: discordgo.SuccessButton, CustomID: "appeal-rmwarn:" + id})
	}
	if a.Timeout {
		row.Components = append(row.Components, discordgo.Button{Label: "End timeout", Style: discordgo.SuccessButton, CustomID: "appeal-endto:" + id})
	}
	row.Components = append(row.Components, discordgo.Button{Label: "Reject", Style: discordgo.DangerButton, CustomID: "appeal-reject:" + id})
	return []discordgo.MessageComponent{row}
}

// removeWarning drops the warning given at at.
func removeWarning(list []warning, at time.Time) ([]warning, bool) {
	for n, w := range list {
		if w.At.Equal(at) {
			return append(list[:n:n], list[n+1:]...), true
		}
	}
	return list, false
}

func interactionUser(i *discordgo.InteractionCreate) *discordgo.User {
	if i.Member != nil {
		return i.Member.User
	}
	return i.User
}

func (b *bot) dm(userID string, e *discordgo.MessageEmbed) {
	if ch, err := b.s.UserChannelCreate(userID); err == nil {
		_, _ = b.s.ChannelMessageSendEmbed(ch.ID, e)
	}
}

// appealDM sends the member e with an Appeal button and records the action.
// Members who don't take DMs from the server can't appeal.
func (b *bot) appealDM(userID string, e *discordgo.MessageEmbed, a appeal) {
	ch, err := b.s.UserChannelCreate(userID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	a.At = now
	var id string
	b.store.update(func(d *storeData) {
		if d.Appeals == nil {
			d.Appeals = map[string]appeal{}
		}
		pruneAppeals(d.Appeals, now)
		id = strconv.FormatInt(now.UnixNano(), 36)
		for _, taken := d.Appeals[id]; taken; _, taken = d.Appeals[id] {
			id += "0"
		}
		d.Appeals[id] = a
	})
	e.Footer = &discordgo.MessageEmbedFooter{Text: "Think this was a mistake? You can appeal once."}
	if _, err := b.s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{e},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Appeal", Style: discordgo.SecondaryButton, CustomID: "appeal:" + id},
		}}},
	}); err != nil {
		b.store.update(func(d *storeData) { delete(d.Appeals, id) })
	}
}

// dmInteraction handles the appeal button and form in DMs, where the
// interaction has a user but no member. It reports whether it handled i.
func (b *bot) dmInteraction(i *discordgo.InteractionCreate) bool {
	switch i.Type {
	case discordgo.InteractionMessageComponent:
		if id, ok := strings.CutPrefix(i.MessageComponentData().CustomID, "appeal:"); ok {
			b.appealForm(i, id)
			return true
		}
		if showID, ok := strings.CutPrefix(i.MessageComponentData().CustomID, "follow-stop:"); ok {
			b.stopFollowButton(i, showID) // the button on episode DMs
			return true
		}
	case discordgo.InteractionModalSubmit:
		if id, ok := strings.CutPrefix(i.ModalSubmitData().CustomID, "appeal-form:"); ok {
			b.sendAppeal(i, id)
			return true
		}
	}
	return false
}

func (b *bot) appealForm(i *discordgo.InteractionCreate, id string) {
	user := interactionUser(i)
	var a appeal
	var found bool
	b.store.view(func(d *storeData) { a, found = d.Appeals[id] })
	if !found || user == nil || a.User != user.ID {
		b.respond(i, true, errorEmbed("This can't be appealed anymore."))
		return
	}
	if a.Text != "" {
		b.respond(i, true, errorEmbed("You already appealed this. The mods will DM you their answer."))
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{CustomID: "appeal-form:" + id, Title: "Appeal", Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{
				CustomID: "why", Label: "Why was it wrong?", Style: discordgo.TextInputParagraph, Required: true, MinLength: 10, MaxLength: 1000,
				Placeholder: "What happened, from your side"}}},
		}},
	}); err != nil {
		log.Printf("appeal form: %v", err)
	}
}

func (b *bot) sendAppeal(i *discordgo.InteractionCreate, id string) {
	user := interactionUser(i)
	text := modalValues(i)["why"]
	var a appeal
	var found, already bool
	b.store.update(func(d *storeData) {
		a, found = d.Appeals[id]
		if !found || user == nil || a.User != user.ID {
			found = false
			return
		}
		if a.Text != "" {
			already = true
			return
		}
		a.Text, a.Status = text, "open"
		d.Appeals[id] = a
	})
	switch {
	case !found:
		b.respond(i, true, errorEmbed("This can't be appealed anymore."))
		return
	case already:
		b.respond(i, true, errorEmbed("You already appealed this. The mods will DM you their answer."))
		return
	}
	e := &discordgo.MessageEmbed{Title: "Appeal", Color: shu,
		Description: fmt.Sprintf("<@%s> (%s) appeals %s: **%s**", a.User, user.Username, a.action(), clip(a.Reason, 300)),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Their side", Value: clip(text, 1000)},
			{Name: "When", Value: fmt.Sprintf("<t:%d:f>", a.At.Unix())},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
	b.modLog(e, appealButtons(id, a), false)
	// Swap the Appeal button for a disabled one on the DM.
	sent := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Appeal sent", Style: discordgo.SecondaryButton, CustomID: "appeal:" + id, Disabled: true},
	}}}
	if i.Message != nil && len(i.Message.Embeds) > 0 {
		i.Message.Embeds[0].Footer = &discordgo.MessageEmbedFooter{Text: "Appeal sent. The mods will DM you their answer."}
		if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{Embeds: i.Message.Embeds, Components: sent},
		}); err != nil {
			log.Printf("appeal sent: %v", err)
		}
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Your appeal was sent to the mods. They'll DM you their answer."})
}

// appealDecision handles the staff buttons on an appeal.
func (b *bot) appealDecision(i *discordgo.InteractionCreate, customID string) {
	if i.Member.Permissions&(discordgo.PermissionModerateMembers|discordgo.PermissionAdministrator) == 0 {
		b.respond(i, true, errorEmbed("Only moderators can do that."))
		return
	}
	kind, id, ok := strings.Cut(strings.TrimPrefix(customID, "appeal-"), ":")
	if !ok {
		return
	}
	mod := i.Member.User.ID
	if kind != "rmwarn" && kind != "endto" && kind != "reject" {
		return
	}
	// Claim the appeal first, so two mods pressing at once can't both act.
	var a appeal
	var claimed bool
	b.store.update(func(d *storeData) {
		x, ok := d.Appeals[id]
		if !ok || x.Status != "open" {
			return
		}
		a, claimed = x, true
		x.Status, x.By = kind, mod
		d.Appeals[id] = x
	})
	if !claimed {
		b.respond(i, true, errorEmbed("This appeal was already handled."))
		return
	}
	var outcome, answer, status string
	switch kind {
	case "rmwarn":
		b.store.update(func(d *storeData) {
			d.Warnings[a.User], _ = removeWarning(d.Warnings[a.User], a.WarnAt)
			if len(d.Warnings[a.User]) == 0 {
				delete(d.Warnings, a.User)
			}
		})
		outcome, status = "Warning removed", "removed"
		answer = "The mods accepted your appeal and removed the warning."
		if a.Timeout {
			if err := b.s.GuildMemberTimeout(b.cfg.GuildID, a.User, nil, discordgo.WithAuditLogReason("Appeal accepted")); err != nil {
				log.Printf("appeal end timeout: %v", err)
			}
			outcome += " and timeout ended"
			answer = "The mods accepted your appeal: the warning is removed and your timeout is over."
		}
	case "endto":
		if err := b.s.GuildMemberTimeout(b.cfg.GuildID, a.User, nil, discordgo.WithAuditLogReason("Appeal accepted")); err != nil {
			log.Printf("appeal end timeout: %v", err)
			b.store.update(func(d *storeData) {
				if x, ok := d.Appeals[id]; ok {
					x.Status, x.By = "open", ""
					d.Appeals[id] = x
				}
			})
			b.respond(i, true, errorEmbed("Couldn't end the timeout. The bot needs **Timeout Members** and a role above theirs."))
			return
		}
		outcome, status = "Timeout ended", "ended"
		answer = "The mods accepted your appeal and ended your timeout."
		if !a.WarnAt.IsZero() {
			answer += " The warning stays on record."
		}
	case "reject":
		outcome, status = "Rejected", "rejected"
		answer = "The mods looked at your appeal and the decision stays."
	}
	b.store.update(func(d *storeData) {
		if x, ok := d.Appeals[id]; ok {
			x.Status = status
			d.Appeals[id] = x
		}
	})
	b.dm(a.User, &discordgo.MessageEmbed{Title: "Your appeal", Color: shu,
		Description: answer + fmt.Sprintf("\n\nIt was about %s: **%s**", a.action(), clip(a.Reason, 300))})
	if len(i.Message.Embeds) == 0 {
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: outcome + "."})
		return
	}
	e := i.Message.Embeds[0]
	e.Color = 0x8A8276
	e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Handled", Value: fmt.Sprintf("%s by <@%s>", outcome, mod)})
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: []discordgo.MessageComponent{}},
	}); err != nil {
		log.Printf("appeal decision: %v", err)
	}
}
