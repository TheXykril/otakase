package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// MyAnimeList links: /link-mal gives a member a button to sign in on
// myanimelist.net; MAL sends them back to /v1/mal/callback on the public API
// and the bot keeps the token, so the watch room can move their MAL list on
// like their AniList one. Needs a MAL API app (myanimelist.net/apiconfig)
// with the callback as its App Redirect URL, set as mal_client_id and
// mal_client_secret. MAL only offers PKCE "plain", so the verifier is the
// challenge.

var (
	malAuthURL  = "https://myanimelist.net/v1/oauth2/authorize"
	malTokenURL = "https://myanimelist.net/v1/oauth2/token"
	malAPI      = "https://api.myanimelist.net/v2"
)

const (
	malStartTTL = 10 * time.Minute
	malCookie   = "otakase_mal"
)

type malLink struct {
	Name         string    `json:"name"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expires      time.Time `json:"expires"`
	Linked       time.Time `json:"linked"`
}

func malRedirect() string { return publicBaseURL() + "/v1/mal/callback" }

func (b *bot) malReady() bool { return b.cfg.MALClientID != "" && b.cfg.MALSecret != "" }

// malStarts are the links /link-mal hands out, by their token: who pressed
// it. malFlows are sign-ins under way, by OAuth state.
type malFlow struct {
	user     string
	verifier string
	expires  time.Time
}

type malFlows struct {
	mu sync.Mutex
	m  map[string]malFlow
}

var malStarts, malSignins = &malFlows{}, &malFlows{}

func (f *malFlows) add(user, verifier string, now time.Time) string {
	key := newToken(24)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[string]malFlow{}
	}
	for k, v := range f.m {
		if now.After(v.expires) {
			delete(f.m, k)
		}
	}
	if len(f.m) > 1000 {
		clear(f.m)
	}
	f.m[key] = malFlow{user: user, verifier: verifier, expires: now.Add(malStartTTL)}
	return key
}

func (f *malFlows) take(key string, now time.Time) (malFlow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[key]
	delete(f.m, key)
	return v, ok && key != "" && !now.After(v.expires)
}

var malLimit = newRateLimit(20, time.Minute)

func init() {
	registerCommand(&discordgo.ApplicationCommand{Name: "link-mal", Description: "Link your MyAnimeList account so the watch room updates it"}, (*bot).linkMALCommand)
	registerCommand(&discordgo.ApplicationCommand{Name: "unlink-mal", Description: "Remove your linked MyAnimeList account"}, (*bot).unlinkMALCommand)
	registerHelp("`/link-mal` links MyAnimeList, so episodes you watch in the watch room count on your list")
	apiHandle("GET /v1/mal", (*bot).apiMALStart)
	apiHandle("GET /v1/mal/callback", (*bot).apiMALCallback)
}

func (b *bot) linkMALCommand(i *discordgo.InteractionCreate) {
	if !b.malReady() {
		b.respond(i, true, errorEmbed("MyAnimeList linking isn't set up on this bot yet."))
		return
	}
	user := interactionUser(i).ID
	start := malStarts.add(user, "", appNow())
	desc := "Press the button, sign in on MyAnimeList and press **Allow**. Episodes you watch in the watch room then count on your MAL list too. `/unlink-mal` removes it.\n\nThe button works once, for 10 minutes."
	var l malLink
	var had bool
	b.store.view(func(d *storeData) { l, had = d.MAL[user] })
	if had {
		desc = fmt.Sprintf("Linked to MyAnimeList **%s**. Press the button to link another account.", l.Name)
	}
	b.respond(i, true, &discordgo.MessageEmbed{Title: "Link MyAnimeList", Description: desc, Color: shu},
		linkButtons("Open MyAnimeList", publicBaseURL()+"/v1/mal?t="+url.QueryEscape(start))...)
}

func (b *bot) unlinkMALCommand(i *discordgo.InteractionCreate) {
	user := interactionUser(i).ID
	var had bool
	b.store.update(func(d *storeData) {
		_, had = d.MAL[user]
		delete(d.MAL, user)
	})
	if !had {
		b.respond(i, true, errorEmbed("You have no MyAnimeList account linked."))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Description: "MyAnimeList unlinked.", Color: shu})
}

func (b *bot) apiMALStart(w http.ResponseWriter, r *http.Request) {
	now := appNow()
	if !malLimit.allow(clientIP(r), now) {
		linkedRolePage(w, http.StatusTooManyRequests, "Slow down", "Too many tries. Wait a minute and try again.")
		return
	}
	if !b.malReady() {
		linkedRolePage(w, http.StatusServiceUnavailable, "Not set up", "MyAnimeList linking isn't set up on this bot yet.")
		return
	}
	start, ok := malStarts.take(r.URL.Query().Get("t"), now)
	if !ok {
		linkedRolePage(w, http.StatusBadRequest, "Link expired", "This link was used or expired. Run /link-mal in Discord again.")
		return
	}
	verifier := newToken(48) // 96 characters, inside PKCE's 43 to 128
	state := malSignins.add(start.user, verifier, now)
	http.SetCookie(w, &http.Cookie{Name: malCookie, Value: state, Path: "/v1/mal", MaxAge: int(malStartTTL / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, malAuthURL+"?"+url.Values{"response_type": {"code"}, "client_id": {b.cfg.MALClientID},
		"state": {state}, "redirect_uri": {malRedirect()}, "code_challenge": {verifier}, "code_challenge_method": {"plain"}}.Encode(), http.StatusFound)
}

func (b *bot) apiMALCallback(w http.ResponseWriter, r *http.Request) {
	now := appNow()
	if !malLimit.allow(clientIP(r), now) {
		linkedRolePage(w, http.StatusTooManyRequests, "Slow down", "Too many tries. Wait a minute and try again.")
		return
	}
	q := r.URL.Query()
	state := q.Get("state")
	c, err := r.Cookie(malCookie)
	flow, ok := malSignins.take(state, now)
	if err != nil || c.Value != state || !ok {
		linkedRolePage(w, http.StatusBadRequest, "Link expired", "This sign-in expired or was opened in another browser. Run /link-mal in Discord again.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: malCookie, Path: "/v1/mal", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	if q.Get("error") != "" || q.Get("code") == "" {
		linkedRolePage(w, http.StatusOK, "Cancelled", "Nothing was linked. You can close this page.")
		return
	}
	tok, err := b.malToken(url.Values{"grant_type": {"authorization_code"}, "code": {q.Get("code")},
		"code_verifier": {flow.verifier}, "redirect_uri": {malRedirect()}})
	if err != nil {
		log.Printf("mal sign-in: %v", err)
		linkedRolePage(w, http.StatusBadGateway, "Sign-in failed", "MyAnimeList didn't accept the sign-in. Run /link-mal again.")
		return
	}
	l := malLink{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken,
		Expires: now.Add(time.Duration(tok.ExpiresIn) * time.Second), Linked: now}
	var me struct {
		Name string `json:"name"`
	}
	if err := malGet(l.AccessToken, "/users/@me", &me); err != nil || me.Name == "" {
		log.Printf("mal sign-in: %v", err)
		linkedRolePage(w, http.StatusBadGateway, "Sign-in failed", "Couldn't read your MyAnimeList account. Try again in a bit.")
		return
	}
	l.Name = me.Name
	b.store.update(func(d *storeData) {
		if d.MAL == nil {
			d.MAL = map[string]malLink{}
		}
		d.MAL[flow.user] = l
	})
	log.Printf("mal linked for member %s", flow.user)
	linkedRolePage(w, http.StatusOK, "Done", fmt.Sprintf("Linked to MyAnimeList %s. Episodes you watch in the watch room now count there too. Go back to Discord.", me.Name))
}

type malTokenAnswer struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

var errMALGrant = errors.New("mal refused the grant")

// malToken posts to MAL's token endpoint. Errors name the status only.
func (b *bot) malToken(form url.Values) (malTokenAnswer, error) {
	form.Set("client_id", b.cfg.MALClientID)
	form.Set("client_secret", b.cfg.MALSecret)
	resp, err := activityClient.PostForm(malTokenURL, form)
	if err != nil {
		return malTokenAnswer{}, errors.New("couldn't reach MyAnimeList")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		return malTokenAnswer{}, fmt.Errorf("%w: %s", errMALGrant, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return malTokenAnswer{}, fmt.Errorf("token: MyAnimeList answered %s", resp.Status)
	}
	var tok malTokenAnswer
	if json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		return malTokenAnswer{}, errors.New("token: MyAnimeList's answer had no token")
	}
	return tok, nil
}

// freshMAL refreshes a member's MAL token when it has (nearly) run out and
// keeps the new one.
func (b *bot) freshMAL(user string, l malLink) (malLink, error) {
	now := appNow()
	if now.Before(l.Expires.Add(-time.Hour)) {
		return l, nil
	}
	tok, err := b.malToken(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {l.RefreshToken}})
	if err != nil {
		return l, err
	}
	l.AccessToken, l.Expires = tok.AccessToken, now.Add(time.Duration(tok.ExpiresIn)*time.Second)
	if tok.RefreshToken != "" {
		l.RefreshToken = tok.RefreshToken
	}
	b.store.update(func(d *storeData) {
		if _, ok := d.MAL[user]; ok {
			d.MAL[user] = l
		}
	})
	return l, nil
}

func malGet(token, path string, out any) error {
	req, _ := http.NewRequest("GET", malAPI+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return getJSON(req, out)
}

// malProgress moves a member's MAL list to episode, the same way
// anilistProgress does AniList's.
func (b *bot) malProgress(user string, l malLink, show watchShow, episode int) (bool, error) {
	if !b.malReady() {
		return false, nil
	}
	l, err := b.freshMAL(user, l)
	if err != nil {
		return false, err
	}
	var cur struct {
		NumEpisodes int `json:"num_episodes"`
		Status      *struct {
			Status      string `json:"status"`
			Watched     int    `json:"num_episodes_watched"`
			IsRewatched bool   `json:"is_rewatching"`
		} `json:"my_list_status"`
	}
	if err := malGet(l.AccessToken, "/anime/"+strconv.Itoa(show.MalID)+"?fields=num_episodes,my_list_status", &cur); err != nil {
		return false, err
	}
	status := "watching"
	if s := cur.Status; s != nil {
		if s.Status == "completed" && !s.IsRewatched || s.Watched >= episode {
			return false, nil
		}
		if s.IsRewatched {
			status = s.Status
		}
	}
	total := cur.NumEpisodes
	if total == 0 {
		total = show.Episodes
	}
	if total > 0 && episode >= total && status == "watching" {
		status = "completed"
	}
	form := url.Values{"num_watched_episodes": {strconv.Itoa(episode)}, "status": {status}}
	req, _ := http.NewRequest("PATCH", malAPI+"/anime/"+strconv.Itoa(show.MalID)+"/my_list_status", strings.NewReader(form.Encode()))
	req.Header.Set("Authorization", "Bearer "+l.AccessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := activityClient.Do(req)
	if err != nil {
		return false, errors.New("couldn't reach MyAnimeList")
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("list update: MyAnimeList answered %s", resp.Status)
	}
	return true, nil
}
