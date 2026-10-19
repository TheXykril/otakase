package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// GitHub lookups shared by the known issues post and the bug report
// follow-up.

// ghRepoAPI is a variable so tests can point it at a fake server.
var ghRepoAPI = "https://api.github.com/repos/TheXykril/otakase"

// updateHowTo is the same advice the version check gives.
const updateHowTo = "Update with `otakase -u` (or `/install` for other ways), then check `otakase -v` and try again."

// ghIssueFull is an issue with the dates and body the support features need.
type ghIssueFull struct {
	ghIssue
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

// fetchReleases returns the published releases, newest first.
func fetchStableReleases(token string) ([]ghRelease, error) {
	var all []ghRelease
	if err := getJSON(githubRequest("GET", ghRepoAPI+"/releases?per_page=30", token), &all); err != nil {
		return nil, err
	}
	var out []ghRelease
	for _, r := range all {
		if !r.Draft && !r.Prerelease && !r.PublishedAt.IsZero() {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].PublishedAt.After(out[b].PublishedAt) })
	return out, nil
}

// releaseAfter is the first release published after t.
func releaseAfter(rels []ghRelease, t time.Time) (ghRelease, bool) {
	var best ghRelease
	found := false
	for _, r := range rels {
		if r.PublishedAt.After(t) && (!found || r.PublishedAt.Before(best.PublishedAt)) {
			best, found = r, true
		}
	}
	return best, found
}

// fetchIssue reads one issue.
func fetchIssue(n int, token string) (ghIssueFull, error) {
	var is ghIssueFull
	err := getJSON(githubRequest("GET", fmt.Sprintf("%s/issues/%d", ghRepoAPI, n), token), &is)
	return is, err
}

// notFound reports whether a Discord call failed because the thing is gone.
func notFound(err error) bool {
	rest, ok := err.(*discordgo.RESTError)
	return ok && rest.Response != nil && rest.Response.StatusCode == 404
}

// postLink links a support post by id.
func postLink(id string) string { return "<#" + id + ">" }

// supportSolvedTag is the id of the support forum's Solved tag, if any.
func supportSolvedTag(forum *discordgo.Channel) string {
	for _, t := range forum.AvailableTags {
		if strings.EqualFold(t.Name, "Solved") {
			return t.ID
		}
	}
	return ""
}

// withUnarchived runs fn in a thread, opening it first when it is archived
// and archiving it again afterwards.
func (b *bot) withUnarchived(threadID string, fn func() error) error {
	th, err := b.s.Channel(threadID)
	if err != nil {
		return err
	}
	archived := th.ThreadMetadata != nil && th.ThreadMetadata.Archived
	if archived {
		open := false
		if _, err := b.s.ChannelEditComplex(threadID, &discordgo.ChannelEdit{Archived: &open}); err != nil {
			return err
		}
	}
	err = fn()
	if archived {
		closed := true
		if _, e := b.s.ChannelEditComplex(threadID, &discordgo.ChannelEdit{Archived: &closed}); e != nil && err == nil {
			err = e
		}
	}
	return err
}
