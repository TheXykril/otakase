package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type rewriteTransport func(*http.Request) (*http.Response, error)

func (send rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) { return send(req) }

// routeHTTPTo sends every request made through sharedHTTPClient to handler,
// whichever host it was meant for; r.Host still names that host. The browser
// sign-in is stubbed to fail so a test can never open one.
func routeHTTPTo(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	previousClient, previousAuth := sharedHTTPClient, authenticateMyAnimeList
	restore := func() { sharedHTTPClient, authenticateMyAnimeList = previousClient, previousAuth }
	t.Cleanup(restore)
	sharedHTTPClient = &http.Client{Transport: rewriteTransport(func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		return http.DefaultTransport.RoundTrip(req)
	})}
	authenticateMyAnimeList = func(*Config, string) (string, error) {
		return "", errors.New("browser sign-in is not available in tests")
	}
}

// stubBrowserSignIn makes the browser sign-in store and return access.
func stubBrowserSignIn(access string) {
	authenticateMyAnimeList = func(_ *Config, tokenPath string) (string, error) {
		token := &OAuthToken{AccessToken: access, RefreshToken: access + "-refresh", ExpiresAt: time.Now().Add(time.Hour)}
		return token.AccessToken, saveToken(tokenPath, token)
	}
}

func myAnimeListTestConfig(t *testing.T, token *OAuthToken) *Config {
	t.Helper()
	config := &Config{StoragePath: t.TempDir(), MyAnimeListClientID: "client-id"}
	if token != nil {
		if err := saveToken(myAnimeListTokenPath(config), token); err != nil {
			t.Fatalf("save token: %v", err)
		}
	}
	return config
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

func TestRefreshMyAnimeListTokenKeepsRefreshTokenWhenResponseOmitsOne(t *testing.T) {
	config := myAnimeListTestConfig(t, &OAuthToken{AccessToken: "stale-access", RefreshToken: "kept-refresh", ExpiresAt: time.Now().Add(-time.Hour)})
	config.MyAnimeListClientSecret = "secret"

	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "myanimelist.net" || r.URL.Path != "/v1/oauth2/token" {
			t.Errorf("unexpected request %s%s", r.Host, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		want := map[string]string{
			"grant_type":    "refresh_token",
			"refresh_token": "kept-refresh",
			"client_id":     "client-id",
			"client_secret": "secret",
		}
		for key, value := range want {
			if got := r.PostForm.Get(key); got != value {
				t.Errorf("%s = %q, want %q", key, got, value)
			}
		}
		writeJSON(t, w, http.StatusOK, `{"access_token":"new-access","token_type":"Bearer","expires_in":3600}`)
	})

	access, err := GetMyAnimeListAccessToken(config)
	if err != nil {
		t.Fatalf("GetMyAnimeListAccessToken: %v", err)
	}
	if access != "new-access" {
		t.Fatalf("access = %q, want new-access", access)
	}

	stored, err := loadToken(myAnimeListTokenPath(config))
	if err != nil {
		t.Fatalf("reading the refreshed token: %v", err)
	}
	if stored.RefreshToken != "kept-refresh" {
		t.Fatalf("stored refresh token = %q, want kept-refresh", stored.RefreshToken)
	}
	if left := time.Until(stored.ExpiresAt); left < 50*time.Minute || left > time.Hour {
		t.Fatalf("stored token expires in %v, want about an hour", left)
	}
}

func TestRefreshMyAnimeListTokenStoresRotatedRefreshToken(t *testing.T) {
	config := myAnimeListTestConfig(t, nil)
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"access_token":"a2","refresh_token":"r2","expires_in":60}`)
	})

	token, err := refreshMyAnimeListToken(config, myAnimeListTokenPath(config), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if token.RefreshToken != "r2" {
		t.Fatalf("refresh token = %q, want the rotated r2", token.RefreshToken)
	}
}

func TestRefreshMyAnimeListTokenErrors(t *testing.T) {
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("refresh_token") == "empty" {
			writeJSON(t, w, http.StatusOK, `{"access_token":""}`)
			return
		}
		writeJSON(t, w, http.StatusBadRequest, `{"error":"invalid_grant"}`)
	})

	config := myAnimeListTestConfig(t, nil)
	tokenFile := myAnimeListTokenPath(config)
	cases := []struct {
		name    string
		config  *Config
		refresh string
		want    string
	}{
		{"no client id", &Config{StoragePath: config.StoragePath}, "r", "MyAnimeList client ID is not configured"},
		{"no refresh token", config, "", "MyAnimeList refresh token is missing"},
		{"rejected", config, "bad", `failed refreshing MyAnimeList token: {"error":"invalid_grant"}`},
		{"no access token", config, "empty", "MyAnimeList refresh response did not include an access token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := refreshMyAnimeListToken(tc.config, tokenFile, tc.refresh)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	if _, err := os.Stat(tokenFile); !os.IsNotExist(err) {
		t.Fatalf("a failed refresh must not write a token file: %v", err)
	}
}

func TestExchangeMyAnimeListAuthorizationCode(t *testing.T) {
	config := myAnimeListTestConfig(t, nil)
	if err := savePendingMyAnimeListAuth(config, pendingMyAnimeListAuth{CodeVerifier: "verifier", State: "state"}); err != nil {
		t.Fatal(err)
	}

	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		want := map[string]string{
			"grant_type":    "authorization_code",
			"code":          "the-code",
			"redirect_uri":  myAnimeListRedirectURI,
			"code_verifier": "verifier",
			"client_id":     "client-id",
		}
		for key, value := range want {
			if got := r.FormValue(key); got != value {
				t.Errorf("%s = %q, want %q", key, got, value)
			}
		}
		if _, sent := r.PostForm["client_secret"]; sent {
			t.Error("client_secret sent although none is configured")
		}
		writeJSON(t, w, http.StatusOK, `{"access_token":"fresh","refresh_token":"fresh-refresh","expires_in":3600}`)
	})

	tokenFile := myAnimeListTokenPath(config)
	access, err := exchangeMyAnimeListAuthorizationCode(config, tokenFile, "verifier", "the-code")
	if err != nil {
		t.Fatal(err)
	}
	if access != "fresh" {
		t.Fatalf("access = %q, want fresh", access)
	}
	if stored, err := loadToken(tokenFile); err != nil || stored.RefreshToken != "fresh-refresh" {
		t.Fatalf("stored token = %+v, %v", stored, err)
	}
	if _, err := loadPendingMyAnimeListAuth(config); !os.IsNotExist(err) {
		t.Fatalf("pending sign-in should be cleared, got %v", err)
	}
}

func TestExchangeMyAnimeListAuthorizationCodeFailureKeepsPendingLogin(t *testing.T) {
	config := myAnimeListTestConfig(t, nil)
	if err := savePendingMyAnimeListAuth(config, pendingMyAnimeListAuth{CodeVerifier: "v", State: "s"}); err != nil {
		t.Fatal(err)
	}
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusBadRequest, ` {"error":"invalid_grant"} `)
	})

	_, err := exchangeMyAnimeListAuthorizationCode(config, myAnimeListTokenPath(config), "v", "c")
	if err == nil || err.Error() != `MyAnimeList token exchange failed: {"error":"invalid_grant"}` {
		t.Fatalf("err = %v", err)
	}
	if _, err := loadPendingMyAnimeListAuth(config); err != nil {
		t.Fatalf("pending sign-in should survive a failed exchange: %v", err)
	}
}

func TestGetMyAnimeListAccessTokenUsesValidTokenWithoutRequests(t *testing.T) {
	config := myAnimeListTestConfig(t, &OAuthToken{AccessToken: "valid", ExpiresAt: time.Now().Add(time.Hour)})
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s%s", r.Host, r.URL.Path)
	})

	access, err := GetMyAnimeListAccessToken(config)
	if err != nil || access != "valid" {
		t.Fatalf("access = %q, %v", access, err)
	}
}

func TestGetMyAnimeListAccessTokenReauthenticatesWhenRefreshFails(t *testing.T) {
	config := myAnimeListTestConfig(t, &OAuthToken{AccessToken: "expired-access", RefreshToken: "rejected-refresh", ExpiresAt: time.Now().Add(-time.Hour)})
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusBadRequest, `{"error":"invalid_grant"}`)
	})
	stubBrowserSignIn("reauth-access")

	access, err := GetMyAnimeListAccessToken(config)
	if err != nil {
		t.Fatalf("expected a fresh sign-in, got %v", err)
	}
	if access != "reauth-access" {
		t.Fatalf("access token = %q, want reauth-access", access)
	}
}

func TestGetMyAnimeListAccessTokenSignsInWithoutTokenFile(t *testing.T) {
	config := myAnimeListTestConfig(t, nil)
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s%s", r.Host, r.URL.Path)
	})
	stubBrowserSignIn("first-login")

	access, err := GetMyAnimeListAccessToken(config)
	if err != nil || access != "first-login" {
		t.Fatalf("access = %q, %v", access, err)
	}
}

func TestMyAnimeListRequestSignsInAgainWhenTheRefreshIsRejected(t *testing.T) {
	config := myAnimeListTestConfig(t, &OAuthToken{AccessToken: "revoked", RefreshToken: "rejected-refresh", ExpiresAt: time.Now().Add(time.Hour)})

	apiCalls := 0
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "myanimelist.net":
			writeJSON(t, w, http.StatusBadRequest, `{"error":"invalid_grant"}`)
		case "api.myanimelist.net":
			apiCalls++
			auth := r.Header.Get("Authorization")
			if apiCalls == 1 {
				if auth != "Bearer revoked" {
					t.Errorf("first Authorization = %q, want the revoked token", auth)
				}
				writeJSON(t, w, http.StatusUnauthorized, `{"error":"invalid_token"}`)
				return
			}
			if auth != "Bearer reauth-access" {
				t.Errorf("retried Authorization = %q, want the new token", auth)
			}
			if got := r.URL.Query().Get("fields"); got != "id,name" {
				t.Errorf("fields = %q, want id,name", got)
			}
			writeJSON(t, w, http.StatusOK, `{"id":123,"name":"tester"}`)
		default:
			t.Errorf("unexpected host %s", r.Host)
		}
	})
	stubBrowserSignIn("reauth-access")

	id, name, err := fetchMyAnimeListProfile(config)
	if err != nil {
		t.Fatalf("the request should sign in again and retry: %v", err)
	}
	if id != 123 || name != "tester" {
		t.Fatalf("profile = %d %q, want 123 tester", id, name)
	}
	if apiCalls != 2 {
		t.Fatalf("API called %d times, want 2", apiCalls)
	}
}

func TestMyAnimeListRequestRetriesOnlyOnce(t *testing.T) {
	config := myAnimeListTestConfig(t, &OAuthToken{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)})
	apiCalls := 0
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "myanimelist.net" {
			writeJSON(t, w, http.StatusOK, `{"access_token":"b","expires_in":3600}`)
			return
		}
		apiCalls++
		writeJSON(t, w, http.StatusUnauthorized, `{"error":"invalid_token"}`)
	})

	err := myAnimeListRequest(config, http.MethodGet, "/users/@me", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "failed with status 401") {
		t.Fatalf("err = %v, want the second 401 reported", err)
	}
	if apiCalls != 2 {
		t.Fatalf("API called %d times, want 2", apiCalls)
	}
}

func TestMyAnimeListRequestEncodesForm(t *testing.T) {
	config := myAnimeListTestConfig(t, &OAuthToken{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)})
	type seen struct{ method, rawQuery, contentType, body string }
	var got []seen
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Method, r.URL.RawQuery, r.Header.Get("Content-Type"), string(body)})
		if r.URL.Path != "/v2/anime/1/my_list_status" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, `{}`)
	})

	form := url.Values{"score": {"8"}}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if err := myAnimeListRequest(config, method, "/anime/1/my_list_status", form, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := myAnimeListRequest(config, http.MethodDelete, "/anime/1/my_list_status", nil, nil); err != nil {
		t.Fatal(err)
	}

	want := []seen{
		{http.MethodGet, "score=8", "", ""},
		{http.MethodPut, "", "application/x-www-form-urlencoded", "score=8"},
		{http.MethodDelete, "", "", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d requests, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestMyAnimeListRequestReportsStatusAndBody(t *testing.T) {
	config := myAnimeListTestConfig(t, &OAuthToken{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)})
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, ` {"error":"not_found"} `)
	})

	err := myAnimeListRequest(config, http.MethodGet, "/anime/1", nil, nil)
	want := `MyAnimeList API GET /anime/1 failed with status 404: {"error":"not_found"}`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestPendingMyAnimeListAuthRoundTrip(t *testing.T) {
	config := &Config{StoragePath: t.TempDir() + "/nested"}
	want := pendingMyAnimeListAuth{CodeVerifier: "v", State: "s", CreatedAt: time.Now().UTC().Truncate(time.Second)}
	if err := savePendingMyAnimeListAuth(config, want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(myAnimeListPendingAuthPath(config))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil || fields["code_verifier"] != "v" || fields["state"] != "s" {
		t.Fatalf("pending file = %s", data)
	}

	got, err := loadPendingMyAnimeListAuth(config)
	if err != nil || !got.CreatedAt.Equal(want.CreatedAt) || got.State != want.State {
		t.Fatalf("loaded %+v, %v", got, err)
	}
	clearPendingMyAnimeListAuth(config)
	if _, err := loadPendingMyAnimeListAuth(config); !os.IsNotExist(err) {
		t.Fatalf("pending file should be gone, got %v", err)
	}
}

func TestRandomOAuthString(t *testing.T) {
	for _, length := range []int{10, 43, 64} {
		value, err := randomOAuthString(length)
		if err != nil {
			t.Fatal(err)
		}
		if want := max(length, 43); len(value) != want {
			t.Fatalf("length %d gave %d characters, want %d", length, len(value), want)
		}
		if strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~") != "" {
			t.Fatalf("%q has characters PKCE does not allow", value)
		}
	}
}

func TestMyAnimeListClientCredentialsFallBackToEnvironment(t *testing.T) {
	t.Setenv("OTAKASE_MAL_CLIENT_ID", "")
	t.Setenv("MYANIMELIST_CLIENT_ID", " env-id ")
	t.Setenv("OTAKASE_MAL_CLIENT_SECRET", "env-secret")

	id, secret := myAnimeListClientCredentials(&Config{})
	if id != "env-id" || secret != "env-secret" {
		t.Fatalf("got %q, %q", id, secret)
	}
	id, secret = myAnimeListClientCredentials(&Config{MyAnimeListClientID: "cfg-id", MyAnimeListClientSecret: "cfg-secret"})
	if id != "cfg-id" || secret != "cfg-secret" {
		t.Fatalf("config should win, got %q, %q", id, secret)
	}
	if id, secret := myAnimeListClientCredentials(nil); id != "" || secret != "" {
		t.Fatalf("nil config gave %q, %q", id, secret)
	}
}
