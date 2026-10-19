package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// A pinned "Known issues" post in the support forum lists the open GitHub
// issues labelled known-issue and the ones fixed in recent releases. The bot
// edits the post's first message every half hour.

const (
	knownIssueLabel  = "known-issue"
	knownIssuesTitle = "Known issues"
	knownIssuesEvery = 30 * time.Minute
	knownIssuesMax   = 15 // open issues listed before "and N more"
)

var knownIssuesURL = repoURL + "/issues?q=label%3A" + knownIssueLabel

// knownIssuesLast is the text the post was last edited to, so an unchanged
// list is not edited again. knownIssuesMu keeps the loop and the dashboard
// from refreshing at the same time.
var (
	knownIssuesLast string
	knownIssuesMu   sync.Mutex
)

func (b *bot) knownIssuesLoop() {
	if b.cfg.SupportForum == "" {
		return
	}
	time.Sleep(20 * time.Second) // let the startup work settle
	for {
		if err := b.refreshKnownIssues(); err != nil {
			b.jobFailed("Known issues post", err)
		} else {
			b.jobOK("Known issues post")
		}
		time.Sleep(knownIssuesEvery)
	}
}

func fetchKnownIssues(token string) ([]ghIssueFull, error) {
	var all []ghIssueFull
	url := ghRepoAPI + "/issues?labels=" + knownIssueLabel + "&state=all&sort=updated&direction=desc&per_page=100"
	if err := getJSON(githubRequest("GET", url, token), &all); err != nil {
		return nil, err
	}
	var out []ghIssueFull
	for _, is := range all {
		if is.PullRequest == nil {
			out = append(out, is)
		}
	}
	return out, nil
}

// knownIssueStatus is the short status shown after an open issue.
func knownIssueStatus(is ghIssueFull) string {
	if len(is.Assignees) > 0 {
		return "being worked on"
	}
	return fmt.Sprintf("reported <t:%d:R>", is.CreatedAt.Unix())
}

func knownIssueLine(is ghIssueFull, status string) string {
	title := strings.NewReplacer("[", "(", "]", ")").Replace(clip(is.Title, 80))
	line := fmt.Sprintf("- [#%d %s](%s)", is.Number, title, is.HTMLURL)
	if status != "" {
		line += " · " + status
	}
	return line
}

// knownIssuesText builds the post: open issues, then the ones fixed in the
// latest release (closed between the two newest releases) and the ones
// fixed since then, which come in the next release. rels is newest first.
func knownIssuesText(issues []ghIssueFull, rels []ghRelease) string {
	var open, inLatest, inNext []ghIssueFull
	var latest, previous time.Time
	if len(rels) > 0 {
		latest = rels[0].PublishedAt
	}
	if len(rels) > 1 {
		previous = rels[1].PublishedAt
	}
	for _, is := range issues {
		switch {
		case is.State == "open":
			open = append(open, is)
		case is.StateReason != "completed" || is.ClosedAt == nil || len(rels) == 0:
			// not planned, duplicates and anything without a release to name
		case is.ClosedAt.After(latest):
			inNext = append(inNext, is)
		case is.ClosedAt.After(previous):
			inLatest = append(inLatest, is)
		}
	}
	newest := func(xs []ghIssueFull) {
		sort.Slice(xs, func(a, b int) bool { return xs[a].Number > xs[b].Number })
	}
	newest(open)
	newest(inLatest)
	newest(inNext)

	var parts []string
	if len(open) == 0 {
		parts = append(parts, "No known problems right now.")
	} else {
		lines := []string{"These are problems we already know about. No need to make a new post for them: open one to follow it or add details."}
		for k, is := range open {
			if k == knownIssuesMax {
				lines = append(lines, fmt.Sprintf("…and %d more on GitHub", len(open)-k))
				break
			}
			lines = append(lines, knownIssueLine(is, knownIssueStatus(is)))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	section := func(title string, xs []ghIssueFull) {
		if len(xs) == 0 {
			return
		}
		lines := []string{"**" + title + "**"}
		for k, is := range xs {
			if k == 10 {
				lines = append(lines, fmt.Sprintf("…and %d more", len(xs)-k))
				break
			}
			lines = append(lines, knownIssueLine(is, ""))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	if len(rels) > 0 {
		section("Fixed in the latest release ("+rels[0].version()+")", inLatest)
		if len(inLatest) > 0 {
			parts = append(parts, updateHowTo)
		}
	}
	section("Fixed, coming in the next release", inNext)
	return clip(strings.Join(parts, "\n\n"), 4000)
}

// withKnownNotes puts the notes staff added on the dashboard above the
// GitHub list.
func withKnownNotes(text string, notes []string) string {
	if len(notes) == 0 {
		return text
	}
	return clip("- "+strings.Join(notes, "\n- ")+"\n\n"+text, 4000)
}

func knownIssuesEmbed(text string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: knownIssuesTitle, Description: text, Color: shu,
		Footer: &discordgo.MessageEmbedFooter{Text: "Kept up to date from GitHub by the bot"}}
}

// knownIssuesTag picks a tag for the post: one named like "Known issues"
// or "Info", or when the forum requires a tag, the first that isn't Solved.
func knownIssuesTag(forum *discordgo.Channel) []string {
	for _, want := range []string{"known issues", "known issue", "info", "announcement"} {
		for _, t := range forum.AvailableTags {
			if strings.EqualFold(t.Name, want) {
				return []string{t.ID}
			}
		}
	}
	if forum.Flags&discordgo.ChannelFlagRequireTag != 0 {
		for _, t := range forum.AvailableTags {
			if !strings.EqualFold(t.Name, "Solved") {
				return []string{t.ID}
			}
		}
	}
	return nil
}

func (b *bot) refreshKnownIssues() error {
	knownIssuesMu.Lock()
	defer knownIssuesMu.Unlock()
	issues, err := fetchKnownIssues(b.cfg.GitHubToken)
	if err != nil {
		return err
	}
	rels, err := fetchStableReleases(b.cfg.GitHubToken)
	if err != nil {
		return err
	}
	var notes []string
	b.store.view(func(d *storeData) { notes = append(notes, d.KnownNotes...) })
	text := withKnownNotes(knownIssuesText(issues, rels), notes)

	var id string
	b.store.view(func(d *storeData) { id = d.KnownIssuesPost })
	if id != "" {
		th, err := b.s.Channel(id)
		switch {
		case notFound(err):
			id = ""
		case err != nil:
			return err
		case th.ParentID != b.cfg.SupportForum:
			id = ""
		default:
			if err := b.keepKnownIssuesOpen(th); err != nil {
				return err
			}
		}
	}
	if id == "" {
		return b.createKnownIssues(text)
	}
	if text == knownIssuesLast {
		return nil
	}
	e := knownIssuesEmbed(text)
	// A forum post's first message has the same id as the post.
	_, err = b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: id, Channel: id, Embeds: &[]*discordgo.MessageEmbed{e}})
	if notFound(err) {
		// The first message is gone, so the post can't be edited: start over.
		if _, err := b.s.ChannelDelete(id); err != nil && !notFound(err) {
			return err
		}
		return b.createKnownIssues(text)
	}
	if err != nil {
		return err
	}
	knownIssuesLast = text
	return nil
}

// keepKnownIssuesOpen reopens the post if it was archived. If staff unpin
// it (a forum has one pinned post), it stays unpinned.
func (b *bot) keepKnownIssuesOpen(th *discordgo.Channel) error {
	if th.ThreadMetadata == nil || !th.ThreadMetadata.Archived {
		return nil
	}
	open := false
	_, err := b.s.ChannelEditComplex(th.ID, &discordgo.ChannelEdit{Archived: &open})
	return err
}

func (b *bot) createKnownIssues(text string) error {
	forum, err := b.s.Channel(b.cfg.SupportForum)
	if err != nil {
		return err
	}
	th, err := b.s.ForumThreadStartComplex(b.cfg.SupportForum, &discordgo.ThreadStart{
		Name: knownIssuesTitle, AutoArchiveDuration: 10080, AppliedTags: knownIssuesTag(forum),
	}, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{knownIssuesEmbed(text)},
		Components: linkButtons("All known issues on GitHub", knownIssuesURL),
	})
	if err != nil {
		return err
	}
	b.store.update(func(d *storeData) { d.KnownIssuesPost = th.ID })
	knownIssuesLast = text
	// Pinned keeps it on top of the forum; locked keeps it to the list.
	flags := th.Flags | discordgo.ChannelFlagPinned
	locked := true
	_, err = b.s.ChannelEditComplex(th.ID, &discordgo.ChannelEdit{Flags: &flags, Locked: &locked})
	return err
}
