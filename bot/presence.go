package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The bot shows as Do Not Disturb (red, like the Shu accent) and cycles
// through a few activities. Bots can't show rich presence images or buttons,
// only an activity type and text.

const presenceEvery = 2 * time.Minute

type presence struct {
	mu      sync.Mutex
	version string // latest release, "" until known
	next    int
}

// activities lists what the bot cycles through.
func (p *presence) activities() []*discordgo.Activity {
	p.mu.Lock()
	v := p.version
	p.mu.Unlock()
	list := []*discordgo.Activity{
		{Name: "anime · /help", Type: discordgo.ActivityTypeWatching},
	}
	if v != "" {
		list = append(list, &discordgo.Activity{Name: "Otakase " + v, Type: discordgo.ActivityTypeGame})
	}
	list = append(list,
		&discordgo.Activity{Name: "Custom Status", Type: discordgo.ActivityTypeCustom, State: "otakase.xyverion.com"},
		&discordgo.Activity{Name: "your terminal", Type: discordgo.ActivityTypeListening},
	)
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
