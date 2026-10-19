package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The bot shows as Do Not Disturb (red, like the Shu accent) and cycles
// through a few custom statuses. Discord's member list hides the text of
// Playing/Watching/Listening activities for bots, but shows a custom status,
// so every entry is one. Bots can't show rich presence images or buttons.

const presenceEvery = 2 * time.Minute

type presence struct {
	mu      sync.Mutex
	version string // latest release, "" until known
	next    int
	// custom returns the statuses staff set on the dashboard, if any.
	custom func() []string
}

// defaultStatuses are the built-in statuses, after the version one.
var defaultStatuses = []string{"Get it: otakase.xyverion.com", "Anime in your terminal", "/suggest an idea"}

func customStatus(text string) *discordgo.Activity {
	return &discordgo.Activity{Name: "Custom Status", Type: discordgo.ActivityTypeCustom, State: text}
}

// activities lists what the bot cycles through.
func (p *presence) activities() []*discordgo.Activity {
	p.mu.Lock()
	v := p.version
	p.mu.Unlock()
	list := []*discordgo.Activity{customStatus("Watching anime · /help")}
	if v != "" {
		list = append(list, customStatus("Otakase "+v+" is out"))
	}
	texts := defaultStatuses
	if p.custom != nil {
		if c := p.custom(); len(c) > 0 {
			texts = c
		}
	}
	for _, t := range texts {
		list = append(list, customStatus(t))
	}
	return list
}

func (p *presence) show(s *discordgo.Session) {
	list := p.activities()
	p.mu.Lock()
	a := list[p.next%len(list)]
	p.next++
	p.mu.Unlock()
	if err := s.UpdateStatusComplex(discordgo.UpdateStatusData{Status: string(discordgo.StatusDoNotDisturb), Activities: []*discordgo.Activity{a}}); err != nil {
		log.Printf("updating status: %v", err)
	}
}

func (p *presence) refreshVersion(token string) {
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := getJSON(githubRequest("GET", "https://api.github.com/repos/TheXykril/otakase/releases/latest", token), &r); err != nil {
		log.Printf("latest release: %v", err)
		return
	}
	p.mu.Lock()
	p.version = strings.TrimPrefix(r.TagName, "v")
	p.mu.Unlock()
}

// run cycles the activity and checks for a new release every hour.
func (p *presence) run(s *discordgo.Session, token string) {
	p.refreshVersion(token)
	tick := time.NewTicker(presenceEvery)
	checked := time.Now()
	for range tick.C {
		if time.Since(checked) > time.Hour {
			p.refreshVersion(token)
			checked = time.Now()
		}
		p.show(s)
	}
}
