package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /provider-status runs `otakase -provider-status` (the add-on image ships
// the latest release) and shows which sources answer from here.

type providerResult struct {
	Name    string
	OK      bool
	Enabled bool
	Detail  string
	adult   bool
}

var (
	providerLineRe = regexp.MustCompile(`^\s+([✓✗])\s+(\S+)\s+\[([^\]]*)\]`)
)

// adultProviders only serve 18+ titles. They are counted only for members
// whose linked AniList shows adult content.
var adultProviders = map[string]string{"sukebei": "nyaa 18+"}

// parseProviderStatus reads the CLI's report.
func parseProviderStatus(out string) []providerResult {
	var list []providerResult
	var cur *providerResult
	for _, line := range strings.Split(out, "\n") {
		if m := providerLineRe.FindStringSubmatch(line); m != nil {
			_, adult := adultProviders[m[2]]
			list = append(list, providerResult{Name: m[2], OK: m[1] == "✓", Enabled: !strings.Contains(m[3], "disabled"), adult: adult})
			cur = &list[len(list)-1]
			continue
		}
		t := strings.TrimSpace(line)
		if cur == nil || t == "" {
			continue
		}
		if strings.HasPrefix(t, "reason:") {
			if strings.Contains(strings.ToLower(t), "adult") {
				cur.adult = true
			}
			continue
		}
		if cur.Detail == "" {
			cur.Detail = t
		}
	}
	return list
}

// visibleProviders is the list without the 18+ sources unless adult is set.
func visibleProviders(list []providerResult, adult bool) []providerResult {
	var out []providerResult
	for _, p := range list {
		if adult || !p.adult {
			out = append(out, p)
		}
	}
	return out
}

// providerEmbed is the source check by count only: members see how many
// sources work, never which. The 18+ ones are counted on a line of their
// own, and only for members whose AniList allows adult content.
func providerEmbed(list []providerResult, version string, adult bool) *discordgo.MessageEmbed {
	count := func(adultOnes bool) (working, total int) {
		for _, p := range list {
			if p.adult != adultOnes {
				continue
			}
			total++
			if p.OK {
				working++
			}
		}
		return working, total
	}
	working, total := count(false)
	var desc string
	switch {
	case total == 0:
		desc = "No sources were checked."
	case working == total:
		desc = "Everything should play."
	case working > 0:
		desc = "Some sources are down. The app tries the others by itself."
	default:
		desc = "No source answers right now. Try again later."
	}
	if adult {
		if w, t := count(true); t > 0 {
			desc += fmt.Sprintf("\n18+ sources: %d of %d working", w, t)
		}
	}
	e := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Sources: %d of %d working", working, total),
		Description: desc,
		Color:       shu,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Checked with " + version},
		Timestamp:   time.Now().Format(time.RFC3339),
	}
	if working == 0 {
		e.Color = 0x8A8276
	}
	return e
}

// providerCache keeps the last report for five minutes, so a busy support
// channel doesn't run the check over and over.
type providerCache struct {
	mu      sync.Mutex
	at      time.Time
	list    []providerResult
	version string
}

// providerStatus is the source check as one viewer should see it: 18+
// sources only when adult is set.
func (b *bot) providerStatus(adult bool) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	list, version, fail := b.checkProviders()
	if fail != nil {
		return fail, nil
	}
	return withAppReports(providerEmbed(list, version, adult), appNow(), adult), nil
}

// providerStatusFor answers /provider-status. The reply is only seen by
// the member who asked, so it can list the 18+ sources when their AniList
// allows adult content.
func (b *bot) providerStatusFor(userID string) func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	return func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		return b.providerStatus(b.adultAllowed(userID))
	}
}

// adultAllowed reports whether the member's linked AniList has adult
// content turned on. Members without a linked AniList, or linked by name
// only (the bot can't read their settings then), count as off.
func (b *bot) adultAllowed(userID string) bool {
	var link anilistLink
	b.store.view(func(d *storeData) { link = d.AniList[userID] })
	if link.Token == "" {
		return false
	}
	on, err := anilistDisplaysAdult(link.Token)
	if err != nil {
		log.Printf("adult setting: %v", err)
		return false
	}
	return on
}

// anilistDisplaysAdult reads the viewer's "18+ content" setting.
func anilistDisplaysAdult(token string) (bool, error) {
	var r struct {
		Data struct {
			Viewer *struct {
				Options struct {
					DisplayAdultContent bool `json:"displayAdultContent"`
				} `json:"options"`
			} `json:"Viewer"`
		} `json:"data"`
	}
	if err := anilistSocialPost(token, `{ Viewer { options { displayAdultContent } } }`, nil, &r); err != nil {
		return false, err
	}
	if r.Data.Viewer == nil {
		return false, fmt.Errorf("no viewer")
	}
	return r.Data.Viewer.Options.DisplayAdultContent, nil
}

// checkProviders runs the CLI's check, or returns the cached result. fail
// is set instead when there is nothing to show.
func (b *bot) checkProviders() (list []providerResult, version string, fail *discordgo.MessageEmbed) {
	c := &b.providers
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.list != nil && time.Since(c.at) < 5*time.Minute {
		return c.list, c.version, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	home, _ := os.MkdirTemp("", "otakase-status")
	defer os.RemoveAll(home)
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, b.cfg.OtakaseCLI, args...)
		// The bot reads every source's line; members are shown counts only.
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home, "OTAKASE_PROVIDER_STATUS_DETAILS=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := run("-provider-status")
	list = parseProviderStatus(out)
	if len(visibleProviders(list, false)) == 0 {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, "", errorEmbed("The source check isn't available on this bot right now. Run `otakase -provider-status` yourself instead.")
		}
		if err != nil {
			return nil, "", errorEmbed("Couldn't run the source check here (%v). Run `otakase -provider-status` yourself instead.", err)
		}
		return nil, "", errorEmbed("The source check gave no results. Run `otakase -provider-status` yourself instead.")
	}
	version = "otakase"
	if v, err := run("-v"); err == nil {
		if i := strings.LastIndex(strings.TrimSpace(v), " "); i >= 0 {
			version = "otakase " + strings.TrimSpace(v)[i+1:]
		}
	}
	c.list, c.version, c.at = list, version, time.Now()
	return list, version, nil
}

// downRe spots "is it down?" questions in chat, which get the source check
// as a reply instead of the FAQ answer.
var downRe = regexp.MustCompile(`(?i)\b(is|are)\s+(it|otakase|the\s+\w+|any\s+\w+|\w+)\s+(down|broken|dead|offline)\b` +
	`|\b(sources?|providers?|sites?|servers?)\s+(are\s+|is\s+)?(down|broken|dead|offline|not working)\b` +
	`|\bdown for (everyone|anyone|me)\b` +
	`|\banyone else\b.*\b(not working|broken|down|no results|nothing found)\b`)

// downCheck answers a "is it down?" message with the source check, at most
// once per channel every ten minutes.
func (b *bot) downCheck(m *discordgo.MessageCreate) {
	// Without the CLI there's nothing useful to answer with.
	if _, err := exec.LookPath(b.cfg.OtakaseCLI); err != nil {
		return
	}
	if !b.issues.allowKey(m.ChannelID + "/down") {
		return
	}
	go func() {
		_ = b.s.ChannelTyping(m.ChannelID)
		// Everyone in the channel sees this one, so no 18+ sources.
		e, _ := b.providerStatus(false)
		_, err := b.s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Content:         "Here's what works from the bot's side right now:",
			Embeds:          []*discordgo.MessageEmbed{e},
			Reference:       m.Reference(),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		})
		if err != nil {
			log.Printf("down check: %v", err)
		}
	}()
}
