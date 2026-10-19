package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// inboxFor is what waits for an app, by token.
func inboxFor(b *bot, tok string) []inboxItem { return b.inboxOf(hashToken(tok)) }

func TestReplyNotice(t *testing.T) {
	m := &discordgo.Message{ID: "9", ChannelID: "7", Content: "Try  the\nother source <@1>",
		Author: &discordgo.User{ID: "2", Username: "helper", GlobalName: "Helper"}}
	it := replyNotice("1", "mpv won't start", m)
	if it.Kind != "notice" || it.URL != "https://discord.com/channels/1/7/9" {
		t.Errorf("item: %+v", it)
	}
	if it.Text != "Helper replied in “mpv won't start”: Try the other source <@1>" {
		t.Errorf("text = %q", it.Text)
	}
	m.Member = &discordgo.Member{Nick: "Mod Xy"}
	if it := replyNotice("1", "", m); !strings.HasPrefix(it.Text, "Mod Xy replied:") {
		t.Errorf("nickname first, no post name: %q", it.Text)
	}
}

func TestSupportPostOwner(t *testing.T) {
	b := appTestBot(t)
	b.store.update(func(d *storeData) {
		d.FormPosts = map[string]string{"t1": "u1"}
		d.AppReports["t2"] = appReport{User: "u2"}
	})
	if b.supportPostOwner("t1") != "u1" || b.supportPostOwner("t2") != "u2" || b.supportPostOwner("t3") != "" {
		t.Error("owners of form and app posts")
	}
}

func TestSupportReplyAlertLimit(t *testing.T) {
	replyAlertLimit = newRateLimit(1, replyAlertEvery)
	now := time.Now()
	if !replyAlertLimit.allow("t1", now) || replyAlertLimit.allow("t1", now.Add(5*time.Minute)) {
		t.Error("a second reply within ten minutes should not alert")
	}
	if !replyAlertLimit.allow("t2", now) || !replyAlertLimit.allow("t1", now.Add(11*time.Minute)) {
		t.Error("other posts, and the same post later, alert")
	}
}

func TestBugFixedNotice(t *testing.T) {
	b := appTestBot(t)
	tok := testToken(b, "u1")
	b.store.update(func(d *storeData) { d.AppReports["t1"] = appReport{User: "u1"} })
	is := ghIssueFull{ghIssue: ghIssue{Number: 42, Title: "Crash on start"}}
	rel := ghRelease{TagName: "v26.9.0", HTMLURL: "https://github.com/TheXykril/otakase/releases/tag/v26.9.0"}
	// The post belongs to the bot: the member who sent the report gets it.
	b.noticeBugFixed("t1", "bot", is, rel)
	items := inboxFor(b, tok)
	if len(items) != 1 || items[0].Title != "Fixed in v26.9.0" || items[0].URL != rel.HTMLURL || !strings.Contains(items[0].Text, "#42") {
		t.Fatalf("items: %+v", items)
	}
}

func TestAiringItem(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	it := airingItem(followAiring{MediaID: 154587, Episode: 5, Episodes: 28, Show: followedShow{ID: 154587, Title: "Frieren"}}, now)
	if it.Kind != "airing" || it.AniListID != 154587 || it.Episode != 5 || it.Title != "Frieren" || it.Text != "Episode 5 of 28 is out" {
		t.Errorf("item: %+v", it)
	}
	if !it.Expires.Equal(now.Add(3*24*time.Hour)) || !strings.Contains(it.URL, "ep=5") {
		t.Errorf("expires %v, url %q", it.Expires, it.URL)
	}
	if it := airingItem(followAiring{MediaID: 1, Episode: 2}, now); it.Text != "Episode 2 is out" {
		t.Errorf("unknown count: %q", it.Text)
	}
	b := appTestBot(t)
	tok := testToken(b, "u1")
	b.sendAiringToApps("u1", []followAiring{{MediaID: 1, Episode: 1}, {MediaID: 2, Episode: 3}})
	b.sendAiringToApps("u2", []followAiring{{MediaID: 1, Episode: 1}})
	if n := len(inboxFor(b, tok)); n != 2 {
		t.Errorf("%d airing items, want 2", n)
	}
}

func TestRemoteItem(t *testing.T) {
	now := time.Now()
	it := remoteItem("skip", now)
	if it.Kind != "remote" || it.Key != "skip" || it.Title != "Skip 85 s" || !it.Expires.Equal(now.Add(30*time.Second)) {
		t.Errorf("item: %+v", it)
	}
	for _, a := range remoteActions {
		if remoteLabel(a.Key) == "" {
			t.Errorf("%s has no label", a.Key)
		}
	}
	if remoteLabel("rm -rf") != "" {
		t.Error("unknown action has a label")
	}
}

func TestPlanNudge(t *testing.T) {
	var d storeData
	fillMaps(&d)
	d.AppLinks["h1"] = appLink{User: "u1"}
	d.AppLinks["h2"] = appLink{User: "u1"}
	if notice, dm := planNudge(&d, "u1", "h1", "26.9.0"); !notice || !dm {
		t.Error("first old heartbeat: notice and DM")
	}
	if notice, dm := planNudge(&d, "u1", "h1", "26.9.0"); notice || dm {
		t.Error("same app, same release: nothing")
	}
	if notice, dm := planNudge(&d, "u1", "h2", "26.9.0"); !notice || dm {
		t.Error("second app: its notice, but one DM per member")
	}
	if notice, dm := planNudge(&d, "u1", "h1", "26.10.0"); !notice || !dm {
		t.Error("a newer release nudges again")
	}
	d.AppNudges["gone"] = "26.9.0"
	planNudge(&d, "u1", "h1", "26.10.0")
	if _, ok := d.AppNudges["gone"]; ok {
		t.Error("unlinked apps are forgotten")
	}
}

func TestNudgeUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v26.9.0"}`))
	}))
	defer srv.Close()
	old := releasesLatestURL
	releasesLatestURL = srv.URL
	versionCheck.latest = ""
	defer func() { releasesLatestURL = old; versionCheck.latest = "" }()

	b := appTestBot(t)
	tok, other := testToken(b, "u1"), testToken(b, "u1")
	ev := heartbeatEvent{User: "u1", Hash: hashToken(tok), Version: "26.8.1", At: time.Now()}
	b.nudgeUpdate(ev)
	b.nudgeUpdate(ev)
	items := inboxFor(b, tok)
	if len(items) != 1 || items[0].Kind != "notice" || items[0].URL != repoURL+"/releases/tag/v26.9.0" || !strings.Contains(items[0].Text, "26.8.1") {
		t.Fatalf("one notice for the old app: %+v", items)
	}
	if len(inboxFor(b, other)) != 0 {
		t.Error("the notice goes to the app that is out of date only")
	}
	ev.Version = "26.9.0"
	ev.Hash = hashToken(other)
	b.nudgeUpdate(ev)
	if len(inboxFor(b, other)) != 0 {
		t.Error("an up to date app got a notice")
	}
	b.store.view(func(d *storeData) {
		if d.UpdateNudges["u1"] != "26.9.0" {
			t.Errorf("member nudges: %v", d.UpdateNudges)
		}
	})
}

func TestSendToApp(t *testing.T) {
	b := appTestBot(t)
	tok := testToken(b, "u1")
	if b.sendToApp("nope", inboxItem{Kind: "notice"}) {
		t.Error("sent to an app that isn't linked")
	}
	if !b.sendToApp(hashToken(tok), inboxItem{Kind: "notice", Title: "Hi"}) {
		t.Fatal("not sent")
	}
	if items := inboxFor(b, tok); len(items) != 1 || items[0].ID == "" || items[0].Expires.IsZero() {
		t.Errorf("items: %+v", items)
	}
}

func TestShareProblem(t *testing.T) {
	friend := &discordgo.User{ID: "2"}
	cases := []struct {
		target   *discordgo.User
		inServer bool
		want     string
	}{
		{friend, true, ""},
		{friend, false, "isn't in the server"},
		{nil, false, "isn't in the server"},
		{&discordgo.User{ID: "3", Bot: true}, true, "Bots"},
		{&discordgo.User{ID: "1"}, true, "That's you"},
	}
	for _, c := range cases {
		got := shareProblem("1", c.target, c.inServer)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("shareProblem(%+v, %v) = %q, want %q", c.target, c.inServer, got, c.want)
		}
	}
}

func TestShareDM(t *testing.T) {
	msg := shareDM("Xy*", 154587, "Frieren", "https://anilist.co/anime/154587", "https://img/c.jpg", "So good\n@everyone")
	e := msg.Embeds[0]
	if e.Title != "Frieren" || e.Thumbnail == nil || !strings.Contains(e.Description, `**Xy\*** recommends`) {
		t.Errorf("embed: %+v", e)
	}
	if !strings.Contains(e.Description, "> So good\n> @​everyone") {
		t.Errorf("note quoted and quiet: %q", e.Description)
	}
	if msg.AllowedMentions == nil || len(msg.Components) != 1 {
		t.Fatalf("mentions %v, components %d", msg.AllowedMentions, len(msg.Components))
	}
	row := msg.Components[0].(discordgo.ActionsRow)
	if len(row.Components) != 2 || row.Components[0].(discordgo.Button).Label != "Play in Otakase" ||
		row.Components[1].(discordgo.Button).CustomID != "app-queue:154587" {
		t.Errorf("buttons: %+v", row.Components)
	}
}

func TestShareResult(t *testing.T) {
	if d := shareResult("2", "Frieren", true, 2).Description; !strings.Contains(d, "in their DMs and in their Otakase (2 apps)") {
		t.Errorf("both: %q", d)
	}
	if d := shareResult("2", "Frieren", false, 1).Description; !strings.Contains(d, "in their Otakase (1 app)") || strings.Contains(d, "DMs") {
		t.Errorf("app only: %q", d)
	}
	if d := shareResult("2", "Frieren", false, 0).Description; !strings.Contains(d, "Couldn't reach") {
		t.Errorf("nowhere: %q", d)
	}
}

func TestTesterChange(t *testing.T) {
	if testerChange([]string{"a"}, []string{"a", "t"}, "t") != 1 {
		t.Error("gained")
	}
	if testerChange([]string{"t", "a"}, []string{"a"}, "t") != -1 {
		t.Error("lost")
	}
	if testerChange([]string{"t"}, []string{"t", "b"}, "t") != 0 || testerChange(nil, []string{"b"}, "t") != 0 {
		t.Error("other role changes are no change")
	}
}

func TestBetaSetting(t *testing.T) {
	on, off := betaSetting(true), betaSetting(false)
	if on.Kind != "setting" || on.Key != "DevBuilds" || on.Value != "true" || on.Text == "" {
		t.Errorf("on: %+v", on)
	}
	if off.Key != "DevBuilds" || off.Value != "false" || off.Text == "" {
		t.Errorf("off: %+v", off)
	}
	if !strings.Contains(betaReply(true, true), "switches to development builds") || !strings.Contains(betaReply(false, false), "turn off **DevBuilds**") {
		t.Error("beta replies")
	}
}
