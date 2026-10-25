package megavid

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/providers"
)

// Provider is the megavid source. Its show keys are MAL ids.
type Provider struct {
	client *client
}

func (p *Provider) Name() string { return "megavid" }

func (p *Provider) api() *client {
	if p.client == nil {
		p.client = newClient()
	}
	return p.client
}

// SearchAnime returns AniList matches keyed by MAL id. Each carries its MAL id
// as a SearchHit, so the app ties it to the list entry exactly instead of by
// title.
func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	shows, err := p.api().search(query)
	if err != nil {
		return nil, err
	}
	options := make([]providers.SelectionOption, 0, len(shows))
	for _, s := range shows {
		options = append(options, providers.SelectionOption{
			Key:       strconv.Itoa(s.MalID),
			Title:     s.Title,
			Label:     describe(s),
			ExtraData: providers.SearchHit{MalID: s.MalID, Episodes: s.Episodes},
		})
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("megavid: nothing found for %q", query)
	}
	return options, nil
}

func describe(s show) string {
	parts := []string{}
	if s.Format != "" {
		parts = append(parts, strings.ReplaceAll(s.Format, "_", " "))
	}
	if s.Year > 0 {
		parts = append(parts, strconv.Itoa(s.Year))
	}
	if len(parts) == 0 {
		return s.Title
	}
	return fmt.Sprintf("%s (%s)", s.Title, strings.Join(parts, ", "))
}

// EpisodesList lists the episodes that have aired. megavid has no listing of
// its own, so the count comes from AniList. A dub is only listed when the
// first episode really has one.
func (p *Provider) EpisodesList(showID, mode string) ([]string, error) {
	malID, err := parseID(showID)
	if err != nil {
		return nil, err
	}
	count, err := p.api().episodeCount(malID)
	if err != nil {
		return nil, err
	}
	if count <= 0 {
		return nil, fmt.Errorf("megavid: %d has no aired episodes", malID)
	}
	if normalizeLang(mode) == "dub" {
		if _, err := p.resolve(malID, 1, "dub"); err != nil {
			return nil, fmt.Errorf("megavid: no dub for %d: %w", malID, err)
		}
	}
	out := make([]string, count)
	for i := range out {
		out[i] = strconv.Itoa(i + 1)
	}
	return out, nil
}

func (p *Provider) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	urls, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, config.SubOrDub)
	return urls, err
}

func (p *Provider) GetEpisodeURLForMode(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, error) {
	urls, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, mode)
	return urls, err
}

// GetEpisodeURLForModeWithHints returns every copy of the episode megavid
// has, its own first, with the captions that come with each.
func (p *Provider) GetEpisodeURLForModeWithHints(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	malID, err := parseID(id)
	if err != nil {
		return nil, nil, err
	}
	lang := normalizeLang(mode)
	found, err := p.resolve(malID, epNo, lang)
	if err != nil {
		return nil, nil, err
	}
	urls := make([]string, 0, len(found))
	hints := make(map[string]providers.StreamPlaybackHint, len(found))
	for _, src := range found {
		urls = append(urls, src.Source)
		subtitle := preferredSubtitle(src.Tracks)
		if lang == "dub" {
			subtitle = "" // offered, not loaded: the audio is already English
		}
		hints[src.Source] = providers.StreamPlaybackHint{
			Referrer:  referer,
			Subtitle:  subtitle,
			Subtitles: subtitleTracks(src.Tracks),
			Headers:   map[string]string{"Referer": referer, "User-Agent": userAgent},
		}
	}
	return urls, hints, nil
}

// resolve fetches each copy of an episode in one language.
//
// megavid answers a dub request for a show that has no dub with the sub
// stream. That would play Japanese audio under a dub label, and a cast would
// then skip burning the subtitles it needs, so a dub whose stream is the sub
// one is refused.
func (p *Provider) resolve(malID, epNo int, lang string) ([]source, error) {
	var found []source
	var errs []string
	seen := map[string]bool{}
	for _, copyParam := range copies {
		src, err := p.api().source(malID, epNo, lang, copyParam)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if lang == "dub" {
			sub, err := p.api().source(malID, epNo, "sub", copyParam)
			if err == nil && streamIdentity(sub.Source) == streamIdentity(src.Source) {
				errs = append(errs, fmt.Sprintf("megavid %d episode %d: the dub is the sub stream", malID, epNo))
				continue
			}
		}
		if err := hostExists(src.Source); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if seen[streamIdentity(src.Source)] {
			continue
		}
		seen[streamIdentity(src.Source)] = true
		found = append(found, src)
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("megavid: no %s stream for %d episode %d: %s", lang, malID, epNo, strings.Join(errs, "; "))
	}
	return found, nil
}

// lookupHost is net.DefaultResolver.LookupHost, replaceable in tests.
var lookupHost = net.DefaultResolver.LookupHost

// hostExists refuses a stream on a host that has no DNS records. megavid's
// relayed copy names s-NN.megavid.se hosts that did not exist in October
// 2026; handing one to the player only delays the fallback. Any other lookup
// failure lets the stream through, for the player to try.
func hostExists(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = lookupHost(ctx, u.Hostname())
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return fmt.Errorf("megavid: stream host %s does not exist", u.Hostname())
	}
	return nil
}

// streamIdentity is what names the video in a stream URL. megavid's /vid/
// links carry the video in their first segment and a token, different on
// every request, in the rest; anything else is named by its whole path.
func streamIdentity(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	path := u.Path
	if rest, ok := strings.CutPrefix(path, "/vid/"); ok {
		video, _, _ := strings.Cut(rest, "/")
		path = "/vid/" + video
	}
	return u.Host + path
}

// subtitleTracks lists the caption tracks. megavid labels the original
// release's own track just "Subtitles"; it is English.
func subtitleTracks(tracks []track) []providers.SubtitleTrack {
	var out []providers.SubtitleTrack
	for _, t := range tracks {
		if !t.caption() {
			continue
		}
		label := strings.TrimSpace(t.Label)
		language := label
		if strings.EqualFold(label, "subtitles") || label == "" {
			language = "English"
		}
		out = append(out, providers.SubtitleTrack{URL: t.File, Language: language, Label: label})
	}
	return out
}

// preferredSubtitle is the track marked default, else the first English one,
// else the unnamed release track.
func preferredSubtitle(tracks []track) string {
	for _, t := range tracks {
		if t.Default && t.caption() {
			return t.File
		}
	}
	for _, t := range tracks {
		if t.caption() && strings.Contains(strings.ToLower(t.Label), "english") {
			return t.File
		}
	}
	for _, t := range tracks {
		if t.caption() && strings.EqualFold(strings.TrimSpace(t.Label), "subtitles") {
			return t.File
		}
	}
	return ""
}

// SkipRange reads the intro and outro from the chapters megavid sends with
// the stream.
func (p *Provider) SkipRange(id, mode string, epNo int) (intro, outro []int, err error) {
	malID, err := parseID(id)
	if err != nil {
		return nil, nil, err
	}
	found, err := p.resolve(malID, epNo, normalizeLang(mode))
	if err != nil {
		return nil, nil, err
	}
	for _, src := range found {
		for _, ch := range src.Chapters {
			title := strings.ToLower(ch.Title)
			span := validSpan(ch.Start, ch.End)
			switch {
			case span == nil:
			case intro == nil && (strings.Contains(title, "intro") || strings.Contains(title, "opening")):
				intro = span
			case outro == nil && (strings.Contains(title, "outro") || strings.Contains(title, "ending")):
				outro = span
			}
		}
	}
	return intro, outro, nil
}

// validSpan accepts only a well-formed [start, end] pair.
func validSpan(start, end float64) []int {
	s, e := int(math.Round(start)), int(math.Round(end))
	if s < 0 || e <= s {
		return nil
	}
	return []int{s, e}
}
