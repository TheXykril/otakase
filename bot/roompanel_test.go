package main

import (
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestParseRoomLimit(t *testing.T) {
	for in, want := range map[string]int{"0": 0, " 5 ": 5, "99": 99} {
		if n, ok := parseRoomLimit(in); !ok || n != want {
			t.Errorf("%q: %d %v", in, n, ok)
		}
	}
	for _, in := range []string{"", "-1", "100", "ten", "2.5"} {
		if _, ok := parseRoomLimit(in); ok {
			t.Errorf("%q should be refused", in)
		}
	}
}

func TestLongestPresent(t *testing.T) {
	now := time.Now()
	joined := map[string]time.Time{"a": now, "b": now.Add(-time.Minute), "c": now.Add(time.Minute)}
	if got := longestPresent([]string{"a", "b", "c"}, joined); got != "b" {
		t.Errorf("got %q", got)
	}
	// No join time: there since before a restart.
	if got := longestPresent([]string{"a", "d"}, joined); got != "d" {
		t.Errorf("unknown first: %q", got)
	}
	if got := longestPresent(nil, joined); got != "" {
		t.Errorf("empty: %q", got)
	}
}

func TestRoomJoins(t *testing.T) {
	var j roomJoins
	t0 := time.Now()
	j.join("c", "a", t0)
	j.join("c", "b", t0.Add(time.Second))
	j.join("c", "a", t0.Add(time.Hour)) // rejoining event keeps the first time
	if got := j.times("c")["a"]; !got.Equal(t0) {
		t.Errorf("a joined %v", got)
	}
	j.leave("c", "a")
	j.leave("c", "b")
	if len(j.times("c")) != 0 || len(j.at) != 0 {
		t.Errorf("not cleared: %v", j.at)
	}
}

func TestRoomStateAndPanel(t *testing.T) {
	c := &discordgo.Channel{UserLimit: 4, PermissionOverwrites: []*discordgo.PermissionOverwrite{
		{ID: "g", Type: discordgo.PermissionOverwriteTypeRole, Deny: discordgo.PermissionVoiceConnect | discordgo.PermissionVoiceSpeak},
	}}
	st := roomStateOf(c, "g", "o")
	if !st.Locked || st.Hidden || st.Limit != 4 || st.Owner != "o" {
		t.Fatalf("state %+v", st)
	}
	e, comps := roomPanel(st)
	row := comps[0].(discordgo.ActionsRow).Components
	if row[1].(discordgo.Button).Label != "Unlock" || row[3].(discordgo.Button).Label != "Hide" {
		t.Errorf("labels %v %v", row[1], row[3])
	}
	if e.Fields[1].Value != "Locked" || e.Fields[3].Value != "4 people" {
		t.Errorf("fields %v %v", e.Fields[1].Value, e.Fields[3].Value)
	}
	if st := roomStateOf(&discordgo.Channel{}, "g", "o"); st.Locked || st.Hidden {
		t.Errorf("no overwrite: %+v", st)
	}
}

func TestAdultRoomPanel(t *testing.T) {
	c := &discordgo.Channel{NSFW: true, PermissionOverwrites: roomOverwrites("g", "bot", "o", nil, true)}
	st := roomStateOf(c, "g", "o")
	if !st.Adult || !st.Locked || !st.Hidden {
		t.Fatalf("state %+v", st)
	}
	e, comps := roomPanel(st)
	row := comps[0].(discordgo.ActionsRow).Components
	if !row[1].(discordgo.Button).Disabled || !row[3].(discordgo.Button).Disabled {
		t.Errorf("lock and hide should be off in an 18+ room: %v %v", row[1], row[3])
	}
	if e.Fields[1].Value != "Invite only" || e.Fields[2].Value != "Hidden, 18+" {
		t.Errorf("fields %v %v", e.Fields[1].Value, e.Fields[2].Value)
	}
	if b := comps[1].(discordgo.ActionsRow).Components[0].(discordgo.Button); b.CustomID != "rc-invite" {
		t.Errorf("invite button: %+v", b)
	}
	_, comps = roomPanel(roomState{Owner: "o"})
	if comps[0].(discordgo.ActionsRow).Components[1].(discordgo.Button).Disabled {
		t.Error("lock disabled in a normal room")
	}
}

func TestRoomPrivacyChoices(t *testing.T) {
	var vals []any
	for _, c := range roomCommands()[0].Options[0].Choices {
		vals = append(vals, c.Value)
	}
	if len(vals) != 3 || vals[2] != roomAdult {
		t.Errorf("choices %v", vals)
	}
}
