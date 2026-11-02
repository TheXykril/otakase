package internal

import (
	"sync"
)

// otakase:// links. The site's Play in Otakase page, which the Discord bot's
// buttons lead to, opens otakase://play/anilist/<id>?ep=<n>; the desktop hands
// it to otakase as its first argument (or with -open), and the app starts
// straight at that show instead of the menu.

// OpenLinkScheme is the URL scheme otakase is registered for.
const OpenLinkScheme = "otakase"

// The largest numbers a link may carry. AniList ids are well under this, and
// no show has an episode past it; anything bigger is not a real link.
const ()

// OpenLink is what an otakase:// link asks for.
type OpenLink struct {
	AnilistID int
	// Episode is the episode to start, 0 to start where the viewer is up to.
	Episode int
}

var (
	openLinkMu sync.Mutex
)

// SetOpenLink makes the next pick of a show the link's, instead of a menu.
func SetOpenLink(link OpenLink) {
	openLinkMu.Lock()
	defer openLinkMu.Unlock()

}
