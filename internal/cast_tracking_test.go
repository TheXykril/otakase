package internal

import "testing"

// A cast handed to a spawned terminal reaches CastEpisode before anything has
// signed the child in: main returns into RunCastSession above the call to
// EnsureConfiguredTrackersReady, and SetGlobalUser has already stored a
// zero-value User. GetGlobalUser is therefore non-nil with an empty Token, so
// a nil check alone lets an unauthenticated mutation go to AniList -- it
// fails, the failure is only logged, and the viewer is told the episode was
// marked while the tracker never moves.
func TestShouldPushRemoteProgress(t *testing.T) {
	tracking := &Config{TrackingRemote: "anilist"}

	for _, tc := range []struct {
		name string
		cfg  *Config
		user *User
		want bool
	}{
		{"a signed-in user on a tracked config", tracking, &User{Token: "real-token"}, true},
		{"no user at all", tracking, nil, false},
		{"a user that was never signed in", tracking, &User{}, false},
		{"a token of only whitespace", tracking, &User{Token: "   "}, false},
		{"local-only tracking", &Config{TrackingRemote: "none"}, &User{Token: "real-token"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPushRemoteProgress(tc.cfg, tc.user); got != tc.want {
				t.Errorf("shouldPushRemoteProgress = %v, want %v", got, tc.want)
			}
		})
	}
}
