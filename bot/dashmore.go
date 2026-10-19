package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// More dashboard controls: texts, known issue notes, bot statuses, slash
// commands, data backups, voice rooms, watch parties, level roles and
// starting a guess round.

const (
	knownNotesMax = 10
	statusesMax   = 10
	statusMaxLen  = 128
)

// dashMoreControls are the controls in this file, by their "do".
var dashMoreControls = map[string]func(b *bot, in dashInput, who string) error{
	"text-save":    (*bot).dashTextSave,
	"text-reset":   (*bot).dashTextReset,
	"known-add":    (*bot).dashKnownAdd,
	"known-remove": (*bot).dashKnownRemove,
	"known-refresh": func(b *bot, _ dashInput, _ string) error {
		return b.dashKnownRefresh("The known issues post is being updated now.")
	},
	"status-add":        (*bot).dashStatusAdd,
	"status-remove":     (*bot).dashStatusRemove,
	"status-reset":      (*bot).dashStatusReset,
	"commands":          (*bot).dashCommands,
	"restore":           (*bot).dashRestore,
	"room-rename":       (*bot).dashRoomRename,
	"room-close":        (*bot).dashRoomClose,
	"party-cancel":      (*bot).dashPartyCancel,
	"level-role-add":    (*bot).dashLevelRoleAdd,
	"level-role-remove": (*bot).dashLevelRoleRemove,
	"level-roles-reset": (*bot).dashLevelRolesReset,
	"riddle-remove":     (*bot).dashRiddleRemove,
}

func dashNotConnected(b *bot) error {
	if b.s == nil || b.s.State == nil || b.s.State.User == nil {
		return fmt.Errorf("The bot isn't connected to Discord.")
	}
	return nil
}

// Texts.

func (b *bot) dashTexts() template.HTML {
	var set map[string]string
	b.store.view(func(d *storeData) {
		set = map[string]string{}
		for k, v := range d.Texts {
			set[k] = v
		}
	})
	parts := []template.HTML{dashNote("What the bot says in these places. Discord markdown works. Saving works at once; Reset goes back to the built-in text. The rules and FAQ channel posts aren't the bot's, so edit those in Discord.")}
	for _, t := range editableTexts() {
		now := "built-in"
		ctls := []dashControl{{Section: "texts", Do: "text-save", Name: t.Key, Text: "Text", TextValue: b.text(t.Key), Button: "Save"}}
		if strings.TrimSpace(set[t.Key]) != "" {
			now = "changed"
			ctls = append(ctls, dashControl{Section: "texts", Do: "text-reset", Name: t.Key, Button: "Reset", Quiet: true})
		}
		parts = append(parts, dashExec("setting", struct {
			Name, Desc, Now string
			Controls        template.HTML
		}{t.Label, "", now, dashControls(ctls...)}))
	}
	return dashJoin(parts...)
}

func (b *bot) dashTextSave(in dashInput, who string) error {
	t, ok := findText(in.Name)
	if !ok {
		return fmt.Errorf("There's no text called %s.", in.Name)
	}
	if in.Text == "" {
		return fmt.Errorf("The text is empty. Use Reset for the built-in one.")
	}
	if len([]rune(in.Text)) > 2000 {
		return fmt.Errorf("Keep it under 2000 characters.")
	}
	b.store.update(func(d *storeData) {
		if d.Texts == nil {
			d.Texts = map[string]string{}
		}
		d.Texts[t.Key] = in.Text
	})
	b.staffNote(&discordgo.MessageEmbed{Title: "Text changed", Color: shu, Description: fmt.Sprintf("%s changed **%s**.", who, t.Label)})
	setFlash(false, "Saved %s.", t.Label)
	return nil
}

func (b *bot) dashTextReset(in dashInput, who string) error {
	t, ok := findText(in.Name)
	if !ok {
		return fmt.Errorf("There's no text called %s.", in.Name)
	}
	b.store.update(func(d *storeData) { delete(d.Texts, t.Key) })
	b.staffNote(&discordgo.MessageEmbed{Title: "Text reset", Color: shu, Description: fmt.Sprintf("%s put **%s** back to the built-in text.", who, t.Label)})
	setFlash(false, "%s is back to the built-in text.", t.Label)
	return nil
}

// Known issues notes, on the support page.

func (b *bot) dashKnownNotes() template.HTML {
	var notes []string
	b.store.view(func(d *storeData) { notes = append(notes, d.KnownNotes...) })
	var rows [][]template.HTML
	for _, n := range notes {
		rows = append(rows, []template.HTML{dashText(template.HTMLEscapeString(n)),
			dashControls(dashControl{Section: "support", Do: "known-remove", Name: n, Button: "Remove", Quiet: true})})
	}
	parts := []template.HTML{
		dashHeading("Known issues post"),
		dashNote("The pinned post lists GitHub issues labelled known-issue by itself. Notes added here go above that list, for problems that aren't on GitHub."),
		dashHTMLTable([]string{"Note", ""}, rows, "No notes."),
	}
	var ctls []dashControl
	if len(notes) < knownNotesMax {
		ctls = append(ctls, dashControl{Section: "support", Do: "known-add", Line: "A known problem, like: Source X is down", Button: "Add"})
	}
	ctls = append(ctls, dashControl{Section: "support", Do: "known-refresh", Button: "Update post now", Quiet: true})
	return dashJoin(append(parts, dashControls(ctls...))...)
}

func (b *bot) dashKnownRefresh(done string) error {
	if b.s == nil || b.cfg.SupportForum == "" {
		return fmt.Errorf("There's no support forum for the post.")
	}
	go func() {
		if err := b.refreshKnownIssues(); err != nil {
			b.jobFailed("Known issues post", err)
		} else {
			b.jobOK("Known issues post")
		}
	}()
	setFlash(false, "%s", done)
	return nil
}

func (b *bot) dashKnownAdd(in dashInput, who string) error {
	note := strings.Join(strings.Fields(in.Text), " ")
	if note == "" || len([]rune(note)) > 200 {
		return fmt.Errorf("A note is one line of up to 200 characters.")
	}
	full := false
	b.store.update(func(d *storeData) {
		if full = len(d.KnownNotes) >= knownNotesMax; !full {
			d.KnownNotes = append(d.KnownNotes, note)
		}
	})
	if full {
		return fmt.Errorf("That's %d notes already. Remove one first.", knownNotesMax)
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s added a known issue note: %s", who, note)}, nil, false)
	if b.dashKnownRefresh("") != nil {
		setFlash(false, "Note saved. It shows once the support forum is found.")
		return nil
	}
	setFlash(false, "Note added. The post updates in a moment.")
	return nil
}

func (b *bot) dashKnownRemove(in dashInput, who string) error {
	found := false
	b.store.update(func(d *storeData) {
		for k, n := range d.KnownNotes {
			if n == in.Name {
				d.KnownNotes = append(d.KnownNotes[:k:k], d.KnownNotes[k+1:]...)
				found = true
				break
			}
		}
	})
	if !found {
		return fmt.Errorf("That note is already gone.")
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s removed a known issue note: %s", who, in.Name)}, nil, false)
	if b.dashKnownRefresh("") != nil {
		setFlash(false, "Note removed.")
		return nil
	}
	setFlash(false, "Note removed. The post updates in a moment.")
	return nil
}

// The bot page: statuses, commands and backups.

func (b *bot) dashBotPage() template.HTML {
	var custom []string
	b.store.view(func(d *storeData) { custom = append(custom, d.Statuses...) })
	list, mine := defaultStatuses, len(custom) > 0
	if mine {
		list = custom
	}
	var rows [][]template.HTML
	for _, st := range list {
		cell := []template.HTML{dashText(template.HTMLEscapeString(st))}
		if mine {
			cell = append(cell, dashControls(dashControl{Section: "bot", Do: "status-remove", Name: st, Button: "Remove", Quiet: true}))
		} else {
			cell = append(cell, dashText(`<span class="note">built-in</span>`))
		}
		rows = append(rows, cell)
	}
	ctls := []dashControl{}
	if len(custom) < statusesMax {
		ctls = append(ctls, dashControl{Section: "bot", Do: "status-add", Line: "Watching new episodes", TextMax: statusMaxLen, Button: "Add"})
	}
	if mine {
		ctls = append(ctls, dashControl{Section: "bot", Do: "status-reset", Button: "Back to built-in", Quiet: true})
	}
	return dashJoin(
		dashHeading("Statuses"),
		dashNote("The bot cycles through these every 2 minutes, after \"Watching anime · /help\" and the new release one. Adding the first one of your own replaces the built-in list."),
		dashHTMLTable([]string{"Status", ""}, rows, ""),
		dashControls(ctls...),
		dashHeading("Slash commands"),
		dashNote("Send the bot's slash commands to Discord again, if one is missing or out of date. This also happens every time the bot starts."),
		dashControls(dashControl{Section: "bot", Do: "commands", Button: "Re-sync commands", Quiet: true}),
		dashHeading("Backup"),
		dashText(`<p><a class="btn" href="?download=data" download>Download data file</a></p>`),
		dashNote("Everything the bot remembers: warnings, follows, suggestions, settings, levels and more. Restoring replaces all of it with the file; a copy of the current data is kept next to it as otakase-bot.json.before-restore."),
		dashControls(dashControl{Section: "bot", Do: "restore", File: true, Confirm: "replace everything", Button: "Restore", Quiet: true}),
	)
}

func (b *bot) dashStatusAdd(in dashInput, who string) error {
	st := strings.Join(strings.Fields(in.Text), " ")
	if st == "" || len([]rune(st)) > statusMaxLen {
		return fmt.Errorf("A status is one line of up to %d characters.", statusMaxLen)
	}
	full := false
	b.store.update(func(d *storeData) {
		if full = len(d.Statuses) >= statusesMax; !full {
			d.Statuses = append(d.Statuses, st)
		}
	})
	if full {
		return fmt.Errorf("That's %d statuses already. Remove one first.", statusesMax)
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s added the bot status: %s", who, st)}, nil, false)
	setFlash(false, "Status added. It comes up in the next rounds.")
	return nil
}

func (b *bot) dashStatusRemove(in dashInput, who string) error {
	found := false
	b.store.update(func(d *storeData) {
		for k, s := range d.Statuses {
			if s == in.Name {
				d.Statuses = append(d.Statuses[:k:k], d.Statuses[k+1:]...)
				found = true
				break
			}
		}
	})
	if !found {
		return fmt.Errorf("That status is already gone.")
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s removed the bot status: %s", who, in.Name)}, nil, false)
	setFlash(false, "Status removed.")
	return nil
}

func (b *bot) dashStatusReset(_ dashInput, who string) error {
	b.store.update(func(d *storeData) { d.Statuses = nil })
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: who + " put the bot statuses back to the built-in ones."}, nil, false)
	setFlash(false, "The bot uses the built-in statuses again.")
	return nil
}

func (b *bot) dashCommands(_ dashInput, who string) error {
	if err := dashNotConnected(b); err != nil {
		return err
	}
	if err := b.registerCommands(); err != nil {
		log.Printf("dashboard commands: %v", err)
		return fmt.Errorf("Discord said no: %v.", err)
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: who + " re-synced the slash commands."}, nil, false)
	setFlash(false, "Slash commands sent to Discord. They can take a minute to show.")
	return nil
}

func (b *bot) dashRestore(in dashInput, who string) error {
	if len(in.File) == 0 {
		return fmt.Errorf("Pick a backup file first.")
	}
	if err := b.store.restore(in.File); err != nil {
		return fmt.Errorf("That isn't a bot data file: %v.", err)
	}
	b.staffNote(&discordgo.MessageEmbed{Title: "Data restored", Color: shu,
		Description: who + " restored the bot's data from a backup. The data from before is kept as otakase-bot.json.before-restore."})
	setFlash(false, "Restored. The old data is kept as otakase-bot.json.before-restore.")
	return nil
}

// dashDownload sends the data file.
func (b *bot) dashDownload(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="otakase-bot-%s.json"`, time.Now().UTC().Format("2006-01-02")))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.Write(b.store.backup())
}

// Voice rooms and watch parties.

func (b *bot) roomByID(id string) (room, bool) {
	var r room
	var ok bool
	b.store.view(func(d *storeData) { r, ok = d.Rooms[id] })
	return r, ok
}

func (b *bot) dashRoomRename(in dashInput, who string) error {
	if err := dashNotConnected(b); err != nil {
		return err
	}
	if _, ok := b.roomByID(in.Name); !ok {
		return fmt.Errorf("That room is gone.")
	}
	name := strings.Join(strings.Fields(in.Text), " ")
	if name == "" || len([]rune(name)) > 100 {
		return fmt.Errorf("A room name is up to 100 characters.")
	}
	if _, err := b.s.ChannelEdit(in.Name, &discordgo.ChannelEdit{Name: name}); err != nil {
		log.Printf("dashboard rename room: %v", err)
		return fmt.Errorf("Discord said no: %v.", err)
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s renamed voice room <#%s>.", who, in.Name)}, nil, false)
	setFlash(false, "Room renamed. Discord allows two renames per channel every 10 minutes.")
	return nil
}

func (b *bot) dashRoomClose(in dashInput, who string) error {
	if err := dashNotConnected(b); err != nil {
		return err
	}
	r, ok := b.roomByID(in.Name)
	if !ok {
		return fmt.Errorf("That room is gone.")
	}
	name := b.dashChannel(in.Name)
	b.deleteRoom(in.Name, r, "Closed from the dashboard")
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s closed voice room %s.", who, name)}, nil, false)
	setFlash(false, "Closed %s.", name)
	return nil
}

func (b *bot) dashPartyCancel(in dashInput, who string) error {
	if err := dashNotConnected(b); err != nil {
		return err
	}
	ev, err := b.s.GuildScheduledEvent(b.cfg.GuildID, in.Name, false)
	if err != nil {
		return fmt.Errorf("That watch party is already over or cancelled.")
	}
	if err := b.s.GuildScheduledEventDelete(b.cfg.GuildID, in.Name); err != nil {
		log.Printf("dashboard cancel party: %v", err)
		return fmt.Errorf("Couldn't cancel the event: %v.", err)
	}
	b.partyCancelled(in.Name, "")
	b.dropPartyRoom(in.Name)
	dashEvents.Lock()
	dashEvents.at = time.Time{}
	dashEvents.Unlock()
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s cancelled the watch party **%s**.", who, ev.Name)}, nil, false)
	setFlash(false, "Cancelled %s. Members who were going get a DM.", ev.Name)
	return nil
}

// Level roles and guess rounds, on the games page.

// levelRoleChoices are the roles a level can give: not @everyone, not managed by
// an app, highest first.
func (b *bot) levelRoleChoices() []*discordgo.Role {
	if b.s == nil || b.s.State == nil {
		return nil
	}
	g, err := b.s.State.Guild(b.cfg.GuildID)
	if err != nil {
		return nil
	}
	var out []*discordgo.Role
	for _, r := range g.Roles {
		if r.ID != g.ID && !r.Managed {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position > out[j].Position })
	return out
}

func (b *bot) roleName(id string) string {
	for _, r := range b.levelRoleChoices() {
		if r.ID == id {
			return "@" + r.Name
		}
	}
	return "role …" + id[max(0, len(id)-6):]
}

// chatChannels are the text channels a guess round can run in.
func (b *bot) chatChannels() []*discordgo.Channel {
	var out []*discordgo.Channel
	for _, c := range b.postChannels() {
		if p := b.place(c.ID); !p.Support && !p.Staff {
			out = append(out, c)
		}
	}
	return out
}

func (b *bot) dashLevelRoles() template.HTML {
	var picked bool
	b.store.view(func(d *storeData) { picked = len(d.LevelRoles) > 0 })
	roles := b.levelRoles()
	levels := make([]int, 0, len(roles))
	for l := range roles {
		levels = append(levels, l)
	}
	sort.Ints(levels)
	var rows [][]template.HTML
	for _, l := range levels {
		cells := []template.HTML{dashText(fmt.Sprint(l)), dashText(template.HTMLEscapeString(b.roleName(roles[l])))}
		if picked {
			cells = append(cells, dashControls(dashControl{Section: "games", Do: "level-role-remove", Name: fmt.Sprint(l), Button: "Remove", Quiet: true}))
		} else {
			cells = append(cells, dashText(`<span class="note">by name</span>`))
		}
		rows = append(rows, cells)
	}
	parts := []template.HTML{
		dashHeading("Level roles"),
		dashNote("Members get the role of the highest level they reached, and lose the lower ones. Without picks here, roles named \"Level 5\", \"Level 10\", \"Level 20\" and \"Level 30\" are used. Members get a new role the next time they chat."),
		dashHTMLTable([]string{"Level", "Role", ""}, rows, "No level roles."),
	}
	var ctls []dashControl
	if rs := b.levelRoleChoices(); len(rs) > 0 {
		var choices []dashChoice
		for _, r := range rs {
			choices = append(choices, dashChoice{Value: r.ID, Label: "@" + r.Name})
		}
		ctls = append(ctls, dashControl{Section: "games", Do: "level-role-add", Choices: choices,
			Number: &dashNumber{Value: "5", Min: "1", Max: "500", Step: "1", Field: "text"}, Button: "Add"})
	}
	if picked {
		ctls = append(ctls, dashControl{Section: "games", Do: "level-roles-reset", Button: "Back to Level N names", Quiet: true})
	}
	parts = append(parts, dashControls(ctls...))
	return dashJoin(parts...)
}

func (b *bot) dashLevelRoleAdd(in dashInput, who string) error {
	level, err := strconv.Atoi(in.Text)
	if err != nil || level < 1 || level > 500 {
		return fmt.Errorf("The level is a number from 1 to 500.")
	}
	ok := false
	for _, r := range b.levelRoleChoices() {
		ok = ok || r.ID == in.V
	}
	if !ok {
		return fmt.Errorf("That role isn't on the server.")
	}
	// The first pick keeps the roles found by name, so nothing is lost.
	byName := b.levelRoles()
	b.store.update(func(d *storeData) {
		if len(d.LevelRoles) == 0 {
			d.LevelRoles = byName
		}
		if d.LevelRoles == nil {
			d.LevelRoles = map[int]string{}
		}
		d.LevelRoles[level] = in.V
	})
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s set level %d to give <@&%s>.", who, level, in.V)}, nil, false)
	setFlash(false, "Level %d now gives %s.", level, b.roleName(in.V))
	return nil
}

func (b *bot) dashLevelRoleRemove(in dashInput, who string) error {
	level, _ := strconv.Atoi(in.Name)
	found := false
	b.store.update(func(d *storeData) {
		if _, found = d.LevelRoles[level]; found {
			delete(d.LevelRoles, level)
		}
	})
	if !found {
		return fmt.Errorf("Level %d has no role.", level)
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s removed the level %d role.", who, level)}, nil, false)
	setFlash(false, "Level %d gives no role now.", level)
	return nil
}

func (b *bot) dashLevelRolesReset(_ dashInput, who string) error {
	b.store.update(func(d *storeData) { d.LevelRoles = nil })
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: who + " put the level roles back to the \"Level N\" roles."}, nil, false)
	setFlash(false, "Level roles are found by name again.")
	return nil
}

func (b *bot) dashRiddleRemove(in dashInput, who string) error {
	r, ok := b.store.removeRiddle(in.Name)
	if !ok {
		return fmt.Errorf("That riddle is already gone.")
	}
	b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s removed the emoji riddle %s (%s).", who, r.Emojis, r.Name)}, nil, false)
	setFlash(false, "Removed the riddle for %s.", r.Name)
	return nil
}
