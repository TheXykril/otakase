package animex

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/providers"
)

// Provider is the animex source. Its show keys are AniList ids.
type Provider struct {
	client *client
}

func (p *Provider) Name() string { return "animex" }

func (p *Provider) api() *client {
	if p.client == nil {
		p.client = newClient()
	}
	return p.client
}

// SearchAnime returns AniList matches keyed by AniList id. Each carries its MAL
// id as a SearchHit, so the app ties it to the list entry exactly instead of
// by title.
func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	shows, err := p.api().search(query)
	if err != nil {
		return nil, err
	}
	options := make([]providers.SelectionOption, 0, len(shows))
	for _, s := range shows {
		options = append(options, providers.SelectionOption{
			Key:       strconv.Itoa(s.AnilistID),
			Title:     s.Title,
			Label:     describe(s),
			ExtraData: providers.SearchHit{MalID: s.MalID, Episodes: s.Episodes},
		})
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("animex: nothing found for %q", query)
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

// EpisodesList lists the episodes that have aired, counted by AniList. The
// show must be on animex, and a dub is only listed when the first episode
// really has one.
func (p *Provider) EpisodesList(showID, mode string) ([]string, error) {
	anilistID, err := parseID(showID)
	if err != nil {
		return nil, err
	}
	slug, err := p.api().slug(anilistID)
	if err != nil {
		return nil, err
	}
	count, err := p.api().episodeCount(anilistID)
	if err != nil {
		return nil, err
	}
	if count <= 0 {
		return nil, fmt.Errorf("animex: %d has no aired episodes", anilistID)
	}
	if normalizeLang(mode) == "dub" {
		if _, err := p.resolve(slug, 1, "dub"); err != nil {
			return nil, fmt.Errorf("animex: no dub for %d: %w", anilistID, err)
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

// GetEpisodeURLForModeWithHints returns the episode from each server that has
// it, with the headers its host asked for and its captions.
func (p *Provider) GetEpisodeURLForModeWithHints(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	anilistID, err := parseID(id)
	if err != nil {
		return nil, nil, err
	}
	slug, err := p.api().slug(anilistID)
	if err != nil {
		return nil, nil, err
	}
	lang := normalizeLang(mode)
	found, err := p.resolve(slug, epNo, lang)
	if err != nil {
		return nil, nil, err
	}
	urls := make([]string, 0, len(found))
	hints := make(map[string]providers.StreamPlaybackHint, len(found))
	for _, src := range found {
		urls = append(urls, src.Stream)
		subtitle := preferredSubtitle(src.Tracks)
		if lang == "dub" {
			subtitle = "" // offered, not loaded: the audio is already English
		}
		headers := map[string]string{"User-Agent": userAgent}
		for k, v := range src.Headers {
			if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
				headers[k] = v
			}
		}
		hints[src.Stream] = providers.StreamPlaybackHint{
			Referrer:  headers["Referer"],
			Subtitle:  subtitle,
			Subtitles: subtitleTracks(src.Tracks),
			Headers:   headers,
			HLS:       !strings.HasSuffix(strings.ToLower(streamPath(src.Stream)), ".m3u8"),
		}
	}
	return urls, hints, nil
}

// resolve asks each server that lists the episode in the language.
//
// A dub whose stream is the server's sub stream is refused: it would play
// Japanese audio under a dub label, and a cast would then skip burning the
// subtitles it needs.
func (p *Provider) resolve(slug string, epNo int, lang string) ([]source, error) {
	list, err := p.api().serverList(slug, epNo)
	if err != nil {
		return nil, err
	}
	offered := list.Sub
	if lang == "dub" {
		offered = list.Dub
	}
	var found []source
	var errs []string
	for _, server := range servers {
		if !slices.Contains(offered, server) {
			continue
		}
		src, err := p.api().source(slug, epNo, lang, server)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if lang == "dub" {
			sub, err := p.api().source(slug, epNo, "sub", server)
			if err == nil && streamIdentity(sub.Stream) == streamIdentity(src.Stream) {
				errs = append(errs, fmt.Sprintf("animex %s episode %d (%s): the dub is the sub stream", slug, epNo, server))
				continue
			}
		}
		found = append(found, src)
	}
	if len(found) == 0 {
		why := "no server carries it"
		if len(errs) > 0 {
			why = strings.Join(errs, "; ")
		}
		return nil, fmt.Errorf("animex: no %s stream for %s episode %d: %s", lang, slug, epNo, why)
	}
	return found, nil
}

// streamIdentity is a stream URL without its query, which carries a token
// that changes on every answer.
func streamIdentity(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Host + u.Path
}

func streamPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Path
}

// subtitleTracks lists the caption tracks, named by language.
func subtitleTracks(tracks []track) []providers.SubtitleTrack {
	var out []providers.SubtitleTrack
	for _, t := range tracks {
		if !t.caption() {
			continue
		}
		label := strings.TrimSpace(t.Label)
		if label == "" {
			label = strings.TrimSpace(t.Lang)
		}
		out = append(out, providers.SubtitleTrack{URL: t.URL, Language: language(label), Label: label})
	}
	return out
}

// language is the first word of a label such as "English (English - [Full])"
// or "Chinese (- Simplified)", which is how animex names its tracks.
func language(label string) string {
	name, _, _ := strings.Cut(label, "(")
	name = strings.TrimSpace(name)
	if name == "" {
		return label
	}
	return name
}

// preferredSubtitle is the track marked default, else the first English one.
func preferredSubtitle(tracks []track) string {
	for _, t := range tracks {
		if t.Default && t.caption() {
			return t.URL
		}
	}
	for _, t := range tracks {
		if t.caption() && (strings.EqualFold(language(t.Label), "english") || strings.EqualFold(strings.TrimSpace(t.Lang), "en")) {
			return t.URL
		}
	}
	return ""
}

// SkipRange reads the intro and outro from the chapters animex sends with
// the stream.
func (p *Provider) SkipRange(id, mode string, epNo int) (intro, outro []int, err error) {
	anilistID, err := parseID(id)
	if err != nil {
		return nil, nil, err
	}
	slug, err := p.api().slug(anilistID)
	if err != nil {
		return nil, nil, err
	}
	found, err := p.resolve(slug, epNo, normalizeLang(mode))
	if err != nil {
		return nil, nil, err
	}
	for _, src := range found {
		for _, ch := range src.Chapter {
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
