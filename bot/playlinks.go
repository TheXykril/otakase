package main

import (
	"net/url"
	"strconv"

	"github.com/bwmarrin/discordgo"
)

// Play in Otakase. Discord link buttons only take http(s) URLs, so the
// button opens a small page on the site that hands the show to the app
// through its otakase:// link and offers the download to anyone without it.

const playPageURL = siteURL + "open/"

// playURL is the site's open page for an AniList show, starting at episode ep
// when ep is above 0. It is "" for an id that cannot be a show.
func playURL(anilistID, ep int) string {
	if anilistID <= 0 {
		return ""
	}
	q := url.Values{"anilist": {strconv.Itoa(anilistID)}}
	if ep > 0 {
		q.Set("ep", strconv.Itoa(ep))
	}
	return playPageURL + "?" + q.Encode()
}

// playButton is the Play in Otakase link button, or nil when there is no
// show to link to.
func playButton(anilistID, ep int) discordgo.MessageComponent {
	u := playURL(anilistID, ep)
	if u == "" {
		return nil
	}
	return discordgo.Button{Label: "Play in Otakase", Style: discordgo.LinkButton, URL: u}
}

// withPlayButton puts the Play in Otakase button first in a row of buttons,
// and Queue in Otakase after the rest while there is room, leaving the row
// as it was when there is no show or the row is already at Discord's five.
func withPlayButton(buttons []discordgo.MessageComponent, anilistID, ep int) []discordgo.MessageComponent {
	b := playButton(anilistID, ep)
	if b == nil || len(buttons) >= 5 {
		return buttons
	}
	buttons = append([]discordgo.MessageComponent{b}, buttons...)
	if q := queueButtonFor(anilistID); q != nil && len(buttons) < 5 {
		buttons = append(buttons, q)
	}
	return buttons
}
