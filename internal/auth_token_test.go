package internal

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// writeAnilistTokenFileForTest saves token as the AniList sign-in under dir,
// valid for another day.
func writeAnilistTokenFileForTest(tb *testing.T, storage, access string) {
	tb.Helper()
	path := filepath.Join(storage, "anilist_token.json")
	saved := &AnilistToken{AccessToken: access, TokenType: "Bearer", ExpiresIn: 365 * 24 * 60 * 60}
	saved.ExpiresAt = time.Now().AddDate(0, 0, 1)
	if err := saveToken(path, saved); err != nil {
		tb.Fatalf("saving test token: %v", err)
	}
}

// withGlobalConfig installs cfg for the length of the test.
func withGlobalConfig(tb *testing.T, cfg *Config) {
	tb.Helper()
	restore := GetGlobalConfig()
	tb.Cleanup(func() { SetGlobalConfig(restore) })
	SetGlobalConfig(cfg)
}

// tokenFixture is the global config a token test runs under: none at all, a
// storage dir with nothing saved, or one holding a saved AniList token.
type tokenFixture int

const (
	noConfig tokenFixture = iota
	emptyStorage
	storedAniList
)

const fixtureAniListToken = "stored-anilist-token"

func (f tokenFixture) install(t *testing.T) {
	t.Helper()
	if f == noConfig {
		withGlobalConfig(t, nil)
		return
	}
	storage := t.TempDir()
	if f == storedAniList {
		writeAnilistTokenFileForTest(t, storage, fixtureAniListToken)
	}
	withGlobalConfig(t, &Config{StoragePath: storage})
}

func TestAnilistTokenForAPI(t *testing.T) {
	cases := []struct {
		name    string
		fixture tokenFixture
		held    string
		want    string
	}{
		// What the caller holds may be the MyAnimeList token; the saved
		// AniList one has to win over it.
		{"saved token wins", storedAniList, "mal-jwt-token", fixtureAniListToken},
		{"nothing saved", emptyStorage, "some-token", "some-token"},
		{"no config", noConfig, "some-token", "some-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.fixture.install(t)
			if got := anilistTokenForAPI(nil, tc.held); got != tc.want {
				t.Fatalf("anilistTokenForAPI(%q) = %q, want %q", tc.held, got, tc.want)
			}
		})
	}
}

func TestTryRenewAniListTokenForAPI(t *testing.T) {
	cases := []struct {
		name        string
		fixture     tokenFixture
		rejected    string
		wantToken   string
		wantRenewed bool
	}{
		// A rejected MyAnimeList token is swapped for the saved AniList one;
		// no browser sign-in.
		{"mal token rejected", storedAniList, "mal-jwt-token", fixtureAniListToken, true},
		{"empty token rejected", storedAniList, "", fixtureAniListToken, true},
		// Nothing to compare against and nobody to sign in: no renewal, no panic.
		{"no config", noConfig, "token", "token", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.fixture.install(t)
			token, renewed, err := tryRenewAniListTokenForAPI(tc.rejected, "invalid token")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if renewed != tc.wantRenewed || token != tc.wantToken {
				t.Fatalf("got (%q, %v), want (%q, %v)", token, renewed, tc.wantToken, tc.wantRenewed)
			}
		})
	}
}

func TestInvalidTokenDetection(t *testing.T) {
	if !isInvalidTokenResponse(401, nil) {
		t.Error("a 401 should count as a rejected token")
	}
	if !isInvalidTokenResponse(400, []byte(`{"error":"invalid_token"}`)) {
		t.Error("an invalid_token body should count as a rejected token")
	}
	if isInvalidTokenResponse(500, []byte("oops")) {
		t.Error("a plain 500 is not about the token")
	}
	if !isInvalidTokenError(errors.New("graphql errors: [{\"message\":\"Invalid token\"}]")) {
		t.Error("AniList's Invalid token message should be recognised")
	}
	if isInvalidTokenError(nil) {
		t.Error("nil is not a token error")
	}
}
