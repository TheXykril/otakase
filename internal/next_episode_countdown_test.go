package internal

import (
	"os"
	"strings"
	"testing"
)

func countdownAnime() *Anime {
	anime := &Anime{AnilistId: 21, TotalEpisodes: 24}
	anime.Ep.Number = 12
	anime.Ep.Duration = 1440
	anime.Ep.SkipTimes.Ed = Skip{Start: 1300, End: 1390}
	return anime
}

func TestCountdownStart(t *testing.T) {
	config := &Config{NextEpisodeCountdown: 5}

	if got := countdownStart(config, countdownAnime()); got != 1300 {
		t.Fatalf("with a known ending got %d, want its start", got)
	}

	unknown := countdownAnime()
	unknown.Ep.SkipTimes = SkipTimes{}
	if got := countdownStart(config, unknown); got != 1440-countdownFallbackLead {
		t.Fatalf("with no ending got %d", got)
	}

	cases := map[string]func(*Config, *Anime){
		"off":             func(c *Config, a *Anime) { c.NextEpisodeCountdown = 0 },
		"ending skipped":  func(c *Config, a *Anime) { c.SkipEd = true },
		"last episode":    func(c *Config, a *Anime) { a.Ep.Number = 24 },
		"no duration yet": func(c *Config, a *Anime) { a.Ep.Duration = 0 },
		"short clip": func(c *Config, a *Anime) {
			a.Ep.Duration = 150
			a.Ep.SkipTimes = SkipTimes{}
		},
	}
	for name, change := range cases {
		c, a := &Config{NextEpisodeCountdown: 5}, countdownAnime()
		change(c, a)
		if got := countdownStart(c, a); got != 0 {
			t.Errorf("%s: got %d, want no countdown", name, got)
		}
	}
}

func TestCountdownTick(t *testing.T) {
	s := &countdownState{startedAt: -1}

	if action, _ := s.tick("21:12", 1300, 1200, 5, ""); action != countdownIdle {
		t.Fatalf("before the ending: %v", action)
	}
	action, remaining := s.tick("21:12", 1300, 1300, 5, "")
	if action != countdownShow || remaining != 5 {
		t.Fatalf("at the ending: %v %d", action, remaining)
	}
	// Paused: the position does not move, so neither does the countdown.
	if _, remaining := s.tick("21:12", 1300, 1300, 5, ""); remaining != 5 {
		t.Fatalf("paused countdown moved to %d", remaining)
	}
	if _, remaining := s.tick("21:12", 1300, 1303, 5, ""); remaining != 2 {
		t.Fatalf("three seconds in: %d", remaining)
	}
	if action, _ := s.tick("21:12", 1300, 1305, 5, ""); action != countdownAdvance {
		t.Fatalf("at zero: %v", action)
	}
	// Once done it stays done for this episode.
	if action, _ := s.tick("21:12", 1300, 1306, 5, ""); action != countdownIdle {
		t.Fatalf("after moving on: %v", action)
	}
	// The next episode starts over.
	if action, _ := s.tick("21:13", 1300, 1300, 5, ""); action != countdownShow {
		t.Fatalf("next episode: %v", action)
	}
}

func TestCountdownAnswers(t *testing.T) {
	s := &countdownState{startedAt: -1}
	s.tick("a", 100, 100, 5, "")
	if action, _ := s.tick("a", 100, 101, 5, "play"); action != countdownAdvance {
		t.Fatalf("Enter: %v", action)
	}

	s = &countdownState{startedAt: -1}
	s.tick("a", 100, 100, 5, "")
	if action, _ := s.tick("a", 100, 101, 5, "cancel"); action != countdownHide {
		t.Fatalf("Esc: %v", action)
	}
	if action, _ := s.tick("a", 100, 110, 5, ""); action != countdownIdle {
		t.Fatalf("after Esc the credits play on, got %v", action)
	}
	if !s.declinedFor("a") {
		t.Fatal("Esc should end playback with this episode")
	}
	if s.declinedFor("b") {
		t.Fatal("Esc on one episode declined another")
	}
}

// Leaving a declined episode for another releases the held last frame once,
// and the new episode counts down as usual.
func TestCountdownDeclineIsPerEpisode(t *testing.T) {
	s := &countdownState{startedAt: -1}
	s.tick("a", 100, 100, 5, "")
	s.tick("a", 100, 101, 5, "cancel")
	if s.enter("a") {
		t.Fatal("still on the declined episode, nothing to release")
	}
	if !s.enter("b") {
		t.Fatal("moving on should release the hold")
	}
	if s.enter("b") {
		t.Fatal("the hold was released twice")
	}
	if s.declinedFor("b") {
		t.Fatal("the new episode starts undeclined")
	}
	if action, _ := s.tick("b", 100, 100, 5, ""); action != countdownShow {
		t.Fatalf("new episode countdown: %v", action)
	}
}

func TestCountdownSeekBackHides(t *testing.T) {
	s := &countdownState{startedAt: -1}
	s.tick("a", 100, 102, 5, "")
	if action, _ := s.tick("a", 100, 50, 5, ""); action != countdownHide {
		t.Fatalf("seek back: %v", action)
	}
	if _, remaining := s.tick("a", 100, 100, 5, ""); remaining != 5 {
		t.Fatalf("countdown did not start over, remaining %d", remaining)
	}
}

func TestCountdownScriptArgs(t *testing.T) {
	dir := t.TempDir()
	args := countdownScriptArgs(&Config{StoragePath: dir, NextEpisodeCountdown: 5})
	if len(args) != 1 || !strings.HasPrefix(args[0], "--script=") {
		t.Fatalf("args = %v", args)
	}
	body, err := os.ReadFile(strings.TrimPrefix(args[0], "--script="))
	if err != nil || !strings.Contains(string(body), countdownProperty) || !strings.Contains(string(body), countdownAnswerProperty) {
		t.Fatalf("script missing or wrong: %v", err)
	}
	if args := countdownScriptArgs(&Config{StoragePath: dir}); args != nil {
		t.Fatalf("countdown off still loads the script: %v", args)
	}
}
