package main

import (
	"fmt"
	"regexp"
	"time"

	"github.com/bwmarrin/discordgo"
)

// When /bug or "Report on GitHub" is used in a support post, the bot finds
// the GitHub issue made from it (the issue links back to the post) and,
// once the issue is closed and a release with the fix is out, tells the
// poster in the post how to update. Issues closed without a fix are
// mentioned once too.

const (
	bugFollowEvery  = 30 * time.Minute
	bugFindFor      = 14 * 24 * time.Hour  // how long to look for the issue
	bugFollowFor    = 365 * 24 * time.Hour // how long to wait for the fix
	bugFixed        = "fixed"
	bugNotPlanned   = "not_planned"
	bugDuplicate    = "duplicate"
	bugStillWaiting = ""
)

// bugReport is a support post that was turned into a GitHub issue.
type bugReport struct {
	Poster   string    `json:"poster"`
	Reported time.Time `json:"reported"`
	Issue    int       `json:"issue,omitempty"` // 0 until the issue is found
}

// postURLRe finds the support post link the pre-filled issue ends with.
var postURLRe = regexp.MustCompile(`discord(?:app)?\.com/channels/(\d+)/(\d+)`)

// threadFromIssueBody is the support post an issue links to in guild.
func threadFromIssueBody(body, guild string) string {
	for _, m := range postURLRe.FindAllStringSubmatch(body, -1) {
		if m[1] == guild {
			return m[2]
		}
	}
	return ""
}

// bugOutcome says what to tell the post about an issue, and for a fix, the
// release it's in. rels is any order.
func bugOutcome(is ghIssueFull, rels []ghRelease) (string, ghRelease) {
	if is.State != "closed" || is.ClosedAt == nil {
		return bugStillWaiting, ghRelease{}
	}
	switch is.StateReason {
	case "not_planned":
		return bugNotPlanned, ghRelease{}
	case "duplicate":
		return bugDuplicate, ghRelease{}
	}
	if r, ok := releaseAfter(rels, *is.ClosedAt); ok {
		return bugFixed, r
	}
	return bugStillWaiting, ghRelease{}
}

// rememberBugReport notes a support post that is being reported on GitHub.
func (b *bot) rememberBugReport(th *discordgo.Channel) {
	owner := b.postOwner(th) // the member, also for posts the bot made for them
	b.store.update(func(d *storeData) {
		if d.BugReports == nil {
			d.BugReports = map[string]bugReport{}
		}
		if _, ok := d.BugReports[th.ID]; !ok {
			d.BugReports[th.ID] = bugReport{Poster: owner, Reported: time.Now()}
		}
	})
}

func (b *bot) bugFollowLoop() {
	if b.cfg.SupportForum == "" {
		return
	}
	time.Sleep(3 * time.Minute)
	for {
		if err := b.followBugReports(time.Now()); err != nil {
			b.jobFailed("Bug report follow-up", err)
		} else {
			b.jobOK("Bug report follow-up")
		}
		time.Sleep(bugFollowEvery)
	}
}

func (b *bot) followBugReports(now time.Time) error {
	reports := map[string]bugReport{}
	b.store.update(func(d *storeData) {
		for id, r := range d.BugReports {
			if (r.Issue == 0 && now.Sub(r.Reported) > bugFindFor) || now.Sub(r.Reported) > bugFollowFor {
				delete(d.BugReports, id)
				continue
			}
			reports[id] = r
		}
	})
	if len(reports) == 0 {
		return nil
	}
	if err := b.findBugIssues(reports); err != nil {
		return err
	}
	var rels []ghRelease
	for thread, r := range reports {
		if r.Issue == 0 {
			continue
		}
		is, err := fetchIssue(r.Issue, b.cfg.GitHubToken)
		if err != nil {
			return err
		}
		if is.State != "closed" {
			continue
		}
		if rels == nil {
			if rels, err = fetchStableReleases(b.cfg.GitHubToken); err != nil {
				return err
			}
		}
		outcome, rel := bugOutcome(is, rels)
		if outcome == bugStillWaiting {
			continue
		}
		err = b.withUnarchived(thread, func() error {
			_, err := b.s.ChannelMessageSendComplex(thread, bugFollowMessage(outcome, r.Poster, is, rel))
			return err
		})
		if err != nil && !notFound(err) {
			return err
		}
		if outcome == bugFixed {
			b.noticeBugFixed(thread, r.Poster, is, rel)
		}
		b.store.update(func(d *storeData) { delete(d.BugReports, thread) })
	}
	return nil
}

// findBugIssues looks through issues changed since the oldest report for
// ones that link to a reported post, and remembers their numbers.
func (b *bot) findBugIssues(reports map[string]bugReport) error {
	var oldest time.Time
	for _, r := range reports {
		if r.Issue == 0 && (oldest.IsZero() || r.Reported.Before(oldest)) {
			oldest = r.Reported
		}
	}
	if oldest.IsZero() {
		return nil
	}
	var issues []ghIssueFull
	url := ghRepoAPI + "/issues?state=all&sort=created&direction=desc&per_page=100&since=" +
		oldest.Add(-time.Hour).UTC().Format(time.RFC3339)
	if err := getJSON(githubRequest("GET", url, b.cfg.GitHubToken), &issues); err != nil {
		return err
	}
	found := map[string]int{}
	for _, is := range issues {
		if is.PullRequest != nil {
			continue
		}
		thread := threadFromIssueBody(is.Body, b.cfg.GuildID)
		if r, ok := reports[thread]; ok && r.Issue == 0 && (found[thread] == 0 || is.Number < found[thread]) {
			found[thread] = is.Number
		}
	}
	if len(found) == 0 {
		return nil
	}
	b.store.update(func(d *storeData) {
		for thread, n := range found {
			if r, ok := d.BugReports[thread]; ok {
				r.Issue = n
				d.BugReports[thread] = r
			}
		}
	})
	for thread, n := range found {
		r := reports[thread]
		r.Issue = n
		reports[thread] = r
	}
	return nil
}

func bugFollowMessage(outcome, poster string, is ghIssueFull, rel ghRelease) *discordgo.MessageSend {
	link := fmt.Sprintf("[#%d](%s)", is.Number, is.HTMLURL)
	msg := &discordgo.MessageSend{
		Content:         "<@" + poster + ">",
		AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{poster}},
	}
	e := &discordgo.MessageEmbed{Color: shu}
	switch outcome {
	case bugFixed:
		v := rel.version()
		e.Title = "Fixed in " + v
		e.Description = fmt.Sprintf("The GitHub issue from this post, %s, is fixed in **%s**.\n\n%s", link, v, updateHowTo)
		msg.Components = linkButtons("Changelog", fmt.Sprintf("%s/blob/%s/CHANGELOG.md", repoURL, rel.TagName), "Release", rel.HTMLURL)
	case bugDuplicate:
		e.Color = 0x8A8276
		e.Description = fmt.Sprintf("The GitHub issue from this post, %s, was closed as a duplicate of another issue. "+
			"The issue links to the one being worked on, so follow that one for updates.", link)
	default:
		e.Color = 0x8A8276
		e.Description = fmt.Sprintf("The GitHub issue from this post, %s, was closed as not planned. "+
			"Sorry this one won't be fixed; the issue on GitHub says why. Reply here if you have questions.", link)
	}
	msg.Embeds = []*discordgo.MessageEmbed{e}
	return msg
}
