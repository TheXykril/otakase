package main

// The Otakase app and the bot (protocol v1). The app calls the public API
// (publicapi.go) with JSON bodies and, once linked, an
// "Authorization: Bearer <token>" header. Errors are {"error": "..."}; a 401
// means the token is gone and the app forgets it and offers to link again.
//
//	POST /v1/link/start {}             → {"code":"ABCD-2345","poll":"<secret>","expires_in":600}, 10/hour per IP
//	POST /v1/link/poll {"poll":"…"}    → 202 {"status":"pending"} | 200 {"token","discord_name"} once | 410 expired
//	POST /v1/unlink (auth)             → {} and this token is revoked
//	POST /v1/heartbeat (auth)          → {"ok":true,"discord_name"}, 4/min per token (appheartbeat.go)
//	GET  /v1/news                      → latest announcement and member counts, cached 5 min (appnews.go)
//	POST /v1/report (auth, multipart)  → {"url"} of a new support post, 3/hour per member (appreport.go)
//
// Linking: the app starts a link and shows the code; the member runs
// /link-app code:ABCD-2345 in Discord; the app's next poll gets a token,
// which the bot keeps only as its hash in AppLinks. Pending codes live in
// memory for 10 minutes and a member gets 5 wrong codes per 10 minutes.
// /unlink-app revokes all of a member's apps, /linked-apps lists them with
// an unlink button each. A member keeps at most 10 apps; linking another
// drops the one unused longest.

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
)

const (
	linkCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I
	linkCodeTTL      = 10 * time.Minute
	maxPendingLinks  = 500
	maxAppsPerMember = 10
	appInviteURL     = "https://otakase.xyverion.com/discord/"
)

// appNow is the clock for the app features, a variable for tests.
var appNow = time.Now

// pendingLink is a link the app started and polls for.
type pendingLink struct {
	code     string // 8 characters, no dash
	pollHash string
	expires  time.Time
	user     string // set by /link-app
	name     string
}

var pendingLinks = struct {
	sync.Mutex
	byCode map[string]*pendingLink
	byPoll map[string]*pendingLink // by hash of the poll secret
}{byCode: map[string]*pendingLink{}, byPoll: map[string]*pendingLink{}}

var (
	linkStartLimit = newRateLimit(10, time.Hour)
	linkPollLimit  = newRateLimit(120, time.Minute)
	linkCodeFails  = newRateLimit(5, 10*time.Minute) // wrong /link-app codes per member
)

var (
	errLinkBusy     = errors.New("too many pending links, try again in a few minutes")
	errLinkWrong    = errors.New("wrong or expired code")
	errLinkTooMany  = errors.New("too many wrong codes")
	errLinkNotFound = errors.New("expired")
)

func init() {
	apiHandle("POST /v1/link/start", (*bot).apiLinkStart)
	apiHandle("POST /v1/link/poll", (*bot).apiLinkPoll)
	apiHandle("POST /v1/unlink", (*bot).apiUnlink)
}

// full reports whether key used up its n requests, without counting one.
func (l *rateLimit) full(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			n++
		}
	}
	return n >= l.n
}

// bearerToken is the request's app token, as appUser reads it.
func bearerToken(r *http.Request) string {
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return strings.TrimSpace(tok)
}

func newLinkCode() string {
	buf := make([]byte, 8)
	rand.Read(buf)
	for k := range buf {
		buf[k] = linkCodeAlphabet[int(buf[k])%len(linkCodeAlphabet)] // 256 is a multiple of 32: no bias
	}
	return string(buf)
}

// normalizeLinkCode reads a code as members type it: any case, with or
// without the dash or spaces.
func normalizeLinkCode(s string) (string, bool) {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "", "–", "").Replace(strings.TrimSpace(s)))
	if len(s) != 8 {
		return "", false
	}
	for _, c := range s {
		if !strings.ContainsRune(linkCodeAlphabet, c) {
			return "", false
		}
	}
	return s, true
}

func formatLinkCode(c string) string { return c[:4] + "-" + c[4:] }

func prunePendingLinks(now time.Time) {
	for code, p := range pendingLinks.byCode {
		if now.After(p.expires) {
			delete(pendingLinks.byCode, code)
			delete(pendingLinks.byPoll, p.pollHash)
		}
	}
}

// startLink makes a new pending link and returns its code and poll secret.
func startLink(now time.Time) (code, poll string, err error) {
	pendingLinks.Lock()
	defer pendingLinks.Unlock()
	prunePendingLinks(now)
	if len(pendingLinks.byCode) >= maxPendingLinks {
		return "", "", errLinkBusy
	}
	for {
		code = newLinkCode()
		if _, taken := pendingLinks.byCode[code]; !taken {
			break
		}
	}
	poll = newToken(32)
	p := &pendingLink{code: code, pollHash: hashToken(poll), expires: now.Add(linkCodeTTL)}
	pendingLinks.byCode[code], pendingLinks.byPoll[p.pollHash] = p, p
	return code, poll, nil
}

// bindLink gives a pending code to the member who typed it in Discord.
func bindLink(user, name, input string, now time.Time) error {
	if linkCodeFails.full(user, now) {
		return errLinkTooMany
	}
	code, ok := normalizeLinkCode(input)
	pendingLinks.Lock()
	defer pendingLinks.Unlock()
	prunePendingLinks(now)
	p := pendingLinks.byCode[code]
	if !ok || p == nil || (p.user != "" && p.user != user) {
		linkCodeFails.allow(user, now)
		return errLinkWrong
	}
	p.user, p.name = user, name
	if soon := now.Add(2 * time.Minute); p.expires.Before(soon) {
		p.expires = soon // time for the app's next poll
	}
	return nil
}

// pollLink answers the app's poll: pending, or the bound member and a new
// token (once), or errLinkNotFound when the code expired or was used.
func (b *bot) pollLink(poll string, now time.Time) (token, name string, pending bool, err error) {
	h := hashToken(poll)
	pendingLinks.Lock()
	p := pendingLinks.byPoll[h]
	if p == nil || now.After(p.expires) {
		if p != nil {
			delete(pendingLinks.byCode, p.code)
			delete(pendingLinks.byPoll, h)
		}
		pendingLinks.Unlock()
		return "", "", false, errLinkNotFound
	}
	if p.user == "" {
		pendingLinks.Unlock()
		return "", "", true, nil
	}
	delete(pendingLinks.byCode, p.code)
	delete(pendingLinks.byPoll, h)
	pendingLinks.Unlock()

	token = newToken(32)
	b.store.update(func(d *storeData) { addAppLink(d, hashToken(token), appLink{User: p.user, Created: now}) })
	log.Printf("app linked for member %s", p.user)
	return token, p.name, false, nil
}

// addAppLink stores a link, dropping the member's longest unused app when
// they have too many.
func addAppLink(d *storeData, hash string, l appLink) {
	for {
		var oldest string
		var oldestAt time.Time
		n := 0
		for h, x := range d.AppLinks {
			if x.User != l.User {
				continue
			}
			n++
			at := x.LastSeen
			if at.IsZero() {
				at = x.Created
			}
			if oldest == "" || at.Before(oldestAt) {
				oldest, oldestAt = h, at
			}
		}
		if n < maxAppsPerMember {
			break
		}
		delete(d.AppLinks, oldest)
	}
	d.AppLinks[hash] = l
}

func (b *bot) apiLinkStart(w http.ResponseWriter, r *http.Request) {
	now := appNow()
	if !linkStartLimit.allow(clientIP(r), now) {
		apiError(w, http.StatusTooManyRequests, "too many links started, try again later")
		return
	}
	code, poll, err := startLink(now)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": formatLinkCode(code), "poll": poll, "expires_in": int(linkCodeTTL / time.Second)})
}

func (b *bot) apiLinkPoll(w http.ResponseWriter, r *http.Request) {
	now := appNow()
	if !linkPollLimit.allow(clientIP(r), now) {
		apiError(w, http.StatusTooManyRequests, "polling too fast")
		return
	}
	var req struct {
		Poll string `json:"poll"`
	}
	if !readJSON(w, r, 1024, &req) {
		return
	}
	if len(req.Poll) < 32 || len(req.Poll) > 128 {
		apiError(w, http.StatusBadRequest, "bad request")
		return
	}
	token, name, pending, err := b.pollLink(req.Poll, now)
	switch {
	case err != nil:
		apiError(w, http.StatusGone, "expired")
	case pending:
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"token": token, "discord_name": name})
	}
}

func (b *bot) apiUnlink(w http.ResponseWriter, r *http.Request) {
	if b.requireApp(w, r) == "" {
		return
	}
	h := hashToken(bearerToken(r))
	b.store.update(func(d *storeData) { delete(d.AppLinks, h) })
	writeJSON(w, http.StatusOK, struct{}{})
}

// unlinkAll revokes every app of a member and says how many there were.
func (b *bot) unlinkAll(user string) int {
	n := 0
	b.store.update(func(d *storeData) {
		for h, l := range d.AppLinks {
			if l.User == user {
				delete(d.AppLinks, h)
				n++
			}
		}
	})
	if n > 0 {
		setWatching(user, false, nil, appNow())
	}
	return n
}

// Discord side.

func appCommands() []*discordgo.ApplicationCommand {
	member := []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Whose (default: yours)"},
	}
	return []*discordgo.ApplicationCommand{
		{Name: "link-app", Description: "Link the Otakase app with the code it shows", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "code", Description: "The code Otakase shows, like ABCD-2345", Required: true, MaxLength: 12},
		}},
		{Name: "unlink-app", Description: "Unlink every Otakase app linked to you"},
		{Name: "linked-apps", Description: "Your linked Otakase apps, with a button to unlink each"},
		{Name: "whos-watching", Description: "Who is watching what in Otakase right now"},
		{Name: "watch-time", Description: "How long someone has watched in Otakase", Options: member},
	}
}

// appCommand answers the app commands above.
func (b *bot) appCommand(i *discordgo.InteractionCreate) {
	d := i.ApplicationCommandData()
	user := i.Member.User.ID
	switch d.Name {
	case "link-app":
		code := ""
		for _, o := range d.Options {
			if o.Name == "code" {
				code = o.StringValue()
			}
		}
		switch err := bindLink(user, displayName(i.Member), code, appNow()); err {
		case nil:
			b.respond(i, true, &discordgo.MessageEmbed{Color: shu,
				Description: "Linked. Go back to Otakase, it finishes by itself in a few seconds.\nSee your apps with `/linked-apps`."})
		case errLinkTooMany:
			b.respond(i, true, errorEmbed("Too many wrong codes. Wait a few minutes and start the link again in Otakase."))
		default:
			b.respond(i, true, errorEmbed("That code is wrong or expired. Codes last 10 minutes; run `otakase -discord-link` for a new one."))
		}
	case "unlink-app":
		if n := b.unlinkAll(user); n == 0 {
			b.respond(i, true, errorEmbed("You have no Otakase app linked."))
		} else {
			b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Unlinked %s. They stop sending anything right away.", dashPlural(n, "app"))})
		}
	case "linked-apps":
		e, comps := linkedAppsView(b.memberApps(user))
		b.respond(i, true, e, comps...)
	case "whos-watching":
		b.whosWatchingAnswer(i) // with join buttons, partyask.go
	case "watch-time":
		who := i.Member.User
		for _, o := range d.Options {
			if o.Name == "member" {
				if u := o.UserValue(nil); u != nil {
					who = u
				}
			}
		}
		b.respondQuiet(i, false, b.watchTimeEmbed(who.ID, who.ID == user))
	}
}

// respondQuiet is respond for answers that show text members wrote: nothing
// in it can ping anyone.
func (b *bot) respondQuiet(i *discordgo.InteractionCreate, ephemeral bool, e *discordgo.MessageEmbed) {
	data := &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, AllowedMentions: &discordgo.MessageAllowedMentions{}}
	if ephemeral {
		data.Flags = discordgo.MessageFlagsEphemeral
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource, Data: data}); err != nil {
		log.Printf("respond: %v", err)
	}
}

// deferQuiet is deferThen for answers that show text members wrote.
func (b *bot) deferQuiet(i *discordgo.InteractionCreate, ephemeral bool, fn func() *discordgo.MessageEmbed) {
	var flags discordgo.MessageFlags
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: flags}}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	e := fn()
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{e}, AllowedMentions: &discordgo.MessageAllowedMentions{}}); err != nil {
		log.Printf("edit: %v", err)
	}
}

// memberApp is one of a member's linked apps; ID is a short handle for its
// unlink button, the start of the token hash.
type memberApp struct {
	ID string
	appLink
}

func (b *bot) memberApps(user string) []memberApp {
	var apps []memberApp
	b.store.view(func(d *storeData) {
		for h, l := range d.AppLinks {
			if l.User == user {
				apps = append(apps, memberApp{ID: h[:12], appLink: l})
			}
		}
	})
	sort.Slice(apps, func(i, j int) bool { return apps[i].Created.Before(apps[j].Created) })
	return apps
}

// osLabel names a system the way the app reports it (Go's GOOS).
func osLabel(goos string) string {
	switch goos {
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "android":
		return "Android"
	case "freebsd":
		return "FreeBSD"
	case "":
		return "Unknown system"
	}
	return goos
}

// linkedAppsView lists a member's apps with an unlink button each.
func linkedAppsView(apps []memberApp) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	if len(apps) == 0 {
		return &discordgo.MessageEmbed{Color: shu, Description: "No Otakase app is linked. Run `otakase -discord-link` (or **Discord** in the app's menu) and follow it."},
			[]discordgo.MessageComponent{}
	}
	var lines []string
	var row discordgo.ActionsRow
	var comps []discordgo.MessageComponent
	for k, a := range apps {
		what := osLabel(a.OS)
		if a.Version != "" {
			what += " · Otakase " + a.Version
		}
		seen := "not used yet"
		if !a.LastSeen.IsZero() {
			seen = fmt.Sprintf("last seen <t:%d:R>", a.LastSeen.Unix())
		}
		lines = append(lines, fmt.Sprintf("**%d.** %s · linked <t:%d:d> · %s", k+1, what, a.Created.Unix(), seen))
		row.Components = append(row.Components, discordgo.Button{Label: fmt.Sprintf("Unlink %d", k+1), Style: discordgo.DangerButton, CustomID: "app-revoke:" + a.ID})
		if len(row.Components) == 5 {
			comps, row = append(comps, row), discordgo.ActionsRow{}
		}
	}
	if len(row.Components) > 0 {
		comps = append(comps, row)
	}
	return &discordgo.MessageEmbed{Title: "Your linked Otakase apps", Color: shu, Description: strings.Join(lines, "\n"),
		Footer: &discordgo.MessageEmbedFooter{Text: "/unlink-app unlinks them all"}}, comps
}

// revokeApp is an unlink button under /linked-apps.
func (b *bot) revokeApp(i *discordgo.InteractionCreate, id string) {
	user := i.Member.User.ID
	removed := false
	if len(id) == 12 {
		b.store.update(func(d *storeData) {
			for h, l := range d.AppLinks {
				if l.User == user && strings.HasPrefix(h, id) {
					delete(d.AppLinks, h)
					removed = true
				}
			}
		})
	}
	apps := b.memberApps(user)
	if len(apps) == 0 {
		setWatching(user, false, nil, appNow())
	}
	e, comps := linkedAppsView(apps)
	if removed {
		e.Footer = &discordgo.MessageEmbedFooter{Text: "Unlinked. That app stops sending anything right away."}
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}}); err != nil {
		log.Printf("app revoke: %v", err)
	}
}

// Text from the app.

// cleanText keeps text from the app safe to store and show: no control or
// direction-override characters (newlines and tabs only when multiline),
// trimmed.
func cleanText(s string, multiline bool) string {
	s = strings.ToValidUTF8(strings.ReplaceAll(s, "\r\n", "\n"), "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			if multiline {
				return r
			}
			return ' '
		case unicode.IsControl(r), r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0xFEFF:
			return -1
		}
		return r
	}, s)
	if !multiline {
		s = strings.Join(strings.Fields(s), " ")
	}
	return strings.TrimSpace(s)
}

// quiet breaks @everyone, @here and <@…> mentions in member text, as a
// second guard next to empty AllowedMentions.
func quiet(s string) string {
	return strings.NewReplacer("@everyone", "@​everyone", "@here", "@​here", "<@", "<​@").Replace(s)
}

// showText is a title from the app for markdown: quiet and escaped.
func showText(s string) string { return mdEscape(quiet(s)) }
