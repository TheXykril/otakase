package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLinkedRoleValues(t *testing.T) {
	if got := linkedRoleValues(appStats{Seconds: 3*3600 + 3599}, true); got != (linkedRoleMeta{Hours: 3, App: true}) {
		t.Errorf("values: %+v", got)
	}
	m := roleConnectionMetadata(linkedRoleMeta{Hours: 42})
	if m["hours_watched"] != "42" || m["app_linked"] != "0" || len(m) != 2 {
		t.Errorf("metadata: %v", m)
	}
	for _, f := range linkedRoleFields {
		if m := roleConnectionMetadata(linkedRoleMeta{App: true}); m[f.Key] == "" {
			t.Errorf("field %s has no value", f.Key)
		}
	}
	if !sameFields(append([]roleConnectionField(nil), linkedRoleFields...), linkedRoleFields) {
		t.Error("same fields differ")
	}
	changed := append([]roleConnectionField(nil), linkedRoleFields...)
	changed[0].Name = "Hours"
	if sameFields(changed, linkedRoleFields) || sameFields(nil, linkedRoleFields) {
		t.Error("changed fields look the same")
	}
}

func TestLinkedRoleDue(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	l := linkedRole{Pushed: linkedRoleMeta{Hours: 3, App: true}, PushedAt: now.Add(-10 * time.Minute)}
	cases := []struct {
		want linkedRoleMeta
		at   time.Time
		due  bool
	}{
		{linkedRoleMeta{Hours: 3, App: true}, now.Add(5 * time.Hour), false}, // nothing changed
		{linkedRoleMeta{Hours: 4, App: true}, now, false},                    // pushed 10 min ago
		{linkedRoleMeta{Hours: 4, App: true}, now.Add(time.Hour), true},
		{linkedRoleMeta{Hours: 3, App: false}, now, true}, // unlinked: at once
	}
	for k, c := range cases {
		if got := linkedRoleDue(l, c.want, c.at); got != c.due {
			t.Errorf("case %d: due %v, want %v", k, got, c.due)
		}
	}
}

func TestLinkedRoleStates(t *testing.T) {
	s := &oauthStates{}
	now := time.Now()
	a := s.add(now)
	if len(a) != 32 || s.add(now) == a {
		t.Errorf("state %q", a)
	}
	if !s.take(a, now.Add(time.Minute)) || s.take(a, now) {
		t.Error("a state works exactly once")
	}
	old := s.add(now)
	if s.take(old, now.Add(linkedRoleStateTTL+time.Second)) {
		t.Error("expired state accepted")
	}
	if s.take("", now) || s.take("nope", now) {
		t.Error("unknown state accepted")
	}
	// Expired states are pruned on the next add.
	s.add(now)
	s.add(now.Add(time.Hour))
	if len(s.m) != 1 {
		t.Errorf("kept %d states", len(s.m))
	}
}

func TestLinkedRoleAuthURL(t *testing.T) {
	u, err := url.Parse(linkedRoleAuthURL("123", "https://bot.example/v1/linked-role/callback", "st"))
	if err != nil || u.Host != "discord.com" || u.Path != "/oauth2/authorize" {
		t.Fatalf("auth url: %v %v", u, err)
	}
	q := u.Query()
	if q.Get("client_id") != "123" || q.Get("redirect_uri") != "https://bot.example/v1/linked-role/callback" ||
		q.Get("response_type") != "code" || q.Get("scope") != "role_connections.write identify" || q.Get("state") != "st" {
		t.Errorf("query: %v", q)
	}
	t.Setenv("OTAKASE_PUBLIC_URL", "https://example.org/")
	if linkedRoleRedirect() != "https://example.org/v1/linked-role/callback" {
		t.Errorf("redirect %s", linkedRoleRedirect())
	}
	t.Setenv("OTAKASE_PUBLIC_URL", "")
	if linkedRoleRedirect() != "https://bot.xyverion.com/v1/linked-role/callback" {
		t.Errorf("default redirect %s", linkedRoleRedirect())
	}
}

// fakeDiscord answers the OAuth2 token, /users/@me, role connection and
// metadata endpoints.
type fakeDiscord struct {
	mu       sync.Mutex
	access   map[string]bool // access tokens that work
	refresh  map[string]bool // refresh tokens that work
	issued   int
	pushes   map[string]string // access token → body pushed
	fields   []roleConnectionField
	fieldPut int
	botAuth  string
}

func newFakeDiscord(t *testing.T) *fakeDiscord {
	t.Helper()
	f := &fakeDiscord{access: map[string]bool{}, refresh: map[string]bool{}, pushes: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	old := discordAPI
	discordAPI = srv.URL
	oldID, oldMember := linkedRoleClientID, linkedRoleMember
	linkedRoleClientID = func(*bot) string { return "app1" }
	linkedRoleMember = func(_ *bot, user string) (string, bool) { return "Xy", user != "stranger" }
	t.Cleanup(func() { discordAPI, linkedRoleClientID, linkedRoleMember = old, oldID, oldMember })
	return f
}

func (f *fakeDiscord) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case r.URL.Path == "/oauth2/token":
		r.ParseForm()
		if r.Form.Get("client_id") != "app1" || r.Form.Get("client_secret") != "sec" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ok := false
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			ok = r.Form.Get("code") == "good" && r.Form.Get("redirect_uri") == linkedRoleRedirect()
		case "refresh_token":
			ok = f.refresh[r.Form.Get("refresh_token")]
			delete(f.refresh, r.Form.Get("refresh_token"))
		}
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		f.issued++
		a, rt := "a"+string(rune('0'+f.issued)), "r"+string(rune('0'+f.issued))
		f.access[a], f.refresh[rt] = true, true
		json.NewEncoder(w).Encode(map[string]any{"access_token": a, "refresh_token": rt, "expires_in": 604800, "scope": linkedRoleScope})
	case r.URL.Path == "/users/@me" && f.access[bearer]:
		w.Write([]byte(`{"id":"u1","username":"xy"}`))
	case r.URL.Path == "/users/@me/applications/app1/role-connection" && r.Method == "PUT":
		if !f.access[bearer] {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Name     string            `json:"platform_name"`
			User     string            `json:"platform_username"`
			Metadata map[string]string `json:"metadata"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.pushes[bearer] = body.Name + "|" + body.User + "|" + body.Metadata["hours_watched"] + "|" + body.Metadata["app_linked"]
		w.Write([]byte(`{}`))
	case r.URL.Path == "/applications/app1/role-connections/metadata":
		f.botAuth = r.Header.Get("Authorization")
		if r.Method == "PUT" {
			f.fieldPut++
			json.NewDecoder(r.Body).Decode(&f.fields)
		}
		json.NewEncoder(w).Encode(f.fields)
	default:
		w.WriteHeader(http.StatusUnauthorized)
	}
}

func TestLinkedRoleFieldsRegistered(t *testing.T) {
	f := newFakeDiscord(t)
	if err := putLinkedRoleFields("app1", "bt"); err != nil {
		t.Fatal(err)
	}
	if f.fieldPut != 1 || f.botAuth != "Bot bt" || !sameFields(f.fields, linkedRoleFields) {
		t.Fatalf("put %d auth %q fields %+v", f.fieldPut, f.botAuth, f.fields)
	}
	if err := putLinkedRoleFields("app1", "bt"); err != nil || f.fieldPut != 1 {
		t.Errorf("registered again: %d %v", f.fieldPut, err)
	}
}

func TestLinkedRoleRefresh(t *testing.T) {
	f := newFakeDiscord(t)
	b := appTestBot(t)
	b.cfg.DiscordSecret = "sec"
	now := time.Now()
	f.refresh["r0"] = true

	// An expired token is refreshed before the push.
	l := linkedRole{AccessToken: "old", RefreshToken: "r0", Expires: now.Add(-time.Hour)}
	l, err := b.pushLinkedRole("u1", l, linkedRoleMeta{Hours: 5, App: true}, now)
	if err != nil || l.AccessToken != "a1" || l.RefreshToken != "r1" || !l.Expires.After(now.Add(6*24*time.Hour)) {
		t.Fatalf("refresh: %+v %v", l, err)
	}
	if f.pushes["a1"] != "Otakase|Xy|5|1" || l.Pushed != (linkedRoleMeta{Hours: 5, App: true}) || !l.PushedAt.Equal(now) {
		t.Errorf("push %q, %+v", f.pushes["a1"], l)
	}

	// A token Discord turns down before it expires is refreshed once.
	f.access["a1"] = false
	l, err = b.pushLinkedRole("u1", l, linkedRoleMeta{Hours: 6, App: true}, now)
	if err != nil || l.AccessToken != "a2" || f.pushes["a2"] != "Otakase|Xy|6|1" {
		t.Errorf("retry: %+v %v", l, err)
	}

	// A revoked sign-in drops the stored tokens.
	b.store.update(func(d *storeData) {
		d.LinkedRoles = map[string]linkedRole{
			"u1": {AccessToken: "x", RefreshToken: "dead", Expires: now.Add(-time.Hour), PushedAt: now.Add(-2 * time.Hour)},
			"u2": l,
		}
		d.AppStats["u1"] = appStats{Seconds: 7200}
		d.AppStats["u2"] = appStats{Seconds: 9 * 3600}
		d.AppLinks["h"] = appLink{User: "u2"}
	})
	b.pushLinkedRoles(now.Add(2 * time.Hour))
	var got map[string]linkedRole
	b.store.view(func(d *storeData) { got = d.LinkedRoles })
	if _, ok := got["u1"]; ok || got["u2"].Pushed != (linkedRoleMeta{Hours: 9, App: true}) {
		t.Errorf("after loop: %+v", got)
	}

	// Unlinking every app pushes app_linked=0 at the next tick.
	b.store.update(func(d *storeData) { delete(d.AppLinks, "h") })
	b.pushLinkedRoles(now.Add(2*time.Hour + 5*time.Minute))
	if f.pushes["a2"] != "Otakase|Xy|9|0" {
		t.Errorf("unlinked push %q", f.pushes["a2"])
	}
}

func TestLinkedRoleSignIn(t *testing.T) {
	f := newFakeDiscord(t)
	b := appTestBot(t)
	linkedRoleLimit = newRateLimit(20, time.Minute)
	linkedRoleStates = &oauthStates{}
	b.cfg.DiscordSecret = "sec"
	b.store.update(func(d *storeData) {
		d.AppStats["u1"] = appStats{Seconds: 50 * 3600}
		d.AppLinks["h"] = appLink{User: "u1"}
	})

	w := apiDo(t, b, "GET", "/v1/linked-role", nil, nil)
	if w.Code != http.StatusFound {
		t.Fatalf("start: %d", w.Code)
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	state := loc.Query().Get("state")
	cookie := w.Result().Cookies()[0]
	if cookie.Name != linkedRoleCookie || cookie.Value != state || !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("cookie %+v state %q", cookie, state)
	}

	// The state must come with its cookie.
	w = apiDo(t, b, "GET", "/v1/linked-role/callback?code=good&state="+state, nil, map[string]string{"Cookie": linkedRoleCookie + "=other"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("wrong cookie: %d", w.Code)
	}
	// That used the state up: start again.
	w = apiDo(t, b, "GET", "/v1/linked-role", nil, nil)
	loc, _ = url.Parse(w.Header().Get("Location"))
	state = loc.Query().Get("state")
	w = apiDo(t, b, "GET", "/v1/linked-role/callback?code=good&state="+state, nil, map[string]string{"Cookie": linkedRoleCookie + "=" + state})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Go back to Discord") || !strings.Contains(w.Body.String(), "50 hours") {
		t.Fatalf("callback: %d %s", w.Code, w.Body)
	}
	var l linkedRole
	b.store.view(func(d *storeData) { l = d.LinkedRoles["u1"] })
	if l.AccessToken != "a1" || l.RefreshToken != "r1" || l.Name != "Xy" || f.pushes["a1"] != "Otakase|Xy|50|1" {
		t.Errorf("stored %+v, pushed %q", l, f.pushes["a1"])
	}
	// Replayed callbacks are refused.
	w = apiDo(t, b, "GET", "/v1/linked-role/callback?code=good&state="+state, nil, map[string]string{"Cookie": linkedRoleCookie + "=" + state})
	if w.Code != http.StatusBadRequest {
		t.Errorf("replay: %d", w.Code)
	}
}
