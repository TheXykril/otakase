package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The public API is what the Otakase app talks to. It listens on its own
// port (8100 by default as a Home Assistant add-on), reached from the
// internet through a Cloudflare Tunnel, and never shares a port with the
// ingress-only dashboard. See wiki/Developer-App-Link.md for the protocol.

// appLink is one linked Otakase app, stored by the hash of its token.
type appLink struct {
	User     string    `json:"user"` // Discord user id
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen,omitzero"`
	Version  string    `json:"version,omitempty"`
	OS       string    `json:"os,omitempty"`
}

var apiRoutes = map[string]http.HandlerFunc{}

// apiHandle registers a route on the public API, e.g. apiHandle("POST
// /v1/link/start", h). Call it from init() in the feature's own file.
func apiHandle(pattern string, h func(b *bot, w http.ResponseWriter, r *http.Request)) {
	apiBot := func(w http.ResponseWriter, r *http.Request) { h(currentBot, w, r) }
	apiRoutes[pattern] = apiBot
}

// currentBot is the running bot, for API handlers.
var currentBot *bot

// publicAddr is OTAKASE_PUBLIC_ADDR, else :8100 as an add-on, else off.
func publicAddr() string {
	if a := os.Getenv("OTAKASE_PUBLIC_ADDR"); a != "" {
		return a
	}
	if p, err := strconv.Atoi(os.Getenv("OTAKASE_PUBLIC_PORT")); err == nil && p > 0 {
		return ":" + strconv.Itoa(p)
	}
	if _, err := os.Stat("/data/options.json"); err == nil {
		return ":8100"
	}
	return ""
}

func (b *bot) servePublicAPI() {
	addr := publicAddr()
	if addr == "" {
		select {} // off; keepRunning must not restart it
	}
	currentBot = b
	mux := http.NewServeMux()
	for p, h := range apiRoutes {
		mux.HandleFunc(p, h)
	}
	mux.HandleFunc("GET /v1/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": botVersion()})
	})
	srv := &http.Server{Addr: addr, Handler: apiHeaders(mux), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 60 * time.Second, IdleTimeout: 2 * time.Minute}
	log.Printf("public API on %s", addr)
	if err := srv.ListenAndServe(); err != nil {
		b.jobFailed("Public API", err)
		time.Sleep(time.Minute)
	}
}

func apiHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// apiError answers {"error": msg}.
func apiError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// readJSON decodes a JSON body of at most max bytes.
func readJSON(w http.ResponseWriter, r *http.Request, max int64, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		apiError(w, http.StatusBadRequest, "bad request")
		return false
	}
	return true
}

// clientIP is the caller's address: Cloudflare's header when the request
// came through the tunnel, else the connection's.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimit allows n requests per key per window.
type rateLimit struct {
	mu     sync.Mutex
	n      int
	window time.Duration
	hits   map[string][]time.Time
}

func newRateLimit(n int, window time.Duration) *rateLimit {
	return &rateLimit{n: n, window: window, hits: map[string][]time.Time{}}
}

func (l *rateLimit) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.n {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	if len(l.hits) > 10000 { // forget everyone rather than grow forever
		l.hits = map[string][]time.Time{key: l.hits[key]}
	}
	return true
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newToken is a random secret of n bytes, hex encoded.
func newToken(n int) string {
	buf := make([]byte, n)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

var errNoToken = errors.New("not linked")

// appUser checks the Bearer token and returns the linked Discord user id.
// It also notes when the app was last seen.
func (b *bot) appUser(r *http.Request) (string, error) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || len(tok) < 32 {
		return "", errNoToken
	}
	h := hashToken(strings.TrimSpace(tok))
	var user string
	b.store.view(func(d *storeData) {
		if l, ok := d.AppLinks[h]; ok {
			user = l.User
		}
	})
	if user == "" {
		return "", errNoToken
	}
	return user, nil
}

// requireApp answers 401 and returns "" when the request has no valid token.
func (b *bot) requireApp(w http.ResponseWriter, r *http.Request) string {
	u, err := b.appUser(r)
	if err != nil {
		apiError(w, http.StatusUnauthorized, "link this app with otakase -discord-link first")
		return ""
	}
	return u
}
