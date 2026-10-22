package cast

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startTestHub serves the page on a free port, and stops nothing: the
// listener goes with the test process, as the real one goes with otakase's.
func startTestHub(t *testing.T) *browserHub {
	t.Helper()
	hub, err := startBrowserHub(BrowserSettings{Port: 0, Token: "tok3n"})
	if err != nil {
		t.Skipf("no LAN address to serve on: %v", err)
	}
	t.Cleanup(func() { hub.listener.Close() })
	return hub
}

// testViewer is a page as the hub sees it: an event stream and reports.
type testViewer struct {
	t        *testing.T
	hub      *browserHub
	id       int
	messages chan map[string]any
}

func openTestViewer(t *testing.T, hub *browserHub) *testViewer {
	t.Helper()
	response, err := http.Get(hub.link() + "/events")
	if err != nil {
		t.Fatal(err)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("events served as %q", got)
	}
	v := &testViewer{t: t, hub: hub, messages: make(chan map[string]any, 16)}
	t.Cleanup(func() { response.Body.Close() })
	go func() {
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			data, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var message map[string]any
			if json.Unmarshal([]byte(data), &message) == nil {
				v.messages <- message
			}
		}
	}()
	hello := v.next("hello")
	v.id = int(hello["client"].(float64))
	return v
}

func (v *testViewer) next(cmd string) map[string]any {
	v.t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case message := <-v.messages:
			if message["cmd"] == cmd {
				return message
			}
		case <-timeout:
			v.t.Fatalf("no %q message", cmd)
		}
	}
}

func (v *testViewer) report(r browserReport) {
	v.t.Helper()
	r.Client = v.id
	body, _ := json.Marshal(r)
	response, err := http.Post(v.hub.link()+"/state", "application/json", strings.NewReader(string(body)))
	if err != nil {
		v.t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		v.t.Fatalf("report answered %s", response.Status)
	}
}

func TestBrowserHubAnswersOnlyUnderItsToken(t *testing.T) {
	hub := startTestHub(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for path, want := range map[string]int{
		"/":             http.StatusNotFound,
		"/wrong/":       http.StatusNotFound,
		"/tok3n":        http.StatusFound,
		"/tok3n/":       http.StatusOK,
		"/tok3n/hls.js": http.StatusOK,
		"/tok3n/nope":   http.StatusNotFound,
	} {
		response, err := client.Get(hub.baseURL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Errorf("%s answered %d, want %d", path, response.StatusCode, want)
		}
	}
}

func TestBrowserPlayerFollowsThePage(t *testing.T) {
	hub := startTestHub(t)
	viewer := openTestViewer(t, hub)
	player := &BrowserPlayer{hub: hub}
	player.Describe(BrowserMedia{Title: "Show · Episode 3", Subtitles: "http://x/subs.vtt", SubtitleSize: 120, Duration: 1440})

	if err := player.PlayFrom("http://host:1/g0/"+PlaylistName, 90); err != nil {
		t.Fatal(err)
	}
	load := viewer.next("load")
	if load["url"] != "http://host:1/g0/"+PlaylistName || load["ts"] != "http://host:1/g0/"+ProgressiveName {
		t.Errorf("load names %v and %v", load["url"], load["ts"])
	}
	if load["start"] != 90.0 || load["subs"] != "http://x/subs.vtt" || load["subSize"] != 120.0 || load["duration"] != 1440.0 {
		t.Errorf("load carries %v", load)
	}

	// Until the page reports on this load, its position means nothing.
	if progress, err := player.Progress(); err != nil || !progress.Settling {
		t.Fatalf("before a report: %+v, %v", progress, err)
	}
	viewer.report(browserReport{Load: 1, Position: 12.5, Volume: 0.8})
	progress, err := player.Progress()
	if err != nil || progress.Settling || progress.Position != 12.5 || progress.Idle {
		t.Fatalf("after a report: %+v, %v", progress, err)
	}
	if got := player.Volume(); got != 0.8 {
		t.Errorf("volume %v, want the page's 0.8", got)
	}

	// A seek within the stream is counted from where it lands, the way a
	// receiver counts a stream it was just handed.
	if err := player.SeekWithin(300); err != nil {
		t.Fatal(err)
	}
	seek := viewer.next("seek")
	if seek["at"] != 300.0 || seek["seek"] != 1.0 {
		t.Errorf("seek carries %v", seek)
	}
	viewer.report(browserReport{Load: 1, Position: 20})
	if progress, _ := player.Progress(); !progress.Settling {
		t.Errorf("a report from before the seek was believed: %+v", progress)
	}
	viewer.report(browserReport{Load: 1, Seek: 1, Position: 304})
	if progress, _ := player.Progress(); progress.Settling || progress.Position != 4 {
		t.Errorf("after the seek: %+v, want position 4", progress)
	}

	viewer.report(browserReport{Load: 1, Seek: 1, Position: 900, Ended: true})
	if progress, _ := player.Progress(); !progress.Idle {
		t.Errorf("an ended page is not idle: %+v", progress)
	}

	if err := player.Pause(); err != nil {
		t.Fatal(err)
	}
	viewer.next("pause")
	if err := player.SetVolume(1.5); err != nil {
		t.Fatal(err)
	}
	if v := viewer.next("volume"); v["volume"] != 1.0 {
		t.Errorf("volume sent as %v, want it held at 1", v["volume"])
	}
	if err := player.Stop(); err != nil {
		t.Fatal(err)
	}
	viewer.next("stop")
}

func TestBrowserLateViewerGetsWhatIsPlaying(t *testing.T) {
	hub := startTestHub(t)
	player := &BrowserPlayer{hub: hub}
	if err := player.PlayFrom("http://host:1/g2/"+PlaylistName, 0); err != nil {
		t.Fatal(err)
	}
	if err := player.SeekWithin(60); err != nil {
		t.Fatal(err)
	}
	late := openTestViewer(t, hub)
	load := late.next("load")
	if load["at"] != 60.0 || load["seek"] != 1.0 {
		t.Errorf("a late page is not told where the stream is: %v", load)
	}
}

func TestBrowserPlayerLosesAClosedPage(t *testing.T) {
	hub := startTestHub(t)
	player := &BrowserPlayer{hub: hub}
	defer func(was time.Duration) { browserGone = was }(browserGone)
	browserGone = 50 * time.Millisecond

	viewer := openTestViewer(t, hub)
	_ = player.PlayFrom("http://host:1/g0/"+PlaylistName, 0)
	viewer.report(browserReport{Load: 1, Position: 1})
	if _, err := player.Progress(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := player.Progress(); err == nil {
		t.Error("a page silent for longer than browserGone is still followed")
	}
}

func TestBrowserCommandsReachTheHandler(t *testing.T) {
	hub := startTestHub(t)
	got := make(chan string, 1)
	SetBrowserCommands(func(command string) { got <- command })
	defer SetBrowserCommands(nil)

	post := func(body string) int {
		response, err := http.Post(hub.link()+"/command", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if code := post(`{"cmd":"forward"}`); code != http.StatusNoContent {
		t.Fatalf("command answered %d", code)
	}
	if command := <-got; command != "forward" {
		t.Errorf("handler got %q", command)
	}
	if code := post(`{"cmd":"rm -rf"}`); code != http.StatusBadRequest {
		t.Errorf("an unknown command answered %d", code)
	}
}

func TestNewBrowserTokenIsShortAndUnambiguous(t *testing.T) {
	for range 50 {
		token := NewBrowserToken()
		if len(token) != 5 || strings.ContainsAny(token, "01ilo") {
			t.Fatalf("token %q", token)
		}
	}
}

func TestBrowserPlayerWaitsLongerForAPausedPage(t *testing.T) {
	hub := startTestHub(t)
	player := &BrowserPlayer{hub: hub}
	defer func(was time.Duration) { browserGone = was }(browserGone)
	browserGone = 50 * time.Millisecond

	viewer := openTestViewer(t, hub)
	_ = player.PlayFrom("http://host:1/g0/"+PlaylistName, 0)
	viewer.report(browserReport{Load: 1, Position: 1})
	_ = player.Pause()
	time.Sleep(100 * time.Millisecond)
	if _, err := player.Progress(); err != nil {
		t.Errorf("a paused page, quiet for a moment, is lost: %v", err)
	}
}

func TestBrowserStatusReachesThePage(t *testing.T) {
	hub := startTestHub(t)
	browserMu.Lock()
	was := browserRunning
	browserRunning = hub
	browserMu.Unlock()
	defer func() {
		browserMu.Lock()
		browserRunning = was
		browserMu.Unlock()
	}()

	viewer := openTestViewer(t, hub)
	BrowserStatus("Preparing the stream…\n\n█▀▄ █\n▄▄█ ▀")
	if got := viewer.next("status")["text"]; got != "Preparing the stream…" {
		t.Errorf("status sent as %q, want the text without the code", got)
	}
	// A page opened while nothing plays is told the latest at once.
	late := openTestViewer(t, hub)
	if got := late.next("status")["text"]; got != "Preparing the stream…" {
		t.Errorf("a late page got %q", got)
	}
}

func TestBrowserPlayerReportsBuffering(t *testing.T) {
	hub := startTestHub(t)
	viewer := openTestViewer(t, hub)
	player := &BrowserPlayer{hub: hub}
	_ = player.PlayFrom("http://host:1/g0/"+PlaylistName, 0)
	viewer.report(browserReport{Load: 1, Position: 3, Buffering: true})
	if progress, _ := player.Progress(); !progress.Buffering {
		t.Errorf("a waiting page is not buffering: %+v", progress)
	}
}
