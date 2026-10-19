package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/browser"
)

// The AniList API client otakase signs in with. The redirect must match the
// one registered for the client, so the callback server listens on that port.
// Sign-in uses the implicit grant, so there is no client secret.
const (
	anilistOAuthBase    = "https://anilist.co/api/v2/oauth"
	anilistClientID     = "27869"
	anilistRedirectURI  = "http://localhost:8000/oauth/callback"
	anilistCallbackAddr = ":8000"

	// AniList access tokens last a year.
	anilistTokenLifetime = 365 * 24 * time.Hour
	loginTimeout         = 5 * time.Minute
)

// AnilistToken is a stored OAuth token. MyAnimeList tokens use the same shape.
type AnilistToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func newAniListToken(access string) *AnilistToken {
	return &AnilistToken{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int(anilistTokenLifetime / time.Second),
		ExpiresAt:   time.Now().Add(anilistTokenLifetime),
	}
}

func loadToken(path string) (*AnilistToken, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var token AnilistToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("token file %s: %w", path, err)
	}
	return &token, nil
}

func saveToken(path string, token *AnilistToken) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func isTokenValid(token *AnilistToken) bool {
	return token != nil && token.AccessToken != "" && time.Now().Before(token.ExpiresAt)
}

// GetTokenFromFile returns the access token stored at path. The file is JSON;
// a file holding just the token as text is read too.
func GetTokenFromFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading the token: %w", err)
	}
	var token AnilistToken
	if json.Unmarshal(data, &token) == nil {
		if !isTokenValid(&token) {
			return "", errors.New("token has expired")
		}
		return token.AccessToken, nil
	}
	if plain := strings.TrimSpace(string(data)); plain != "" {
		return plain, nil
	}
	return "", errors.New("empty token file")
}

// ChangeToken signs in to AniList and stores the new token. It opens the
// browser first; when that fails it asks for a token pasted by hand.
func ChangeToken(config *Config, user *User) {
	tokenPath := filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json")

	fmt.Println("Starting browser-based authentication...")
	token, err := anilistBrowserLogin(tokenPath)
	if err != nil {
		Log("AniList browser sign-in: " + err.Error())
		fmt.Printf("Browser authentication failed: %v\n", err)
		token = anilistPastedLogin(config)
		if err := saveToken(tokenPath, newAniListToken(token)); err != nil {
			Exit(fmt.Errorf("failed to save token: %w", err))
		}
	}
	if token == "" {
		Exit(errors.New("no token provided"))
	}
	user.Token = token
	fmt.Println("Token saved successfully!")
}

// anilistPastedLogin asks for a token copied from AniList's implicit-grant
// page. Backing out ends the program, since nothing works without one.
func anilistPastedLogin(config *Config) string {
	fmt.Println("Falling back to manual token entry...")
	fmt.Printf("Please visit: %s/authorize?client_id=%s&response_type=token&redirect_uri=%s\n",
		anilistOAuthBase, anilistClientID, anilistRedirectURI)
	pasted, cancelled, err := promptCancelable(config, "Login",
		"Paste your AniList access token",
		"the part of the redirect URL after access_token=")
	if err != nil {
		Log("reading the pasted token: " + err.Error())
	}
	if cancelled || pasted == "" {
		Exit(errors.New("no token provided"))
	}
	return pasted
}

// anilistBrowserLogin runs AniList's implicit sign-in, which needs no client
// secret: AniList sends the browser back to the local callback with the token
// in the URL fragment. Browsers never send the fragment to a server, so the
// callback serves a small page that posts it back to the local listener,
// together with a nonce only that page knows. A stored token that is still
// valid is returned without asking.
func anilistBrowserLogin(tokenPath string) (string, error) {
	if token, err := loadToken(tokenPath); err == nil && isTokenValid(token) {
		return token.AccessToken, nil
	}

	nonce, err := randomOAuthString(32)
	if err != nil {
		return "", err
	}
	listener, err := net.Listen("tcp", anilistCallbackAddr)
	if err != nil {
		return "", fmt.Errorf("listening for the sign-in callback: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()
	result := make(chan anilistCallbackResult, 1)
	server := &http.Server{Handler: anilistCallbackHandler(nonce, result), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			sendCallbackResult(result, anilistCallbackResult{err: err})
		}
	}()
	defer server.Shutdown(context.Background())

	authURL := anilistOAuthBase + "/authorize?client_id=" + anilistClientID + "&response_type=token"
	fmt.Println("Opening browser for AniList authentication...")
	fmt.Printf("If the browser doesn't open automatically, visit: %s\n", authURL)
	if err := browser.OpenURL(authURL); err != nil {
		fmt.Println("Could not open the browser; open the address above yourself.")
	}

	select {
	case r := <-result:
		if r.err != nil {
			return "", r.err
		}
		if err := saveToken(tokenPath, newAniListToken(r.token)); err != nil {
			return "", fmt.Errorf("saving the token: %w", err)
		}
		fmt.Println("Authentication successful!")
		return r.token, nil
	case <-ctx.Done():
		return "", errors.New("no sign-in within 5 minutes")
	}
}

type anilistCallbackResult struct {
	token string
	err   error
}

func sendCallbackResult(ch chan<- anilistCallbackResult, r anilistCallbackResult) {
	select {
	case ch <- r:
	default:
	}
}

// anilistCallbackHandler serves the redirect page and takes the token it
// posts back. Only a post carrying the page's nonce counts, so another site
// can't push its own token in while the sign-in is open.
func anilistCallbackHandler(nonce string, result chan<- anilistCallbackResult) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		if reason := r.URL.Query().Get("error"); reason != "" {
			writeLoginPage(w, http.StatusBadRequest, "Authentication failed: "+reason, "You can close this window and try again.")
			sendCallbackResult(result, anilistCallbackResult{err: fmt.Errorf("AniList refused the sign-in: %s", reason)})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, anilistCallbackPage, html.EscapeString(DisplayName), nonce)
	})
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := r.ParseForm(); err != nil || r.PostForm.Get("nonce") != nonce {
			http.Error(w, "unexpected sign-in", http.StatusForbidden)
			return
		}
		token := strings.TrimSpace(r.PostForm.Get("access_token"))
		if token == "" {
			http.Error(w, "no token", http.StatusBadRequest)
			sendCallbackResult(result, anilistCallbackResult{err: errors.New("AniList sent no access token")})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		sendCallbackResult(result, anilistCallbackResult{token: token})
	})
	return mux
}

// anilistCallbackPage reads the token from the fragment and posts it to the
// local listener. %[1]s is the app name, %[2]s the nonce.
const anilistCallbackPage = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>%[1]s sign-in</title>
<style>body{font-family:system-ui,sans-serif;margin:4rem auto;max-width:32rem;text-align:center;background:#16161a;color:#eee}h1{font-size:1.3rem}</style>
</head><body><h1 id="h">Signing in…</h1><p id="p"></p>
<script>
const show = (h, p) => { document.getElementById("h").textContent = h; document.getElementById("p").textContent = p; };
const got = new URLSearchParams(location.hash.slice(1));
history.replaceState(null, "", location.pathname);
const token = got.get("access_token");
if (!token) {
  show("No token received", "You can close this window and try again.");
} else {
  fetch("/oauth/token", {method: "POST", body: new URLSearchParams({access_token: token, nonce: "%[2]s"})})
    .then(r => r.ok ? show("Signed in", "You can close this window and go back to %[1]s.")
                    : show("Sign-in failed", "You can close this window and try again."))
    .catch(() => show("Sign-in failed", "You can close this window and try again."));
}
</script></body></html>`

func writeLoginPage(w http.ResponseWriter, status int, heading, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>%[1]s sign-in</title>
<style>body{font-family:system-ui,sans-serif;margin:4rem auto;max-width:32rem;text-align:center;background:#16161a;color:#eee}h1{font-size:1.3rem}</style>
</head><body><h1>%[2]s</h1><p>%[3]s</p></body></html>`,
		html.EscapeString(DisplayName), html.EscapeString(heading), html.EscapeString(detail))
}
