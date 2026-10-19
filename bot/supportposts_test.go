package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func day(n int) time.Time { return time.Date(2026, 9, n, 12, 0, 0, 0, time.UTC) }

func issueAt(n int, state, reason string, closed *time.Time) ghIssueFull {
	is := ghIssueFull{ClosedAt: closed, CreatedAt: day(1)}
	is.Number, is.Title, is.State, is.StateReason = n, fmt.Sprintf("Issue %d", n), state, reason
	is.HTMLURL = fmt.Sprintf("https://github.com/TheXykril/otakase/issues/%d", n)
	return is
}

func tptr(t time.Time) *time.Time { return &t }

var testStableReleases = []ghRelease{ // newest first
	{TagName: "v26.6.0", PublishedAt: day(20), HTMLURL: "https://example.com/26.6.0"},
	{TagName: "v26.5.0", PublishedAt: day(10), HTMLURL: "https://example.com/26.5.0"},
}

func TestKnownIssuesTextEmpty(t *testing.T) {
	if got := knownIssuesText(nil, testStableReleases); got != "No known problems right now." {
		t.Fatalf("got %q", got)
	}
}

func TestKnownIssuesText(t *testing.T) {
	open := issueAt(5, "open", "", nil)
	working := issueAt(6, "open", "", nil)
	working.Assignees = append(working.Assignees, struct {
		Login string `json:"login"`
	}{"dev"})
	issues := []ghIssueFull{
		open, working,
		issueAt(3, "closed", "completed", tptr(day(15))),   // fixed in 26.6.0
		issueAt(4, "closed", "completed", tptr(day(25))),   // fixed after it
		issueAt(2, "closed", "completed", tptr(day(5))),    // older, not shown
		issueAt(7, "closed", "not_planned", tptr(day(15))), // not shown
	}
	got := knownIssuesText(issues, testStableReleases)
	for _, want := range []string{"[#5 Issue 5]", "#6 Issue 6](", "being worked on", "reported <t:",
		"**Fixed in the latest release (26.6.0)**\n- [#3 Issue 3]", "otakase -u", "**Fixed, coming in the next release**\n- [#4 Issue 4]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, not := range []string{"#2 ", "#7 ", "No known problems"} {
		if strings.Contains(got, not) {
			t.Errorf("unexpected %q in:\n%s", not, got)
		}
	}
	if strings.Index(got, "#6") > strings.Index(got, "#5") {
		t.Error("newest open issue should come first")
	}
}

func TestKnownIssuesTextNoOpenButFixed(t *testing.T) {
	got := knownIssuesText([]ghIssueFull{issueAt(3, "closed", "completed", tptr(day(15)))}, testStableReleases)
	if !strings.HasPrefix(got, "No known problems right now.") || !strings.Contains(got, "#3 Issue 3") {
		t.Fatalf("got %q", got)
	}
}

func TestKnownIssuesTag(t *testing.T) {
	forum := &discordgo.Channel{AvailableTags: []discordgo.ForumTag{{ID: "1", Name: "Solved"}, {ID: "2", Name: "Windows"}}}
	if got := knownIssuesTag(forum); got != nil {
		t.Errorf("no tag wanted, got %v", got)
	}
	forum.Flags = discordgo.ChannelFlagRequireTag
	if got := knownIssuesTag(forum); len(got) != 1 || got[0] != "2" {
		t.Errorf("required tag: got %v", got)
	}
	forum.AvailableTags = append(forum.AvailableTags, discordgo.ForumTag{ID: "3", Name: "Known issues"})
	if got := knownIssuesTag(forum); len(got) != 1 || got[0] != "3" {
		t.Errorf("named tag: got %v", got)
	}
}

func TestReleaseAfter(t *testing.T) {
	if r, ok := releaseAfter(testStableReleases, day(12)); !ok || r.version() != "26.6.0" {
		t.Errorf("got %v %v", r, ok)
	}
	if r, ok := releaseAfter(testStableReleases, day(1)); !ok || r.version() != "26.5.0" {
		t.Errorf("first release after: got %v %v", r, ok)
	}
	if _, ok := releaseAfter(testStableReleases, day(21)); ok {
		t.Error("no release after the newest")
	}
}

func TestFetchReleasesSkipsDrafts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]ghRelease{
			{TagName: "v26.5.0", PublishedAt: day(10)},
			{TagName: "v26.7.0-rc1", PublishedAt: day(25), Prerelease: true},
			{TagName: "v26.6.0", PublishedAt: day(20)},
			{TagName: "v26.8.0", Draft: true},
		})
	}))
	defer srv.Close()
	old := ghRepoAPI
	ghRepoAPI = srv.URL
	defer func() { ghRepoAPI = old }()
	rels, err := fetchStableReleases("")
	if err != nil || len(rels) != 2 || rels[0].version() != "26.6.0" {
		t.Fatalf("got %v %v", rels, err)
	}
}

func TestBugOutcome(t *testing.T) {
	cases := []struct {
		is      ghIssueFull
		outcome string
		version string
	}{
		{issueAt(1, "open", "", nil), bugStillWaiting, ""},
		{issueAt(1, "closed", "completed", tptr(day(15))), bugFixed, "26.6.0"},
		{issueAt(1, "closed", "", tptr(day(5))), bugFixed, "26.5.0"},
		{issueAt(1, "closed", "completed", tptr(day(22))), bugStillWaiting, ""},
		{issueAt(1, "closed", "not_planned", tptr(day(15))), bugNotPlanned, ""},
		{issueAt(1, "closed", "duplicate", tptr(day(15))), bugDuplicate, ""},
	}
	for k, c := range cases {
		got, rel := bugOutcome(c.is, testStableReleases)
		if got != c.outcome || (c.version != "" && rel.version() != c.version) {
			t.Errorf("case %d: got %q %q", k, got, rel.version())
		}
	}
}

func TestBugFollowMessage(t *testing.T) {
	is := issueAt(9, "closed", "completed", tptr(day(15)))
	m := bugFollowMessage(bugFixed, "42", is, testStableReleases[0])
	if m.Content != "<@42>" || m.Embeds[0].Title != "Fixed in 26.6.0" || !strings.Contains(m.Embeds[0].Description, "otakase -u") {
		t.Errorf("fixed message: %+v %+v", m, m.Embeds[0])
	}
	m = bugFollowMessage(bugNotPlanned, "42", is, ghRelease{})
	if !strings.Contains(m.Embeds[0].Description, "not planned") {
		t.Errorf("not planned message: %q", m.Embeds[0].Description)
	}
}

func TestThreadFromIssueBody(t *testing.T) {
	u, _ := url.Parse(bugReportURL("mpv fails", "it breaks", "https://discord.com/channels/111/222"))
	body := u.Query().Get("body")
	if got := threadFromIssueBody(body, "111"); got != "222" {
		t.Errorf("got %q from %q", got, body)
	}
	if got := threadFromIssueBody(body, "999"); got != "" {
		t.Errorf("other server: got %q", got)
	}
	if got := threadFromIssueBody("no link here", "111"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestFindSimilarWords(t *testing.T) {
	posts := []supportPost{
		{ID: "10", Title: "mpv not found on windows", Text: "installed with scoop, otakase says mpv is not found when playing an episode"},
		{ID: "11", Title: "Chromecast missing", Text: "my tv never shows up in the cast menu", Solved: true},
		{ID: "12", Title: "AniList sync broken", Text: "progress doesn't update on my list after watching"},
	}
	hits := findSimilar(posts, "20", nil, "Windows: mpv not found", "After installing through scoop it says mpv not found when I pick an episode to play")
	if len(hits) != 1 || hits[0].ID != "10" {
		t.Fatalf("got %+v", hits)
	}
	for _, q := range [][2]string{
		{"Search shows nothing", "I search for frieren and get no results at all"},
		{"Help", "it doesn't work, please help"},
		{"Windows install", "how do I install on windows"},
	} {
		if hits := findSimilar(posts, "20", nil, q[0], q[1]); len(hits) != 0 {
			t.Errorf("%q matched %+v", q[0], hits)
		}
	}
}

func TestFindSimilarVectors(t *testing.T) {
	posts := []supportPost{
		{ID: "1", Vec: packVec([]float64{1, 0, 0})},
		{ID: "2", Vec: packVec([]float64{0.9, 0.1, 0}), Solved: true},
		{ID: "3", Vec: packVec([]float64{0, 1, 0})},
		{ID: "4", Vec: packVec([]float64{0.95, 0.05, 0})},
		{ID: "5", Vec: packVec([]float64{0.99, 0.01, 0.01})},
		{ID: "6", Vec: packVec([]float64{1, 0, 0})}, // the post itself
	}
	hits := findSimilar(posts, "6", []float64{1, 0, 0}, "", "")
	if len(hits) != 3 || hits[0].ID != "2" || hits[1].ID != "1" || hits[2].ID != "5" {
		t.Fatalf("got %+v", hits)
	}
}

func TestPackedVecJSON(t *testing.T) {
	v := []float64{0.12, -0.5, 0.33, 0.01, -0.07}
	p := supportPost{ID: "1", Vec: packVec(v)}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var back supportPost
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if c := cosine(v, back.Vec.floats()); c < 0.999 {
		t.Errorf("cosine after round trip %f", c)
	}
	data, _ = json.Marshal(supportPost{ID: "2"})
	if strings.Contains(string(data), "vec") {
		t.Errorf("empty vector stored: %s", data)
	}
}

func TestAddSupportPost(t *testing.T) {
	var posts []supportPost
	for k := 1; k <= similarKeep+5; k++ {
		posts = addSupportPost(posts, supportPost{ID: fmt.Sprint(1000 + k)})
	}
	if len(posts) != similarKeep || posts[0].ID != fmt.Sprint(1000+similarKeep+5) || posts[len(posts)-1].ID != "1006" {
		t.Fatalf("len %d first %s last %s", len(posts), posts[0].ID, posts[len(posts)-1].ID)
	}
	posts = addSupportPost(posts, supportPost{ID: "1100", Solved: true})
	n := 0
	for _, p := range posts {
		if p.ID == "1100" {
			n++
			if !p.Solved {
				t.Error("not replaced")
			}
		}
	}
	if n != 1 || len(posts) != similarKeep {
		t.Errorf("copies %d len %d", n, len(posts))
	}
}

func TestSummarizePost(t *testing.T) {
	staff := func(u string) bool { return u == "mod" }
	cases := []struct {
		msgs []queueMessage
		want postActivity
	}{
		{[]queueMessage{{Author: "op"}, {Author: "bot", Bot: true}}, postActivity{}},
		{[]queueMessage{{Author: "op"}, {Author: "helper"}}, postActivity{Replied: true}},
		{[]queueMessage{{Author: "helper"}, {Author: "mod"}, {Author: "op"}}, postActivity{Replied: true, StaffReplied: true}},
	}
	for k, c := range cases {
		if got := summarizePost("op", c.msgs, staff); got != c.want {
			t.Errorf("case %d: got %+v", k, got)
		}
	}
}

func TestQueueSection(t *testing.T) {
	if queueSection(nil, nil) != "None" {
		t.Error("empty section")
	}
	ids := []string{"1", "2", "3", "4", "5", "6", "7"}
	got := queueSection(ids, func(string) string { return "x" })
	if strings.Count(got, "<#") != queueShown || !strings.HasSuffix(got, "…and 2 more") {
		t.Errorf("got %q", got)
	}
}
