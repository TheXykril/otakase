package main

import (
	"fmt"
	"time"
)

// New episodes of /follow'ed shows in the app: next to the DM, the
// member's linked apps get an "airing" item, which the app shows as a
// desktop notification and keeps under Community → From Discord, ready to
// play from that episode. It expires after a few days: an app that was off
// for a week has no use for last week's episode alerts.

const airingItemKeep = 3 * 24 * time.Hour

// airingItem is the inbox item for one aired episode.
func airingItem(a followAiring, now time.Time) inboxItem {
	text := fmt.Sprintf("Episode %d is out", a.Episode)
	if a.Episodes > 0 {
		text = fmt.Sprintf("Episode %d of %d is out", a.Episode, a.Episodes)
	}
	return inboxItem{Kind: "airing", AniListID: a.MediaID, Episode: a.Episode, Title: clip(a.Show.Title, 100),
		Text: text, URL: playURL(a.MediaID, a.Episode), Expires: now.Add(airingItemKeep)}
}

// sendAiringToApps sends a member's aired episodes to their linked apps.
func (b *bot) sendAiringToApps(user string, eps []followAiring) {
	if !b.hasApp(user) {
		return
	}
	now := appNow()
	for _, a := range eps {
		b.sendToApps(user, airingItem(a, now))
	}
}
