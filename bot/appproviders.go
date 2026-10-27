package main

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Source reports from linked apps: when a source fails in someone's app (a
// search error, no episode links, a stream that won't start) the app says
// so, and now and then that one worked. /provider-status and the "is it
// down?" answer add what the apps saw lately, counted by member and never
// naming anyone. Kept in memory: after a restart the apps fill it again.
//
//	POST /v1/provider-report {"provider":"allanime","ok":false,"kind":"search","error":"…"} (auth) → {}

const (
	providerReportWindow = 3 * time.Hour
	maxProviderReports   = 5000
)

var providerReportLimit = newRateLimit(60, time.Hour)

// providerReport is the latest a member's apps said about one source.
type providerReport struct {
	OK    bool
	Kind  string
	Error string
	At    time.Time
}

// appProviderReports holds the latest report by source, then by member.
var appProviderReports = struct {
	sync.Mutex
	m map[string]map[string]providerReport
}{m: map[string]map[string]providerReport{}}

var (
	providerNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,31}$`)
	reportKindRe   = regexp.MustCompile(`^[a-z_]{1,20}$`)
)

func init() {
	apiHandle("POST /v1/provider-report", (*bot).apiProviderReport)
}

func (b *bot) apiProviderReport(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	var req struct {
		Provider string `json:"provider"`
		OK       bool   `json:"ok"`
		Kind     string `json:"kind"`
		Error    string `json:"error"`
	}
	if !readJSON(w, r, 4<<10, &req) {
		return
	}
	name := strings.ToLower(strings.TrimSpace(req.Provider))
	if !providerNameRe.MatchString(name) {
		apiError(w, http.StatusBadRequest, "bad provider")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if !reportKindRe.MatchString(kind) {
		kind = ""
	}
	if !providerReportLimit.allow(user, appNow()) {
		apiError(w, http.StatusTooManyRequests, "too many source reports, try again later")
		return
	}
	noteProviderReport(name, user, providerReport{OK: req.OK, Kind: kind, Error: clip(cleanText(req.Error, false), 200), At: appNow()})
	writeJSON(w, http.StatusOK, map[string]any{})
}

// noteProviderReport keeps a member's latest word on a source, dropping what
// is too old.
func noteProviderReport(provider, user string, rep providerReport) {
	appProviderReports.Lock()
	defer appProviderReports.Unlock()
	total := 0
	for p, byUser := range appProviderReports.m {
		for u, old := range byUser {
			if rep.At.Sub(old.At) > providerReportWindow {
				delete(byUser, u)
			}
		}
		if len(byUser) == 0 {
			delete(appProviderReports.m, p)
		}
		total += len(byUser)
	}
	if total >= maxProviderReports {
		appProviderReports.m = map[string]map[string]providerReport{}
	}
	if appProviderReports.m[provider] == nil {
		appProviderReports.m[provider] = map[string]providerReport{}
	}
	appProviderReports.m[provider][user] = rep
}

// providerTally is how many members' apps found a source failing or working.
type providerTally struct {
	Name           string
	Failing, Works int
	Recent         time.Time // the latest failure
}

// providerTallies counts the reports of the last window by source, failing
// ones first. Adult-only sources are left out unless adult is set, as in
// the source check.
func providerTallies(now time.Time, adult bool) []providerTally {
	appProviderReports.Lock()
	defer appProviderReports.Unlock()
	var list []providerTally
	for name, byUser := range appProviderReports.m {
		if _, ok := adultProviders[name]; ok && !adult {
			continue
		}
		t := providerTally{Name: name}
		for _, rep := range byUser {
			if now.Sub(rep.At) > providerReportWindow {
				continue
			}
			if rep.OK {
				t.Works++
				continue
			}
			t.Failing++
			if rep.At.After(t.Recent) {
				t.Recent = rep.At
			}
		}
		if t.Failing+t.Works > 0 {
			list = append(list, t)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Failing != list[j].Failing {
			return list[i].Failing > list[j].Failing
		}
		return list[i].Name < list[j].Name
	})
	return list
}

// providerReportLines are the apps' side for the source check: one line
// per source, by count only.
func providerReportLines(list []providerTally, now time.Time) []string {
	var lines []string
	for _, t := range list {
		name := providerDisplayName(t.Name)
		switch {
		case t.Failing > 0 && t.Works > 0:
			lines = append(lines, fmt.Sprintf("🟠 **%s** failing for %s, working for %d (latest %s)", name, dashPlural(t.Failing, "member"), t.Works, agoText(now.Sub(t.Recent))))
		case t.Failing > 0:
			lines = append(lines, fmt.Sprintf("🔴 **%s** failing for %s (latest %s)", name, dashPlural(t.Failing, "member"), agoText(now.Sub(t.Recent))))
		default:
			lines = append(lines, fmt.Sprintf("🟢 **%s** working for %s", name, dashPlural(t.Works, "member")))
		}
		if len(lines) == 10 {
			break
		}
	}
	return lines
}

// agoText is a rough "12 min ago".
func agoText(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
}

// withAppReports is a copy of the source check with what the apps reported
// added as a field; the cached check itself is left alone.
func withAppReports(e *discordgo.MessageEmbed, now time.Time, adult bool) *discordgo.MessageEmbed {
	lines := providerReportLines(providerTallies(now, adult), now)
	if e == nil || len(lines) == 0 {
		return e
	}
	c := *e
	c.Fields = append(append([]*discordgo.MessageEmbedField{}, e.Fields...), &discordgo.MessageEmbedField{
		Name:  "Apps report (last 3 hours)",
		Value: clip(strings.Join(lines, "\n"), 1024),
	})
	return &c
}
