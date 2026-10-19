package internal

import "testing"

// Burning follows the audio that actually plays, read from the stream, not the
// mode that was asked for: an English track needs no subtitles drawn over it,
// and a Japanese one does whatever the request said. Only when the stream does
// not say does the requested mode decide.
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
		audio string
		want  bool
	}{
		{"subbed audio with subtitles", withSubs("sub"), on, "", true},
		{"English audio is a dub whatever the label", withSubs("sub"), on, "en", false},
		{"Japanese audio under a dub label still gets them", withSubs("dub"), on, "ja", true},
		{"an unknown language on a dub request trusts the request", withSubs("dub"), on, "", false},
		{"the configured dub preference counts as the request", withSubs(""), &Config{CastBurnSubtitles: true, SubOrDub: "dub"}, "", false},
		{"turned off in the config", withSubs("sub"), &Config{CastBurnSubtitles: false, SubOrDub: "sub"}, "ja", false},
		{"nothing to burn", &Anime{}, on, "ja", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := castShouldBurnSubtitles(tc.cfg, tc.anime, tc.audio); got != tc.want {
				t.Errorf("castShouldBurnSubtitles = %v, want %v", got, tc.want)
			}
		})
	}
}
