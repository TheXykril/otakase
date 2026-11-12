package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func accountHeaders(secret, user, device string) map[string]string {
	return map[string]string{"X-Otakase-Account": secret, "X-Discord-User": user, "X-Otakase-Device": device}
}

func TestAccountCallsActAsTheMember(t *testing.T) {
	b := appTestBot(t)
	inboxLimit = newRateLimit(30, time.Minute)
	b.cfg.AccountSecret = "shared-secret"
	if w := apiDo(t, b, http.MethodGet, "/v1/inbox", nil, accountHeaders("wrong", "222222222222", "dev-1")); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret: %d", w.Code)
	}
	if w := apiDo(t, b, http.MethodGet, "/v1/inbox", nil, accountHeaders("shared-secret", "not-an-id", "dev-1")); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad user: %d", w.Code)
	}
	if w := apiDo(t, b, http.MethodGet, "/v1/inbox", nil, accountHeaders("shared-secret", "222222222222", "dev-1")); w.Code != http.StatusOK {
		t.Fatalf("account call: %d %s", w.Code, w.Body)
	}
	b.store.view(func(d *storeData) {
		if d.AppLinks["account:dev-1"].User != "222222222222" {
			t.Errorf("device not noted as a linked app: %v", d.AppLinks)
		}
	})
	if !b.hasApp("222222222222") || !b.hasAccount("222222222222") {
		t.Fatal("member should count as having the app through an account")
	}
	// Without a secret set, the headers count for nothing.
	b.cfg.AccountSecret = ""
	if w := apiDo(t, b, http.MethodGet, "/v1/inbox", nil, accountHeaders("", "222222222222", "dev-1")); w.Code != http.StatusUnauthorized {
		t.Fatalf("no secret: %d", w.Code)
	}
}

// fakeAccountServer records what the bot sends to the account server.
type fakeAccountServer struct {
	sync.Mutex
	inbox   []map[string]any
	roles   []map[string]any
	outbox  []outboxItem
	deleted string
}

func (f *fakeAccountServer) start(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.Lock()
		defer f.Unlock()
		if r.Header.Get("Authorization") != "Bearer shared-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if r.Body != nil {
			buf, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(buf, &body)
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /bot/inbox":
			f.inbox = append(f.inbox, body)
			writeJSON(w, http.StatusOK, map[string]any{"delivered": 1, "account": true})
		case "POST /bot/role":
			f.roles = append(f.roles, body)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "GET /bot/outbox":
			writeJSON(w, http.StatusOK, map[string]any{"items": f.outbox})
		case "DELETE /bot/outbox":
			f.deleted = r.URL.Query().Get("ids")
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestInboxItemsGoThroughTheAccount(t *testing.T) {
	b := appTestBot(t)
	f := &fakeAccountServer{}
	b.cfg.AccountURL, b.cfg.AccountSecret = f.start(t), "shared-secret"
	old := testToken(b, "u1")
	b.accountLink("u1", "account:dev-1")
	b.accountLink("u1", "account:dev-2")
	n := b.sendToApps("u1", inboxItem{Kind: "airing", Title: "Show", Episode: 3})
	if n != 2 {
		t.Fatalf("delivered to %d, want the old link and the account", n)
	}
	if len(f.inbox) != 1 || f.inbox[0]["discord"] != "u1" {
		t.Fatalf("account inbox: %v", f.inbox)
	}
	if item := f.inbox[0]["item"].(map[string]any); item["kind"] != "airing" || item["episode"].(float64) != 3 {
		t.Fatalf("item: %v", item)
	}
	if len(b.inboxOf(hashToken(old))) != 1 || len(b.inboxOf("account:dev-1")) != 0 {
		t.Fatal("only the old link keeps an inbox on the bot")
	}
	b.accountRole("u1", true)
	if len(f.roles) != 1 || f.roles[0]["role"] != "tester" {
		t.Fatalf("roles: %v", f.roles)
	}
}

func TestOutboxRunsQueuedCalls(t *testing.T) {
	b := appTestBot(t)
	f := &fakeAccountServer{}
	b.cfg.AccountURL, b.cfg.AccountSecret = f.start(t), "shared-secret"
	var seen []string
	apiRoutes["POST /v1/outbox-test"] = func(w http.ResponseWriter, r *http.Request) {
		user, err := b.appUser(r)
		buf, _ := io.ReadAll(r.Body)
		seen = append(seen, user+" "+r.Header.Get("Content-Type")+" "+string(buf))
		if err != nil || strings.Contains(string(buf), "later") {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	outboxPaths["/v1/outbox-test"] = true
	t.Cleanup(func() { delete(apiRoutes, "POST /v1/outbox-test"); delete(outboxPaths, "/v1/outbox-test") })
	f.outbox = []outboxItem{
		{ID: 1, Method: "POST", Path: "/v1/outbox-test", Discord: "222222222222", Device: "dev-1", ContentType: "application/json", Body: base64.StdEncoding.EncodeToString([]byte(`{"a":1}`))},
		{ID: 2, Method: "POST", Path: "/v1/outbox-test", Discord: "222222222222", Device: "dev-1", ContentType: "application/json", Body: base64.StdEncoding.EncodeToString([]byte(`later`))},
		// Only the support calls may be queued.
		{ID: 3, Method: "POST", Path: "/v1/link/start", Discord: "222222222222", Device: "dev-1", Body: ""},
	}
	if err := b.drainOutbox(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != `222222222222 application/json {"a":1}` {
		t.Fatalf("replayed: %v", seen)
	}
	if f.deleted != "1,3" {
		t.Fatalf("removed %q: the failed one stays queued", f.deleted)
	}
}

func TestCloudflareUsageWarnsOnceADayAt70Percent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Variables map[string]string `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		if r.Header.Get("Authorization") != "Bearer cf-token" || q.Variables["account"] != "acc" || q.Variables["day"] != "2026-11-12" {
			writeJSON(w, http.StatusOK, map[string]any{"errors": []map[string]string{{"message": "bad query"}}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{map[string]any{
			"workers": []any{map[string]any{"sum": map[string]any{"requests": 50000}}, map[string]any{"sum": map[string]any{"requests": 25000}}},
			"objects": []any{map[string]any{"sum": map[string]any{"requests": 1000}}},
			"d1":      []any{map[string]any{"sum": map[string]any{"rowsRead": 100, "rowsWritten": 90000}}},
		}}}}})
	}))
	t.Cleanup(srv.Close)
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	used, err := cloudflareUsage(context.Background(), srv.URL, "cf-token", "acc", now)
	if err != nil {
		t.Fatal(err)
	}
	if used["Worker requests"] != 75000 || used["D1 rows written"] != 90000 {
		t.Fatalf("used: %v", used)
	}
	usageWarned.day = ""
	e := usageWarning(used, now)
	if e == nil || !strings.Contains(e.Description, "Worker requests") || !strings.Contains(e.Description, "D1 rows written") ||
		strings.Contains(e.Description, "rows read") || !strings.Contains(e.Description, "(75%)") {
		t.Fatalf("warning: %+v", e)
	}
	if usageWarning(used, now.Add(time.Hour)) != nil {
		t.Fatal("said twice in one day")
	}
	if usageWarning(used, now.Add(24*time.Hour)) == nil {
		t.Fatal("a new day warns again")
	}
	if _, err := cloudflareUsage(context.Background(), srv.URL, "wrong", "acc", now); err == nil {
		t.Fatal("an error answer must fail")
	}
}
