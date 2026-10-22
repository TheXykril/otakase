package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// touchGrassBot is a bot that answers the badge claim, counting claims.
func touchGrassBot(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var claims atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/badge/touch-grass" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("unexpected %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		claims.Add(1)
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"new": true, "finders": 2})
	}))
	t.Cleanup(srv.Close)
	return srv, &claims
}

func linkForTest(t *testing.T, config *Config) {
	t.Helper()
	if err := saveDiscordLink(config, discordLink{Token: "tok"}); err != nil {
		t.Fatal(err)
	}
}

// Only the lucky roll drops it, and only once: after it has dropped, even
// another lucky roll leaves it alone.
func TestTouchGrassDropsOnceOnTheLuckyRoll(t *testing.T) {
	srv, claims := touchGrassBot(t, http.StatusOK)
	config := newDiscordTestConfig(t, srv.URL)
	linkForTest(t, config)

	dropTouchGrassBadge(config, rolls(1))
	if touchGrassState(config) != "" || claims.Load() != 0 {
		t.Fatal("an unlucky roll dropped the badge")
	}
	dropTouchGrassBadge(config, rolls(0))
	if got := touchGrassState(config); got != touchGrassClaimed || claims.Load() != 1 {
		t.Fatalf("lucky roll: state %q, %d claims", got, claims.Load())
	}
	dropTouchGrassBadge(config, rolls(0))
	if claims.Load() != 1 {
		t.Fatal("the badge dropped a second time")
	}
}

// Not linked when it drops: it waits as pending, and goes over once the
// app is linked.
func TestTouchGrassWaitsForALink(t *testing.T) {
	srv, claims := touchGrassBot(t, http.StatusOK)
	config := newDiscordTestConfig(t, srv.URL)

	dropTouchGrassBadge(config, rolls(0))
	if got := touchGrassState(config); got != touchGrassPending || claims.Load() != 0 {
		t.Fatalf("unlinked drop: state %q, %d claims", got, claims.Load())
	}
	claimPendingTouchGrass(config)
	if claims.Load() != 0 {
		t.Fatal("claimed without a link")
	}
	linkForTest(t, config)
	claimPendingTouchGrass(config)
	if got := touchGrassState(config); got != touchGrassClaimed || claims.Load() != 1 {
		t.Fatalf("after linking: state %q, %d claims", got, claims.Load())
	}
	claimPendingTouchGrass(config)
	if claims.Load() != 1 {
		t.Fatal("a claimed badge was sent again")
	}
}

// A bot that fails keeps it pending for the next launch.
func TestTouchGrassStaysPendingWhenTheBotFails(t *testing.T) {
	srv, _ := touchGrassBot(t, http.StatusBadGateway)
	config := newDiscordTestConfig(t, srv.URL)
	linkForTest(t, config)
	dropTouchGrassBadge(config, rolls(0))
	if got := touchGrassState(config); got != touchGrassPending {
		t.Fatalf("state %q after a failed claim", got)
	}
}

// With the Discord features off there is nowhere for it to go, so it does
// not drop, and stays available for later.
func TestTouchGrassNeedsDiscordOn(t *testing.T) {
	config := newDiscordTestConfig(t, "")
	dropTouchGrassBadge(config, rolls(0))
	if got := touchGrassState(config); got != "" {
		t.Fatalf("dropped with Discord off: %q", got)
	}
}
