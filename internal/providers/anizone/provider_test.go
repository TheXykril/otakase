package anizone

import (
	"strings"
	"testing"
)

// The payload is escaped to different depths depending on where it sits: the
// player's configuration once, the search results again on top of that because
// they live inside a Livewire snapshot attribute. Decoding has to survive both
// without caring which it was handed.
func TestDecodeHandlesEveryEscapingDepth(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"html entities", `&quot;src&quot;`, `"src"`},
		{"unicode-escaped quotes", `"src"`, `"src"`},
		{"singly escaped slash", `http:\/\/anizone.to`, `http://anizone.to`},
		{"doubly escaped slash", `http:\\\/\\\/anizone.to`, `http://anizone.to`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := decode(tc.in); got != tc.want {
				t.Errorf("decode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The subtitle tracks arrive in whatever order the site lists them -- Arabic
// first, as it happens -- so the English one has to be chosen rather than
// taken from the front.
func TestPreferredSubtitlePicksEnglish(t *testing.T) {
	player := playerConfig{}
	player.Subtitles = append(player.Subtitles,
		struct {
			Title    string `json:"title"`
			Format   string `json:"format"`
			Language string `json:"language"`
			Default  bool   `json:"default"`
			File     string `json:"file"`
		}{Title: "Arabic (Saudi Arabia)", Language: "ar", File: "https://cdn.test/0_ar.ass"},
		struct {
			Title    string `json:"title"`
			Format   string `json:"format"`
			Language string `json:"language"`
			Default  bool   `json:"default"`
			File     string `json:"file"`
		}{Title: "English", Language: "en", File: "https://cdn.test/3_en.ass"},
	)

	if got := (&Provider{}).preferredSubtitle(player); !strings.HasSuffix(got, "3_en.ass") {
		t.Errorf("preferredSubtitle = %q, want the English track", got)
	}
}

// A show with no subtitle track at all must resolve to no track rather than to
// a track that does not exist.
func TestPreferredSubtitleToleratesNone(t *testing.T) {
	if got := (&Provider{}).preferredSubtitle(playerConfig{}); got != "" {
		t.Errorf("preferredSubtitle = %q, want empty", got)
	}
}

// A page with no player is a page this provider cannot use, and it has to say
// so rather than hand back an empty stream the caller would try to play.
func TestPlayerConfigRequiresAPlayer(t *testing.T) {
	if playerConfigRE.MatchString("<html>nothing here</html>") {
		t.Error("the player pattern matched a page with no player on it")
	}
}

func TestHasEnglishAudio(t *testing.T) {
	cases := map[string]bool{
		// A plain stream: Japanese audio muxed in, nothing to choose.
		"#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nv.m3u8\n":                                                                                             false,
		"#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"Japanese\",LANGUAGE=\"ja\"\n":                                                          false,
		"#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"Japanese\",LANGUAGE=\"ja\"\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",LANGUAGE=\"eng\"\n": true,
		"#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"English\"\n":                                                                           true,
		"#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",LANGUAGE=\"en-US\"\n":                                                                         true,
		// English subtitles are not an English dub.
		"#EXTM3U\n#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"s\",NAME=\"English\",LANGUAGE=\"en\"\n": false,
	}
	for master, want := range cases {
		if got := hasEnglishAudio(master); got != want {
			t.Errorf("hasEnglishAudio(%q) = %v, want %v", master, got, want)
		}
	}
}
