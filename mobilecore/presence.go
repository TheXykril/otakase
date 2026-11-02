package mobilecore

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tr1xem/go-discordrpc/client"
)

// discordAppID is the Discord application a profile shows as
// "Watching Otakase".
const discordAppID = "1556589176164589578"

// Discord fetches presence images itself, so they must be public URLs. They
// are read from main, so a new image works without a site deploy.
const (
	discordImageBase  = "https://raw.githubusercontent.com/TheXykril/otakase/main/site/img/"
	discordIconImage  = discordImageBase + "icon-512.png"
	discordMarkImage  = discordImageBase + "mark-256.png"
	discordPauseImage = discordImageBase + "pause-256.png"
)

// presenceRequest is what the desktop app shows in the viewer's Discord
// status while something plays.
type presenceRequest struct {
	Title    string           `json:"title"`
	State    string           `json:"state"`
	Cover    string           `json:"cover"`
	Paused   bool             `json:"paused"`
	Position float64          `json:"position"`
	Duration float64          `json:"duration"`
	Buttons  []presenceButton `json:"buttons"`
}

type presenceButton struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// presence talks to the Discord app running on this computer over its local
// IPC socket or pipe. There is none on Android; there every update fails
// quietly.
type presence struct {
	mu sync.Mutex
	// open dials Discord; replaced in tests.
	open func() (presenceClient, error)
	conn presenceClient
}

type presenceClient interface {
	SetActivity(client.Activity) error
	Logout() error
}

func dialDiscord() (presenceClient, error) {
	rpc := client.NewClient(discordAppID)
	if err := rpc.Login(); err != nil {
		return nil, fmt.Errorf("discord is not running")
	}
	return rpc, nil
}

func (p *presence) set(req presenceRequest) error {
	activity, err := presenceActivity(req, time.Now())
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	open := p.open
	if open == nil {
		open = dialDiscord
	}
	for attempt := 0; attempt < 2; attempt++ {
		if p.conn == nil {
			conn, err := open()
			if err != nil {
				return err
			}
			p.conn = conn
		}
		if err = p.conn.SetActivity(activity); err == nil {
			return nil
		}
		// Discord restarted or closed the socket: dial once more.
		_ = p.conn.Logout()
		p.conn = nil
	}
	return err
}

// clear removes the status: Discord drops an activity when its socket
// closes.
func (p *presence) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		_ = p.conn.Logout()
		p.conn = nil
	}
}

func presenceActivity(req presenceRequest, now time.Time) (client.Activity, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 128 || len(req.State) > 128 {
		return client.Activity{}, fmt.Errorf("a title of 1 to 128 bytes is required")
	}
	if len(req.Buttons) > 2 {
		return client.Activity{}, fmt.Errorf("Discord shows two buttons at most")
	}
	var buttons []*client.Button
	for _, b := range req.Buttons {
		u, err := url.Parse(b.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || len(b.URL) > 512 || b.Label == "" || len(b.Label) > 32 {
			return client.Activity{}, fmt.Errorf("a button needs a short label and an https link")
		}
		buttons = append(buttons, &client.Button{Label: b.Label, Url: b.URL})
	}
	cover := req.Cover
	if u, err := url.Parse(cover); err != nil || u.Scheme != "https" || len(cover) > 512 {
		cover = discordIconImage
	}
	position := max(0, req.Position)
	started := now.Add(-time.Duration(position * float64(time.Second)))
	times := &client.Timestamps{Start: &started}
	if !req.Paused && req.Duration > 60 && req.Duration > position {
		ends := now.Add(time.Duration((req.Duration - position) * float64(time.Second)))
		times.End = &ends
	}
	badge, badgeText := discordMarkImage, "Otakase"
	if req.Paused {
		badge, badgeText = discordPauseImage, "Paused"
	}
	return client.Activity{
		Type:       3, // Watching
		Name:       title,
		Details:    title,
		State:      req.State,
		LargeImage: cover,
		LargeText:  title,
		SmallImage: badge,
		SmallText:  badgeText,
		Timestamps: times,
		Buttons:    buttons,
	}, nil
}
