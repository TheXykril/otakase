package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Typing #123 in chat shows that GitHub issue or pull request.

// issueRefRe matches #123 at the start of a word, so channel mentions
// (<#id>) and URL fragments are left alone.
var issueRefRe = regexp.MustCompile(`(?:^|[\s(])#(\d{1,5})\b`)

// codeRe drops inline and fenced code before looking for references.
var codeRe = regexp.MustCompile("(?s)```.*?```|`[^`]*`")

// issueRefs returns up to three distinct issue numbers mentioned in text.
func issueRefs(text string) []int {
	text = codeRe.ReplaceAllString(text, "")
	var out []int
	for _, m := range issueRefRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[1])
		if n == 0 || containsInt(out, n) {
			continue
		}
		out = append(out, n)
		if len(out) == 3 {
			break
		}
	}
	return out
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// issueCooldown stops the same number being shown again and again in one
// channel while people discuss it.
type issueCooldown struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (c *issueCooldown) allow(channel string, n int) bool {
	return c.allowKey(fmt.Sprintf("%s/%d", channel, n))
}

// allowKey reports whether key wasn't seen in the last ten minutes, and
// marks it seen.
func (c *issueCooldown) allowKey(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		c.last = map[string]time.Time{}
	}
	if time.Since(c.last[key]) < 10*time.Minute {
		return false
	}
	c.last[key] = time.Now()
	return true
}

type ghIssue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	StateReason string `json:"state_reason"`
	HTMLURL     string `json:"html_url"`
	Comments    int    `json:"comments"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest *struct {
		MergedAt *time.Time `json:"merged_at"`
	} `json:"pull_request"`
	Draft bool `json:"draft"`
}

// issueStatus names what the item is and its state, with a colour for it.
func issueStatus(is ghIssue) (string, int) {
	kind := "Issue"
	if is.PullRequest != nil {
		kind = "Pull request"
	}
	switch {
	case is.PullRequest != nil && is.PullRequest.MergedAt != nil:
		return kind + " · merged", 0x8957E5
	case is.State == "open" && is.Draft:
		return kind + " · draft", 0x8A8276
	case is.State == "open":
		return kind + " · open", 0x3FB950
	case is.StateReason == "not_planned":
		return kind + " · closed, not planned", 0x8A8276
	case is.PullRequest != nil:
		return kind + " · closed", 0xB8361F
	default:
		return kind + " · closed", 0x8957E5
	}
}

func issueEmbed(is ghIssue) *discordgo.MessageEmbed {
	status, color := issueStatus(is)
	var labels []string
	for _, l := range is.Labels {
		labels = append(labels, l.Name)
	}
	footer := status + " · by " + is.User.Login
	if len(labels) > 0 {
		footer += " · " + strings.Join(labels, ", ")
	}
	return &discordgo.MessageEmbed{
		Title:  clip(fmt.Sprintf("#%d %s", is.Number, is.Title), 256),
		URL:    is.HTMLURL,
		Color:  color,
		Footer: &discordgo.MessageEmbedFooter{Text: footer},
	}
}

// clip shortens s to n runes, ending with … when it cuts.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// linkIssues replies to a message that mentions #numbers with a small card
// for each one that exists.
func (b *bot) linkIssues(m *discordgo.MessageCreate) {
	var embeds []*discordgo.MessageEmbed
	for _, n := range issueRefs(m.Content) {
		if !b.issues.allow(m.ChannelID, n) {
			continue
		}
		var is ghIssue
		url := fmt.Sprintf("https://api.github.com/repos/TheXykril/otakase/issues/%d", n)
		if err := getJSON(githubRequest("GET", url, b.cfg.GitHubToken), &is); err != nil {
			continue // not an issue here, or GitHub is unreachable
		}
		embeds = append(embeds, issueEmbed(is))
	}
	if len(embeds) == 0 {
		return
	}
	_, _ = b.s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds:          embeds,
		Reference:       m.Reference(),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
}
