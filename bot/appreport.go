package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

// POST /v1/report: "Report a problem" in the app. The bot opens a support
// post for the member with their description, the app's diagnostics in a
// code block and the log attached, tagged with their system, and runs the
// usual new post help on it (checklist, log reader, version check). The
// post belongs to the bot, so the member who reported it is remembered and
// may mark it solved.

const (
	reportTitleMax = 100
	reportDescMax  = 4000
	reportDiagMax  = 8000
	reportLogMax   = 1 << 20
	maxAppReports  = 500
)

var reportLimit = newRateLimit(3, time.Hour)

func init() {
	apiHandle("POST /v1/report", (*bot).apiReport)
}

type appReportData struct {
	Title, Description, Diag string
	Log                      []byte // nil when none was sent
}

func (b *bot) apiReport(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, reportLogMax+64<<10)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			apiError(w, http.StatusRequestEntityTooLarge, "report too large (the log may be 1 MB at most)")
			return
		}
		apiError(w, http.StatusBadRequest, "bad request")
		return
	}
	defer r.MultipartForm.RemoveAll()
	rep, problem := readReport(r.MultipartForm)
	if problem != "" {
		apiError(w, http.StatusBadRequest, problem)
		return
	}
	if !reportLimit.allow(user, appNow()) {
		apiError(w, http.StatusTooManyRequests, "you can send 3 reports an hour, try again later")
		return
	}
	if b.s == nil || b.cfg.SupportForum == "" {
		apiError(w, http.StatusServiceUnavailable, "the support forum is not available right now")
		return
	}
	var link appLink
	h := hashToken(bearerToken(r))
	b.store.view(func(d *storeData) { link = d.AppLinks[h] })
	url, err := b.postReport(user, link, rep)
	if err != nil {
		log.Printf("app report: %v", err)
		apiError(w, http.StatusBadGateway, "couldn't make the support post, try again later")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// readReport checks the form; problem says what is wrong with it.
func readReport(f *multipart.Form) (rep appReportData, problem string) {
	value := func(k string) string {
		if v := f.Value[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	raw := map[string]string{"title": value("title"), "description": value("description"), "diag": value("diag")}
	for k, n := range map[string]int{"title": reportTitleMax, "description": reportDescMax, "diag": reportDiagMax} {
		if utf8.RuneCountInString(strings.TrimSpace(raw[k])) > n {
			return rep, fmt.Sprintf("%s is too long (%d characters at most)", k, n)
		}
	}
	rep.Title = cleanText(raw["title"], false)
	rep.Description = cleanText(raw["description"], true)
	rep.Diag = cleanText(raw["diag"], true)
	if rep.Title == "" {
		return rep, "title is required"
	}
	if files := f.File["log"]; len(files) > 0 {
		fh := files[0]
		if fh.Size > reportLogMax {
			return rep, "log is too large (1 MB at most)"
		}
		file, err := fh.Open()
		if err != nil {
			return rep, "bad log"
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, reportLogMax+1))
		if err != nil || len(data) > reportLogMax {
			return rep, "log is too large (1 MB at most)"
		}
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return rep, "log must be text"
		}
		if len(bytes.TrimSpace(data)) > 0 {
			rep.Log = data
		}
	}
	return rep, ""
}

// codeSafe keeps text from closing a code block early.
func codeSafe(s string) string { return strings.ReplaceAll(s, "```", "`​``") }

// reportMessage is the post's first message: who sent it, their words, the
// diagnostics in a code block (as a file when too long for one) and the log.
func reportMessage(user string, rep appReportData, link appLink) *discordgo.MessageSend {
	content := fmt.Sprintf("<@%s> sent this from the Otakase app.", user)
	if link.Version != "" {
		content += fmt.Sprintf(" Otakase version: %s on %s.", link.Version, osLabel(link.OS))
	}
	msg := &discordgo.MessageSend{Content: content, AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{user}}}
	size := 0
	if rep.Description != "" {
		d := quiet(rep.Description)
		msg.Embeds = append(msg.Embeds, &discordgo.MessageEmbed{Description: d, Color: shu})
		size += utf8.RuneCountInString(d)
	}
	if rep.Diag != "" {
		block := "```\n" + codeSafe(rep.Diag) + "\n```"
		if n := utf8.RuneCountInString(block); n <= 4096 && size+n+len("Diagnostics") <= 5900 {
			msg.Embeds = append(msg.Embeds, &discordgo.MessageEmbed{Title: "Diagnostics", Description: block, Color: 0x8A8276})
		} else {
			msg.Files = append(msg.Files, &discordgo.File{Name: "diagnostics.txt", ContentType: "text/plain", Reader: strings.NewReader(rep.Diag)})
		}
	}
	if rep.Log != nil {
		msg.Files = append(msg.Files, &discordgo.File{Name: "otakase.log", ContentType: "text/plain", Reader: bytes.NewReader(rep.Log)})
	}
	return msg
}

// reportTags picks the tag for the member's system, if the forum has one;
// when the forum requires a tag and none fits, the first that isn't Solved.
func reportTags(forum *discordgo.Channel, goos string) []string {
	want := map[string][]string{"linux": {"linux"}, "windows": {"windows"}, "darwin": {"macos", "mac"}, "android": {"android"}}[goos]
	for _, w := range want {
		for _, t := range forum.AvailableTags {
			if strings.Contains(strings.ToLower(t.Name), w) {
				return []string{t.ID}
			}
		}
	}
	if forum.Flags&discordgo.ChannelFlagRequireTag != 0 {
		for _, t := range forum.AvailableTags {
			if !strings.EqualFold(t.Name, "Solved") {
				return []string{t.ID}
			}
		}
	}
	return nil
}

// postReport makes the support post and returns its link.
func (b *bot) postReport(user string, link appLink, rep appReportData) (string, error) {
	forum, err := b.s.Channel(b.cfg.SupportForum)
	if err != nil {
		return "", err
	}
	th, err := b.s.ForumThreadStartComplex(forum.ID, &discordgo.ThreadStart{
		Name: rep.Title, AutoArchiveDuration: 10080, AppliedTags: reportTags(forum, link.OS),
	}, reportMessage(user, rep, link))
	if err != nil {
		return "", err
	}
	now := appNow()
	b.store.update(func(d *storeData) {
		d.AppReports[th.ID] = appReport{User: user, At: now}
		pruneAppReports(d.AppReports, maxAppReports)
	})
	if err := b.s.ThreadMemberAdd(th.ID, user); err != nil {
		log.Printf("app report member: %v", err)
	}
	go b.helpAppReport(th.ID, link, rep)
	return fmt.Sprintf("https://discord.com/channels/%s/%s", b.cfg.GuildID, th.ID), nil
}

// pruneAppReports keeps the newest n reports.
func pruneAppReports(m map[string]appReport, n int) {
	for len(m) > n {
		var oldest string
		for id, r := range m {
			if oldest == "" || r.At.Before(m[oldest].At) {
				oldest = id
			}
		}
		delete(m, oldest)
	}
}

// appReporter reports whether user sent support post thread from the app.
func (b *bot) appReporter(thread, user string) bool {
	var r appReport
	b.store.view(func(d *storeData) { r = d.AppReports[thread] })
	return r.User != "" && r.User == user
}

var plainVersionRe = regexp.MustCompile(`^\d{1,2}\.\d{1,3}\.\d{1,3}$`)

// helpAppReport runs the new post help the bot gives members' own posts,
// which skips posts the bot made.
func (b *bot) helpAppReport(thread string, link appLink, rep appReportData) {
	time.Sleep(2 * time.Second) // after the post's first message
	b.greetSupportPost(thread, rep.Title, rep.Description)
	if hints := b.logs.fresh(thread, diagnoseLog(rep.Diag+"\n"+string(rep.Log))); len(hints) > 0 {
		if _, err := b.s.ChannelMessageSendComplex(thread, logReply(hints)); err != nil {
			log.Printf("app report log reader: %v", err)
		}
	}
	have := link.Version
	if !plainVersionRe.MatchString(have) { // a dev build, or not known yet
		if v, ok := mentionedVersion(rep.Diag); ok && link.Version == "" {
			have = v
		} else {
			return
		}
	}
	latest, err := latestVersion(b.cfg.GitHubToken)
	if err != nil {
		log.Printf("version check: %v", err)
		return
	}
	if !olderVersion(have, latest) {
		return
	}
	versionCheck.Lock()
	versionCheck.warned[thread] = true
	versionCheck.Unlock()
	if _, err := b.s.ChannelMessageSendComplex(thread, &discordgo.MessageSend{
		Embeds:          []*discordgo.MessageEmbed{outdatedEmbed(have, latest)},
		Components:      linkButtons("Changelog", fmt.Sprintf("%s/blob/v%s/CHANGELOG.md", repoURL, latest), "Latest release", repoURL+"/releases/latest"),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Printf("version check reply: %v", err)
	}
}
