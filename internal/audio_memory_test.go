package internal

import (
	"sync"
	"testing"
)

func resetConfiguredAudio() {
	configuredAudio.once = sync.Once{}
	configuredAudio.mode = ""
}

func TestApplyShowAudioMode(t *testing.T) {
	resetConfiguredAudio()
	t.Cleanup(resetConfiguredAudio)
	dir := t.TempDir()
	config := &Config{StoragePath: dir, SubOrDub: "sub"}

	rememberShowAudioMode(dir, 7, "dub")

	dubbed := &Anime{AnilistId: 7}
	applyShowAudioMode(config, dubbed)
	if config.SubOrDub != "dub" {
		t.Fatalf("remembered show plays %q", config.SubOrDub)
	}

	// The next show in the same run gets the configured audio back, not the
	// last show's.
	applyShowAudioMode(config, &Anime{AnilistId: 8})
	if config.SubOrDub != "sub" {
		t.Fatalf("other show plays %q", config.SubOrDub)
	}

	// -sub for this run wins over the memory.
	config.SubOrDubFlag = true
	applyShowAudioMode(config, dubbed)
	if config.SubOrDub != "sub" {
		t.Fatalf("flag overridden: %q", config.SubOrDub)
	}
}

func TestSwitchCastAudio(t *testing.T) {
	dir := t.TempDir()
	previous := switchAudioResolver
	t.Cleanup(func() { switchAudioResolver = previous })

	var sawFallback bool
	switchAudioResolver = func(config *Config, anime *Anime) bool {
		sawFallback = config.AutoAudioFallback
		anime.Ep.Mode = config.SubOrDub
		anime.Ep.Links = []string{"https://cdn.test/" + config.SubOrDub + ".m3u8"}
		return true
	}
	config := &Config{StoragePath: dir, SubOrDub: "sub"}
	anime := &Anime{AnilistId: 9}
	anime.Ep.Number = 4
	anime.Ep.Mode = "sub"
	anime.Ep.Links = []string{"https://cdn.test/sub.m3u8"}

	switchCastAudio(config, anime, 312.4)
	if !sawFallback {
		t.Fatal("the lookup could have prompted under the cast panel")
	}
	if anime.Ep.Mode != "dub" || config.SubOrDub != "dub" || anime.Ep.Links[0] != "https://cdn.test/dub.m3u8" {
		t.Fatalf("not switched: mode=%q config=%q links=%v", anime.Ep.Mode, config.SubOrDub, anime.Ep.Links)
	}
	if config.AutoAudioFallback {
		t.Fatal("AutoAudioFallback left changed")
	}
	if !anime.Ep.Resume || syncedResumeFor(anime) != 312 {
		t.Fatalf("resume lost: resume=%v at=%d", anime.Ep.Resume, syncedResumeFor(anime))
	}
	if got := loadShowPrefs(dir)["9"].AudioMode; got != "dub" {
		t.Fatalf("not remembered: %q", got)
	}

	// No sub available: the lookup falls back to dub, which is no switch.
	switchAudioResolver = func(config *Config, anime *Anime) bool {
		anime.Ep.Mode = "dub"
		anime.Ep.Links = []string{"https://other.test/x.m3u8"}
		return true
	}
	switchCastAudio(config, anime, 400)
	if anime.Ep.Mode != "dub" || config.SubOrDub != "dub" || anime.Ep.Links[0] != "https://cdn.test/dub.m3u8" {
		t.Fatalf("a failed switch changed the episode: mode=%q links=%v", anime.Ep.Mode, anime.Ep.Links)
	}
	if syncedResumeFor(anime) != 400 {
		t.Fatalf("a failed switch lost the position: %d", syncedResumeFor(anime))
	}
	if got := loadShowPrefs(dir)["9"].AudioMode; got != "dub" {
		t.Fatalf("a failed switch was remembered: %q", got)
	}
}

func TestDecodeCastKeySwitchAudio(t *testing.T) {
	for _, key := range []string{"a", "A"} {
		if command, n := decodeCastKey([]byte(key)); command != castCmdSwitchAudio || n != 1 {
			t.Fatalf("%q decoded as %v/%d", key, command, n)
		}
	}
	stop, _, err := applyCastCommand(castCmdSwitchAudio, nil, new(bool), nil, 10)
	if !stop || err != nil {
		t.Fatalf("switch did not end the stream: %v %v", stop, err)
	}
}
