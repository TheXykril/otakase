package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Update nudge: a heartbeat from an app older than the latest release gets
// that app a notice with the update link, and the member one DM, once per
// release. Development builds are left alone, and a member who doesn't
// take DMs from the server only misses the DM.

// nudgeCheckLimit looks at each app's version at most every half hour.
var nudgeCheckLimit = newRateLimit(1, 30*time.Minute)

// nudgeMu keeps two heartbeats from nudging for the same release.
var nudgeMu sync.Mutex

func init() {
	onHeartbeat(func(b *bot, ev heartbeatEvent) {
		// Without a Discord session (tests) there is nobody to nudge.
		if b.s == nil || ev.DevBuild || !plainVersionRe.MatchString(ev.Version) || !nudgeCheckLimit.allow(ev.Hash, ev.At) {
			return
		}
		go b.nudgeUpdate(ev)
	})
}

// releaseURL is a release's page on GitHub.
func releaseURL(version string) string { return repoURL + "/releases/tag/v" + version }

// planNudge marks a release as nudged for the app and the member and says
// which of the two still needed it.
func planNudge(d *storeData, user, hash, latest string) (notice, dm bool) {
	if d.UpdateNudges == nil {
		d.UpdateNudges = map[string]string{}
	}
	if d.AppNudges == nil {
		d.AppNudges = map[string]string{}
	}
	for h := range d.AppNudges {
		if _, ok := d.AppLinks[h]; !ok {
			delete(d.AppNudges, h) // unlinked since
		}
	}
	notice, dm = d.AppNudges[hash] != latest, d.UpdateNudges[user] != latest
	d.AppNudges[hash], d.UpdateNudges[user] = latest, latest
	return notice, dm
}

// updateNotice is the inbox item for an app on an older version.
func updateNotice(have, latest string) inboxItem {
	return inboxItem{Kind: "notice", Title: "Otakase " + latest + " is out",
		Text: fmt.Sprintf("You have %s. Update with otakase -u.", have), URL: releaseURL(latest)}
}

func updateDM(have, latest string) *discordgo.MessageSend {
	return &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{Title: "Otakase " + latest + " is out", Color: shu,
			Description: fmt.Sprintf("Your linked Otakase app is on **%s**. Update with `otakase -u` (or `/install` in the server for other ways).", have),
			Footer:      &discordgo.MessageEmbedFooter{Text: "One message per release · development builds are never nudged"}}},
		Components: linkButtons("Changelog", fmt.Sprintf("%s/blob/v%s/CHANGELOG.md", repoURL, latest), "Release", releaseURL(latest)),
	}
}

func (b *bot) nudgeUpdate(ev heartbeatEvent) {
	latest, err := latestVersion(b.cfg.GitHubToken)
	if err != nil {
		log.Printf("update nudge: %v", err)
		return
	}
	if !olderVersion(ev.Version, latest) {
		return
	}
	nudgeMu.Lock()
	var notice, dm bool
	b.store.update(func(d *storeData) { notice, dm = planNudge(d, ev.User, ev.Hash, latest) })
	nudgeMu.Unlock()
	if notice {
		b.sendToApp(ev.Hash, updateNotice(ev.Version, latest))
	}
	if !dm || b.s == nil {
		return
	}
	ch, err := b.s.UserChannelCreate(ev.User)
	if err == nil {
		_, err = b.s.ChannelMessageSendComplex(ch.ID, updateDM(ev.Version, latest))
	}
	if err != nil {
		log.Printf("update nudge DM %s: %v", ev.User, err) // usually DMs turned off
	}
}

// sendToApp is sendToApps for one app, by its token hash.
func (b *bot) sendToApp(hash string, it inboxItem) bool {
	now := appNow()
	it.ID, it.At = newToken(8), now
	if it.Expires.IsZero() {
		it.Expires = now.Add(inboxKeep)
	}
	sent := false
	b.store.touch(func(d *storeData) {
		if _, ok := d.AppLinks[hash]; !ok {
			return
		}
		if d.AppInbox == nil {
			d.AppInbox = map[string][]inboxItem{}
		}
		box := pruneInbox(d.AppInbox[hash], now)
		if len(box) >= maxInboxItems {
			box = box[len(box)-maxInboxItems+1:]
		}
		d.AppInbox[hash], sent = append(box, it), true
	})
	if sent {
		inboxWaiters.Lock()
		for _, c := range inboxWaiters.m[hash] {
			select {
			case c <- struct{}{}:
			default:
			}
		}
		inboxWaiters.Unlock()
	}
	return sent
}
