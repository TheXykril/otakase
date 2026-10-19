package internal

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Source reports: a linked app tells the Otakase Discord bot when a source
// fails here (a search error, no episode links, a stream that won't start),
// and now and then that one works, so the server's source check can say
// "failing for 4 members" and tell a broken source from an unused one. Only
// the source's name, whether it worked, the kind of step and a short cleaned
// error are sent; nothing about the show.
//
// A source's failure is sent once per run (again after it worked in
// between, up to three times), a success at most once an hour.

const (
	providerOKEvery      = time.Hour
	maxProviderFailSends = 3
	providerErrorMax     = 160
)

// providerReportState is what this run has sent about one source.
type providerReportState struct {
	lastOK    bool // the last report sent said it worked
	sent      bool // anything was sent
	failSends int
	okAt      time.Time
}

var providerReports = struct {
	sync.Mutex
	m map[string]*providerReportState
}{m: map[string]*providerReportState{}}

// sendProviderReport posts one report; tests replace it.
var sendProviderReport = func(config *Config, token string, body providerReportBody) {
	ctx, cancel := context.WithTimeout(context.Background(), discordRequestTimeout)
	defer cancel()
	if _, err := discordPostJSON(ctx, config, "/v1/provider-report", token, body, nil); err != nil {
		Log(fmt.Sprintf("Discord: source report for %s: %v", body.Provider, err))
	}
}

type providerReportBody struct {
	Provider string `json:"provider"`
	OK       bool   `json:"ok"`
	Kind     string `json:"kind"`
	Error    string `json:"error,omitempty"`
}

// providerReportDue reports whether a result is worth sending, and notes
// it as sent when it is.
func providerReportDue(st *providerReportState, ok bool, now time.Time) bool {
	switch {
	case ok && st.sent && st.lastOK && now.Sub(st.okAt) < providerOKEvery:
		return false
	case !ok && st.sent && !st.lastOK:
		return false
	case !ok && st.failSends >= maxProviderFailSends:
		return false
	}
	st.sent, st.lastOK = true, ok
	if ok {
		st.okAt = now
	} else {
		st.failSends++
	}
	return true
}

var (
	reportURLRe   = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://\S+`)
	reportQuoteRe = regexp.MustCompile(`"[^"]*"|'[^']*'`)
)

// cleanProviderError keeps an error short and free of anything that would
// say what was watched or where from: links and quoted text (queries,
// titles) are taken out.
func cleanProviderError(err error) string {
	if err == nil {
		return ""
	}
	s := reportQuoteRe.ReplaceAllString(err.Error(), "…")
	s = reportURLRe.ReplaceAllString(s, "<url>")
	s = strings.Join(strings.Fields(s), " ")
	return clipRunes(s, providerErrorMax)
}

// noteProviderResult records how a source did at one step ("search",
// "episode", "playback") and tells the bot when it is due. err nil means it
// worked. A source answering "no results" works; a skipped one (cooling
// down, switched off) says nothing new and is left out.
func noteProviderResult(provider, kind string, err error) {
	provider = normalizeProviderName(provider)
	config := GetGlobalConfig()
	if provider == "" || !discordLinked(config) {
		return
	}
	ok := err == nil
	if err != nil {
		var cooling *errProviderCooling
		if errors.As(err, &cooling) {
			return
		}
		switch (providerFailure{provider: provider, err: err}).kind() {
		case failureDisabled:
			return
		case failureNoResults:
			ok = true
		}
	}
	providerReports.Lock()
	st := providerReports.m[provider]
	if st == nil {
		st = &providerReportState{}
		providerReports.m[provider] = st
	}
	due := providerReportDue(st, ok, time.Now())
	providerReports.Unlock()
	if !due {
		return
	}
	body := providerReportBody{Provider: provider, OK: ok, Kind: kind}
	if !ok {
		body.Error = cleanProviderError(err)
	}
	token := loadDiscordLink(config).Token
	go sendProviderReport(config, token, body)
}
