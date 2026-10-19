package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestPartyReminderAt(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	if got := partyReminderAt(now, now.Add(15*time.Minute)); !got.Equal(now.Add(10 * time.Minute)) {
		t.Errorf("15 min ahead: %v", got)
	}
	if got := partyReminderAt(now, now.Add(5*time.Minute)); !got.Equal(now) {
		t.Errorf("5 min ahead: %v", got)
	}
	if got := partyReminderAt(now, now.Add(2*time.Minute)); !got.Equal(now.Add(2 * time.Minute)) {
		t.Errorf("short notice should remind at the start: %v", got)
	}
}

func TestToggleRSVP(t *testing.T) {
	p := &watchParty{}
	if !toggleRSVP(p, "a", true) || !slices.Equal(p.Going, []string{"a"}) {
		t.Fatalf("join: %+v", p)
	}
	if !toggleRSVP(p, "a", false) || len(p.Going) != 0 || !slices.Equal(p.Out, []string{"a"}) {
		t.Fatalf("switch to out: %+v", p)
	}
	if toggleRSVP(p, "a", false) || len(p.Out) != 0 {
		t.Fatalf("toggle out off: %+v", p)
	}
	toggleRSVP(p, "a", true)
	toggleRSVP(p, "b", true)
	if toggleRSVP(p, "a", true) || !slices.Equal(p.Going, []string{"b"}) {
		t.Fatalf("toggle in off: %+v", p)
	}
}

func TestGoingList(t *testing.T) {
	if got := goingList(nil, 20); !strings.Contains(got, "Nobody") {
		t.Errorf("empty: %q", got)
	}
	if got := goingList([]string{"1", "2"}, 20); got != "<@1> <@2>" {
		t.Errorf("two: %q", got)
	}
	var many []string
	for k := 0; k < 23; k++ {
		many = append(many, "x")
	}
	got := goingList(many, 20)
	if strings.Count(got, "<@x>") != 20 || !strings.HasSuffix(got, "+3 more") {
		t.Errorf("many: %q", got)
	}
}

func TestPartyPostButtons(t *testing.T) {
	_, comps := partyPost("ev1", watchParty{Show: "Frieren", Link: "https://discord.com/events/g/ev1", Going: []string{"h"}, Out: []string{"z"}})
	var ids []string
	for _, c := range comps[0].(discordgo.ActionsRow).Components {
		if b := c.(discordgo.Button); b.CustomID != "" {
			ids = append(ids, b.CustomID)
		}
	}
	if !slices.Equal(ids, []string{"party-in:ev1", "party-out:ev1", "party-cancel:ev1"}) {
		t.Errorf("buttons: %v", ids)
	}
}
