package internal

import "testing"

// Casting never reached main's skip-time goroutine: StartPlayback runs the
// whole cast inline, returns no socket path, and main exits on that before the
// goroutine is started. So an episode cast with no times already resolved got
// none at all -- nothing skipped, and pressing s correctly reported that
// nothing was known.
func TestEnsureCastSkipTimesResolvesWhenEmpty(t *testing.T) {
	anime := &Anime{AnilistId: 154587}
	anime.Ep.Number = 1

	called := false
	resolver := func(a *Anime, episode int, config *Config, provider any) {
		called = true
		a.Ep.SkipTimes = SkipTimes{Op: Skip{Start: 90, End: 180}}
	}

	ensureCastSkipTimes(anime, &Config{}, resolver)

	if !called {
		t.Fatal("no skip times were resolved for a cast that had none")
	}
	if anime.Ep.SkipTimes.Op.End != 180 {
		t.Errorf("the resolved times were not applied: %+v", anime.Ep.SkipTimes)
	}
}

// The rofi handoff carries times in its session file, and a caller that
// already resolved them should not pay for a second lookup across four
// sources.
func TestEnsureCastSkipTimesKeepsWhatItWasGiven(t *testing.T) {
	anime := &Anime{}
	anime.Ep.SkipTimes = SkipTimes{Op: Skip{Start: 90, End: 180}}

	called := false
	ensureCastSkipTimes(anime, &Config{}, func(*Anime, int, *Config, any) { called = true })

	if called {
		t.Error("skip times were looked up again despite already being known")
	}
}

// Sending chapter markers to a player that is not running is a round trip to
// nowhere, and it logged an error every time a cast resolved its times.
func TestSendSkipTimesToMPVSkipsWithoutASocket(t *testing.T) {
	anime := &Anime{}
	anime.Ep.Player.SocketPath = ""

	if err := SendSkipTimesToMPV(anime); err != nil {
		t.Errorf("sending with no socket should be a no-op, got %v", err)
	}
}
