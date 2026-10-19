package anipub

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/providerhost"
)

// megaplay's CDN (kotocdn.site) sometimes answers with a playlist whose
// segments are ad images, 1x1 PNGs from ByteDance's ad servers, or that
// redirect there. mpv cannot decode any of them and sits on its idle "Drop
// files or URLs to play here." screen, so such a stream is refused before it
// reaches the player and the caller moves on to another provider.

// adHostMarkers appear in the URL of every ad segment seen so far.
var adHostMarkers = []string{"ibyteimg.com", "byteimg.com", "ad-site-i18n"}

// playlistLimit caps how much of a playlist is read.
const playlistLimit = 2 << 20

// playlistAccept asks for HLS but takes whatever the CDN sends.
const playlistAccept = "application/vnd.apple.mpegurl, application/x-mpegURL, */*"

// streamInfTag opens each variant entry of a master playlist.
const streamInfTag = "#EXT-X-STREAM-INF"

// isMegaplayCDNHost is true for kotocdn.site and its subdomains. The ads have
// only been seen there, so other hosts are not checked.
func isMegaplayCDNHost(hostname string) bool {
	h := strings.ToLower(strings.TrimSpace(hostname))
	return h == "kotocdn.site" || strings.HasSuffix(h, ".kotocdn.site")
}

// validateResolvedStream refuses a kotocdn stream that is made of ads.
// Streams on any other host pass unchecked.
func validateResolvedStream(streamURL string) error {
	master, err := url.Parse(strings.TrimSpace(streamURL))
	if err != nil || master.Scheme == "" || master.Host == "" {
		return fmt.Errorf("invalid anipub stream url %q", streamURL)
	}
	if !isMegaplayCDNHost(master.Host) {
		return nil // not where the ads come from
	}
	client := providerhost.HTTPClient()

	segments, err := streamSegments(client, master, streamURL)
	if err != nil {
		return err
	}
	if err := adShareError(segments, streamURL); err != nil {
		return err
	}

	// A segment URL that looks clean can still redirect to an ad, so the
	// first one is opened to see what it holds.
	clean := slices.IndexFunc(segments, func(s string) bool { return !isAdSegment(s) })
	if clean >= 0 && notVideo(probe(client, segments[clean])) {
		return fmt.Errorf("anipub stream %q first media segment is not video content", streamURL)
	}
	return nil
}

// streamSegments follows a master playlist to its best variant and returns
// the segment URIs listed there.
func streamSegments(client *http.Client, master *url.URL, streamURL string) ([]string, error) {
	manifest, err := fetchPlaylist(client, master.String())
	if err != nil {
		return nil, fmt.Errorf("anipub stream manifest fetch failed: %w", err)
	}
	if !strings.Contains(manifest, "#EXTM3U") {
		return nil, fmt.Errorf("anipub stream manifest %q is not an HLS playlist", streamURL)
	}
	variant, err := mediaPlaylistURL(master, manifest)
	if err != nil {
		return nil, err
	}
	media, err := fetchPlaylist(client, variant)
	if err != nil {
		return nil, fmt.Errorf("anipub stream media playlist fetch failed: %w", err)
	}
	segments := playlistURIs(media)
	if len(segments) == 0 {
		return nil, fmt.Errorf("anipub stream %q has no media segments", streamURL)
	}
	return segments, nil
}

// adShareError refuses a playlist when at least half its segments are ads.
func adShareError(segments []string, streamURL string) error {
	ads := 0
	for _, s := range segments {
		if isAdSegment(s) {
			ads++
		}
	}
	switch {
	case ads == len(segments):
		return fmt.Errorf("anipub stream %q is an ad-injected decoy: all %d segments are ad/decoy content", streamURL, ads)
	case ads*2 >= len(segments):
		return fmt.Errorf("anipub stream %q is an ad-injected decoy: %d/%d segments are ad/decoy content", streamURL, ads, len(segments))
	default:
		return nil
	}
}

// megaplayGet is a GET request carrying the headers megaplay's CDN expects.
func megaplayGet(target string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"User-Agent": {userAgent},
		"Referer":    {megaplayBaseURL + "/"},
	}
	return req, nil
}

// fetchPlaylist downloads a playlist, up to playlistLimit bytes of it.
func fetchPlaylist(client *http.Client, playlistURL string) (string, error) {
	if client == nil {
		return "", errors.New("http client not configured")
	}
	req, err := megaplayGet(playlistURL)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", playlistAccept)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if !providerhost.HTTPStatusOK(resp.StatusCode) {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", providerhost.HTTPStatusError("megaplay hls manifest", resp.StatusCode, snippet)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, playlistLimit))
	return string(body), err
}

// probe returns the first 16 bytes of a segment, or nil when they could not
// be read. A failed probe does not count against the stream: mpv may still
// manage where the probe did not.
func probe(client *http.Client, segmentURL string) []byte {
	if client == nil {
		return nil
	}
	req, err := megaplayGet(segmentURL)
	if err != nil {
		return nil
	}
	req.Header.Set("Range", "bytes=0-15")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if !providerhost.HTTPStatusOK(resp.StatusCode) {
		return nil
	}
	head := make([]byte, 16)
	n, _ := io.ReadFull(resp.Body, head)
	return head[:n]
}

// mediaPlaylistURL returns the highest-bandwidth variant of a master
// playlist, or the URL itself when the playlist is already a media one.
func mediaPlaylistURL(master *url.URL, playlist string) (string, error) {
	if !strings.Contains(playlist, streamInfTag) {
		return master.String(), nil
	}
	best, pick := -1, ""
	rows := strings.Split(playlist, "\n")
	for i, row := range rows {
		row = strings.TrimSpace(row)
		if !strings.HasPrefix(row, streamInfTag) {
			continue
		}
		bw := bandwidth(row)
		if bw <= best {
			continue // ties keep the earlier variant
		}
		if uri := nextURI(rows[i+1:]); uri != "" {
			best, pick = bw, uri
		}
	}
	if pick == "" {
		return "", errors.New("no variant playlist found in master playlist")
	}
	ref, err := url.Parse(pick)
	if err != nil {
		return "", fmt.Errorf("invalid variant playlist uri %q: %w", pick, err)
	}
	return master.ResolveReference(ref).String(), nil
}

// nextURI is the first URI line among lines, skipping tags and blanks.
func nextURI(lines []string) string {
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

// bandwidth reads BANDWIDTH= from a stream tag, 0 when absent or unreadable.
func bandwidth(tag string) int {
	tag = strings.ToUpper(tag)
	i := strings.Index(tag, "BANDWIDTH=")
	if i < 0 {
		return 0
	}
	value, _, _ := strings.Cut(tag[i+len("BANDWIDTH="):], ",")
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return n
	}
	return 0
}

// playlistURIs returns the URI lines of a playlist as written.
func playlistURIs(body string) []string {
	var uris []string
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			uris = append(uris, line)
		}
	}
	return uris
}

// isAdSegment reports whether a segment URI points at a known ad host.
func isAdSegment(uri string) bool {
	lower := strings.ToLower(uri)
	return slices.ContainsFunc(adHostMarkers, func(marker string) bool {
		return strings.Contains(lower, marker)
	})
}

// notVideo reports whether the first bytes of a segment are an image or an
// HTML page rather than MPEG-TS or fMP4.
func notVideo(head []byte) bool {
	if len(head) < 4 {
		return false // too little to judge; let the player try
	}
	image := bytes.HasPrefix(head, []byte("\x89PNG")) ||
		bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}) ||
		bytes.HasPrefix(head, []byte("GIF8")) ||
		(len(head) >= 12 && bytes.HasPrefix(head, []byte("RIFF")) && string(head[8:12]) == "WEBP")
	lower := strings.ToLower(string(head))
	page := strings.HasPrefix(lower, "<") || strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype")
	return image || page
}
