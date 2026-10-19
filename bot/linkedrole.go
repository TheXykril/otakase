package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Linked Roles: a member connects Otakase to their Discord account once,
// Discord shows the connection on their profile with their Otakase hours,
// and a server role can ask for it (Server Settings → Roles → the role →
// Links). At start the bot tells Discord which values the connection
// holds. The member signs in at /v1/linked-role with the app's OAuth2
// (role_connections.write identify), the bot keeps their tokens and pushes
// new values as they watch, at most once an hour (at once when their app
// link changes).
//
//	GET /v1/linked-role          → redirect to Discord's sign-in; the Linked Roles Verification URL
//	GET /v1/linked-role/callback → where Discord sends them back; a small "Done" page
//
// It needs the app's client secret (discord_client_secret, the same one as
// the Activity) and this callback in the app's OAuth2 redirects.

const (
	linkedRoleStateTTL  = 10 * time.Minute
	linkedRolePushEvery = time.Hour
	linkedRoleTick      = 5 * time.Minute
	linkedRoleMaxPushes = 50 // per tick
	linkedRoleCookie    = "otakase_linked_role"
	linkedRoleScope     = "role_connections.write identify"
)

// linkedRole is a member's role connection: their OAuth2 tokens (secrets,
// never logged) and what was last pushed to Discord.
type linkedRole struct {
	AccessToken  string         `json:"access_token"`
	RefreshToken string         `json:"refresh_token"`
	Expires      time.Time      `json:"expires"`
	Name         string         `json:"name,omitempty"` // shown as their Otakase name
	Pushed       linkedRoleMeta `json:"pushed"`
	PushedAt     time.Time      `json:"pushed_at,omitzero"`
}

// linkedRoleMeta is what the connection shows.
type linkedRoleMeta struct {
	Hours int  `json:"hours_watched"`
	App   bool `json:"app_linked"`
}

// roleConnectionField is one value the connection holds, as Discord
// describes it. Type 2 is "at least", 7 is "is true".
type roleConnectionField struct {
	Type        int    `json:"type"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

var linkedRoleFields = []roleConnectionField{
	{Type: 2, Key: "hours_watched", Name: "Hours watched", Description: "Hours watched in the Otakase app"},
	{Type: 7, Key: "app_linked", Name: "Otakase app linked", Description: "Has the Otakase app linked to Discord"},
}

var (
	discordAuthorizeURL = "https://discord.com/oauth2/authorize"
	linkedRoleLimit     = newRateLimit(20, time.Minute)
	errInvalidGrant     = errors.New("Discord no longer accepts the member's sign-in")
	errTokenRejected    = errors.New("Discord rejected the access token")
)

// linkedRoleClientID and linkedRoleMember are variables so tests run
// without Discord.
var (
	linkedRoleClientID = (*bot).appID
	// linkedRoleMember is the member's server name, and false when they
	// are not in the server.
	linkedRoleMember = func(b *bot, user string) (string, bool) {
		name, gone := b.appMemberName(user)
		return name, !gone
	}
)

func init() {
	apiHandle("GET /v1/linked-role", (*bot).apiLinkedRole)
	apiHandle("GET /v1/linked-role/callback", (*bot).apiLinkedRoleCallback)
	registerCommand(&discordgo.ApplicationCommand{Name: "linked-role",
		Description: "Show your Otakase hours on your Discord profile and unlock the linked role"}, (*bot).linkedRoleCommand)
	registerHelp("`/linked-role` shows your Otakase hours on your Discord profile and unlocks the linked role")
	onStart(func(b *bot) {
		if b.cfg.DiscordSecret != "" {
			go b.registerLinkedRoleFields()
		}
	})
	registerJob("Linked roles", (*bot).linkedRoleLoop)
}

// publicBaseURL is where the internet reaches the public API:
// OTAKASE_PUBLIC_URL, else https://bot.xyverion.com.
func publicBaseURL() string {
	if u := strings.TrimRight(strings.TrimSpace(os.Getenv("OTAKASE_PUBLIC_URL")), "/"); u != "" {
		return u
	}
	return "https://bot.xyverion.com"
}

func linkedRoleRedirect() string { return publicBaseURL() + "/v1/linked-role/callback" }

// linkedRoleValues is what the connection should show for a member.
func linkedRoleValues(st appStats, hasApp bool) linkedRoleMeta {
	return linkedRoleMeta{Hours: int(st.Seconds / 3600), App: hasApp}
}

// linkedRoleDue reports whether a member's connection needs a push: at
// once when their app link changed, else at most once an hour.
func linkedRoleDue(l linkedRole, want linkedRoleMeta, now time.Time) bool {
	switch {
	case want == l.Pushed:
		return false
	case want.App != l.Pushed.App:
		return true
	}
	return now.Sub(l.PushedAt) >= linkedRolePushEvery
}

// roleConnectionMetadata is the connection's values as Discord takes them.
func roleConnectionMetadata(m linkedRoleMeta) map[string]string {
	app := "0"
	if m.App {
		app = "1"
	}
	return map[string]string{"hours_watched": strconv.Itoa(m.Hours), "app_linked": app}
}

// sameFields reports whether Discord already has these fields.
func sameFields(have, want []roleConnectionField) bool {
	if len(have) != len(want) {
		return false
	}
	for k := range want {
		if have[k] != want[k] {
			return false
		}
	}
	return true
}

// Sign-in states: a random value kept a few minutes and in a cookie, so a
// callback only counts for the browser that started it.

type oauthStates struct {
	mu sync.Mutex
	m  map[string]time.Time
}

var linkedRoleStates = &oauthStates{}

func (s *oauthStates) add(now time.Time) string {
	state := newToken(16)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]time.Time{}
	}
	for k, exp := range s.m {
		if now.After(exp) {
			delete(s.m, k)
		}
	}
	if len(s.m) > 1000 {
		clear(s.m)
	}
	s.m[state] = now.Add(linkedRoleStateTTL)
	return state
}

// take uses up a state; false when it is unknown or expired.
func (s *oauthStates) take(state string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.m[state]
	delete(s.m, state)
	return ok && state != "" && !now.After(exp)
}

func linkedRoleAuthURL(clientID, redirect, state string) string {
	return discordAuthorizeURL + "?" + url.Values{"client_id": {clientID}, "redirect_uri": {redirect},
		"response_type": {"code"}, "scope": {linkedRoleScope}, "state": {state}, "prompt": {"consent"}}.Encode()
}

// Discord calls. Errors name the status only: the tokens stay out of logs.

type discordToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// discordTokenRequest posts to Discord's token endpoint; errInvalidGrant
// when the code or refresh token is no good any more.
func discordTokenRequest(form url.Values) (discordToken, error) {
	resp, err := activityClient.PostForm(discordAPI+"/oauth2/token", form)
	if err != nil {
		return discordToken{}, errors.New("couldn't reach Discord")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error == "invalid_grant" {
			return discordToken{}, errInvalidGrant
		}
		return discordToken{}, fmt.Errorf("token: Discord answered %s", resp.Status)
	}
	var tok discordToken
	if json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		return discordToken{}, errors.New("token: Discord's answer had no token")
	}
	return tok, nil
}

func (b *bot) linkedRoleExchange(code string) (discordToken, error) {
	return discordTokenRequest(url.Values{"client_id": {linkedRoleClientID(b)}, "client_secret": {b.cfg.DiscordSecret},
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {linkedRoleRedirect()}})
}

// freshLinkedRole refreshes the access token when it has (nearly) run out.
func (b *bot) freshLinkedRole(l linkedRole, now time.Time) (linkedRole, error) {
	if now.Before(l.Expires.Add(-time.Minute)) {
		return l, nil
	}
	tok, err := discordTokenRequest(url.Values{"client_id": {linkedRoleClientID(b)}, "client_secret": {b.cfg.DiscordSecret},
		"grant_type": {"refresh_token"}, "refresh_token": {l.RefreshToken}})
	if err != nil {
		return l, err
	}
	l.AccessToken, l.Expires = tok.AccessToken, now.Add(time.Duration(tok.ExpiresIn)*time.Second)
	if tok.RefreshToken != "" {
		l.RefreshToken = tok.RefreshToken
	}
	return l, nil
}

// discordMe is the signed-in member's id.
func discordMe(access string) (string, error) {
	req, _ := http.NewRequest("GET", discordAPI+"/users/@me", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := activityClient.Do(req)
	if err != nil {
		return "", errors.New("couldn't reach Discord")
	}
	defer resp.Body.Close()
	var u struct {
		ID string `json:"id"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&u) != nil || u.ID == "" {
		return "", fmt.Errorf("users/@me: Discord answered %s", resp.Status)
	}
	return u.ID, nil
}

// putRoleConnection sets the member's connection; errTokenRejected when the
// access token no longer works.
func putRoleConnection(access, appID, name string, m linkedRoleMeta) error {
	body, _ := json.Marshal(map[string]any{"platform_name": "Otakase", "platform_username": clip(name, 100),
		"metadata": roleConnectionMetadata(m)})
	req, _ := http.NewRequest("PUT", discordAPI+"/users/@me/applications/"+url.PathEscape(appID)+"/role-connection", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	resp, err := activityClient.Do(req)
	if err != nil {
		return errors.New("couldn't reach Discord")
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errTokenRejected
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("role connection: Discord answered %s", resp.Status)
	}
	return nil
}

// pushLinkedRole sends a member's values, refreshing their token when it
// ran out or Discord turned it down. errInvalidGrant means drop the link.
func (b *bot) pushLinkedRole(user string, l linkedRole, want linkedRoleMeta, now time.Time) (linkedRole, error) {
	if name, here := linkedRoleMember(b, user); here && name != "" {
		l.Name = name
	}
	appID := linkedRoleClientID(b)
	for try := 0; ; try++ {
		var err error
		if l, err = b.freshLinkedRole(l, now); err != nil {
			return l, err
		}
		err = putRoleConnection(l.AccessToken, appID, l.Name, want)
		if errors.Is(err, errTokenRejected) && try == 0 {
			l.Expires = time.Time{} // refresh and try once more
			continue
		}
		if err != nil {
			return l, err
		}
		l.Pushed, l.PushedAt = want, now
		return l, nil
	}
}

// registerLinkedRoleFields tells Discord what the connection holds, when
// it doesn't know yet.
func (b *bot) registerLinkedRoleFields() {
	if err := putLinkedRoleFields(linkedRoleClientID(b), b.cfg.Token); err != nil {
		log.Printf("linked role fields: %v", err)
	}
}

func putLinkedRoleFields(appID, botToken string) error {
	if appID == "" {
		return errors.New("no application id")
	}
	endpoint := discordAPI + "/applications/" + url.PathEscape(appID) + "/role-connections/metadata"
	call := func(method string, body io.Reader) (*http.Response, error) {
		req, _ := http.NewRequest(method, endpoint, body)
		req.Header.Set("Authorization", "Bot "+botToken)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return activityClient.Do(req)
	}
	resp, err := call("GET", nil)
	if err != nil {
		return err
	}
	var have []roleConnectionField
	ok := resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&have) == nil
	resp.Body.Close()
	if ok && sameFields(have, linkedRoleFields) {
		return nil
	}
	body, _ := json.Marshal(linkedRoleFields)
	if resp, err = call("PUT", bytes.NewReader(body)); err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Discord answered %s", resp.Status)
	}
	log.Printf("linked role fields registered")
	return nil
}

// linkedRoleWants is what each connected member's connection should show.
func (b *bot) linkedRoleWants() (map[string]linkedRole, map[string]linkedRoleMeta) {
	links, wants := map[string]linkedRole{}, map[string]linkedRoleMeta{}
	b.store.view(func(d *storeData) {
		apps := map[string]bool{}
		for _, l := range d.AppLinks {
			apps[l.User] = true
		}
		for user, l := range d.LinkedRoles {
			links[user], wants[user] = l, linkedRoleValues(d.AppStats[user], apps[user])
		}
	})
	return links, wants
}

func (b *bot) linkedRoleLoop() {
	for {
		time.Sleep(linkedRoleTick)
		if b.cfg.DiscordSecret != "" {
			b.pushLinkedRoles(appNow())
		}
	}
}

// pushLinkedRoles updates the connections that changed.
func (b *bot) pushLinkedRoles(now time.Time) {
	links, wants := b.linkedRoleWants()
	pushed, failed := 0, 0
	for user, l := range links {
		if pushed+failed >= linkedRoleMaxPushes {
			break
		}
		if !linkedRoleDue(l, wants[user], now) {
			continue
		}
		next, err := b.pushLinkedRole(user, l, wants[user], now)
		switch {
		case errors.Is(err, errInvalidGrant):
			log.Printf("linked role of member %s: sign-in revoked, dropped", user)
			b.store.update(func(d *storeData) { delete(d.LinkedRoles, user) })
			continue
		case err != nil:
			failed++
		default:
			pushed++
		}
		// Keep refreshed tokens even when the push failed, unless the
		// member signed in again meanwhile.
		b.store.update(func(d *storeData) {
			if cur, ok := d.LinkedRoles[user]; ok && cur.RefreshToken == l.RefreshToken {
				d.LinkedRoles[user] = next
			}
		})
	}
	if failed > 0 {
		log.Printf("linked roles: %d updates failed, retrying later", failed)
	}
}

// The sign-in pages.

func (b *bot) apiLinkedRole(w http.ResponseWriter, r *http.Request) {
	now := appNow()
	if !linkedRoleLimit.allow(clientIP(r), now) {
		linkedRolePage(w, http.StatusTooManyRequests, "Slow down", "Too many tries. Wait a minute and try again.")
		return
	}
	clientID := linkedRoleClientID(b)
	if b.cfg.DiscordSecret == "" || clientID == "" {
		linkedRolePage(w, http.StatusServiceUnavailable, "Not set up", "Linked roles aren't set up on this bot yet.")
		return
	}
	state := linkedRoleStates.add(now)
	http.SetCookie(w, &http.Cookie{Name: linkedRoleCookie, Value: state, Path: "/v1/linked-role", MaxAge: int(linkedRoleStateTTL / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, linkedRoleAuthURL(clientID, linkedRoleRedirect(), state), http.StatusFound)
}

func (b *bot) apiLinkedRoleCallback(w http.ResponseWriter, r *http.Request) {
	now := appNow()
	if !linkedRoleLimit.allow(clientIP(r), now) {
		linkedRolePage(w, http.StatusTooManyRequests, "Slow down", "Too many tries. Wait a minute and try again.")
		return
	}
	if b.cfg.DiscordSecret == "" {
		linkedRolePage(w, http.StatusServiceUnavailable, "Not set up", "Linked roles aren't set up on this bot yet.")
		return
	}
	q := r.URL.Query()
	state := q.Get("state")
	c, err := r.Cookie(linkedRoleCookie)
	if err != nil || c.Value != state || !linkedRoleStates.take(state, now) {
		linkedRolePage(w, http.StatusBadRequest, "Link expired", "This sign-in expired or was opened in another browser. Start again from Discord.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: linkedRoleCookie, Path: "/v1/linked-role", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	if q.Get("error") != "" || q.Get("code") == "" {
		linkedRolePage(w, http.StatusOK, "Cancelled", "Nothing was connected. You can close this page.")
		return
	}
	tok, err := b.linkedRoleExchange(q.Get("code"))
	if err != nil {
		log.Printf("linked role sign-in: %v", err)
		linkedRolePage(w, http.StatusBadGateway, "Sign-in failed", "Discord didn't accept the sign-in. Start again from Discord.")
		return
	}
	if !strings.Contains(tok.Scope, "role_connections.write") {
		linkedRolePage(w, http.StatusBadRequest, "Sign-in failed", "Otakase needs permission to update your role connection. Start again and allow it.")
		return
	}
	user, err := discordMe(tok.AccessToken)
	if err != nil {
		log.Printf("linked role sign-in: %v", err)
		linkedRolePage(w, http.StatusBadGateway, "Sign-in failed", "Couldn't read your Discord account. Try again in a bit.")
		return
	}
	if _, here := linkedRoleMember(b, user); !here {
		linkedRolePage(w, http.StatusForbidden, "Join the server first", "Join the Otakase Discord server, then try again.")
		return
	}
	l := linkedRole{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, Expires: now.Add(time.Duration(tok.ExpiresIn) * time.Second)}
	var want linkedRoleMeta
	b.store.view(func(d *storeData) {
		has := false
		for _, a := range d.AppLinks {
			has = has || a.User == user
		}
		want = linkedRoleValues(d.AppStats[user], has)
	})
	if l, err = b.pushLinkedRole(user, l, want, now); err != nil {
		log.Printf("linked role of member %s: %v", user, err)
		linkedRolePage(w, http.StatusBadGateway, "Couldn't connect", "Discord didn't take the update. Try again in a bit.")
		return
	}
	b.store.update(func(d *storeData) {
		if d.LinkedRoles == nil {
			d.LinkedRoles = map[string]linkedRole{}
		}
		d.LinkedRoles[user] = l
	})
	log.Printf("linked role connected for member %s", user)
	text := fmt.Sprintf("Otakase is connected: your profile shows %d hours watched", want.Hours)
	if !want.App {
		text += ". Link the app with otakase -discord-link so your hours count"
	}
	linkedRolePage(w, http.StatusOK, "Done", text+". Go back to Discord.")
}

// linkedRolePage answers a small page for the browser.
func linkedRolePage(w http.ResponseWriter, code int, title, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(code)
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Otakase · %[1]s</title><style>body{font-family:system-ui,sans-serif;background:#16141a;color:#eee8df;display:grid;place-items:center;min-height:90vh;margin:0 16px}
main{max-width:28rem;text-align:center}h1{color:#d9433b;font-size:1.6rem}p{line-height:1.5}</style></head>
<body><main><h1>%[1]s</h1><p>%[2]s</p></main></body></html>`, html.EscapeString(title), html.EscapeString(text))
}

// /linked-role.

func (b *bot) linkedRoleCommand(i *discordgo.InteractionCreate) {
	if b.cfg.DiscordSecret == "" {
		b.respond(i, true, errorEmbed("Linked roles aren't set up yet."))
		return
	}
	user := interactionUser(i).ID
	var l linkedRole
	var connected bool
	b.store.view(func(d *storeData) { l, connected = d.LinkedRoles[user] })
	b.respond(i, true, linkedRoleView(connected, l.Pushed), linkButtons("Connect Otakase", publicBaseURL()+"/v1/linked-role")...)
}

func linkedRoleView(connected bool, m linkedRoleMeta) *discordgo.MessageEmbed {
	lines := []string{"Connect Otakase to your Discord account to show your Otakase hours on your profile " +
		"and get the roles that ask for it. Press the button below, or pick the role under **Linked Roles** in the server menu.", ""}
	if connected {
		app := "no"
		if m.App {
			app = "yes"
		}
		lines = append(lines, fmt.Sprintf("Connected: your profile shows **%d h** watched, app linked: **%s**. It updates within an hour as you watch.", m.Hours, app))
	} else {
		lines = append(lines, "Not connected yet. Hours count once you link the app with `otakase -discord-link`.")
	}
	return &discordgo.MessageEmbed{Title: "Linked role", Color: shu, Description: strings.Join(lines, "\n"),
		Footer: &discordgo.MessageEmbedFooter{Text: "Remove it any time under User Settings → Authorized Apps"}}
}
