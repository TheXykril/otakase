package main

import (
	"context"
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
	resultsRe      = regexp.MustCompile(`^(\d+) result\(s\) in (.+)$`)
)

// adultProviders are never listed; they only serve 18+ titles.
var adultProviders = map[string]bool{"sukebei": true}

// parseProviderStatus reads the CLI's report.
func parseProviderStatus(out string) []providerResult {
	var list []providerResult
	var cur *providerResult
	for _, line := range strings.Split(out, "\n") {
		if m := providerLineRe.FindStringSubmatch(line); m != nil {
			list = append(list, providerResult{Name: m[2], OK: m[1] == "✓", Enabled: !strings.Contains(m[3], "disabled")})
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
	out2 := list[:0]
	for _, p := range list {
		if !p.adult && !adultProviders[p.Name] {
			out2 = append(out2, p)
		}
	}
	return out2
}

func providerLine(p providerResult) string {
	icon := "🟢"
	detail := p.Detail
	if m := resultsRe.FindStringSubmatch(detail); m != nil {
		detail = m[2]
	}
	if !p.OK {
		icon = "🔴"
		if i := strings.Index(detail, ": "); i >= 0 && strings.HasPrefix(detail, "FAILED") {
			detail = detail[i+2:]
		}
		detail = clip(detail, 80)
	}
	state := ""
	if !p.Enabled {
		state = " · off by default"
	}
	return fmt.Sprintf("%s **%s** · %s%s", icon, p.Name, detail, state)
}

func providerEmbed(list []providerResult, version string) *discordgo.MessageEmbed {
	working := 0
	var lines []string
	for _, p := range list {
		if p.OK {
			working++
		}
		lines = append(lines, providerLine(p))
	}
	e := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Sources: %d of %d working", working, len(list)),
		Description: strings.Join(lines, "\n"),
		Color:       shu,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Checked with " + version + " · run otakase -provider-status to compare with yours"},
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
	mu    sync.Mutex
	at    time.Time
	embed *discordgo.MessageEmbed
}

func (b *bot) providerStatus() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	c := &b.providers
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.embed != nil && time.Since(c.at) < 5*time.Minute {
		return c.embed, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	home, _ := os.MkdirTemp("", "otakase-status")
	defer os.RemoveAll(home)
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, b.cfg.OtakaseCLI, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := run("-provider-status")
	list := parseProviderStatus(out)
	if len(list) == 0 {
		if err != nil {
			return errorEmbed("Couldn't run the source check here (%v). Run `otakase -provider-status` yourself instead.", err), nil
		}
		return errorEmbed("The source check gave no results. Run `otakase -provider-status` yourself instead."), nil
	}
	version := "otakase"
	if v, err := run("-v"); err == nil {
		if i := strings.LastIndex(strings.TrimSpace(v), " "); i >= 0 {
			version = "otakase " + strings.TrimSpace(v)[i+1:]
		}
	}
	c.embed, c.at = providerEmbed(list, version), time.Now()
	return c.embed, nil
}

// downRe spots "is it down?" questions in chat, which get the source check
// as a reply instead of the FAQ answer.
var downRe = regexp.MustCompile(`(?i)\b(is|are)\s+(it|otakase|the\s+\w+|any\s+\w+|\w+)\s+(down|broken|dead|offline)\b` +
	`|\b(sources?|providers?|sites?|servers?)\s+(are\s+|is\s+)?(down|broken|dead|offline|not working)\b` +
	`|\bdown for (everyone|anyone|me)\b` +
	`|\banyone else\b.*\b(not working|broken|down|no results|nothing found)\b`)

// downCheck answers a "is it down?" message with the source check, at most
// once per channel every ten minutes. It reports whether it answered.
func (b *bot) downCheck(m *discordgo.MessageCreate) bool {
	if !downRe.MatchString(m.Content) || !b.issues.allowKey(m.ChannelID+"/down") {
		return false
	}
	go func() {
		_ = b.s.ChannelTyping(m.ChannelID)
		e, _ := b.providerStatus()
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
	return true
}
