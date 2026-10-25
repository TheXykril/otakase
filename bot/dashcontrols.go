package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Dashboard controls: small forms that post back to the page. Home
// Assistant's login guards the page, and every form carries a token made
// when the bot starts, so another site can't press the buttons for you.
// Each change is posted to the bot log channel like the same change from a
// slash command, with the Home Assistant user's name.

// dashToken is the form token for this run of the bot.
var dashToken = func() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}()

// dashFlash is the result of the last control, shown once on the next page.
var dashFlash struct {
	sync.Mutex
	text string
	bad  bool
	at   time.Time
}

func setFlash(bad bool, format string, args ...any) {
	dashFlash.Lock()
	defer dashFlash.Unlock()
	dashFlash.text, dashFlash.bad, dashFlash.at = fmt.Sprintf(format, args...), bad, time.Now()
}

// takeFlash returns the waiting result once, if it is fresh.
func takeFlash() (text string, bad bool) {
	dashFlash.Lock()
	defer dashFlash.Unlock()
	if time.Since(dashFlash.at) < 2*time.Minute {
		text, bad = dashFlash.text, dashFlash.bad
	}
	dashFlash.text = ""
	return text, bad
}

// dashChoice is one option of a select.
type dashChoice struct {
	Value, Label string
	On           bool
}

// dashControl is one form: a button, with an optional select or number
// field sent as "v".
type dashControl struct {
	Section, Do, Button string
	Name                string // the thing it changes, sent as "name"
	Choices             []dashChoice
	Number              *dashNumber
	Text                string // a message box sent as "text", with this placeholder
	TextValue           string // what the message box starts with
	TextMax             int    // longest text, 2000 when 0
	Line                string // a one-line box sent as "text", with this placeholder
	File                bool   // a file picker sent as "file"
	Confirm             string // a tick box that must be ticked first, with this label
	Quiet               bool   // a plain button instead of the accent one
}

type dashNumber struct {
	Value, Min, Max, Step string
	Field                 string // sent as this, "v" when empty
}

func init() {
	template.Must(dashPartsTmpl.New("control").Parse(
		`<form method="post" action="./" class="ctl{{if .C.Text}} wide{{end}}"{{if .C.File}} enctype="multipart/form-data"{{end}}>` +
			`<input type="hidden" name="t" value="{{.Token}}"><input type="hidden" name="s" value="{{.C.Section}}">` +
			`<input type="hidden" name="do" value="{{.C.Do}}">{{with .C.Name}}<input type="hidden" name="name" value="{{.}}">{{end}}` +
			`{{with .C.Choices}}<select name="v" aria-label="Value">{{range .}}<option value="{{.Value}}"{{if .On}} selected{{end}}>{{.Label}}</option>{{end}}</select>{{end}}` +
			`{{with .C.Number}}<input type="number" name="{{or .Field "v"}}" value="{{.Value}}" min="{{.Min}}" max="{{.Max}}" step="{{.Step}}" aria-label="Value">{{end}}` +
			`{{with .C.Text}}<textarea name="text" rows="4" maxlength="{{or $.C.TextMax 2000}}" required placeholder="{{.}}" aria-label="Message">{{$.C.TextValue}}</textarea>{{end}}` +
			`{{with .C.Line}}<input type="text" name="text" maxlength="{{or $.C.TextMax 200}}" required placeholder="{{.}}" aria-label="Text">{{end}}` +
			`{{if .C.File}}<input type="file" name="file" accept=".json,application/json" required aria-label="Backup file">{{end}}` +
			`{{with .C.Confirm}}<label class="sure"><input type="checkbox" required> {{.}}</label>{{end}}` +
			`<button{{if .C.Quiet}} class="quiet"{{end}}>{{.C.Button}}</button></form>`))
	template.Must(dashPartsTmpl.New("controls").Parse(`<div class="ctls">{{range .}}{{.}}{{end}}</div>`))
}

// dashForm draws one control.
func dashForm(c dashControl) template.HTML {
	return dashExec("control", struct {
		Token string
		C     dashControl
	}{dashToken, c})
}

// dashControls puts controls side by side.
func dashControls(cs ...dashControl) template.HTML {
	var parts []template.HTML
	for _, c := range cs {
		parts = append(parts, dashForm(c))
	}
	return dashExec("controls", parts)
}

// dashWho names the Home Assistant user for the log, without anything
// Discord would turn into a mention or formatting.
func dashWho(r *http.Request) string {
	name := r.Header.Get("X-Remote-User-Display-Name")
	if name == "" {
		name = r.Header.Get("X-Remote-User-Name")
	}
	name = strings.Map(func(c rune) rune {
		if strings.ContainsRune("@<>*_~`|#[]()\\", c) || c < ' ' {
			return -1
		}
		return c
	}, name)
	if name = strings.TrimSpace(clip(name, 40)); name == "" {
		name = "Someone"
	}
	return "**" + name + "** (dashboard)"
}

// dashPost runs a control and sends the browser back to the page it came
// from, where the result shows once.
func (b *bot) dashPost(w http.ResponseWriter, r *http.Request) {
	var file []byte
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		// Only the restore form sends a file: a backup of the data file.
		r.Body = http.MaxBytesReader(w, r.Body, dashUploadMax)
		if err := r.ParseMultipartForm(dashUploadMax); err != nil {
			http.Error(w, "The file is too big or the form is broken.", http.StatusBadRequest)
			return
		}
		if f, _, err := r.FormFile("file"); err == nil {
			file, _ = io.ReadAll(f)
			f.Close()
		}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad form.", http.StatusBadRequest)
			return
		}
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" ||
		subtle.ConstantTimeCompare([]byte(r.PostForm.Get("t")), []byte(dashToken)) != 1 {
		http.Error(w, "This page is out of date. Go back and reload it.", http.StatusForbidden)
		return
	}
	slug := "overview"
	for _, s := range dashSections {
		if s.Slug == r.PostForm.Get("s") {
			slug = s.Slug
		}
	}
	f := r.PostForm
	if err := b.dashDo(dashInput{f.Get("do"), f.Get("name"), strings.TrimSpace(f.Get("v")), strings.TrimSpace(f.Get("text")), file}, dashWho(r)); err != nil {
		setFlash(true, "%v", err)
	}
	// Set by hand: http.Redirect would make the link absolute, which leaves
	// the Home Assistant ingress path.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", "./?s="+slug)
	w.WriteHeader(http.StatusSeeOther)
}

// dashInput is what a control sent.
type dashInput struct {
	Do, Name, V, Text string
	File              []byte
}

// dashUploadMax is the largest backup the restore form takes.
const dashUploadMax = 32 << 20

// dashDo runs one control. Results the page should show go in the flash;
// an error is shown as a failure.
func (b *bot) dashDo(in dashInput, who string) error {
	do, name, v := in.Do, in.Name, in.V
	switch do {
	case "setting", "setting-reset":
		s, ok := findSetting(name)
		if !ok {
			return fmt.Errorf("There's no setting called %s.", name)
		}
		old, _ := b.settingValue(name)
		if do == "setting-reset" {
			b.store.update(func(d *storeData) { delete(d.Settings, name) })
			now := s.Default(b.cfg)
			b.staffNote(&discordgo.MessageEmbed{Title: "Setting reset", Color: shu,
				Description: fmt.Sprintf("%s reset **%s** to the add-on option `%s` (was `%s`).", who, name, s.show(now), s.show(old))})
			setFlash(false, "%s is back to the add-on option: %s.", name, s.show(now))
			return nil
		}
		nv, err := s.parse(v)
		if err != nil {
			return fmt.Errorf("That doesn't fit: %v.", err)
		}
		b.store.update(func(d *storeData) {
			if d.Settings == nil {
				d.Settings = map[string]string{}
			}
			d.Settings[name] = nv
		})
		b.staffNote(&discordgo.MessageEmbed{Title: "Setting changed", Color: shu,
			Description: fmt.Sprintf("%s set **%s** to `%s` (was `%s`).", who, name, s.show(nv), s.show(old))})
		setFlash(false, "%s is now %s. It works from now on, no restart needed.", name, s.show(nv))
	case "verify":
		mode := v
		switch v {
		case "auto":
			mode = ""
		case "on", "off":
		default:
			return fmt.Errorf("Verification is auto, on or off.")
		}
		if _, err := b.setVerifyMode(mode, who); err != nil {
			return fmt.Errorf("Couldn't set up verification: %v. The bot needs Manage Roles and Manage Channels.", err)
		}
		setFlash(false, "Join verification is now %s.", modeName(mode))
	case "spoiler":
		if v != spoilerOff && v != spoilerNudge && v != spoilerHide {
			return fmt.Errorf("The spoiler guard is off, nudge or hide.")
		}
		b.store.update(func(d *storeData) { d.SpoilerGuard = v })
		b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s set the spoiler guard to **%s**.", who, v)}, nil, false)
		setFlash(false, "Spoiler guard is now %s.", v)
	case "invites-pause", "invites-resume":
		if b.s == nil {
			return fmt.Errorf("The bot isn't connected to Discord.")
		}
		var until time.Time
		if do == "invites-pause" {
			until = time.Now().Add(invitePause)
		}
		if err := b.pauseInvites(until); err != nil {
			log.Printf("dashboard invites: %v", err)
			return fmt.Errorf("Discord said no: %v. The bot needs Manage Server.", err)
		}
		text := "Invites work again."
		if !until.IsZero() {
			text = fmt.Sprintf("Invites are paused until <t:%d:t>.", until.Unix())
		}
		b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: who + ": " + text}, nil, false)
		if until.IsZero() {
			setFlash(false, "Invites work again.")
		} else {
			setFlash(false, "Invites are paused until %s.", until.UTC().Format("15:04 UTC"))
		}
	case "airing":
		if b.s == nil || b.cfg.AiringChannel == "" {
			return fmt.Errorf("There's no airing channel to post in.")
		}
		if err := b.redrawAiring(time.Now().UTC()); err != nil {
			return fmt.Errorf("Couldn't update the airing board: %v.", err)
		}
		setFlash(false, "The airing board is up to date.")
	case "stale":
		if b.s == nil || b.cfg.SupportForum == "" {
			return fmt.Errorf("There's no support forum to check.")
		}
		go func() {
			if err := b.checkStale(time.Now()); err != nil {
				b.jobFailed("Stale support check", err)
			} else {
				b.jobOK("Stale support check")
			}
		}()
		setFlash(false, "Checking support posts now. Quiet ones get a nudge, nudged ones close.")
	case "suggestion":
		return b.dashSuggestionStatus(name, v, who)
	case "clear-warnings":
		return b.dashClearWarnings(name, who)
	case "ticket-add", "ticket-remove":
		user := strings.TrimSpace(in.Text)
		if !ticketUserID.MatchString(user) {
			return fmt.Errorf("Enter a valid Discord user ID.")
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return fmt.Errorf("Choose 1 to 100 tickets.")
		}
		if do == "ticket-remove" {
			n = -n
		}
		before := b.watchTickets(user)
		balance := b.adjustWatchTickets(user, n)
		b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: fmt.Sprintf("%s changed watch tickets for <@%s> from %d to %d.", who, user, before, balance)}, nil, false)
		setFlash(false, "%s now has %s.", b.dashName(user), dashPlural(balance, "watch ticket"))
	case "say":
		return b.dashSay(v, in.Text, who)
	case "guess-reset-week":
		b.store.resetGuessWeek()
		b.modLog(&discordgo.MessageEmbed{Color: 0x8A8276, Description: who + " reset this week's Guess the anime points."}, nil, false)
		setFlash(false, "This week's guess points are reset. All-time points stay.")
	default:
		if f, ok := dashMoreControls[do]; ok {
			return f(b, in, who)
		}
		return fmt.Errorf("Unknown control.")
	}
	return nil
}

// redrawAiring draws today's board again now, or posts it when there is
// none yet.
func (b *bot) redrawAiring(now time.Time) error {
	has := false
	b.store.update(func(d *storeData) {
		if d.Airing != nil && d.Airing.Message != "" {
			d.Airing.State, has = -1, true // never a real state, so it is edited
		}
	})
	if !has {
		return b.postAiring(now)
	}
	return b.refreshAiring(now)
}
