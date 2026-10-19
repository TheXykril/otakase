package main

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

// Settings backup: a linked app can keep a copy of its config with the bot
// and get it back on another computer once that one is linked too. One
// backup per member, not per app, so a new PC finds it.
//
//	POST   /v1/backup {"config":"…"} (auth) → {"at":"…"}
//	GET    /v1/backup (auth)                → {"config","at","from_os"}, 404 when none
//	DELETE /v1/backup (auth)                → {}
//
// /settings-backup in Discord says when the last one was made and deletes it.

const maxBackupBytes = 64 << 10

var backupLimit = newRateLimit(10, time.Hour)

// appBackup is a member's saved config.
type appBackup struct {
	Config  string    `json:"config"`
	At      time.Time `json:"at"`
	FromOS  string    `json:"from_os,omitempty"`
	Version string    `json:"version,omitempty"`
}

func init() {
	apiHandle("POST /v1/backup", (*bot).apiBackupSave)
	apiHandle("GET /v1/backup", (*bot).apiBackupGet)
	apiHandle("DELETE /v1/backup", (*bot).apiBackupDelete)
	registerCommand(&discordgo.ApplicationCommand{Name: "settings-backup", Description: "When your Otakase settings were last backed up, and delete the backup"},
		(*bot).settingsBackupCommand)
	registerComponent("app-backup-del:", (*bot).deleteBackupButton)
	registerHelp("`/settings-backup` your Otakase settings backup (Community → Back up settings in the app)")
}

// secretKeyRe matches config keys that hold something private. The app
// leaves them out already; the bot drops them too, in case an older or
// changed app does not.
var secretKeyRe = regexp.MustCompile(`(?i)(secret|password|passwd|token|apikey|api_key|clientid|client_id)`)

// cleanBackup keeps the key=value lines and comments of a config, without
// secret-looking keys or anything that is not text.
func cleanBackup(config string) string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(config, "\r\n", "\n"), "\n") {
		line = cleanText(line, false)
		if line == "" {
			continue
		}
		if key, _, ok := strings.Cut(line, "="); ok && secretKeyRe.MatchString(key) {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (b *bot) apiBackupSave(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	var req struct {
		Config string `json:"config"`
	}
	if !readJSON(w, r, maxBackupBytes+4<<10, &req) {
		return
	}
	if len(req.Config) > maxBackupBytes || !utf8.ValidString(req.Config) {
		apiError(w, http.StatusRequestEntityTooLarge, "the settings are too large to back up (64 KB at most)")
		return
	}
	conf := cleanBackup(req.Config)
	if conf == "" {
		apiError(w, http.StatusBadRequest, "no settings to back up")
		return
	}
	if !backupLimit.allow(user, appNow()) {
		apiError(w, http.StatusTooManyRequests, "too many backups, try again later")
		return
	}
	var link appLink
	h := hashToken(bearerToken(r))
	now := appNow()
	b.store.update(func(d *storeData) {
		link = d.AppLinks[h]
		if d.AppBackups == nil {
			d.AppBackups = map[string]appBackup{}
		}
		d.AppBackups[user] = appBackup{Config: conf, At: now, FromOS: link.OS, Version: link.Version}
	})
	writeJSON(w, http.StatusOK, map[string]any{"at": now})
}

func (b *bot) apiBackupGet(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	if !backupLimit.allow(user, appNow()) {
		apiError(w, http.StatusTooManyRequests, "too many requests, try again later")
		return
	}
	var bk appBackup
	var ok bool
	b.store.view(func(d *storeData) { bk, ok = d.AppBackups[user] })
	if !ok {
		apiError(w, http.StatusNotFound, "no settings backup yet")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": bk.Config, "at": bk.At, "from_os": bk.FromOS})
}

func (b *bot) apiBackupDelete(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	b.store.update(func(d *storeData) { delete(d.AppBackups, user) })
	writeJSON(w, http.StatusOK, map[string]any{})
}

// backupView describes a member's backup, with a Delete button when there
// is one.
func backupView(bk appBackup, ok bool) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	if !ok {
		return &discordgo.MessageEmbed{Color: shu, Description: "You have no Otakase settings backup. In the app, link Discord, then use **Community → Back up settings to Discord**."},
			[]discordgo.MessageComponent{}
	}
	from := osLabel(bk.FromOS)
	if bk.Version != "" {
		from += " · Otakase " + bk.Version
	}
	n := len(strings.Split(bk.Config, "\n"))
	e := &discordgo.MessageEmbed{Title: "Your Otakase settings backup", Color: shu,
		Description: fmt.Sprintf("Backed up <t:%d:R> (<t:%d:f>) from %s, %s.\n\nRestore it in the app with **Community → Restore settings from Discord**. Passwords, tokens and keys are never included.",
			bk.At.Unix(), bk.At.Unix(), from, dashPlural(n, "setting"))}
	return e, []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Delete backup", Style: discordgo.DangerButton, CustomID: "app-backup-del:"},
	}}}
}

func (b *bot) memberBackup(user string) (bk appBackup, ok bool) {
	b.store.view(func(d *storeData) { bk, ok = d.AppBackups[user] })
	return bk, ok
}

func (b *bot) settingsBackupCommand(i *discordgo.InteractionCreate) {
	u := interactionUser(i)
	if u == nil {
		return
	}
	e, comps := backupView(b.memberBackup(u.ID))
	b.respond(i, true, e, comps...)
}

func (b *bot) deleteBackupButton(i *discordgo.InteractionCreate, _ string) {
	u := interactionUser(i)
	if u == nil {
		return
	}
	b.store.update(func(d *storeData) { delete(d.AppBackups, u.ID) })
	e, comps := backupView(appBackup{}, false)
	e.Description = "Deleted your Otakase settings backup."
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}}); err != nil {
		log.Printf("backup delete: %v", err)
	}
}
