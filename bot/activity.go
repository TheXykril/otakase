package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The daily puzzle as a Discord Activity: a small web page that opens
// inside Discord (from /activity, its button, or the App Launcher). It is
// served by the public API under /activity/, reached through the
// Cloudflare Tunnel, and Discord's proxy maps the Activity's root to it.
// The page signs in with the Embedded App SDK: Discord gives it a code,
// the bot trades the code for the member's id with the app's client
// secret and hands the page its own session token. Set-up is in
// wiki/Developer-Discord-Bot.md (Activity).

//go:embed assets/activity
var activityFiles embed.FS

const activitySessionTTL = 12 * time.Hour

// Launch intents expire quickly and are consumed only by the launching member.
var activityLaunches = struct {
	sync.Mutex
	modes map[string]activityLaunchMode
}{modes: make(map[string]activityLaunchMode)}

type activityLaunchMode struct {
	mode    string
	expires time.Time
}

func activityLaunchUser(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func setActivityLaunchMode(user, mode string) {
	if user == "" {
		return
	}
	activityLaunches.Lock()
	defer activityLaunches.Unlock()
	if mode == "" {
		delete(activityLaunches.modes, user)
		return
	}
	activityLaunches.modes[user] = activityLaunchMode{mode: mode, expires: time.Now().Add(45 * time.Second)}
}

func takeActivityLaunchMode(user string) string {
	activityLaunches.Lock()
	defer activityLaunches.Unlock()
	hint, ok := activityLaunches.modes[user]
	delete(activityLaunches.modes, user)
	if ok && time.Now().Before(hint.expires) {
		return hint.mode
	}
	return ""
}

type activitySession struct {
	user    string
	expires time.Time
}

type activitySessions struct {
	mu sync.Mutex
	m  map[string]activitySession
}

func (a *activitySessions) add(user string) string {
	var buf [24]byte
	rand.Read(buf[:])
	token := hex.EncodeToString(buf[:])
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.m == nil {
		a.m = map[string]activitySession{}
	}
	now := time.Now()
	for t, s := range a.m {
		if now.After(s.expires) {
			delete(a.m, t)
		}
	}
	a.m[token] = activitySession{user: user, expires: now.Add(activitySessionTTL)}
	return token
}

// user is the member a request's session token belongs to.
func (a *activitySessions) user(r *http.Request) string {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.m[token]
	if !ok || time.Now().After(s.expires) {
		return ""
	}
	return s.user
}

// activityReady reports whether the Activity can sign members in.
func (b *bot) activityReady() bool { return b.cfg.DiscordSecret != "" }

// appID is the Discord application id (the same as the bot's user id).
func (b *bot) appID() string {
	if b.s != nil && b.s.State != nil {
		if b.s.State.Application != nil && b.s.State.Application.ID != "" {
			return b.s.State.Application.ID
		}
		if b.s.State.User != nil {
			return b.s.State.User.ID
		}
	}
	return ""
}

// launchActivity answers an interaction by opening the Activity. False when
// Discord refused (Activities not turned on for the app).
func (b *bot) launchActivity(i *discordgo.InteractionCreate) bool {
	return b.launchActivityMode(i, "")
}

// launchActivityMode launches the same Activity with a short-lived menu selection.
func (b *bot) launchActivityMode(i *discordgo.InteractionCreate, mode string) bool {
	user := activityLaunchUser(i)
	setActivityLaunchMode(user, mode)
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: 12}); err != nil {
		setActivityLaunchMode(user, "")
		log.Printf("launch activity: %v", err)
		if i.Type == discordgo.InteractionMessageComponent {
			b.respond(i, true, errorEmbed("Couldn't open the Activity. Try again, or update Discord."))
			return true
		}
		return false
	}
	return true
}

var activityLimit = newRateLimit(20, time.Minute)

func init() {
	static := func(name, typ string) func(*bot, http.ResponseWriter, *http.Request) {
		return func(_ *bot, w http.ResponseWriter, _ *http.Request) {
			data, err := fs.ReadFile(activityFiles, "assets/activity/"+name)
			if err != nil {
				http.NotFound(w, nil)
				return
			}
			w.Header().Set("Content-Type", typ)
			w.Write(data)
		}
	}
	apiHandle("GET /activity/{$}", static("index.html", "text/html; charset=utf-8"))
	apiHandle("GET /activity/app.js", static("app.js", "text/javascript; charset=utf-8"))
	apiHandle("GET /activity/sdk.js", static("sdk.js", "text/javascript; charset=utf-8"))
	apiHandle("GET /activity/watch.js", static("watch.js", "text/javascript; charset=utf-8"))
	apiHandle("GET /activity/hls.js", static("hls.js", "text/javascript; charset=utf-8"))
	apiHandle("GET /activity/fonts/{name}", func(_ *bot, w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !strings.HasPrefix(name, "Inter-") || !strings.HasSuffix(name, ".otf") {
			http.NotFound(w, r)
			return
		}
		data, err := cardFonts.ReadFile("assets/fonts/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "font/otf")
		w.Header().Set("Cache-Control", "public, max-age=604800")
		w.Write(data)
	})
	apiHandle("GET /activity", func(_ *bot, w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/activity/", http.StatusMovedPermanently)
	})
	apiHandle("GET /activity/api/config", func(b *bot, w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"client_id": b.appID(), "ready": b.activityReady(), "off": b.gamesOff()})
	})
	apiHandle("POST /activity/api/token", (*bot).activityToken)
	apiHandle("GET /activity/api/daily", (*bot).activityDaily)
	apiHandle("POST /activity/api/guess", (*bot).activityGuess)
	apiHandle("GET /activity/api/titles", (*bot).activityTitles)
	apiHandle("GET /activity/api/cover/{id}", (*bot).activityCover)
}

var discordAPI = "https://discord.com/api/v10"

var activityClient = &http.Client{Timeout: 15 * time.Second}

// activityToken trades the SDK's code for the member and a session token.
func (b *bot) activityToken(w http.ResponseWriter, r *http.Request) {
	if !activityLimit.allow(clientIP(r), time.Now()) {
		apiError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	if !b.activityReady() {
		apiError(w, http.StatusServiceUnavailable, "the Activity isn't set up")
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, 4<<10, &in) || in.Code == "" {
		return
	}
	form := url.Values{"client_id": {b.appID()}, "client_secret": {b.cfg.DiscordSecret},
		"grant_type": {"authorization_code"}, "code": {in.Code}}
	resp, err := activityClient.PostForm(discordAPI+"/oauth2/token", form)
	if err != nil {
		apiError(w, http.StatusBadGateway, "couldn't reach Discord")
		return
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&tok) != nil || tok.AccessToken == "" {
		log.Printf("activity token: Discord answered %s", resp.Status)
		apiError(w, http.StatusUnauthorized, "sign in failed")
		return
	}
	req, _ := http.NewRequest("GET", discordAPI+"/users/@me", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	me, err := activityClient.Do(req)
	if err != nil {
		apiError(w, http.StatusBadGateway, "couldn't reach Discord")
		return
	}
	defer me.Body.Close()
	var u struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
	}
	if me.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(me.Body, 64<<10)).Decode(&u) != nil || u.ID == "" {
		apiError(w, http.StatusUnauthorized, "sign in failed")
		return
	}
	if !b.inServer(u.ID) {
		apiError(w, http.StatusForbidden, "join the Otakase server to play")
		return
	}
	name := u.GlobalName
	if name == "" {
		name = u.Username
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": b.acts.add(u.ID), "name": name, "access_token": tok.AccessToken, "mode": takeActivityLaunchMode(u.ID)})
}

// activityState is what the page shows.
type activityState struct {
	Num    int          `json:"num"`
	Tries  int          `json:"tries"`
	Play   dailyPlay    `json:"play"`
	Stats  dailyStats   `json:"stats"`
	Answer *activityHit `json:"answer,omitempty"` // once done
	Grid   string       `json:"grid,omitempty"`
	Score  string       `json:"score,omitempty"`
	Next   int64        `json:"next"` // unix time of the next puzzle
}

type activityHit struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (b *bot) activityStateFor(user string, p dailyPuzzle, pl dailyPlay) activityState {
	st := activityState{Num: p.Num, Tries: dailyTries, Play: pl}
	if st.Play.Guesses == nil {
		st.Play.Guesses = []dailyGuess{}
	}
	b.store.view(func(d *storeData) { st.Stats = d.DailyStats[user] })
	if pl.Done {
		st.Answer = &activityHit{ID: p.Show.ID, Name: p.Show.Name, URL: p.Show.URL}
		st.Grid, st.Score = dailyGrid(pl), dailyScore(pl)
	}
	day, _ := time.Parse(time.DateOnly, p.Day)
	st.Next = day.AddDate(0, 0, 1).Unix()
	return st
}

func (b *bot) activityDaily(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	p, err := b.dailyToday()
	if err != nil {
		apiError(w, http.StatusBadGateway, "couldn't reach AniList, try again in a bit")
		return
	}
	writeJSON(w, http.StatusOK, b.activityStateFor(user, p, b.dailyPlayOf(user, p.Day)))
}

func (b *bot) activityGuess(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	var in struct {
		ID      int    `json:"id"`
		Channel string `json:"channel"`
	}
	if !readJSON(w, r, 4<<10, &in) {
		return
	}
	p, pl, finished, err := b.dailyTry(user, in.ID)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if finished && in.Channel != "" {
		if c := b.channel(in.Channel); c != nil && c.GuildID == b.cfg.GuildID && c.Type != discordgo.ChannelTypeGuildVoice {
			go b.postDailyShare(in.Channel, user, p, pl)
		}
	}
	writeJSON(w, http.StatusOK, b.activityStateFor(user, p, pl))
}

// activityTitles is every show you can guess, for the search box.
func (b *bot) activityTitles(w http.ResponseWriter, r *http.Request) {
	if b.actUser(w, r) == "" {
		return
	}
	pool, err := b.guess.shows()
	if err != nil {
		apiError(w, http.StatusBadGateway, "couldn't reach AniList, try again in a bit")
		return
	}
	type title struct {
		ID    int      `json:"id"`
		Name  string   `json:"name"`
		Year  int      `json:"year,omitempty"`
		Other []string `json:"other,omitempty"`
	}
	out := make([]title, 0, len(pool))
	for _, s := range dailyMatches(pool, "", len(pool)) {
		t := title{ID: s.ID, Name: s.Name, Year: s.Year}
		for _, o := range s.Titles {
			if o != s.Name && len(t.Other) < 4 {
				t.Other = append(t.Other, o)
			}
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}

// activityCover passes a pool show's cover through, since the Activity can
// only load pictures from its own address.
func (b *bot) activityCover(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cover := ""
	if s, ok := b.dailyShowByID(id); ok {
		cover = s.Cover
	}
	if cover == "" {
		b.store.view(func(d *storeData) {
			if d.Daily != nil && d.Daily.Show.ID == id {
				cover = d.Daily.Show.Cover
			}
		})
	}
	proxyAniList(w, r, cover)
}

// proxyAniList passes a picture on AniList's image host through.
func proxyAniList(w http.ResponseWriter, r *http.Request, src string) {
	if !strings.HasPrefix(src, "https://media.kitsu.io/") && !strings.HasPrefix(src, "https://s4.anilist.co/") && !strings.HasPrefix(src, "https://img1.ak.crunchyroll.com/") && !strings.HasPrefix(src, "https://static.crunchyroll.com/") {
		http.NotFound(w, r)
		return
	}
	imageClient := *activityClient
	imageClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := imageClient.Get(src)
	if err != nil || resp.StatusCode != http.StatusOK {
		if err == nil {
			resp.Body.Close()
		}
		http.Error(w, "picture unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	typ := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(typ, "image/") {
		typ = "image/jpeg"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "public, max-age=604800")
	if _, err := io.Copy(w, io.LimitReader(resp.Body, 8<<20)); err != nil {
		log.Printf("activity picture: %v", fmt.Errorf("%s: %w", src, err))
	}
}
