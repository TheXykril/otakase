package internal

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func postCallbackToken(h http.Handler, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAniListCallbackPageCarriesTheNonce(t *testing.T) {
	h := anilistCallbackHandler("n0nce", make(chan anilistCallbackResult, 1))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/oauth/callback", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `nonce: "n0nce"`) {
		t.Fatalf("got %d\n%s", w.Code, w.Body)
	}
}

func TestAniListCallbackTakesTheTokenWithTheNonce(t *testing.T) {
	result := make(chan anilistCallbackResult, 1)
	h := anilistCallbackHandler("n0nce", result)
	if w := postCallbackToken(h, url.Values{"access_token": {"tok"}, "nonce": {"n0nce"}}); w.Code != http.StatusNoContent {
		t.Fatalf("got %d", w.Code)
	}
	if r := <-result; r.err != nil || r.token != "tok" {
		t.Fatalf("got %+v", r)
	}
}

func TestAniListCallbackRefusesAForeignPost(t *testing.T) {
	result := make(chan anilistCallbackResult, 1)
	h := anilistCallbackHandler("n0nce", result)
	if w := postCallbackToken(h, url.Values{"access_token": {"evil"}, "nonce": {"guess"}}); w.Code != http.StatusForbidden {
		t.Fatalf("got %d", w.Code)
	}
	select {
	case r := <-result:
		t.Fatalf("a post without the nonce finished the sign-in: %+v", r)
	default:
	}
}

func TestAniListCallbackReportsARefusal(t *testing.T) {
	result := make(chan anilistCallbackResult, 1)
	h := anilistCallbackHandler("n0nce", result)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/oauth/callback?error=access_denied", nil))
	if r := <-result; r.err == nil || !strings.Contains(r.err.Error(), "access_denied") {
		t.Fatalf("got %+v", r)
	}
}
