package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestSettingParse(t *testing.T) {
	hour, _ := findSetting("airing_hour_utc")
	dm, _ := findSetting("welcome_dm")
	th, _ := findSetting("match_threshold")
	cases := []struct {
		s       setting
		in, out string
		ok      bool
	}{
		{hour, "7", "7", true}, {hour, " 0 ", "0", true}, {hour, "23", "23", true},
		{hour, "24", "", false}, {hour, "-1", "", false}, {hour, "6.5", "", false},
		{dm, "on", "true", true}, {dm, "OFF", "false", true}, {dm, "yes", "true", true}, {dm, "maybe", "", false},
		{th, "0.75", "0.75", true}, {th, "1", "1", true}, {th, "0", "0", true}, {th, "1.2", "", false}, {th, "x", "", false},
	}
	for _, c := range cases {
		got, err := c.s.parse(c.in)
		if (err == nil) != c.ok || got != c.out {
			t.Errorf("%s.parse(%q) = %q, %v", c.s.Name, c.in, got, err)
		}
	}
}

func TestSettingsTable(t *testing.T) {
	if len(settings) > 25 {
		t.Fatal("Discord allows 25 choices")
	}
	seen := map[string]bool{}
	for _, s := range settings {
		if seen[s.Name] || s.Desc == "" || s.Default == nil {
			t.Errorf("setting %q: duplicate or incomplete", s.Name)
		}
		seen[s.Name] = true
		if _, err := s.parse(s.Default(config{AiringHour: 6, AIThreshold: 0.6})); err != nil {
			t.Errorf("%s default doesn't parse: %v", s.Name, err)
		}
	}
	for _, name := range []string{"daily_airing", "airing_hour_utc", "welcome_dm", "match_threshold"} {
		if !seen[name] {
			t.Errorf("setting %s is read by the bot but missing", name)
		}
	}
}

func TestSettingOverrides(t *testing.T) {
	b := &bot{cfg: config{AiringHour: 6, WelcomeDM: true, DailyAiring: true, AIThreshold: 0.6},
		store: openStore(filepath.Join(t.TempDir(), "s.json"))}
	if b.settingInt("airing_hour_utc") != 6 || !b.settingBool("welcome_dm") || b.settingFloat("match_threshold") != 0.6 {
		t.Fatal("defaults should come from the add-on options")
	}
	b.store.update(func(d *storeData) {
		d.Settings = map[string]string{"airing_hour_utc": "9", "welcome_dm": "false", "match_threshold": "nonsense"}
	})
	if v, changed := b.settingValue("airing_hour_utc"); v != "9" || !changed {
		t.Errorf("override = %q, %v", v, changed)
	}
	if b.settingBool("welcome_dm") {
		t.Error("welcome_dm override ignored")
	}
	if v, changed := b.settingValue("match_threshold"); v != "0.6" || changed {
		t.Errorf("a broken override should fall back to the default, got %q, %v", v, changed)
	}
	b.store.update(func(d *storeData) { delete(d.Settings, "airing_hour_utc") })
	if b.settingInt("airing_hour_utc") != 6 {
		t.Error("reset should go back to the add-on option")
	}
	if (&bot{cfg: config{DailyAiring: true}}).settingBool("daily_airing") != true {
		t.Error("no store should give the default")
	}
	line := settingLine(settings[0], "false", true, "true")
	if !strings.Contains(line, "`off`") || !strings.Contains(line, "add-on option: `on`") {
		t.Errorf("line = %q", line)
	}
}

func TestJobStatus(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	b := &bot{health: newHealth()}
	b.health.now = func() time.Time { return now }
	b.health.noteStart("Follow DMs")
	b.health.noteStart("Daily airing post")
	b.health.noteStop("Daily airing post")
	if l := jobLine("Follow DMs", b.health.jobsSnapshot()["Follow DMs"]); !strings.Contains(l, "nothing to report") {
		t.Errorf("fresh job: %q", l)
	}
	if l := jobLine("Daily airing post", b.health.jobsSnapshot()["Daily airing post"]); !strings.Contains(l, "off") {
		t.Errorf("stopped job: %q", l)
	}
	b.jobOK("Follow DMs")
	now = now.Add(time.Minute)
	b.health.noteFail("Follow DMs", errors.New("anilist said no"))
	l := jobLine("Follow DMs", b.health.jobsSnapshot()["Follow DMs"])
	if !strings.Contains(l, "failing") || !strings.Contains(l, "anilist said no") || !strings.Contains(l, "last ok") {
		t.Errorf("failing job: %q", l)
	}
	now = now.Add(time.Minute)
	b.health.noteOK("Follow DMs")
	if l := jobLine("Follow DMs", b.health.jobsSnapshot()["Follow DMs"]); strings.Contains(l, "failing") || strings.Contains(l, "anilist said no") {
		t.Errorf("recovered job: %q", l)
	}
}

func TestCountStore(t *testing.T) {
	d := &storeData{
		Follows:     map[string][]followedShow{"a": {{ID: 1}, {ID: 2}}, "b": {{ID: 1}}, "c": nil},
		AniList:     map[string]anilistLink{"a": {}},
		Suggestions: map[string]suggestion{"s": {}},
		Rooms:       map[string]room{"r": {}, "q": {}},
		Warnings:    map[string][]warning{"x": {{}, {}}, "y": {{}}},
	}
	got := countStore(d)
	want := storeStats{Follows: 3, Followers: 2, AniList: 1, Suggestions: 1, Rooms: 2, Warnings: 3, Warned: 2}
	if got != want {
		t.Errorf("countStore = %+v, want %+v", got, want)
	}
}

func TestFormats(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second:                               "1m",
		5 * time.Minute:                                "5m",
		2*time.Hour + 3*time.Minute:                    "2h 3m",
		50*time.Hour + 10*time.Minute:                  "2d 2h 10m",
		24*time.Hour + 59*time.Minute + 50*time.Second: "1d 1h 0m",
	} {
		if got := fmtUptime(d); got != want {
			t.Errorf("fmtUptime(%v) = %q, want %q", d, got, want)
		}
	}
	for n, want := range map[int64]string{512: "512 B", 2048: "2.0 KB", 3 << 20: "3.0 MB"} {
		if got := fmtBytes(n); got != want {
			t.Errorf("fmtBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDisableActions(t *testing.T) {
	comps := disableActions(boardMessage([]airing{{ID: 1, At: 100, Episode: 2, Title: "A"}}, time.Unix(0, 0), "", boardMarks{}))
	comps = append(comps, disableActions(supportButtons())...)
	comps = append(comps, disableActions([]discordgo.MessageComponent{episodeCard(sampleEpisode(nil, time.Unix(0, 0)))})...)
	var walk func(c discordgo.MessageComponent)
	links := 0
	walk = func(c discordgo.MessageComponent) {
		switch v := c.(type) {
		case discordgo.Button:
			if v.Style == discordgo.LinkButton {
				links++
				if v.Disabled {
					t.Error("link buttons should keep working")
				}
			} else if !v.Disabled {
				t.Errorf("button %q not disabled", v.Label)
			}
		case discordgo.SelectMenu:
			if !v.Disabled {
				t.Error("menu not disabled")
			}
		case discordgo.ActionsRow:
			for _, x := range v.Components {
				walk(x)
			}
		case discordgo.Container:
			for _, x := range v.Components {
				walk(x)
			}
		}
	}
	for _, c := range comps {
		walk(c)
	}
	if links < 2 {
		t.Errorf("want link buttons left, got %d", links)
	}
}

func TestSampleEpisode(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := sampleEpisode(nil, now)
	if a.Show.Title == "" || a.MediaID == 0 || a.At >= now.Unix() {
		t.Errorf("fixed sample = %+v", a)
	}
	a = sampleEpisode([]airing{{ID: 1, Title: "Small", Popularity: 5}, {ID: 2, Title: "Big", Popularity: 500, Episode: 3}}, now)
	if a.MediaID != 2 || a.Show.Title != "Big" || a.Episode != 3 {
		t.Errorf("want the most popular show, got %+v", a)
	}
}
