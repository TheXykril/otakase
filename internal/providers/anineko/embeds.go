package anineko

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var (
	bibiembSrcRE    = regexp.MustCompile(`const src = "(https?://[^"]+/master\.m3u8)"`)
	vibeplayerSrcRE = regexp.MustCompile(`const src = "(https?://[^"]+/public/stream/[^"]+/master\.m3u8)"`)
	// A variant line in a master playlist and the URI on the line after it.
	variantRE = regexp.MustCompile(`(?m)^#EXT-X-STREAM-INF:.*NAME="([^"]+)".*\r?\n([^\r\n#]+)`)
)

// bibiembQualities is the order variants are preferred in.
var bibiembQualities = []string{"1080p", "720p", "480p", "360p"}

// stream is one playable URL and what the player needs alongside it.
type stream struct {
	URL      string
	Referrer string
	Subtitle string
}

// resolveBibiemb picks a single variant out of the master playlist, so the
// player does not have to fetch the master with the embed as its referrer.
func resolveBibiemb(embed string) (stream, error) {
	embed = strings.TrimSpace(embed)
	if embed == "" {
		return stream{}, errors.New("empty bibiemb embed url")
	}
	page, err := get(embed, baseURL+"/")
	if err != nil {
		return stream{}, err
	}
	m := bibiembSrcRE.FindSubmatch(page)
	if m == nil {
		return stream{}, errors.New("bibiemb master m3u8 not found")
	}
	variant, err := bestVariant(string(m[1]), embed)
	if err != nil {
		return stream{}, err
	}
	return stream{URL: variant, Referrer: embed, Subtitle: subtitleFor(embed, string(page))}, nil
}

func bestVariant(master, referer string) (string, error) {
	playlist, err := get(master, referer)
	if err != nil {
		return "", err
	}
	matches := variantRE.FindAllStringSubmatch(string(playlist), -1)
	if len(matches) == 0 {
		return "", errors.New("no bibiemb variants in master playlist")
	}
	byName := map[string]string{}
	for _, m := range matches {
		byName[strings.ToLower(strings.TrimSpace(m[1]))] = resolveRef(master, strings.TrimSpace(m[2]))
	}
	for _, quality := range bibiembQualities {
		if u, ok := byName[quality]; ok {
			return u, nil
		}
	}
	return resolveRef(master, strings.TrimSpace(matches[0][2])), nil
}

// resolveVibeplayer hands the stream to the local proxy: vibeplayer disguises
// its video segments as PNG images, which the player cannot read as they are.
func resolveVibeplayer(embed string) (stream, error) {
	embed = strings.TrimSpace(embed)
	if embed == "" {
		return stream{}, errors.New("empty vibeplayer embed url")
	}
	page, err := get(embed, baseURL+"/")
	if err != nil {
		return stream{}, err
	}
	m := vibeplayerSrcRE.FindSubmatch(page)
	if m == nil {
		return stream{}, errors.New("vibeplayer master m3u8 not found")
	}
	local, err := registerVibeStream(string(m[1]), embed)
	if err != nil {
		return stream{}, err
	}
	return stream{URL: local, Referrer: embed, Subtitle: subtitleFor(embed, string(page))}, nil
}

// resolveRef resolves a playlist entry against the playlist it came from.
func resolveRef(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}
