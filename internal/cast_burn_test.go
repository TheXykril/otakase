package internal

import "testing"

// Burning subtitles onto a dub is a re-encode that draws text nobody asked
// for. The stream carries a subtitle URL either way -- whether it is wanted
// depends on what is actually being listened to.
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
		{"dubbed audio needs no subtitles", withSubs("dub"), on, false},
		{"turned off in the config", withSubs("sub"), &Config{CastBurnSubtitles: false, SubOrDub: "sub"}, false},
		{"nothing to burn", &Anime{}, on, false},
		{"mode unset falls back to the configured audio", withSubs(""), &Config{CastBurnSubtitles: true, SubOrDub: "dub"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := castShouldBurnSubtitles(tc.cfg, tc.anime); got != tc.want {
				t.Errorf("castShouldBurnSubtitles = %v, want %v", got, tc.want)
			}
		})
	}
}
