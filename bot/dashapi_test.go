package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func apiPost(t *testing.T, h http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/do", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("X-Otakase-Token", token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDashboardAPI(t *testing.T) {
	b := dashTestBot(t)
	h := b.dashboardHandler(false)

	w := dashGet(t, h, "/api/summary", "127.0.0.1:1", nil)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("summary: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	var s apiSummary
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Token != dashToken || s.Follows != 2 || s.Safety.Warnings30 != 1 || s.Support.Suggestions != 2 || len(s.Settings) == 0 {
		t.Errorf("summary: %+v", s)
	}
	for _, list := range []string{`"jobs":[]`, `"airing":[]`, `"parties":[]`, `"rooms":[]`, `"rounds":[]`, `"levels":[]`} {
		if !strings.Contains(w.Body.String(), list) {
			t.Errorf("missing empty list %s", list)
		}
	}

	if w := apiPost(t, h, "", `{"do":"spoiler","v":"hide"}`); w.Code != http.StatusForbidden {
		t.Fatalf("no token: %d", w.Code)
	}
	if b.spoilerMode() == spoilerHide {
		t.Fatal("refused call changed the guard")
	}
	w = apiPost(t, h, dashToken, `{"do":"spoiler","v":"hide"}`)
	var res struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || !res.OK || b.spoilerMode() != spoilerHide {
		t.Fatalf("spoiler: %d %s", w.Code, w.Body)
	}
	w = apiPost(t, h, dashToken, `{"do":"setting","name":"airing_hour_utc","v":"99"}`)
	if json.Unmarshal(w.Body.Bytes(), &res); res.OK || res.Message == "" {
		t.Errorf("bad value: %s", w.Body)
	}
	if w := dashGet(t, h, "/api/nope", "127.0.0.1:1", nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown path: %d", w.Code)
	}
	if w := dashGet(t, h, "/api/summary", "8.8.8.8:1", nil); w.Code == http.StatusOK {
		t.Skip("handler without ingress-only")
	}
}
