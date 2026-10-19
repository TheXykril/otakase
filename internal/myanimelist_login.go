package internal

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pkg/browser"
)

// The redirect must match the one registered for the MyAnimeList client, so
// the callback server listens on that port.
const (
	myAnimeListAuthorizeURL    = "https://myanimelist.net/v1/oauth2/authorize"
	myAnimeListTokenURL        = "https://myanimelist.net/v1/oauth2/token"
	myAnimeListRedirectURI     = "http://localhost:8123/oauth/callback"
	myAnimeListCallbackAddr    = ":8123"
	myAnimeListTokenFile       = "myanimelist_token.json"
	myAnimeListPendingAuthFile = "myanimelist_oauth_pending.json"
)

// OAuthToken is a stored MyAnimeList token. It has the same shape as AniList's.
type OAuthToken = AnilistToken

// pendingMyAnimeListAuth is a sign-in that was started but not finished. It is
// kept on disk so the callback URL can still be pasted on the next run.
type pendingMyAnimeListAuth struct {
	CodeVerifier string    `json:"code_verifier"`
	State        string    `json:"state"`
	CreatedAt    time.Time `json:"created_at"`
}

// authenticateMyAnimeList is a variable so tests can skip the browser.
var authenticateMyAnimeList = authenticateMyAnimeListWithBrowser

func myAnimeListTokenPath(config *Config) string {
	return myAnimeListFile(config.StoragePath, myAnimeListTokenFile)
}

func myAnimeListPendingAuthPath(config *Config) string {
	return myAnimeListFile(config.StoragePath, myAnimeListPendingAuthFile)
}

func savePendingMyAnimeListAuth(config *Config, pending pendingMyAnimeListAuth) error {
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(os.ExpandEnv(config.StoragePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(myAnimeListPendingAuthPath(config), data, 0o600)
}

func loadPendingMyAnimeListAuth(config *Config) (*pendingMyAnimeListAuth, error) {
	data, err := os.ReadFile(myAnimeListPendingAuthPath(config))
	if err != nil {
		return nil, err
	}
	pending := &pendingMyAnimeListAuth{}
	if err := json.Unmarshal(data, pending); err != nil {
		return nil, err
	}
	return pending, nil
}

func clearPendingMyAnimeListAuth(config *Config) {
	_ = os.Remove(myAnimeListPendingAuthPath(config))
}

// myAnimeListClientCredentials prefers the config and falls back to the
// environment. The secret is optional: PKCE clients have none.
func myAnimeListClientCredentials(config *Config) (string, string) {
	if config == nil {
		return "", ""
	}
	clientID := strings.TrimSpace(config.MyAnimeListClientID)
	if clientID == "" {
		clientID = firstEnv("OTAKASE_MAL_CLIENT_ID", "MYANIMELIST_CLIENT_ID")
	}
	clientSecret := strings.TrimSpace(config.MyAnimeListClientSecret)
	if clientSecret == "" {
		clientSecret = firstEnv("OTAKASE_MAL_CLIENT_SECRET", "MYANIMELIST_CLIENT_SECRET")
	}
	return clientID, clientSecret
}

// firstEnv returns the first of names that is set to a non-empty value.
func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// extractOAuthCodeFromInput reads the authorization code out of what the user
// pasted: the whole callback URL, its query string, or the bare code.
func extractOAuthCodeFromInput(input string, expectedState string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("empty callback input")
	}

	var query url.Values
	isURL := strings.Contains(input, "://")
	switch {
	case isURL:
		parsed, err := url.Parse(input)
		if err != nil {
			return "", err
		}
		query = parsed.Query()
	case strings.Contains(input, "code="):
		parsed, err := url.ParseQuery(strings.TrimPrefix(input, "?"))
		if err != nil {
			return "", err
		}
		query = parsed
	default:
		return input, nil
	}

	// Only a state that differs is rejected; a pasted bare query may have none.
	if state := query.Get("state"); expectedState != "" && state != "" && state != expectedState {
		return "", errors.New("callback state does not match the pending login")
	}
	if code := query.Get("code"); code != "" {
		return code, nil
	}
	if isURL {
		return "", errors.New("callback URL does not include an authorization code")
	}
	return input, nil
}

// randomOAuthString returns a string of PKCE "unreserved" characters. RFC 7636
// wants a verifier of at least 43 characters, so shorter requests are raised.
func randomOAuthString(length int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	length = max(length, 43)
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = chars[int(b)%len(chars)]
	}
	return string(buf), nil
}

// requestMyAnimeListToken posts a grant to the token endpoint and returns the
// token with its expiry filled in. failure prefixes a rejected request and
// missing names what came back without an access token.
func requestMyAnimeListToken(config *Config, form url.Values, failure, missing string) (*OAuthToken, error) {
	clientID, clientSecret := myAnimeListClientCredentials(config)
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequest(http.MethodPost, myAnimeListTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", failure, strings.TrimSpace(string(body)))
	}

	token := &OAuthToken{}
	if err := json.Unmarshal(body, token); err != nil {
		return nil, err
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return nil, fmt.Errorf("MyAnimeList %s did not include an access token", missing)
	}
	token.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return token, nil
}

// exchangeMyAnimeListAuthorizationCode trades the callback code for a token,
// stores it and drops the pending sign-in.
func exchangeMyAnimeListAuthorizationCode(config *Config, tokenPath string, codeVerifier string, code string) (string, error) {
	token, err := requestMyAnimeListToken(config, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {myAnimeListRedirectURI},
		"code_verifier": {codeVerifier},
	}, "MyAnimeList token exchange failed", "token response")
	if err != nil {
		return "", err
	}
	if err := saveToken(tokenPath, token); err != nil {
		return "", err
	}
	clearPendingMyAnimeListAuth(config)
	return token.AccessToken, nil
}

func refreshMyAnimeListToken(config *Config, tokenPath string, refreshToken string) (*OAuthToken, error) {
	if clientID, _ := myAnimeListClientCredentials(config); clientID == "" {
		return nil, errors.New("MyAnimeList client ID is not configured")
	}
	if refreshToken == "" {
		return nil, errors.New("MyAnimeList refresh token is missing")
	}

	token, err := requestMyAnimeListToken(config, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}, "failed refreshing MyAnimeList token", "refresh response")
	if err != nil {
		return nil, err
	}
	// MyAnimeList may not rotate the refresh token; keep the old one then, or
	// the next refresh would have nothing to send.
	if strings.TrimSpace(token.RefreshToken) == "" {
		token.RefreshToken = refreshToken
	}
	if err := saveToken(tokenPath, token); err != nil {
		return nil, err
	}
	setGlobalUserToken(token.AccessToken)
	return token, nil
}

func setGlobalUserToken(accessToken string) {
	if user := GetGlobalUser(); user != nil {
		user.Token = accessToken
	}
}

// renewMyAnimeListAccessToken refreshes the token when it can and falls back
// to a browser sign-in. reason only goes to the log.
func renewMyAnimeListAccessToken(config *Config, tokenPath string, token *OAuthToken, reason string) (string, error) {
	if token != nil && strings.TrimSpace(token.RefreshToken) != "" {
		refreshed, err := refreshMyAnimeListToken(config, tokenPath, token.RefreshToken)
		if err == nil {
			return refreshed.AccessToken, nil
		}
		Log(fmt.Sprintf("Failed to refresh MyAnimeList token after %s: %v", reason, err))
	}

	Out("MyAnimeList sign-in required. Opening browser...")
	Log(fmt.Sprintf("Starting MyAnimeList browser authentication after %s", reason))
	clearStoredTokenFile(tokenPath)

	accessToken, err := authenticateMyAnimeList(config, tokenPath)
	if err != nil {
		return "", err
	}
	setGlobalUserToken(accessToken)
	return accessToken, nil
}

// GetMyAnimeListAccessToken returns a usable access token, refreshing it or
// signing in again when the stored one has expired.
func GetMyAnimeListAccessToken(config *Config) (string, error) {
	tokenPath := myAnimeListTokenPath(config)
	token, err := loadToken(tokenPath)
	if err != nil {
		Log(fmt.Sprintf("Failed to load MyAnimeList token, starting login: %v", err))
		return renewMyAnimeListAccessToken(config, tokenPath, nil, "token load failure")
	}
	if isTokenValid(token) {
		setGlobalUserToken(token.AccessToken)
		return token.AccessToken, nil
	}
	if token.RefreshToken == "" {
		Log("MyAnimeList token is expired and has no refresh token, starting login")
	}
	return renewMyAnimeListAccessToken(config, tokenPath, token, "expired token")
}

func ChangeMyAnimeListToken(config *Config, user *User) error {
	accessToken, err := authenticateMyAnimeList(config, myAnimeListTokenPath(config))
	if err != nil {
		return err
	}
	user.Token = accessToken
	return nil
}

// authenticateMyAnimeListWithBrowser runs the PKCE sign-in. A stored token is
// used or refreshed first. The verifier and state are saved before the browser
// opens, so if the callback never reaches us the user can paste it on the next
// run and the login still finishes.
func authenticateMyAnimeListWithBrowser(config *Config, tokenPath string) (string, error) {
	clientID, _ := myAnimeListClientCredentials(config)
	if clientID == "" {
		return "", errors.New("MyAnimeList client ID is not configured")
	}

	if token, err := loadToken(tokenPath); err == nil {
		if isTokenValid(token) {
			return token.AccessToken, nil
		}
		if token.RefreshToken != "" {
			if refreshed, err := refreshMyAnimeListToken(config, tokenPath, token.RefreshToken); err == nil {
				return refreshed.AccessToken, nil
			}
		}
	}

	if pending, err := loadPendingMyAnimeListAuth(config); err == nil {
		Out("Found an unfinished MyAnimeList login.")
		pasted, err := promptTrackingInput("Paste the full MyAnimeList callback URL (or just the code), or press Enter to start over", true)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(pasted) != "" {
			code, err := extractOAuthCodeFromInput(pasted, pending.State)
			if err != nil {
				return "", err
			}
			return exchangeMyAnimeListAuthorizationCode(config, tokenPath, pending.CodeVerifier, code)
		}
		clearPendingMyAnimeListAuth(config)
	}

	codeVerifier, err := randomOAuthString(64)
	if err != nil {
		return "", err
	}
	state, err := randomOAuthString(32)
	if err != nil {
		return "", err
	}
	if err := savePendingMyAnimeListAuth(config, pendingMyAnimeListAuth{
		CodeVerifier: codeVerifier,
		State:        state,
		CreatedAt:    time.Now(),
	}); err != nil {
		return "", err
	}

	code, err := waitForMyAnimeListCallback(clientID, codeVerifier, state)
	if err != nil {
		return "", err
	}
	return exchangeMyAnimeListAuthorizationCode(config, tokenPath, codeVerifier, code)
}

// waitForMyAnimeListCallback opens the authorize page and waits for the code
// on the local callback. After the timeout it asks for the URL to be pasted.
func waitForMyAnimeListCallback(clientID, codeVerifier, state string) (string, error) {
	const rerunHint = "rerun the command and paste the callback URL to finish login"

	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	// Only the first callback counts; a reload must not block the handler.
	finish := func(code string, err error) {
		select {
		case done <- result{code, err}:
		default:
		}
	}

	listener, err := net.Listen("tcp", myAnimeListCallbackAddr)
	if err != nil {
		return "", fmt.Errorf("%w. %s", err, rerunHint)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			finish("", errors.New("invalid state returned from MyAnimeList"))
			return
		}
		code := query.Get("code")
		if code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			finish("", errors.New("missing authorization code"))
			return
		}
		fmt.Fprint(w, "<html><body><p>Authentication complete. You can close this window.</p></body></html>")
		finish(code, nil)
	})
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			finish("", err)
		}
	}()
	defer server.Shutdown(ctx)

	// MyAnimeList only supports the "plain" challenge, so the challenge is
	// the verifier itself.
	authURL := myAnimeListAuthorizeURL + "?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"state":                 {state},
		"redirect_uri":          {myAnimeListRedirectURI},
		"code_challenge":        {codeVerifier},
		"code_challenge_method": {"plain"},
	}.Encode()

	fmt.Println("Opening browser for MyAnimeList authentication...")
	fmt.Printf("If the browser doesn't open automatically, visit: %s\n", authURL)
	if err := browser.OpenURL(authURL); err != nil {
		fmt.Printf("Failed to open browser automatically: %v\n", err)
	}
	fmt.Println("If otakase does not continue after the browser reaches localhost, rerun the command and paste the full callback URL when prompted.")

	select {
	case res := <-done:
		if res.err != nil {
			return "", fmt.Errorf("%w. %s", res.err, rerunHint)
		}
		return res.code, nil
	case <-ctx.Done():
		pasted, err := promptTrackingInput("Paste the full MyAnimeList callback URL or just the code", false)
		if err != nil {
			return "", errors.New("MyAnimeList authentication timed out; " + rerunHint)
		}
		return extractOAuthCodeFromInput(pasted, state)
	}
}
