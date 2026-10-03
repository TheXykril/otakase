package internal

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/providers"
)

// Quality picks one rendition out of the HLS master playlist most providers
// hand back. Left alone, mpv and ffmpeg start on whatever the master lists
// and adapt upwards, which is the best picture but also the most data -- and
// on a metered or slow link the viewer would rather fix it lower.
//
// It is applied where an episode's links are resolved, so mpv, casting and
// downloads all see the same choice without each growing its own.

// hlsMasterTimeout bounds the extra request made to read the master. Playback
// is waiting on it, and the original link still works if it never answers.
const hlsMasterTimeout = 4 * time.Second

// hlsMasterMaxBytes caps how much of a response is read as a playlist. A
// master is a few kilobytes; anything larger is not one.
const hlsMasterMaxBytes = 512 << 10

// hlsVariant is one #EXT-X-STREAM-INF entry of a master playlist.
type hlsVariant struct {
	URI       string
	Width     int
	Height    int
	Bandwidth int
	// Audio names the EXT-X-MEDIA group the variant's sound comes from. When
	// set, the variant playlist carries picture only.
	Audio string
	// Index is the variant's position in the master, which is also the
	// program number ffmpeg's HLS demuxer gives it.
	Index int
}

// parseQuality reads a Quality value as a number of lines. Zero means best:
// the master untouched. "720p" is accepted as well as "720", since that is
// how people write it.
func parseQuality(value string) int {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, "p")
	if value == "" || value == "best" {
		return 0
	}
	lines, err := strconv.Atoi(value)
	if err != nil || lines <= 0 {
		return 0
	}
	return lines
}

// showPrefsStorage is where show_prefs.json lives for this config.
func showPrefsStorage(config *Config) string {
	if config != nil && strings.TrimSpace(config.StoragePath) != "" {
		return config.StoragePath
	}
	return GetStoragePath()
}

// qualityFor is the height this show should play at: the one set for it,
// else Quality, else 0 for best.
func qualityFor(config *Config, anime *Anime) int {
	if anime != nil && anime.AnilistId > 0 {
		if pref := loadShowPrefs(showPrefsStorage(config))[strconv.Itoa(anime.AnilistId)].Quality; pref != "" {
			return parseQuality(pref)
		}
	}
	if config == nil {
		return 0
	}
	return parseQuality(config.Quality)
}

// parseHLSAttributes splits an attribute list such as
// BANDWIDTH=1280000,RESOLUTION=1280x720,CODECS="avc1.4d401f,mp4a.40.2".
// Quoted values may hold commas, so a plain split on "," would cut CODECS.
func parseHLSAttributes(list string) map[string]string {
	attrs := map[string]string{}
	for len(list) > 0 {
		eq := strings.IndexByte(list, '=')
		if eq < 0 {
			break
		}
		name := strings.ToUpper(strings.TrimSpace(list[:eq]))
		list = list[eq+1:]
		var value string
		if strings.HasPrefix(list, `"`) {
			end := strings.IndexByte(list[1:], '"')
			if end < 0 {
				value, list = list[1:], ""
			} else {
				value, list = list[1:end+1], list[end+2:]
			}
			list = strings.TrimPrefix(list, ",")
		} else if comma := strings.IndexByte(list, ','); comma >= 0 {
			value, list = list[:comma], list[comma+1:]
		} else {
			value, list = list, ""
		}
		attrs[name] = strings.TrimSpace(value)
	}
	return attrs
}

// parseHLSMaster lists the variants of a master playlist, their URIs resolved
// against the master's own URL. A media playlist, which has none, gives none.
func parseHLSMaster(body string, base *url.URL) []hlsVariant {
	var variants []hlsVariant
	var pending *hlsVariant
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64<<10), hlsMasterMaxBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			attrs := parseHLSAttributes(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			variant := hlsVariant{Audio: attrs["AUDIO"]}
			variant.Bandwidth, _ = strconv.Atoi(attrs["BANDWIDTH"])
			if w, h, ok := strings.Cut(strings.ToLower(attrs["RESOLUTION"]), "x"); ok {
				variant.Width, _ = strconv.Atoi(w)
				variant.Height, _ = strconv.Atoi(h)
			}
			pending = &variant
		case strings.HasPrefix(line, "#"):
			continue
		case pending != nil:
			// The line after the tag is the variant's URI.
			uri := line
			if base != nil {
				if ref, err := url.Parse(line); err == nil {
					uri = base.ResolveReference(ref).String()
				}
			}
			pending.URI = uri
			pending.Index = len(variants)
			variants = append(variants, *pending)
			pending = nil
		}
	}
	return variants
}

// pickHLSVariant chooses the variant nearest the wanted height: the tallest
// at or below it, so the viewer never gets more than they asked for, and only
// when every variant is taller, the shortest of those. Variants that state no
// resolution -- audio-only ones -- are never chosen.
func pickHLSVariant(variants []hlsVariant, height int) (hlsVariant, bool) {
	var below, above *hlsVariant
	for i := range variants {
		v := &variants[i]
		if v.Height <= 0 {
			continue
		}
		if v.Height <= height {
			// Same height twice is usually two bitrates; the higher one is
			// the better picture at the size that was asked for.
			if below == nil || v.Height > below.Height || (v.Height == below.Height && v.Bandwidth > below.Bandwidth) {
				below = v
			}
			continue
		}
		if above == nil || v.Height < above.Height || (v.Height == above.Height && v.Bandwidth < above.Bandwidth) {
			above = v
		}
	}
	if below != nil {
		return *below, true
	}
	if above != nil {
		return *above, true
	}
	return hlsVariant{}, false
}

// fetchHLSMaster reads a playlist with the same referrer and headers the
// player would send; CDNs that want them for segments want them here too.
func fetchHLSMaster(client *http.Client, masterURL, referrer string, headers map[string]string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(context.Background(), hlsMasterTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, masterURL, nil)
	if err != nil {
		return "", err
	}
	if referrer != "" {
		req.Header.Set("Referer", referrer)
	}
	for name, value := range headers {
		if strings.TrimSpace(name) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(name, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, hlsMasterMaxBytes))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff")), "#EXTM3U") {
		return "", fmt.Errorf("not an HLS playlist")
	}
	return string(body), nil
}

// looksLikeHLS keeps the master request to links that are playlists. Fetching
// an mp4 to find out it is not one would start downloading the episode.
func looksLikeHLS(link string) bool {
	if !isHTTPStreamLink(link) {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(parsed.Path), ".m3u8")
}

// hlsMasterClient is the client the master is fetched with; tests swap it.
var hlsMasterClient = func() *http.Client { return sharedHTTPClient }

// applyQualityPreference narrows the link that will play to the wanted
// rendition. It changes result in place and never fails: with Quality at best,
// a link that is not HLS, or a master that cannot be read, the result stays
// exactly as the provider gave it.
//
// A variant whose sound lives in a separate EXT-X-MEDIA rendition (an AUDIO
// group) cannot stand in for the master -- its playlist is picture only, and
// playing it alone is silent. For those the master stays and the hint carries
// the variant's bandwidth instead, which mpv takes as --hls-bitrate to choose
// the same variant with its audio. ffmpeg (casting, downloads) plays that
// master as it always has.
func applyQualityPreference(config *Config, anime *Anime, result *ProviderEpisodeResult) {
	if result == nil || len(result.Links) == 0 {
		return
	}
	height := qualityFor(config, anime)
	if height <= 0 {
		return
	}
	selected := PrioritizeLink(result.Links)
	if !looksLikeHLS(selected) {
		return
	}
	hint := result.LinkHints[selected]
	referrer := strings.TrimSpace(hint.Referrer)
	if referrer == "" {
		referrer = providers.Referrer(result.ProviderName)
	}

	body, err := fetchHLSMaster(hlsMasterClient(), selected, referrer, hint.Headers)
	if err != nil {
		Log(fmt.Sprintf("Quality: could not read the master playlist, playing it as given: %v", err))
		return
	}
	base, _ := url.Parse(selected)
	variant, ok := pickHLSVariant(parseHLSMaster(body, base), height)
	if !ok {
		// A media playlist, or a master that states no resolutions.
		return
	}

	hints := make(map[string]StreamPlaybackHint, len(result.LinkHints)+1)
	for key, value := range result.LinkHints {
		hints[key] = value
	}

	if variant.Audio != "" {
		hint.HLSBitrate = variant.Bandwidth
		hints[selected] = hint
		result.LinkHints = hints
		Log(fmt.Sprintf("Quality: %dp wanted, capping the master at %dp (%d bit/s); its audio is a separate rendition", height, variant.Height, variant.Bandwidth))
		return
	}

	links := make([]string, len(result.Links))
	for i, link := range result.Links {
		if link == selected {
			link = variant.URI
		}
		links[i] = link
	}
	// The variant can live on another host, and host decides which link
	// plays. If swapping it in would make another link win, the swap would
	// change the source rather than the quality, so it is not made.
	if PrioritizeLink(links) != variant.URI {
		Log("Quality: the chosen variant would not be the link played; keeping the master")
		return
	}
	delete(hints, selected)
	hints[variant.URI] = hint
	result.Links = links
	result.LinkHints = hints
	Log(fmt.Sprintf("Quality: %dp wanted, playing the %dp variant", height, variant.Height))
}

// mpvHLSBitrateValue is what mpv's hls-bitrate is set to for a stream: the
// variant's bandwidth, or "max" -- mpv's default -- when nothing was chosen.
func mpvHLSBitrateValue(bitrate int) string {
	if bitrate > 0 {
		return strconv.Itoa(bitrate)
	}
	return "max"
}

// hasMPVHLSBitrateArg reports whether the viewer set --hls-bitrate in MpvArgs,
// which then wins over any choice made here.
func hasMPVHLSBitrateArg(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(arg)), "--hls-bitrate") {
			return true
		}
	}
	return false
}

// mpvHLSBitrateArgs caps a new mpv instance at the chosen variant.
func mpvHLSBitrateArgs(bitrate int, callerArgs []string) []string {
	if bitrate <= 0 || hasMPVHLSBitrateArg(callerArgs) {
		return nil
	}
	return []string{"--hls-bitrate=" + mpvHLSBitrateValue(bitrate)}
}

// resetMPVHLSBitrate sets a running instance's hls-bitrate for the episode
// about to load. Like the headers it is global to the instance, so it is set
// every time, back to "max" when this episode has no cap.
func resetMPVHLSBitrate(send mpvCommandSender, ipcSocketPath string, bitrate int) {
	if _, err := send(ipcSocketPath, []interface{}{"set_property", "hls-bitrate", mpvHLSBitrateValue(bitrate)}); err != nil {
		Log(fmt.Sprintf("Failed to set the HLS bitrate: %v", err))
	}
}

// qualityChoices are the per-show values offered in the update menu. Quality
// itself accepts any number of lines; these are the heights hosts serve.
// "setting" stands for the empty value, which clears the override: selection
// menus read an empty key as nothing picked.
var qualityChoices = []SelectionOption{
	{Key: "setting", Label: "Use the Quality setting", Icon: icons.Quality},
	{Key: "best", Label: "Best available"},
	{Key: "1080", Label: "1080p"},
	{Key: "720", Label: "720p"},
	{Key: "480", Label: "480p"},
}

// qualityLabel says a stored per-show value the way the menu does.
func qualityLabel(value string) string {
	if lines := parseQuality(value); lines > 0 {
		return fmt.Sprintf("%dp", lines)
	}
	if strings.TrimSpace(value) == "" {
		return "the Quality setting"
	}
	return "best available"
}

// setShowQuality remembers a quality for one show; "" goes back to the setting.
func setShowQuality(config *Config, anilistID int, value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	return updateShowPrefs(showPrefsStorage(config), anilistID, func(p *ShowPrefs) { p.Quality = value })
}

// PromptShowQuality asks which quality one show should play at and stores the
// answer. It reports false when the viewer backed out, leaving it unchanged.
func PromptShowQuality(config *Config, anilistID int) (bool, error) {
	current := loadShowPrefs(showPrefsStorage(config))[strconv.Itoa(anilistID)].Quality
	Out(fmt.Sprintf("Current quality: %s", qualityLabel(current)))

	selected, err := DynamicSelect(qualityChoices)
	if err != nil {
		return false, err
	}
	value := selected.Key
	switch value {
	case "-1", "-2", "":
		return false, nil
	case "setting":
		value = ""
	}
	if err := setShowQuality(config, anilistID, value); err != nil {
		return false, err
	}
	Out(fmt.Sprintf("Quality for this show: %s", qualityLabel(value)))
	return true, nil
}
