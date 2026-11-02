package cast

import (
	"fmt"
	"strings"
	"time"
)

// Media is one stream for PlayMedia: unlike Play, its address is used as it
// is, and the device is told what it is.
type Media struct {
	URL string
	// ContentType is the stream's media type, such as
	// application/x-mpegURL for a playlist or video/mp4 for a whole file.
	ContentType string
	// Title is what the device shows while it plays.
	Title string
	// Start is where to begin, in seconds. Devices that cannot start at a
	// point are seeked there once they play.
	Start float64
	// Subtitles is the address of a subtitle file, for devices that load
	// one beside the stream. Empty for none.
	Subtitles string
}

// MediaPlayer is a Player that can play a stream it is told the type of.
// Every device kind otakase casts to is one.
type MediaPlayer interface {
	Player
	PlayMedia(m Media) error
}

// HLSContentType is the type a receiver is told an HLS playlist is.
const HLSContentType = "application/x-mpegURL"

// PlayMedia loads m on a Chromecast, starting at m.Start.
func (s *Session) PlayMedia(m Media) error {
	contentType := m.ContentType
	if contentType == "" {
		contentType = HLSContentType
	}
	err := playWithin(func(u string) error {
		return s.app.Load(u, int(m.Start), contentType, false, true, false)
	}, m.URL, DefaultLaunchTimeout)
	if err == nil {
		s.loaded, s.loadedAt = m.URL, time.Now()
	}
	return err
}

// PlayMedia loads m on a DLNA renderer. Renderers here play a transport
// stream or a whole file; the caller seeks to m.Start once it plays.
func (p *dlnaPlayer) PlayMedia(m Media) error {
	info := dlnaProtocolInfo
	if m.ContentType != "" && m.ContentType != "video/mp2t" {
		info = "http-get:*:" + m.ContentType + ":DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000"
	}
	title := m.Title
	if strings.TrimSpace(title) == "" {
		title = "otakase"
	}
	return p.playURI(m.URL, info, title)
}

// PlayMedia opens m on Kodi and adds its subtitles, which Kodi 20 and later
// load beside any stream.
func (p *kodiPlayer) PlayMedia(m Media) error {
	p.mu.Lock()
	p.playing = false
	p.mu.Unlock()
	if err := p.call("Player.Open", map[string]any{"item": map[string]any{"file": m.URL}}, nil); err != nil {
		return err
	}
	if m.Subtitles != "" {
		go p.addSubtitles(m.Subtitles)
	}
	return nil
}

// addSubtitles waits for Kodi to start the stream, then loads the subtitles.
// An older Kodi without Player.AddSubtitle plays on without them.
func (p *kodiPlayer) addSubtitles(address string) {
	for range 30 {
		id, err := p.videoPlayer()
		if err == nil && id >= 0 {
			if err := p.call("Player.AddSubtitle", map[string]any{"playerid": id, "subtitle": address}, nil); err != nil {
				Log(fmt.Sprintf("cast: %s did not take the subtitles: %v", p.name, err))
			}
			return
		}
		time.Sleep(time.Second)
	}
}

// PlayMedia loads m in the browser page, which takes its subtitles itself.
func (p *BrowserPlayer) PlayMedia(m Media) error {
	if m.Subtitles != "" {
		p.media.Subtitles = m.Subtitles
	}
	if m.Title != "" {
		p.media.Title = m.Title
	}
	if err := p.PlayFrom(m.URL, 0); err != nil {
		return err
	}
	if m.Start > 0 {
		return p.SeekWithin(m.Start)
	}
	return nil
}
