package internal

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/thexykril/otakase/internal/providers"
)

// streamHeaderArgs renders a provider's extra HTTP headers as MPV arguments.
//
// --http-header-fields-append is used rather than --http-header-fields so these
// add to whatever the user configured in MpvArgs instead of replacing it. Header
// names are sorted so a given episode always launches MPV the same way, which
// matters when comparing two runs in a log.
func streamHeaderArgs(headers map[string]string) []string {
	lines := streamHeaderLines(headers)
	if len(lines) == 0 {
		return nil
	}
	args := make([]string, 0, len(lines))
	for _, line := range lines {
		args = append(args, "--http-header-fields-append="+line)
	}
	return args
}

// streamHeaderLines renders headers as "Name: value" lines, sorted by name.
func streamHeaderLines(headers map[string]string) []string {
	if len(headers) == 0 {
		return nil
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		if strings.TrimSpace(name) != "" && strings.TrimSpace(headers[name]) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, fmt.Sprintf("%s: %s", strings.TrimSpace(name), strings.TrimSpace(headers[name])))
	}
	return lines
}

// hasMPVHeaderArg reports whether the user set mpv's extra HTTP headers
// themselves, in which case a running instance's list is theirs to keep.
func hasMPVHeaderArg(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(arg)), "--http-header-fields") {
			return true
		}
	}
	return false
}

// mpvDubLanguages and mpvSubLanguages are the audio languages mpv prefers for
// each mode, in both the two- and three-letter codes hosts tag tracks with.
const (
	mpvDubLanguages = "en,eng"
	mpvSubLanguages = "ja,jpn"
)

// mpvAudioLanguageArgs tells mpv which audio to prefer.
//
// Some hosts serve one stream carrying every dub they have, with Japanese
// marked default, and mpv plays the default unless told otherwise: a dub was
// heard in Japanese. A stream with one audio track, or none in the preferred
// language, plays as before -- --alang only ranks the tracks there are. A
// viewer's own --alang in MpvArgs is left to win.
func mpvAudioLanguageArgs(mode string, callerArgs []string) []string {
	for _, arg := range callerArgs {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(arg)), "--alang") {
			return nil
		}
	}
	return []string{"--alang=" + mpvAudioLanguages(mode)}
}

func mpvAudioLanguages(mode string) string {
	if normalizeTranslationType(mode) == "dub" {
		return mpvDubLanguages
	}
	return mpvSubLanguages
}

// streamReferrer is the Referer a provider's streams expect, or empty when it
// registered none -- in which case no Referer is sent at all.
//
// There used to be a default here, AllAnime's, inherited by every provider that
// registered nothing. That named an unrelated site to hosts with no connection
// to it, and outlived AllAnime itself: the domain it pointed at no longer
// resolves. A provider that needs a Referer says so in its registration.
func streamReferrer(provider string) string {
	return providers.Referrer(provider)
}

// mpvFlag lowers a flag and turns IINA's --mpv-name spelling into --name.
func mpvFlag(arg string) string {
	flag := strings.ToLower(strings.TrimSpace(arg))
	if rest, ok := strings.CutPrefix(flag, "--mpv-"); ok {
		return "--" + rest
	}
	return flag
}

func isHTTPStreamLink(link string) bool {
	scheme, _, ok := strings.Cut(strings.TrimSpace(link), "://")
	return ok && (strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https"))
}

// hasMPVReferrerArg reports whether the arguments already send a Referer,
// either through --referrer or as one of the extra header lines.
func hasMPVReferrerArg(args []string) bool {
	for i, arg := range args {
		flag, value, inline := strings.Cut(mpvFlag(arg), "=")
		switch flag {
		case "--referrer":
			return true
		case "--http-header-fields", "--http-header-fields-append":
			if !inline && i+1 < len(args) {
				value = strings.ToLower(args[i+1])
			}
			if strings.Contains(value, "referer:") {
				return true
			}
		}
	}
	return false
}

// hasMPVSubtitleArg reports whether the arguments load a subtitle file of
// their own, which then wins over the provider's.
func hasMPVSubtitleArg(args []string) bool {
	for i, arg := range args {
		flag, _, inline := strings.Cut(strings.ToLower(strings.TrimSpace(arg)), "=")
		switch {
		case flag == "--sub-file":
			return true
		case flag == "--sub-files" && (inline || i+1 < len(args)):
			return true
		}
	}
	return false
}

// normalizeReferrerValue turns a bare host into a full URL, since mpv sends
// the value as is and hosts reject a Referer without a scheme or path.
func normalizeReferrerValue(referrer string) string {
	referrer = strings.TrimSpace(referrer)
	if referrer == "" {
		return ""
	}
	if !strings.Contains(referrer, "://") {
		referrer = "https://" + referrer
	}
	parsed, err := url.Parse(referrer)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return referrer
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String()
}

func normalizeReferrerArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		flag, value, inline := strings.Cut(args[i], "=")
		if flag != "--referrer" && flag != "--mpv-referrer" {
			out = append(out, args[i])
			continue
		}
		switch {
		case inline:
			out = append(out, flag+"="+normalizeReferrerValue(value))
		case i+1 < len(args):
			out = append(out, flag, normalizeReferrerValue(args[i+1]))
			i++
		default:
			out = append(out, args[i])
		}
	}
	return out
}

// playbackArgs are the mpv arguments for one episode: the caller's, the
// user's MpvArgs, then whatever the stream needs that those did not set.
type playbackArgs struct {
	args         []string
	ownSubtitles bool   // the arguments load a subtitle file of their own
	subtitleURL  string // provider subtitles to load, empty with ownSubtitles
	referrer     string // Referer to send, empty when the user set one or none is needed
}

func buildPlaybackArgs(config *Config, anime *Anime, link string, callerArgs []string) playbackArgs {
	args := append(append([]string{}, callerArgs...), config.MpvArgs...)
	var p playbackArgs

	if isHTTPStreamLink(link) && !hasMPVReferrerArg(args) {
		p.referrer = strings.TrimSpace(anime.Ep.StreamReferrer)
		if p.referrer == "" {
			p.referrer = streamReferrer(CurrentAnimeProviderName(anime))
		}
	}
	p.ownSubtitles = hasMPVSubtitleArg(args)
	if !p.ownSubtitles {
		p.subtitleURL = strings.TrimSpace(anime.Ep.SubtitleURL)
	}

	if p.referrer != "" {
		args = append(args, "--referrer="+p.referrer)
	}
	// Some CDNs want more than a referrer. One rejects its own video segments
	// with 403 unless Origin names the player's domain, and --referrer cannot
	// set Origin, so a provider that knows this passes the headers along.
	args = append(args, streamHeaderArgs(anime.Ep.StreamHeaders)...)
	args = append(args, mpvHLSBitrateArgs(anime.Ep.StreamHLSBitrate, args)...)
	args = append(args, mpvAudioLanguageArgs(playlistAudioMode(anime, config), args)...)
	args = append(args, mpvSubtitleLanguageArgs(subtitleLanguageFor(config, anime), args)...)
	if p.subtitleURL != "" {
		args = append(args, "--sub-file="+p.subtitleURL)
	}
	p.args = normalizeReferrerArgs(args)
	return p
}
