package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The Otakase account server (a Cloudflare Worker) now carries the app's
// link to Discord. It passes the app's calls on with a shared secret, the
// member's Discord id and the device; the bot sends inbox items, Tester
// role changes and queued support posts back the other way. Apps linked
// the old way keep working until the old endpoints go, one release later.
//
// Each device of an account shows up here as a link with the key
// "account:<device id>", so heartbeats, stats and support posts treat it
// like any linked app.

const accountKeyPrefix = "account:"

var discordID = regexp.MustCompile(`^\d{5,24}$`)

// accountCaller reads a call the account server passed on.
func (b *bot) accountCaller(r *http.Request) (user, key string, ok bool) {
	secret, got := b.cfg.AccountSecret, r.Header.Get("X-Otakase-Account")
	if secret == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
		return "", "", false
	}
	user, device := r.Header.Get("X-Discord-User"), r.Header.Get("X-Otakase-Device")
	if !discordID.MatchString(user) || device == "" || len(device) > 64 {
		return "", "", false
	}
	return user, accountKeyPrefix + device, true
}

// callerKey is the link a call came from: the account device, or the hash
// of an old link token.
func (b *bot) callerKey(r *http.Request) string {
	if _, key, ok := b.accountCaller(r); ok {
		return key
	}
	return hashToken(bearerToken(r))
}

// accountLink notes a device of an account as a linked app.
func (b *bot) accountLink(user, key string) {
	now := appNow()
	b.store.touch(func(d *storeData) {
		l, ok := d.AppLinks[key]
		if ok && l.User == user {
			return
		}
		if d.AppLinks == nil {
			d.AppLinks = map[string]appLink{}
		}
		d.AppLinks[key] = appLink{User: user, Created: now}
	})
}

// hasAccount says whether a member uses the app through an account.
func (b *bot) hasAccount(user string) bool {
	found := false
	b.store.view(func(d *storeData) {
		for h, l := range d.AppLinks {
			if l.User == user && strings.HasPrefix(h, accountKeyPrefix) {
				found = true
				return
			}
		}
	})
	return found
}

var accountClient = &http.Client{Timeout: 15 * time.Second}

// accountCall calls the account server as the bot.
func (b *bot) accountCall(method, path string, body, out any) error {
	if b.cfg.AccountSecret == "" {
		return errors.New("no account server")
	}
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, b.cfg.AccountURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.cfg.AccountSecret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := accountClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("account server: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// sendToAccount puts an item in the inbox of a member's account, which
// hands it to whichever device is open. It says whether an account took it.
func (b *bot) sendToAccount(user string, it inboxItem) bool {
	var r struct {
		Account bool `json:"account"`
	}
	if err := b.accountCall("POST", "/bot/inbox", map[string]any{"discord": user, "item": it}, &r); err != nil {
		log.Printf("account inbox: %v", err)
		return false
	}
	return r.Account
}

// accountRole keeps the account's role in step with the Tester role.
func (b *bot) accountRole(user string, tester bool) {
	if b.cfg.AccountSecret == "" {
		return
	}
	role := "user"
	if tester {
		role = "tester"
	}
	if err := b.accountCall("POST", "/bot/role", map[string]string{"discord": user, "role": role}, nil); err != nil {
		log.Printf("account role: %v", err)
	}
}

// ---- Support posts queued while the bot was offline -----------------------

type outboxItem struct {
	ID          int64  `json:"id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Discord     string `json:"discord"`
	Device      string `json:"device"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

// outboxPaths are the calls the account server may queue.
var outboxPaths = map[string]bool{"/v1/report": true, "/v1/provider-report": true}

func init() {
	registerJob("Account outbox", (*bot).outboxLoop)
	registerJob("Cloudflare limits", (*bot).usageLoop)
}

func (b *bot) outboxLoop() {
	if b.cfg.AccountSecret == "" {
		select {}
	}
	for {
		if err := b.drainOutbox(); err != nil {
			log.Printf("account outbox: %v", err)
		}
		time.Sleep(time.Minute)
	}
}

// drainOutbox runs the queued calls as if the account server had passed
// them on just now, and removes them from the queue.
func (b *bot) drainOutbox() error {
	var box struct {
		Items []outboxItem `json:"items"`
	}
	if err := b.accountCall("GET", "/bot/outbox", nil, &box); err != nil {
		return err
	}
	if len(box.Items) == 0 {
		return nil
	}
	done := make([]string, 0, len(box.Items))
	for _, it := range box.Items {
		code := b.replay(it)
		// A call the bot can't take now stays queued; one it refused is gone.
		if code >= 500 {
			log.Printf("account outbox: %s %s answered %d, kept", it.Method, it.Path, code)
			continue
		}
		done = append(done, strconv.FormatInt(it.ID, 10))
	}
	if len(done) == 0 {
		return nil
	}
	return b.accountCall("DELETE", "/bot/outbox?ids="+strings.Join(done, ","), nil, nil)
}

func (b *bot) replay(it outboxItem) int {
	if !outboxPaths[it.Path] || it.Method != http.MethodPost {
		return http.StatusBadRequest
	}
	body, err := base64.StdEncoding.DecodeString(it.Body)
	if err != nil {
		return http.StatusBadRequest
	}
	h, ok := apiRoutes["POST "+it.Path]
	if !ok {
		return http.StatusNotFound
	}
	req := httptest.NewRequest(http.MethodPost, it.Path, bytes.NewReader(body))
	req.Header.Set("Content-Type", it.ContentType)
	req.Header.Set("X-Otakase-Account", b.cfg.AccountSecret)
	req.Header.Set("X-Discord-User", it.Discord)
	req.Header.Set("X-Otakase-Device", it.Device)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec.Code
}

// ---- Free plan limits ------------------------------------------------------

// Cloudflare's free plan limits per day that the account server can reach.
var freeLimits = []struct {
	name  string
	limit float64
}{
	{"Worker requests", 100_000},
	{"Durable Object requests", 100_000},
	{"D1 rows read", 5_000_000},
	{"D1 rows written", 100_000},
}

const usageWarnAt = 0.7

var usageWarned = struct {
	sync.Mutex
	day  string
	seen map[string]bool
}{seen: map[string]bool{}}

func (b *bot) usageLoop() {
	if b.cfg.CloudflareToken == "" || b.cfg.CloudflareAccount == "" {
		select {}
	}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		used, err := cloudflareUsage(ctx, cloudflareGraphQL, b.cfg.CloudflareToken, b.cfg.CloudflareAccount, time.Now().UTC())
		cancel()
		if err != nil {
			log.Printf("cloudflare usage: %v", err)
		} else if e := usageWarning(used, time.Now().UTC()); e != nil {
			b.staffNote(e)
		}
		time.Sleep(time.Hour)
	}
}

// usageWarning is the note for limits at 70% or more today, each said once
// a day; nil when there is nothing new to say.
func usageWarning(used map[string]float64, now time.Time) *discordgo.MessageEmbed {
	usageWarned.Lock()
	defer usageWarned.Unlock()
	day := now.Format(time.DateOnly)
	if usageWarned.day != day {
		usageWarned.day, usageWarned.seen = day, map[string]bool{}
	}
	var lines []string
	for _, l := range freeLimits {
		n := used[l.name]
		if n < l.limit*usageWarnAt || usageWarned.seen[l.name] {
			continue
		}
		usageWarned.seen[l.name] = true
		lines = append(lines, fmt.Sprintf("**%s**: %s of %s today (%d%%)", l.name,
			strconv.FormatFloat(n, 'f', 0, 64), strconv.FormatFloat(l.limit, 'f', 0, 64), int(n*100/l.limit)))
	}
	if len(lines) == 0 {
		return nil
	}
	return &discordgo.MessageEmbed{Title: "Account server near a free limit", Color: shu,
		Description: strings.Join(lines, "\n") + "\n\nThe counts start again at 00:00 UTC. Past the limit, sign-in and sync stop until then."}
}

var cloudflareGraphQL = "https://api.cloudflare.com/client/v4/graphql"

const usageQuery = `query($account: String!, $start: Time!, $end: Time!, $day: Date!) {
  viewer { accounts(filter: {accountTag: $account}) {
    workers: workersInvocationsAdaptive(limit: 100, filter: {datetime_geq: $start, datetime_lt: $end}) { sum { requests } }
    objects: durableObjectsInvocationsAdaptiveGroups(limit: 100, filter: {date: $day}) { sum { requests } }
    d1: d1AnalyticsAdaptiveGroups(limit: 100, filter: {date: $day}) { sum { rowsRead rowsWritten } }
  } }
}`

// cloudflareUsage asks Cloudflare's analytics how much of each free limit
// the account has used today (UTC).
func cloudflareUsage(ctx context.Context, endpoint, token, account string, now time.Time) (map[string]float64, error) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	buf, _ := json.Marshal(map[string]any{"query": usageQuery, "variables": map[string]string{
		"account": account, "start": start.Format(time.RFC3339), "end": now.Format(time.RFC3339), "day": start.Format(time.DateOnly),
	}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	type sums = []struct {
		Sum map[string]float64 `json:"sum"`
	}
	var r struct {
		Data struct {
			Viewer struct {
				Accounts []struct {
					Workers sums `json:"workers"`
					Objects sums `json:"objects"`
					D1      sums `json:"d1"`
				} `json:"accounts"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("%s: %w", resp.Status, err)
	}
	if len(r.Errors) > 0 {
		return nil, errors.New(r.Errors[0].Message)
	}
	if len(r.Data.Viewer.Accounts) == 0 {
		return nil, errors.New("no such account")
	}
	a := r.Data.Viewer.Accounts[0]
	total := func(groups sums, key string) float64 {
		n := 0.0
		for _, g := range groups {
			n += g.Sum[key]
		}
		return n
	}
	return map[string]float64{
		"Worker requests":         total(a.Workers, "requests"),
		"Durable Object requests": total(a.Objects, "requests"),
		"D1 rows read":            total(a.D1, "rowsRead"),
		"D1 rows written":         total(a.D1, "rowsWritten"),
	}, nil
}
