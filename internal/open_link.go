package internal

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// otakase:// links. The site's Play in Otakase page, which the Discord bot's
// buttons lead to, opens otakase://play/anilist/<id>?ep=<n>; the desktop hands
// it to otakase as its first argument (or with -open), and the app starts
// straight at that show instead of the menu.

// OpenLinkScheme is the URL scheme otakase is registered for.
const OpenLinkScheme = "otakase"

// The largest numbers a link may carry. AniList ids are well under this, and
// no show has an episode past it; anything bigger is not a real link.
const (
	maxOpenLinkID      = 999999999
	maxOpenLinkEpisode = 99999
)

// OpenLink is what an otakase:// link asks for.
type OpenLink struct {
	AnilistID int
	// Episode is the episode to start, 0 to start where the viewer is up to.
	Episode int
}

// IsOpenLinkArg reports whether a command-line argument is an otakase:// link
// rather than something else.
func IsOpenLinkArg(arg string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(arg)), OpenLinkScheme+":")
}

var errOpenLink = errors.New("not an otakase link")

// IsPartyLink reports whether an otakase:// link joins a synced watch party
// (otakase://party/<event id>, from the site's Join in Otakase page).
func IsPartyLink(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), OpenLinkScheme+"://party/")
}

// ParseOpenLink reads an otakase://play/anilist/<id>?ep=<n> link. Both
// numbers must be plain digits: a link comes from a web page, so anything
// that is not exactly that is refused rather than guessed at.
func ParseOpenLink(raw string) (OpenLink, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return OpenLink{}, errOpenLink
	}
	if !strings.EqualFold(u.Scheme, OpenLinkScheme) || u.User != nil || u.Opaque != "" {
		return OpenLink{}, errOpenLink
	}
	if !strings.EqualFold(u.Host, "play") {
		return OpenLink{}, fmt.Errorf("unknown link action %q", u.Host)
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "anilist") {
		return OpenLink{}, errors.New("the link does not name an AniList show")
	}
	id, ok := parseLinkNumber(parts[1], maxOpenLinkID)
	if !ok {
		return OpenLink{}, fmt.Errorf("%q is not an AniList id", parts[1])
	}
	link := OpenLink{AnilistID: id}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return OpenLink{}, errors.New("the link's options cannot be read")
	}
	if eps, present := query["ep"]; present {
		if len(eps) != 1 {
			return OpenLink{}, errors.New("the link names more than one episode")
		}
		ep, ok := parseLinkNumber(eps[0], maxOpenLinkEpisode)
		if !ok {
			return OpenLink{}, fmt.Errorf("%q is not an episode number", eps[0])
		}
		link.Episode = ep
	}
	return link, nil
}

// parseLinkNumber accepts 1 to max written as plain digits: no sign, no
// spaces, no leading zero.
func parseLinkNumber(s string, max int) (int, bool) {
	if s == "" || len(s) > len(strconv.Itoa(max)) || s[0] == '0' {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > max {
		return 0, false
	}
	return n, true
}

// OpenLinkError is what the viewer is told when a link cannot be used.
func OpenLinkError(err error) string {
	return fmt.Sprintf("That otakase link could not be opened: %v. Starting %s as usual.", err, DisplayName)
}

var (
	openLinkMu      sync.Mutex
	pendingOpenLink *OpenLink
)

// SetOpenLink makes the next pick of a show the link's, instead of a menu.
func SetOpenLink(link OpenLink) {
	openLinkMu.Lock()
	defer openLinkMu.Unlock()
	pendingOpenLink = &link
}

// takeOpenLink hands the link over once: going back from the show it opened
// lands on the menu, as it would after any other pick.
func takeOpenLink() (OpenLink, bool) {
	openLinkMu.Lock()
	defer openLinkMu.Unlock()
	if pendingOpenLink == nil {
		return OpenLink{}, false
	}
	link := *pendingOpenLink
	pendingOpenLink = nil
	return link, true
}

// applyLinkEpisode starts the episode a link asked for, once everything else
// has settled which episode would play. trackerNext is the episode the
// tracker's progress points at, 0 when the show is not on the list. The
// resume position stays only when the link asks for the episode already
// chosen.
//
// A total that is known and smaller than the asked episode means the link is
// wrong, and the episode worked out as usual stands. An episode behind the
// tracker's progress is a look back at one already seen, so the tracker is
// left alone rather than set back to it.
func applyLinkEpisode(config *Config, anime *Anime, episode, trackerNext int) {
	if episode <= 0 {
		return
	}
	if anime.TotalEpisodes > 0 && episode > anime.TotalEpisodes {
		OpenLinkNotice(config, fmt.Sprintf("The link asked for episode %d, but this show has %d. Starting episode %d.", episode, anime.TotalEpisodes, anime.Ep.Number))
		return
	}
	if anime.Ep.Number != episode {
		anime.Ep.Number = episode
		anime.Ep.Player.PlaybackTime = 0
		anime.Ep.Resume = false
	}
	if trackerNext > 0 && episode < trackerNext && !anime.Rewatching && !anime.SkipRemoteSync && UsesRemoteTracking(config) {
		anime.SkipRemoteSync = true
		OpenLinkNotice(config, fmt.Sprintf("You have already watched episode %d, so your %s progress stays as it is.", episode, RemoteTrackingDisplayName(config)))
	}
}

// OpenLinkNotice tells the viewer something about the link they opened, and
// leaves it on screen long enough to read before a menu replaces it. Without
// a terminal it is a notification, which stays by itself.
func OpenLinkNotice(config *Config, message string) {
	Out(message)
	if config != nil && !config.RofiSelection {
		time.Sleep(openLinkNoticePause)
	}
}

// openLinkNoticePause is how long a notice stays up in a terminal.
var openLinkNoticePause = 3 * time.Second
