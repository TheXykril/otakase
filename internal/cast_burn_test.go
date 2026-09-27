package internal

import "testing"

// A stream that carries a subtitle track gets it burned in; that is the only
// question. The audio mode used to gate this too, and it was removed because
// nothing verifies it: the mode is the one that was requested, not the one the
// host served, so a dub request for a show with no dub arrived as sub audio
// labelled dub and skipped the subtitles it needed.
func TestCastShouldBurnSubtitles(t *testing.T) {
	withSubs := func(mode string) *Anime {
		a := &Anime{}
		a.Ep.SubtitleURL = "https://host.test/subs.ass"
		a.Ep.Mode = mode
		return a
	}
	on := &Config{CastBurnSubtitles: true, SubOrDub: "sub"}

	for _, tc := range []struct {
		name  string
		anime *Anime
		cfg   *Config
		want  bool
	}{
		{"subbed audio with subtitles", withSubs("sub"), on, true},
		// Burned even though the mode says dub: the mode is a request, and a
		// dub request for a show without one returns sub audio under this label.
		// A wasted re-encode on a real dub is the lesser failure, and
		// CastBurnSubtitles turns it off.
		{"a dub label does not stop it, because the label is unverified", withSubs("dub"), on, true},
		{"turned off in the config", withSubs("sub"), &Config{CastBurnSubtitles: false, SubOrDub: "sub"}, false},
		{"nothing to burn", &Anime{}, on, false},
		{"a configured dub preference does not stop it either", withSubs(""), &Config{CastBurnSubtitles: true, SubOrDub: "dub"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := castShouldBurnSubtitles(tc.cfg, tc.anime); got != tc.want {
				t.Errorf("castShouldBurnSubtitles = %v, want %v", got, tc.want)
			}
		})
	}
}
