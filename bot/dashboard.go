package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The dashboard is a small web page shown in the Home Assistant
// sidebar through add-on ingress. Home Assistant handles the login, so as
// an add-on the page only answers the ingress proxy. Outside Home Assistant
// it is off unless OTAKASE_DASHBOARD_ADDR is set, and then it listens on
// 127.0.0.1 unless that address names a host. Most pages only show things;
// a few have controls (see dashcontrols.go).

const (
	dashboardPort  = ":8099"
	ingressProxyIP = "172.30.32.2"
)

// startedAt is when the bot started, for the uptime shown on the dashboard.
var startedAt = time.Now()

// dashSection is one page of the dashboard. Render reads what it needs
// (briefly, under the store lock) and returns ready HTML; the helpers
// dashStats, dashTable and dashNote build the usual pieces.
type dashSection struct {
	Slug, Name string
	Render     func(b *bot) template.HTML
	Controls   bool // has forms, so it doesn't refresh by itself
}

// dashSections are the dashboard pages in menu order. The built-in ones are
// set here so they come first; other features add theirs with
// addDashSection from an init function.
var dashSections = []dashSection{
	{Slug: "overview", Name: "Overview", Render: (*bot).dashOverview},
	{Slug: "settings", Name: "Settings", Render: (*bot).dashSettings, Controls: true},
	{Slug: "post", Name: "Post", Render: (*bot).dashPostPage, Controls: true},
	{Slug: "texts", Name: "Texts", Render: (*bot).dashTexts, Controls: true},
	{Slug: "safety", Name: "Safety", Render: (*bot).dashSafety, Controls: true},
	{Slug: "airing", Name: "Airing", Render: (*bot).dashAiring, Controls: true},
	{Slug: "support", Name: "Support", Render: (*bot).dashSupport, Controls: true},
	{Slug: "follows", Name: "Follows", Render: (*bot).dashFollows},
	{Slug: "suggestions", Name: "Suggestions", Render: (*bot).dashSuggestions, Controls: true},
	{Slug: "watch-parties", Name: "Watch parties", Render: (*bot).dashParties, Controls: true},
	{Slug: "rooms", Name: "Voice rooms", Render: (*bot).dashRooms, Controls: true},
	{Slug: "moderation", Name: "Moderation", Render: (*bot).dashModeration, Controls: true},
	{Slug: "games", Name: "Games", Render: (*bot).dashGames, Controls: true},
	{Slug: "riddles", Name: "Riddles", Render: (*bot).dashRiddles, Controls: true},
	{Slug: "anilist", Name: "AniList", Render: (*bot).dashAniList},
	{Slug: "bot", Name: "Bot", Render: (*bot).dashBotPage, Controls: true},
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// addDashSection adds a page to the dashboard menu.
func addDashSection(name string, render func(b *bot) template.HTML) {
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	dashSections = append(dashSections, dashSection{Slug: slug, Name: name, Render: render})
}

// dashboardAddr says where the dashboard listens and whether only the
// ingress proxy may use it. ok is false when the dashboard is off.
func dashboardAddr(env, optionsPath string) (addr string, ingressOnly, ok bool) {
	if env = strings.TrimSpace(env); env != "" {
		host, port, err := net.SplitHostPort(env)
		if err != nil {
			log.Printf("OTAKASE_DASHBOARD_ADDR %q: %v", env, err)
			return "", false, false
		}
		if host == "" {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(host, port), false, true
	}
	if _, err := os.Stat(optionsPath); err == nil {
		return dashboardPort, true, true
	}
	return "", false, false
}

// serveDashboard runs the dashboard web server when it is turned on.
func (b *bot) serveDashboard() {
	addr, ingressOnly, ok := dashboardAddr(os.Getenv("OTAKASE_DASHBOARD_ADDR"), "/data/options.json")
	if !ok {
		return
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           b.dashboardHandler(ingressOnly),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	log.Printf("dashboard on %s", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("dashboard: %v", err)
	}
}

// dashboardHandler serves the dashboard page.
func (b *bot) dashboardHandler(ingressOnly bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ingressOnly {
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			if host != ingressProxyIP {
				http.Error(w, "Open the dashboard from the Home Assistant sidebar.", http.StatusForbidden)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			b.dashAPI(w, r)
			return
		}
		if r.URL.Path != "/" {
			dashStatic(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if r.URL.Query().Get("download") == "data" {
				b.dashDownload(w)
				return
			}
		case http.MethodPost:
			b.dashPost(w, r)
			return
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		sec := dashSections[0]
		for _, s := range dashSections {
			if s.Slug == r.URL.Query().Get("s") {
				sec = s
			}
		}
		flash, bad := takeFlash()
		page := dashPage{
			Flash:    flash,
			FlashBad: bad,
			Base:     ingressBase(r.Header.Get("X-Ingress-Path")),
			Version:  botVersion(),
			Groups:   dashNav(sec.Slug),
			Current:  sec,
			Info:     dashInfo(sec.Slug),
			Body:     b.renderSection(sec),
			Updated:  time.Now().UTC().Format("15:04 UTC"),
		}
		var buf bytes.Buffer
		if err := dashTmpl.Execute(&buf, page); err != nil {
			log.Printf("dashboard: %v", err)
			http.Error(w, "Something went wrong drawing the page.", http.StatusInternalServerError)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'self'; font-src 'self'; base-uri 'self'; form-action 'self'")
		w.Write(buf.Bytes())
	})
}

var ingressPathRe = regexp.MustCompile(`^/[A-Za-z0-9_\-/]*$`)

// ingressBase is the page's base URL from the X-Ingress-Path header, so
// relative links stay inside the ingress path. Empty when not behind ingress.
func ingressBase(p string) string {
	if p == "" || !ingressPathRe.MatchString(p) || strings.Contains(p, "//") {
		return ""
	}
	return strings.TrimSuffix(p, "/") + "/"
}

// renderSection draws one page, so a broken section shows a note instead
// of taking the dashboard down.
func (b *bot) renderSection(s dashSection) (out template.HTML) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("dashboard %s: %v", s.Slug, r)
			out = dashNote("This section could not be drawn right now.")
		}
	}()
	return s.Render(b)
}

type dashPage struct {
	Base, Version, Updated string
	Flash                  string
	FlashBad               bool
	Groups                 []dashNavGroup
	Current                dashSection
	Info                   dashPageInfo
	Body                   template.HTML
}

// Helpers for sections.

type dashStat struct{ Label, Value string }

var dashPartsTmpl = template.Must(template.New("stats").Parse(
	`<div class="stats">{{range .}}<div class="stat"><div class="v">{{.Value}}</div><div class="l">{{.Label}}</div></div>{{end}}</div>`))

func init() {
	template.Must(dashPartsTmpl.New("table").Parse(
		`{{if .Rows}}<div class="tw"><table><thead><tr>{{range .Head}}<th>{{.}}</th>{{end}}</tr></thead><tbody>` +
			`{{range .Rows}}<tr>{{range .}}<td>{{.}}</td>{{end}}</tr>{{end}}</tbody></table></div>` +
			`{{else}}<p class="note">{{.Empty}}</p>{{end}}`))
	template.Must(dashPartsTmpl.New("note").Parse(`<p class="note">{{.}}</p>`))
	template.Must(dashPartsTmpl.New("h").Parse(`<h2>{{.}}</h2>`))
}

func dashExec(name string, data any) template.HTML {
	var buf bytes.Buffer
	if err := dashPartsTmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("dashboard %s: %v", name, err)
	}
	return template.HTML(buf.String())
}

// dashStats is a row of big numbers with labels.
func dashStats(stats ...dashStat) template.HTML { return dashExec("stats", stats) }

// dashTable is a table, or the empty text when there are no rows.
func dashTable(head []string, rows [][]string, empty string) template.HTML {
	return dashExec("table", struct {
		Head  []string
		Rows  [][]string
		Empty string
	}{head, rows, empty})
}

// dashNote is a line of quiet text.
func dashNote(text string) template.HTML { return dashExec("note", text) }

// dashHeading is a small heading inside a section.
func dashHeading(text string) template.HTML { return dashExec("h", text) }

// dashText marks ready HTML built from escaped pieces.
func dashText(s string) template.HTML { return template.HTML(s) }

func init() {
	template.Must(dashPartsTmpl.New("htable").Parse(
		`{{if .Rows}}<div class="tw"><table><thead><tr>{{range .Head}}<th>{{.}}</th>{{end}}</tr></thead><tbody>` +
			`{{range .Rows}}<tr>{{range .}}<td>{{.}}</td>{{end}}</tr>{{end}}</tbody></table></div>` +
			`{{else}}<p class="note">{{.Empty}}</p>{{end}}`))
}

// dashHTMLTable is dashTable with cells that are ready HTML.
func dashHTMLTable(head []string, rows [][]template.HTML, empty string) template.HTML {
	return dashExec("htable", struct {
		Head  []string
		Rows  [][]template.HTML
		Empty string
	}{head, rows, empty})
}

// dashJoin puts pieces of a section together.
func dashJoin(parts ...template.HTML) template.HTML {
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(string(p))
	}
	return template.HTML(sb.String())
}

// dashNames keeps names the gateway cache didn't have, so pages don't ask
// Discord again on every render.
var dashNames = struct {
	sync.Mutex
	m map[string]dashNameHit
}{m: map[string]dashNameHit{}}

type dashNameHit struct {
	name string
	at   time.Time
}

// dashName is a member's display name: gateway cache, else asked from
// Discord (member, then user for people who left), else a short id.
func (b *bot) dashName(id string) string {
	if b.s == nil {
		return dashShortID(id)
	}
	if b.s.State != nil {
		if m, err := b.s.State.Member(b.cfg.GuildID, id); err == nil && m.User != nil {
			return displayName(m)
		}
	}
	dashNames.Lock()
	hit, ok := dashNames.m[id]
	dashNames.Unlock()
	if ok && time.Since(hit.at) < time.Hour {
		return hit.name
	}
	name := ""
	if m, err := b.s.GuildMember(b.cfg.GuildID, id); err == nil && m.User != nil {
		if m.GuildID == "" {
			m.GuildID = b.cfg.GuildID
		}
		if b.s.State != nil {
			_ = b.s.State.MemberAdd(m)
		}
		name = displayName(m)
	} else if u, err := b.s.User(id); err == nil {
		name = u.GlobalName
		if name == "" {
			name = u.Username
		}
	}
	if name == "" {
		name = dashShortID(id)
	}
	dashNames.Lock()
	dashNames.m[id] = dashNameHit{name, time.Now()}
	dashNames.Unlock()
	return name
}

// dashShortID is the last resort name: the end of the id.
func dashShortID(id string) string {
	if len(id) > 6 {
		return "member …" + id[len(id)-6:]
	}
	return "member " + id
}

// dashAgo writes a duration roughly, like "3 hours".
func dashAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return dashPlural(int(d/time.Minute), "minute")
	case d < 48*time.Hour:
		return dashPlural(int(d/time.Hour), "hour")
	default:
		return dashPlural(int(d/(24*time.Hour)), "day")
	}
}

func dashPlural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// Overview.

// failingJobs lists the background jobs currently failing, with when staff
// were last told.
func (h *health) failingJobs() map[string]time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]time.Time{}
	for job := range h.failing {
		out[job] = h.last[job]
	}
	return out
}

func (b *bot) dashOverview() template.HTML {
	latency, members := "not connected", "unknown"
	if b.s != nil {
		if l := b.s.HeartbeatLatency(); l > 0 {
			latency = fmt.Sprintf("%d ms", l.Milliseconds())
		}
		if b.s.State != nil {
			if g, err := b.s.State.Guild(b.cfg.GuildID); err == nil {
				members = fmt.Sprint(g.MemberCount)
			}
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	size := "not saved yet"
	if b.store != nil {
		if fi, err := os.Stat(b.store.path); err == nil {
			size = fmtBytes(fi.Size())
		}
	}
	parts := []template.HTML{dashStats(
		dashStat{"Version", botVersion()},
		dashStat{"Uptime", dashAgo(time.Since(startedAt))},
		dashStat{"Gateway latency", latency},
		dashStat{"Members", members},
		dashStat{"Memory", fmtBytes(int64(mem.HeapAlloc))},
		dashStat{"Data file", size},
	), dashHeading("Background jobs")}

	var failing map[string]time.Time
	var jobs map[string]jobStatus
	if b.health != nil {
		failing, jobs = b.health.failingJobs(), b.health.jobsSnapshot()
	}
	names := map[string]bool{}
	for n := range jobs {
		names[n] = true
	}
	for n := range failing {
		names[n] = true
	}
	var rows [][]string
	bad := 0
	now := time.Now()
	for n := range names {
		state, last, problem := dashJobState(jobs[n], now)
		if _, told := failing[n]; told && state != "failing" {
			state, problem = "failing", "staff were told"
		}
		if state == "failing" {
			bad++
		}
		rows = append(rows, []string{n, state, last, problem})
	}
	sort.Slice(rows, func(i, j int) bool {
		if (rows[i][1] == "failing") != (rows[j][1] == "failing") {
			return rows[i][1] == "failing"
		}
		return rows[i][0] < rows[j][0]
	})
	if bad == 0 {
		parts = append(parts, dashNote("All background jobs are working."))
	} else {
		parts = append(parts, dashNote(dashPlural(bad, "job")+" failing."))
	}
	parts = append(parts, dashTable([]string{"Job", "State", "Last ok", "Problem"}, rows, "No jobs have reported yet."))

	ollama := strings.ReplaceAll(b.ollamaState(), "**", "")
	parts = append(parts, dashHeading("Ollama"), dashNote(ollama))
	return dashJoin(parts...)
}

// dashJobState is a job's state, when it last worked and its last error
// while it is failing.
func dashJobState(j jobStatus, now time.Time) (state, last, problem string) {
	switch {
	case !j.LastFail.IsZero() && j.LastFail.After(j.LastOK):
		state = "failing"
		problem = dashAgo(now.Sub(j.LastFail)) + " ago"
		if j.Err != "" {
			problem += ": " + clip(strings.ReplaceAll(j.Err, "\n", " "), 200)
		}
	case !j.LastOK.IsZero():
		state = "ok"
	case j.Stopped:
		state = "off"
	default:
		state = "running"
	}
	last = "—"
	if !j.LastOK.IsZero() {
		last = dashAgo(now.Sub(j.LastOK)) + " ago"
	}
	return state, last, problem
}

// Follows.

type showCount struct {
	Title string
	Count int
}

// followStats counts members using /follow and how many follow each show.
func (st *store) followStats() (members int, shows []showCount) {
	byID := map[int]*showCount{}
	st.view(func(d *storeData) {
		for _, list := range d.Follows {
			if len(list) > 0 {
				members++
			}
			for _, f := range list {
				c := byID[f.ID]
				if c == nil {
					c = &showCount{Title: f.Title}
					byID[f.ID] = c
				}
				c.Count++
			}
		}
	})
	for _, c := range byID {
		shows = append(shows, *c)
	}
	sort.Slice(shows, func(i, j int) bool {
		if shows[i].Count != shows[j].Count {
			return shows[i].Count > shows[j].Count
		}
		return shows[i].Title < shows[j].Title
	})
	return members, shows
}

func (b *bot) dashFollows() template.HTML {
	members, shows := b.store.followStats()
	var rows [][]string
	for i, s := range shows {
		if i == 25 {
			break
		}
		rows = append(rows, []string{s.Title, fmt.Sprint(s.Count)})
	}
	return dashJoin(
		dashStats(dashStat{"Members using /follow", fmt.Sprint(members)}, dashStat{"Shows followed", fmt.Sprint(len(shows))}),
		dashHeading("Most followed"),
		dashTable([]string{"Show", "Followers"}, rows, "Nobody follows a show yet."),
	)
}

// Suggestions.

type suggestionRow struct {
	ID, Title, Status, Key string
	Up, Down               int
	Created                time.Time
}

func (st *store) suggestionRows() []suggestionRow {
	var out []suggestionRow
	st.view(func(d *storeData) {
		for id, s := range d.Suggestions {
			st := statusByKey(s.Status)
			out = append(out, suggestionRow{id, s.Title, st.Label, st.Key, len(s.Up), len(s.Down), s.Created})
		}
	})
	sort.Slice(out, func(i, j int) bool {
		si, sj := out[i].Up-out[i].Down, out[j].Up-out[j].Down
		if si != sj {
			return si > sj
		}
		return out[i].Created.After(out[j].Created)
	})
	return out
}

func (b *bot) dashSuggestions() template.HTML {
	all := b.store.suggestionRows()
	counts := map[string]int{}
	var rows [][]template.HTML
	for _, s := range all {
		counts[s.Status]++
		if len(rows) < 30 {
			var choices []dashChoice
			for _, st := range suggestionStatuses {
				v := st.Key
				if v == "" {
					v = "open"
				}
				choices = append(choices, dashChoice{v, st.Label, st.Key == s.Key})
			}
			rows = append(rows, []template.HTML{
				dashText(template.HTMLEscapeString(s.Title)),
				dashText(fmt.Sprintf("+%d / −%d", s.Up, s.Down)),
				dashControls(dashControl{Section: "suggestions", Do: "suggestion", Name: s.ID, Choices: choices, Button: "Set"}),
			})
		}
	}
	var stats []dashStat
	for _, st := range suggestionStatuses {
		stats = append(stats, dashStat{st.Label, fmt.Sprint(counts[st.Label])})
	}
	return dashJoin(
		dashStats(stats...),
		dashHeading("By votes"),
		dashHTMLTable([]string{"Suggestion", "Votes", "Status"}, rows, "No suggestions yet."),
		dashNote("A new status updates the suggestion's card, and Done or Declined closes its thread."),
	)
}

// Watch parties.

// dashEvents caches the server's scheduled events for a minute, so the
// page does not ask Discord on every refresh.
var dashEvents struct {
	sync.Mutex
	at   time.Time
	list []*discordgo.GuildScheduledEvent
	err  error
}

// fetchDashEvents asks Discord for the server's scheduled events.
var fetchDashEvents = func(b *bot) ([]*discordgo.GuildScheduledEvent, error) {
	if b.s == nil {
		return nil, fmt.Errorf("not connected")
	}
	return b.s.GuildScheduledEvents(b.cfg.GuildID, true)
}

func (b *bot) scheduledEvents() ([]*discordgo.GuildScheduledEvent, error) {
	dashEvents.Lock()
	defer dashEvents.Unlock()
	if time.Since(dashEvents.at) > time.Minute {
		dashEvents.list, dashEvents.err = fetchDashEvents(b)
		dashEvents.at = time.Now()
	}
	return dashEvents.list, dashEvents.err
}

func (b *bot) dashParties() template.HTML {
	events, err := b.scheduledEvents()
	if err != nil {
		return dashNote("Could not load events from Discord right now.")
	}
	var up []*discordgo.GuildScheduledEvent
	for _, ev := range events {
		if ev.Status == discordgo.GuildScheduledEventStatusScheduled || ev.Status == discordgo.GuildScheduledEventStatusActive {
			up = append(up, ev)
		}
	}
	sort.Slice(up, func(i, j int) bool { return up[i].ScheduledStartTime.Before(up[j].ScheduledStartTime) })
	var rows [][]template.HTML
	for _, ev := range up {
		when := "live now"
		if ev.Status == discordgo.GuildScheduledEventStatusScheduled {
			when = ev.ScheduledStartTime.UTC().Format("Mon 2 Jan 15:04 UTC")
			if d := time.Until(ev.ScheduledStartTime); d > 0 {
				when += " (in " + dashAgo(d) + ")"
			}
		}
		host := "—"
		if id := partyHost(ev); id != "" {
			host = b.dashName(id)
		}
		rows = append(rows, []template.HTML{
			dashText("<b>" + template.HTMLEscapeString(ev.Name) + "</b><br><span class=\"note\">" + template.HTMLEscapeString(when) + "</span>"),
			dashText(template.HTMLEscapeString(host)), dashText(fmt.Sprint(ev.UserCount)),
			dashControls(dashControl{Section: "watch-parties", Do: "party-cancel", Name: ev.ID, Button: "Cancel", Confirm: "sure", Quiet: true}),
		})
	}
	return dashJoin(
		dashStats(dashStat{"Upcoming", fmt.Sprint(len(up))}),
		dashHTMLTable([]string{"Event", "Host", "Interested", ""}, rows, "No watch parties planned. Start one with /watchparty."),
		dashNote("Cancel deletes the event and its voice channel, and DMs members who said they're going."),
	)
}

// Moderation.

type warnCount struct {
	User          string
	Recent, Total int
	Last          time.Time
}

// warningCounts counts each member's warnings, and those since a time.
func (st *store) warningCounts(since time.Time) []warnCount {
	var out []warnCount
	st.view(func(d *storeData) {
		for user, ws := range d.Warnings {
			c := warnCount{User: user, Total: len(ws)}
			for _, w := range ws {
				if w.At.After(since) {
					c.Recent++
				}
				if w.At.After(c.Last) {
					c.Last = w.At
				}
			}
			if c.Total > 0 {
				out = append(out, c)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Recent != out[j].Recent {
			return out[i].Recent > out[j].Recent
		}
		return out[i].Last.After(out[j].Last)
	})
	return out
}

func (b *bot) dashModeration() template.HTML {
	counts := b.store.warningCounts(time.Now().Add(-30 * 24 * time.Hour))
	recent := 0
	var rows [][]template.HTML
	for _, c := range counts {
		recent += c.Recent
		if len(rows) < 30 {
			rows = append(rows, []template.HTML{
				dashText("<b>" + template.HTMLEscapeString(b.dashName(c.User)) + "</b><br><span class=\"note\">latest " +
					template.HTMLEscapeString(dashAgo(time.Since(c.Last))) + " ago</span>"),
				dashText(fmt.Sprintf("%d · %d", c.Recent, c.Total)),
				dashControls(dashControl{Section: "moderation", Do: "clear-warnings", Name: c.User, Button: "Clear", Confirm: "sure", Quiet: true}),
			})
		}
	}
	return dashJoin(
		dashStats(dashStat{"Warnings, last 30 days", fmt.Sprint(recent)}, dashStat{"Members ever warned", fmt.Sprint(len(counts))}),
		dashHTMLTable([]string{"Member", "30 days · all", "Clear all"}, rows, "No warnings. Nice and calm."),
		dashNote("Reasons and messages stay in Discord; use /warnings there."),
	)
}

// AniList.

// anilistCounts counts linked AniList accounts and how many were proven
// by signing in.
func (st *store) anilistCounts() (linked, verified int) {
	st.view(func(d *storeData) {
		for _, l := range d.AniList {
			linked++
			if l.Verified {
				verified++
			}
		}
	})
	return linked, verified
}

func (b *bot) dashAniList() template.HTML {
	linked, verified := b.store.anilistCounts()
	return dashStats(
		dashStat{"Linked members", fmt.Sprint(linked)},
		dashStat{"Signed in", fmt.Sprint(verified)},
		dashStat{"By username", fmt.Sprint(linked - verified)},
	)
}
