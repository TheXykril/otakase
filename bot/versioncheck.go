package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// versionRe finds an otakase version someone mentions, like the line
// `otakase -v` prints ("Otakase version: 26.5.0") or "I'm on v2.2.1". A
// keyword must come first so mpv or OS versions are not picked up.
var versionRe = regexp.MustCompile(`(?i)\b(?:otakase(?:\s+version)?|version|ver|on)\s*[:=]?\s*v?(\d{1,2}\.\d{1,3}\.\d{1,3})\b`)

// releasesLatestURL is a variable so tests can point it at a fake server.
var releasesLatestURL = "https://api.github.com/repos/TheXykril/otakase/releases/latest"

var versionCheck = struct {
	sync.Mutex
	latest  string
	fetched time.Time
	warned  map[string]bool // support posts already told to update
}{warned: map[string]bool{}}

// mentionedVersion returns the otakase version in text, if any. Only the
// old 2.x line and the year-based releases (26.x and later) count.
func mentionedVersion(text string) (string, bool) {
	for _, m := range versionRe.FindAllStringSubmatch(text, -1) {
		major, _ := strconv.Atoi(strings.SplitN(m[1], ".", 2)[0])
		if major == 2 || major >= 26 {
			return m[1], true
		}
	}
	return "", false
}

// olderVersion reports whether version a is older than b.
func olderVersion(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for k := 0; k < 3; k++ {
		x, _ := strconv.Atoi(pa[k])
		y, _ := strconv.Atoi(pb[k])
		if x != y {
			return x < y
		}
	}
	return false
}

// latestVersion is the newest release on GitHub, cached for ten minutes.
func latestVersion(token string) (string, error) {
	versionCheck.Lock()
	defer versionCheck.Unlock()
	if versionCheck.latest != "" && time.Since(versionCheck.fetched) < 10*time.Minute {
		return versionCheck.latest, nil
	}
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := getJSON(githubRequest("GET", releasesLatestURL, token), &r); err != nil {
		return "", err
	}
	v := strings.TrimPrefix(r.TagName, "v")
	if strings.Count(v, ".") != 2 {
		return "", fmt.Errorf("odd release tag %q", r.TagName)
	}
	versionCheck.latest, versionCheck.fetched = v, time.Now()
	return v, nil
}

func outdatedEmbed(have, latest string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title: "Your Otakase is out of date",
		Description: fmt.Sprintf("You're on **%s**, the latest is **%s**. Many problems are already fixed there.\n\n"+
			"Update with `otakase -u` (or `/install` for other ways), then check `otakase -v` and try again.", have, latest),
		Color: shu,
	}
}

// checkVersion answers a support post that mentions an older otakase with
// the update command. Each post is told at most once.
func (b *bot) checkVersion(m *discordgo.MessageCreate) { b.checkVersionIn(m, m.Content) }

// checkVersionIn is checkVersion on text from the message, such as an
// attached diagnostics block.
func (b *bot) checkVersionIn(m *discordgo.MessageCreate, text string) {
	have, ok := mentionedVersion(text)
	if !ok || b.cfg.SupportForum == "" {
		return
	}
	ch, err := b.s.State.Channel(m.ChannelID)
	if err != nil {
		if ch, err = b.s.Channel(m.ChannelID); err != nil {
			return
		}
	}
	if ch.ParentID != b.cfg.SupportForum {
		return
	}
	versionCheck.Lock()
	done := versionCheck.warned[ch.ID]
	versionCheck.Unlock()
	if done {
		return
	}
	latest, err := latestVersion(b.cfg.GitHubToken)
	if err != nil {
		log.Printf("version check: %v", err)
		return
	}
	if !olderVersion(have, latest) {
		return
	}
	versionCheck.Lock()
	versionCheck.warned[ch.ID] = true
	versionCheck.Unlock()
	msg, err := b.s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds:          []*discordgo.MessageEmbed{outdatedEmbed(have, latest)},
		Components:      append(linkButtons("Changelog", fmt.Sprintf("%s/blob/v%s/CHANGELOG.md", repoURL, latest), "Latest release", repoURL+"/releases/latest"), answerRow()),
		Reference:       m.Reference(),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
	if err != nil {
		log.Printf("version check reply: %v", err)
		return
	}
	b.trackAnswer(msg, []string{"version"}, m.Content)
}
