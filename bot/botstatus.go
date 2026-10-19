package main

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /bot-status shows staff how the bot is doing: version, uptime, gateway
// latency, each background job, Ollama, the data file and memory.

var botStarted = time.Now()

// jobStatus is what a background job last reported.
type jobStatus struct {
	Started  time.Time
	Stopped  bool // its loop returned (turned off, or finished its work)
	LastOK   time.Time
	LastFail time.Time
	Err      string
}

func (h *health) job(name string) *jobStatus {
	if h.jobs == nil {
		h.jobs = map[string]*jobStatus{}
	}
	j := h.jobs[name]
	if j == nil {
		j = &jobStatus{}
		h.jobs[name] = j
	}
	return j
}

func (h *health) noteStart(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	j := h.job(name)
	j.Started, j.Stopped = h.now(), false
}

func (h *health) noteStop(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.job(name).Stopped = true
}

func (h *health) noteOK(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.job(name).LastOK = h.now()
}

func (h *health) noteFail(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	j := h.job(name)
	j.LastFail = h.now()
	if err != nil {
		j.Err = err.Error()
	}
}

// jobsSnapshot copies every job's status, by name.
func (h *health) jobsSnapshot() map[string]jobStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]jobStatus{}
	for name, j := range h.jobs {
		out[name] = *j
	}
	return out
}

// jobLine is one job on the status card.
func jobLine(name string, j jobStatus) string {
	var state string
	switch {
	case !j.LastFail.IsZero() && j.LastFail.After(j.LastOK):
		state = "**failing**"
	case !j.LastOK.IsZero():
		state = "ok"
	case j.Stopped:
		state = "off"
	default:
		state = "running, nothing to report yet"
	}
	line := fmt.Sprintf("**%s** · %s", name, state)
	var when []string
	if !j.LastOK.IsZero() {
		when = append(when, fmt.Sprintf("last ok <t:%d:R>", j.LastOK.Unix()))
	}
	if !j.LastFail.IsZero() {
		when = append(when, fmt.Sprintf("last failure <t:%d:R>", j.LastFail.Unix()))
	}
	if len(when) > 0 {
		line += "\n-# " + strings.Join(when, " · ")
	}
	if j.Err != "" && j.LastFail.After(j.LastOK) {
		line += "\n-# " + clip(strings.ReplaceAll(j.Err, "\n", " "), 160)
	}
	return line
}

// storeStats counts what the data file holds.
type storeStats struct {
	Follows, Followers, AniList, Suggestions, Rooms, Warnings, Warned int
}

func countStore(d *storeData) storeStats {
	s := storeStats{AniList: len(d.AniList), Suggestions: len(d.Suggestions), Rooms: len(d.Rooms), Warned: len(d.Warnings)}
	for _, l := range d.Follows {
		if len(l) > 0 {
			s.Followers++
			s.Follows += len(l)
		}
	}
	for _, l := range d.Warnings {
		s.Warnings += len(l)
	}
	return s
}

// fmtUptime is "3d 4h 12m", "4h 12m" or "12m".
func fmtUptime(d time.Duration) string {
	d = d.Round(time.Minute)
	days, hours, mins := int(d/(24*time.Hour)), int(d/time.Hour)%24, int(d/time.Minute)%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// fmtBytes is a size in B, KB or MB.
func fmtBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func (sm *semantic) isReady() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.ready
}

// ollamaState says whether Ollama answers and the matcher is ready.
func (b *bot) ollamaState() string {
	if b.cfg.OllamaURL == "" {
		return "off (no Ollama URL set)"
	}
	c := &http.Client{Timeout: 3 * time.Second}
	reach := "reachable"
	resp, err := c.Get(strings.TrimRight(b.cfg.OllamaURL, "/") + "/api/tags")
	if err != nil {
		reach = "**not reachable**"
	} else {
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			reach = "**answers with " + resp.Status + "**"
		}
	}
	ready := "matching not ready yet"
	if b.sem != nil && b.sem.isReady() {
		ready = "matching ready"
	}
	return fmt.Sprintf("%s · %s · threshold %s", reach, ready, formatFloat(b.settingFloat("match_threshold")))
}

func botStatusCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{Name: "bot-status", Description: "How the bot is doing: jobs, Ollama, data and memory", DefaultMemberPermissions: &permMod}
}

func (b *bot) botStatus(i *discordgo.InteractionCreate) {
	if !b.staffOnly(i) {
		return
	}
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		return b.statusEmbed(), nil
	})
}

func (b *bot) statusEmbed() *discordgo.MessageEmbed {
	jobs := b.health.jobsSnapshot()
	names := make([]string, 0, len(jobs))
	for n := range jobs {
		names = append(names, n)
	}
	sort.Strings(names)
	var lines []string
	for _, n := range names {
		lines = append(lines, jobLine(n, jobs[n]))
	}
	if len(lines) == 0 {
		lines = []string{"No jobs have reported yet."}
	}

	var st storeStats
	b.store.view(func(d *storeData) { st = countStore(d) })
	size := "not saved yet"
	if fi, err := os.Stat(b.store.path); err == nil {
		size = fmtBytes(fi.Size())
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	latency := "not measured yet"
	if l := b.s.HeartbeatLatency(); l > 0 {
		latency = fmt.Sprintf("%d ms", l.Milliseconds())
	}
	return &discordgo.MessageEmbed{
		Title: "Bot status",
		Color: shu,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Version", Value: botVersion(), Inline: true},
			{Name: "Uptime", Value: fmtUptime(time.Since(botStarted)), Inline: true},
			{Name: "Gateway latency", Value: latency, Inline: true},
			{Name: "Background jobs", Value: clip(strings.Join(lines, "\n"), 1024)},
			{Name: "Ollama", Value: b.ollamaState()},
			{Name: "Data file", Value: fmt.Sprintf("%s · %d follows by %d members · %d linked AniList · %d suggestions · %d rooms · %d warnings for %d members",
				size, st.Follows, st.Followers, st.AniList, st.Suggestions, st.Rooms, st.Warnings, st.Warned)},
			{Name: "Memory", Value: fmt.Sprintf("%s in use · %s from the system · %d goroutines", fmtBytes(int64(mem.HeapAlloc)), fmtBytes(int64(mem.Sys)), runtime.NumGoroutine())},
		},
		Footer:    &discordgo.MessageEmbedFooter{Text: "Started"},
		Timestamp: botStarted.Format(time.RFC3339),
	}
}
