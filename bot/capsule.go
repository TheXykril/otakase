package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The time capsule: Otakase's birthday is April 12 (the first was in 2026).
// All year, members seal a message for the next birthday with /capsule or
// from the app. On April 12 the capsule opens: the server gets a post with
// one message picked at random, and every app shows one as it starts.
//
// Nobody has to approve anything. Every message goes through the filter
// (capsulefilter.go) and, when Ollama is set up, a meaning check against
// examples of insults, creepy and personal messages. One that's shown and
// reported by 3 members hides itself. Staff can still take any message out
// with the Remove button on its line in mod chat, or /capsule-remove.
//
//	POST   /v1/capsule {"text":"…"} (auth)  → {"opens":2027,"replaced":false} | 422 {"error":"why"}
//	GET    /v1/capsule/mine (auth)          → {"text":"…","opens":2027} | 404
//	DELETE /v1/capsule (auth)               → {}
//	GET    /v1/capsule                      → {"id","text","written":"November 2026","year":2027} on the birthday, else 404
//	POST   /v1/capsule/report {"id"} (auth) → {"hidden":false}

const (
	capsuleReportsToHide = 3
	capsuleMemberAge     = 7 * 24 * time.Hour
	// capsulePostHour is the UTC hour of April 12 the server post goes up.
	capsulePostHour = 9
	// capsuleFirstYear is the year Otakase was born.
	capsuleFirstYear = 2026
)

// capsuleNote is one sealed message.
type capsuleNote struct {
	ID      string    `json:"id"`
	User    string    `json:"user"`
	Text    string    `json:"text"`
	Written time.Time `json:"written"`
	Opens   int       `json:"opens"` // the birthday's year
	// Checked is set once the meaning check passed it, or straight away
	// when there's no Ollama. Unchecked messages are never shown.
	Checked   bool     `json:"checked,omitempty"`
	Hidden    bool     `json:"hidden,omitempty"` // reported by enough members
	Reporters []string `json:"reporters,omitempty"`
	LogMsg    string   `json:"log_msg,omitempty"` // its line in mod chat
}

var (
	capsuleSealLimit   = newRateLimit(5, time.Hour)              // tries per member, blocked ones too
	capsuleOpenLimit   = newRateLimit(30, time.Hour)             // GET /v1/capsule per IP
	capsuleReportLimit = newRateLimit(10, time.Hour)             // reports per member
	capsuleRand        = func(n int) int { return rand.IntN(n) } // for tests
)

func init() {
	registerCommand(&discordgo.ApplicationCommand{Name: "capsule", Description: "Seal a message in the time capsule, opened on Otakase's birthday (April 12)",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "message", Description: "Your message for next April 12 (leave out to see yours)", MaxLength: capsuleMaxRunes},
		}}, (*bot).capsuleCommand)
	manage := int64(discordgo.PermissionManageMessages)
	registerCommand(&discordgo.ApplicationCommand{Name: "capsule-remove", Description: "Take a message out of the time capsule",
		DefaultMemberPermissions: &manage,
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "id", Description: "The message's id, shown on its line in mod chat", Required: true},
		}}, (*bot).capsuleRemoveCommand)
	registerComponent("capsule-another:", (*bot).capsuleAnother)
	registerComponent("capsule-report:", (*bot).capsuleReportButton)
	registerComponent("capsule-remove:", (*bot).capsuleRemoveButton)
	registerComponent("capsule-unseal:", (*bot).capsuleUnsealButton)
	registerJob("Time capsule", (*bot).capsuleLoop)
	registerHelp("`/capsule` seal a message for the time capsule, opened on Otakase's birthday, April 12")
	apiHandle("POST /v1/capsule", (*bot).apiCapsuleSeal)
	apiHandle("GET /v1/capsule/mine", (*bot).apiCapsuleMine)
	apiHandle("DELETE /v1/capsule", (*bot).apiCapsuleUnseal)
	apiHandle("GET /v1/capsule", (*bot).apiCapsuleOpen)
	apiHandle("POST /v1/capsule/report", (*bot).apiCapsuleReport)
}

// capsuleBirthday is midnight UTC on April 12 of year.
func capsuleBirthday(year int) time.Time {
	return time.Date(year, time.April, 12, 0, 0, 0, 0, time.UTC)
}

// The birthday lasts from when April 12 starts anywhere (UTC+14) until it
// ends everywhere (UTC-12).
const (
	capsuleEarliestZone = 14 * time.Hour
	capsuleLatestZone   = 36 * time.Hour
)

// capsuleOpens is the birthday a message sealed at t opens on: this year's
// if it hasn't started anywhere yet, else next year's.
func capsuleOpens(t time.Time) int {
	y := t.UTC().Year()
	if t.Before(capsuleBirthday(y).Add(-capsuleEarliestZone)) {
		return y
	}
	return y + 1
}

// capsuleOpenYear is the birthday going on at t somewhere in the world.
func capsuleOpenYear(t time.Time) (int, bool) {
	y := t.UTC().Year()
	day := capsuleBirthday(y)
	if !t.Before(day.Add(-capsuleEarliestZone)) && t.Before(day.Add(capsuleLatestZone)) {
		return y, true
	}
	return 0, false
}

// capsuleShowable is a message that may be shown for the birthday of year.
func capsuleShowable(n capsuleNote, year int) bool {
	return n.Opens == year && n.Checked && !n.Hidden
}

// pickCapsule is a random showable message for year, other than skip.
func pickCapsule(notes map[string]capsuleNote, year int, skip string) (capsuleNote, bool) {
	var list []capsuleNote
	for _, n := range notes {
		if capsuleShowable(n, year) && n.ID != skip {
			list = append(list, n)
		}
	}
	if len(list) == 0 {
		return capsuleNote{}, false
	}
	// Map order is random already, but not uniformly so.
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list[capsuleRand(len(list))], true
}

// capsuleOf is user's message for the birthday of year.
func capsuleOf(notes map[string]capsuleNote, user string, year int) (capsuleNote, bool) {
	for _, n := range notes {
		if n.User == user && n.Opens == year {
			return n, true
		}
	}
	return capsuleNote{}, false
}

// capsuleMemberReason says why a member can't seal a message yet, "".
func capsuleMemberReason(m *discordgo.Member, now time.Time) string {
	if m == nil {
		return "Join the Otakase Discord to leave a message in the time capsule."
	}
	if m.User != nil && m.User.Bot {
		return "Bots don't get a time capsule."
	}
	if !m.JoinedAt.IsZero() && now.Sub(m.JoinedAt) < capsuleMemberAge {
		days := int((capsuleMemberAge-now.Sub(m.JoinedAt)).Hours()/24) + 1
		return fmt.Sprintf("New members can leave a time capsule message after their first week here. Come back in %s.", plural(days, "day"))
	}
	return ""
}

// sealCapsule checks text and seals it as user's message for the next
// birthday, in place of one they sealed before. reason, when not "", tells
// the member why it was not sealed.
func (b *bot) sealCapsule(user, text string, now time.Time) (note capsuleNote, replaced bool, reason string) {
	text = cleanText(text, false)
	if !capsuleSealLimit.allow(user, now) {
		return note, false, "That's a lot of tries. Give it an hour and try again."
	}
	if r := capsuleTextReason(text); r != "" {
		return note, false, r
	}
	blocked, checked := b.capsuleMeaning(text)
	if blocked {
		return note, false, "That doesn't sound friendly enough for the time capsule. Try saying it another way."
	}
	note = capsuleNote{ID: newToken(4), User: user, Text: text, Written: now.UTC(), Opens: capsuleOpens(now), Checked: checked}
	var old capsuleNote
	b.store.update(func(d *storeData) {
		if d.Capsules == nil {
			d.Capsules = map[string]capsuleNote{}
		}
		if old, replaced = capsuleOf(d.Capsules, user, note.Opens); replaced {
			delete(d.Capsules, old.ID)
		}
		d.Capsules[note.ID] = note
	})
	if replaced {
		b.capsuleLogDone(old, "Replaced by a newer message from the same member.")
	}
	if id := b.capsuleLog(note); id != "" {
		b.store.update(func(d *storeData) {
			if n, ok := d.Capsules[note.ID]; ok {
				n.LogMsg = id
				d.Capsules[note.ID] = n
			}
		})
	}
	log.Printf("time capsule: %s sealed %s for %d", user, note.ID, note.Opens)
	return note, replaced, ""
}

// takeCapsule removes a message by id and says it in mod chat.
func (b *bot) takeCapsule(id, why string) (capsuleNote, bool) {
	var n capsuleNote
	var ok bool
	b.store.update(func(d *storeData) {
		if n, ok = d.Capsules[id]; ok {
			delete(d.Capsules, id)
		}
	})
	if ok {
		b.capsuleLogDone(n, why)
	}
	return n, ok
}

// reportCapsule counts user's report of a message and hides it at
// capsuleReportsToHide reports. counted is false for a repeat report.
func (b *bot) reportCapsule(user, id string) (hidden, counted, found bool) {
	var n capsuleNote
	b.store.update(func(d *storeData) {
		if n, found = d.Capsules[id]; !found || n.User == user || contains(n.Reporters, user) {
			return
		}
		counted = true
		n.Reporters = append(n.Reporters, user)
		if len(n.Reporters) >= capsuleReportsToHide && !n.Hidden {
			n.Hidden = true
			hidden = true
		}
		d.Capsules[id] = n
	})
	if hidden {
		b.capsuleLogDone(n, fmt.Sprintf("Hidden after %d reports.", capsuleReportsToHide))
	}
	return hidden, counted, found
}

func capsuleWritten(t time.Time) string { return t.UTC().Format("January 2006") }

// capsuleLogEmbed is a message's line in mod chat.
func capsuleLogEmbed(n capsuleNote) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: "Time capsule message", Color: shu, Description: quoteLines(n.Text),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "From", Value: "<@" + n.User + ">", Inline: true},
			{Name: "Opens", Value: fmt.Sprintf("April 12, %d", n.Opens), Inline: true},
			{Name: "Id", Value: "`" + n.ID + "`", Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{Text: "Passed the filter. Remove it if it shouldn't be there."}}
}

// capsuleLog puts a new message in mod chat with a Remove button and
// returns that line's id.
func (b *bot) capsuleLog(n capsuleNote) string {
	if b.s == nil || b.cfg.ModChannel == "" {
		return ""
	}
	m, err := b.s.ChannelMessageSendComplex(b.cfg.ModChannel, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{capsuleLogEmbed(n)}, AllowedMentions: &discordgo.MessageAllowedMentions{},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Remove", Style: discordgo.DangerButton, CustomID: "capsule-remove:" + n.ID},
		}}}})
	if err != nil {
		log.Printf("time capsule log: %v", err)
		return ""
	}
	return m.ID
}

// capsuleLogDone marks a message's line in mod chat with what became of it.
func (b *bot) capsuleLogDone(n capsuleNote, why string) {
	if b.s == nil || b.cfg.ModChannel == "" || n.LogMsg == "" {
		return
	}
	e := capsuleLogEmbed(n)
	e.Color = 0x8A8276
	e.Footer = &discordgo.MessageEmbedFooter{Text: why}
	comps := []discordgo.MessageComponent{}
	if _, err := b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: n.LogMsg, Channel: b.cfg.ModChannel,
		Embeds: &[]*discordgo.MessageEmbed{e}, Components: &comps}); err != nil {
		log.Printf("time capsule log edit: %v", err)
	}
}

// quoteLines quotes text as a Discord block quote, mentions defused.
func quoteLines(text string) string {
	return "> " + quiet(text)
}

// capsuleView shows an opened message, with Report and Open another.
func capsuleView(n capsuleNote, year int, title string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	e := &discordgo.MessageEmbed{Title: title, Color: shu,
		Description: quoteLines(n.Text) + "\n\n— someone here, " + capsuleWritten(n.Written),
		Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Leave one for April 12, %d with /capsule", year+1)}}
	return e, []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Open another", Style: discordgo.PrimaryButton, CustomID: "capsule-another:"},
		discordgo.Button{Label: "Report", Style: discordgo.SecondaryButton, CustomID: "capsule-report:" + n.ID},
	}}}
}

// capsuleAge is "Otakase turns 1 today" and so on.
func capsuleAge(year int) string {
	return fmt.Sprintf("Otakase turns %d today", year-capsuleFirstYear)
}

// capsuleLoop checks messages Ollama couldn't check when they were sealed,
// and opens the capsule in the server on the birthday.
func (b *bot) capsuleLoop() {
	for {
		b.capsuleRecheck()
		b.capsulePostIfDue(appNow())
		time.Sleep(20 * time.Minute)
	}
}

func (b *bot) capsuleRecheck() {
	var todo []capsuleNote
	b.store.view(func(d *storeData) {
		for _, n := range d.Capsules {
			if !n.Checked {
				todo = append(todo, n)
			}
		}
	})
	for _, n := range todo {
		blocked, checked := b.capsuleMeaning(n.Text)
		if !checked {
			return // Ollama still away; try again next round
		}
		if blocked {
			if _, ok := b.takeCapsule(n.ID, "Taken out by the meaning check."); ok && b.s != nil {
				b.dm(n.User, &discordgo.MessageEmbed{Color: shu, Title: "Time capsule",
					Description: "Your time capsule message didn't pass the filter, so it was taken out. You can write a new one with `/capsule` in the server."})
			}
			continue
		}
		b.store.update(func(d *storeData) {
			if cur, ok := d.Capsules[n.ID]; ok {
				cur.Checked = true
				d.Capsules[n.ID] = cur
			}
		})
	}
}

// capsulePostIfDue opens the capsule in the server once, on April 12 from
// capsulePostHour UTC.
func (b *bot) capsulePostIfDue(now time.Time) {
	year, open := capsuleOpenYear(now)
	if !open || now.Before(capsuleBirthday(year).Add(capsulePostHour*time.Hour)) || b.s == nil {
		return
	}
	var done bool
	var n capsuleNote
	var have bool
	b.store.view(func(d *storeData) {
		done = d.CapsuleOpened >= year
		n, have = pickCapsule(d.Capsules, year, "")
	})
	if done {
		return
	}
	channel := b.capsuleChannel()
	if channel == "" {
		return
	}
	msg := &discordgo.MessageSend{AllowedMentions: &discordgo.MessageAllowedMentions{}}
	if have {
		e, comps := capsuleView(n, year, capsuleAge(year))
		e.Description = "The time capsule is open. One of the messages left here this past year:\n\n" + e.Description
		msg.Embeds, msg.Components = []*discordgo.MessageEmbed{e}, comps
	} else {
		msg.Embeds = []*discordgo.MessageEmbed{{Title: capsuleAge(year), Color: shu,
			Description: fmt.Sprintf("The time capsule is open, and empty this time. Leave a message for April 12, %d with `/capsule`.", year+1)}}
	}
	if _, err := b.s.ChannelMessageSendComplex(channel, msg); err != nil {
		log.Printf("time capsule post: %v", err)
		return
	}
	b.store.update(func(d *storeData) { d.CapsuleOpened = year })
}

// capsuleChannel is the chat channel the birthday post goes in, else
// announcements.
func (b *bot) capsuleChannel() string {
	if id := b.textChannelNamed("general", "chat", "lounge", "off-topic"); id != "" {
		return id
	}
	return b.announcementsChannel()
}

func (b *bot) capsuleCommand(i *discordgo.InteractionCreate) {
	user := i.Member.User.ID
	var text string
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "message" {
			text = o.StringValue()
		}
	}
	now := appNow()
	if text == "" {
		var n capsuleNote
		var have bool
		b.store.view(func(d *storeData) { n, have = capsuleOf(d.Capsules, user, capsuleOpens(now)) })
		if !have {
			b.respond(i, true, &discordgo.MessageEmbed{Title: "Time capsule", Color: shu,
				Description: fmt.Sprintf("Seal a message with `/capsule message:` and it opens on Otakase's birthday, April 12, %d. "+
					"Someone in the server or in the app might read it that day. No names, links or contact details.", capsuleOpens(now))})
			return
		}
		b.respond(i, true, capsuleMineEmbed(n), discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Take it out", Style: discordgo.SecondaryButton, CustomID: "capsule-unseal:" + n.ID}}})
		return
	}
	if r := capsuleMemberReason(i.Member, now); r != "" {
		b.respond(i, true, errorEmbed("%s", r))
		return
	}
	// The meaning check can take a moment.
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		n, replaced, reason := b.sealCapsule(user, text, now)
		if reason != "" {
			return errorEmbed("%s", reason), nil
		}
		return capsuleSealedEmbed(n, replaced), nil
	})
}

func capsuleSealedEmbed(n capsuleNote, replaced bool) *discordgo.MessageEmbed {
	what := "Sealed."
	if replaced {
		what = "Sealed, in place of your earlier message."
	}
	return &discordgo.MessageEmbed{Title: "Time capsule", Color: shu,
		Description: fmt.Sprintf("%s It opens on April 12, %d.\n\n%s", what, n.Opens, quoteLines(n.Text)),
		Footer:      &discordgo.MessageEmbedFooter{Text: "/capsule without a message shows it or takes it out"}}
}

func capsuleMineEmbed(n capsuleNote) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: "Your time capsule message", Color: shu,
		Description: fmt.Sprintf("%s\n\nOpens on April 12, %d. Seal a new one with `/capsule message:` to replace it.", quoteLines(n.Text), n.Opens)}
}

func (b *bot) capsuleRemoveCommand(i *discordgo.InteractionCreate) {
	if !b.staffOnly(i) {
		return
	}
	id := i.ApplicationCommandData().Options[0].StringValue()
	if _, ok := b.takeCapsule(id, "Removed by "+i.Member.User.Username+"."); !ok {
		b.respond(i, true, errorEmbed("No time capsule message with id `%s`.", quiet(id)))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Taken out of the time capsule."})
}

func (b *bot) capsuleRemoveButton(i *discordgo.InteractionCreate, id string) {
	if !b.staffOnly(i) {
		return
	}
	if _, ok := b.takeCapsule(id, "Removed by "+i.Member.User.Username+"."); !ok {
		b.respond(i, true, errorEmbed("That message is already gone."))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Taken out of the time capsule."})
}

func (b *bot) capsuleUnsealButton(i *discordgo.InteractionCreate, id string) {
	user := interactionUser(i).ID
	var mine bool
	b.store.view(func(d *storeData) { mine = d.Capsules[id].User == user })
	if !mine {
		b.respond(i, true, errorEmbed("That message is already gone."))
		return
	}
	b.takeCapsule(id, "Taken out by the member who wrote it.")
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Taken out. Seal a new one any time with `/capsule`."})
}

func (b *bot) capsuleAnother(i *discordgo.InteractionCreate, _ string) {
	year, open := capsuleOpenYear(appNow())
	if !open {
		b.respond(i, true, errorEmbed("The time capsule is closed again until next April 12."))
		return
	}
	var n capsuleNote
	var have bool
	b.store.view(func(d *storeData) { n, have = pickCapsule(d.Capsules, year, "") })
	if !have {
		b.respond(i, true, errorEmbed("No more messages in the time capsule this year."))
		return
	}
	e, comps := capsuleView(n, year, "From the time capsule")
	b.respond(i, true, e, comps...)
}

func (b *bot) capsuleReportButton(i *discordgo.InteractionCreate, id string) {
	user := interactionUser(i).ID
	if !capsuleReportLimit.allow(user, appNow()) {
		b.respond(i, true, errorEmbed("That's a lot of reports. Try again later, or tell a moderator."))
		return
	}
	hidden, _, found := b.reportCapsule(user, id)
	if !found {
		b.respond(i, true, errorEmbed("That message is already gone."))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Thanks, reported. Enough reports take a message out on their own."})
	// The server's birthday post shows the next message instead.
	if hidden && i.Message != nil && i.Message.Flags&discordgo.MessageFlagsEphemeral == 0 {
		b.capsuleSwapPost(i.Message, id)
	}
}

// capsuleSwapPost puts another message on the birthday post in place of
// a hidden one.
func (b *bot) capsuleSwapPost(m *discordgo.Message, hidden string) {
	year, open := capsuleOpenYear(appNow())
	if !open {
		year = capsuleOpens(appNow()) - 1
	}
	var n capsuleNote
	var have bool
	b.store.view(func(d *storeData) { n, have = pickCapsule(d.Capsules, year, hidden) })
	var embeds []*discordgo.MessageEmbed
	comps := []discordgo.MessageComponent{}
	if have {
		e, c := capsuleView(n, year, capsuleAge(year))
		e.Description = "The time capsule is open. One of the messages left here this past year:\n\n" + e.Description
		embeds, comps = []*discordgo.MessageEmbed{e}, c
	} else {
		embeds = []*discordgo.MessageEmbed{{Title: capsuleAge(year), Color: shu, Description: "The time capsule is open."}}
	}
	if _, err := b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: m.ID, Channel: m.ChannelID, Embeds: &embeds, Components: &comps}); err != nil {
		log.Printf("time capsule post edit: %v", err)
	}
}

// The app's side.

func (b *bot) apiCapsuleSeal(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, 4<<10, &req) {
		return
	}
	now := appNow()
	if b.s != nil {
		m, err := b.s.GuildMember(b.cfg.GuildID, user)
		if err != nil {
			m = nil
		}
		if reason := capsuleMemberReason(m, now); reason != "" {
			apiError(w, http.StatusForbidden, reason)
			return
		}
	}
	n, replaced, reason := b.sealCapsule(user, req.Text, now)
	if reason != "" {
		apiError(w, http.StatusUnprocessableEntity, reason)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"opens": n.Opens, "replaced": replaced})
}

func (b *bot) apiCapsuleMine(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	var n capsuleNote
	var have bool
	b.store.view(func(d *storeData) { n, have = capsuleOf(d.Capsules, user, capsuleOpens(appNow())) })
	if !have {
		apiError(w, http.StatusNotFound, "no message sealed yet")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": n.Text, "opens": n.Opens})
}

func (b *bot) apiCapsuleUnseal(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	var n capsuleNote
	var have bool
	b.store.view(func(d *storeData) { n, have = capsuleOf(d.Capsules, user, capsuleOpens(appNow())) })
	if have {
		b.takeCapsule(n.ID, "Taken out by the member who wrote it.")
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (b *bot) apiCapsuleOpen(w http.ResponseWriter, r *http.Request) {
	now := appNow()
	if !capsuleOpenLimit.allow(clientIP(r), now) {
		apiError(w, http.StatusTooManyRequests, "try again later")
		return
	}
	year, open := capsuleOpenYear(now)
	if !open {
		apiError(w, http.StatusNotFound, "the time capsule opens on April 12")
		return
	}
	var n capsuleNote
	var have bool
	b.store.view(func(d *storeData) { n, have = pickCapsule(d.Capsules, year, "") })
	if !have {
		apiError(w, http.StatusNotFound, "the time capsule is empty this year")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": n.ID, "text": n.Text, "written": capsuleWritten(n.Written), "year": year})
}

func (b *bot) apiCapsuleReport(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !readJSON(w, r, 1<<10, &req) {
		return
	}
	if !capsuleReportLimit.allow(user, appNow()) {
		apiError(w, http.StatusTooManyRequests, "that's a lot of reports, try again later")
		return
	}
	hidden, _, found := b.reportCapsule(user, req.ID)
	if !found {
		apiError(w, http.StatusNotFound, "that message is already gone")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hidden": hidden})
}

// The meaning check.

// capsuleBadExamples are what the time capsule should never hold. A message
// closer in meaning to one of these than to any good example (and close
// enough) is blocked.
var capsuleBadExamples = []string{
	"you are stupid and worthless", "everyone here is an idiot", "I hate all of you", "nobody likes you, just leave",
	"go kill yourself", "I hope you die", "I want to hurt someone", "I will find you and make you pay",
	"you're ugly and pathetic", "this server is trash and so are you", "people like them are disgusting animals",
	"I know where you live", "send me pictures of yourself", "message me privately, I'm lonely, how old are you",
	"add me on snapchat", "here is my phone number, call me", "my home address is", "my password is",
	"the admins are losers", "this app is garbage and the developer is an idiot", "want to see something naughty",
	"buy cheap followers, free nitro, click here",
}

// capsuleGoodExamples are the kind of messages the capsule is for.
var capsuleGoodExamples = []string{
	"happy birthday otakase", "hope you are all still watching anime next year", "whoever reads this, have a great day",
	"I hope I finally finished one piece by now", "thanks for making this app", "can't wait for the next season",
	"stay healthy and keep watching", "greetings from last year", "this community is awesome",
	"I hope my favourite show got a sequel", "remember to drink water and sleep", "I'm proud of how much I watched this year",
	"to future me: I hope you're happy", "frieren was the best show of the year", "hello from the past",
}

var capsuleExamples = struct {
	sync.Mutex
	bad, good [][]float64
}{}

// capsuleMeaning compares text in meaning with the examples. checked is
// false when Ollama couldn't be asked; with no Ollama set up, every message
// counts as checked.
func (b *bot) capsuleMeaning(text string) (blocked, checked bool) {
	if b.sem == nil || b.sem.url == "" {
		return false, true
	}
	capsuleExamples.Lock()
	if capsuleExamples.bad == nil {
		bad, err := b.capsuleEmbedAll(capsuleBadExamples)
		if err == nil {
			var good [][]float64
			if good, err = b.capsuleEmbedAll(capsuleGoodExamples); err == nil {
				capsuleExamples.bad, capsuleExamples.good = bad, good
			}
		}
		if err != nil {
			capsuleExamples.Unlock()
			log.Printf("time capsule meaning check: %v", err)
			return false, false
		}
	}
	bad, good := capsuleExamples.bad, capsuleExamples.good
	capsuleExamples.Unlock()
	v, err := b.sem.embed([]string{text})
	if err != nil {
		log.Printf("time capsule meaning check: %v", err)
		return false, false
	}
	return capsuleJudged(closest(v[0], bad), closest(v[0], good), b.settingFloat("capsule_threshold")), true
}

// capsuleEmbedAll embeds texts one at a time (see semantic.load).
func (b *bot) capsuleEmbedAll(texts []string) ([][]float64, error) {
	var out [][]float64
	for _, t := range texts {
		v, err := b.sem.embed([]string{t})
		if err != nil {
			return nil, err
		}
		out = append(out, v[0])
	}
	return out, nil
}

func closest(v []float64, examples [][]float64) float64 {
	best := -1.0
	for _, e := range examples {
		best = max(best, cosine(v, e))
	}
	return best
}

// capsuleJudged blocks a message that is close enough to a bad example and
// closer to it than to any good one.
func capsuleJudged(bad, good, threshold float64) bool {
	return bad >= threshold && bad > good
}
