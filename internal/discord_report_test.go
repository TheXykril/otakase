package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeReportUI answers a report's questions from a script.
type fakeReportUI struct {
	answers []string
	choices []string
	asked   []string
	said    []string
	linkOK  bool
	linked  int
}

func (ui *fakeReportUI) ask(question, hint string) (string, bool) {
	ui.asked = append(ui.asked, question)
	if len(ui.answers) == 0 {
		return "", false
	}
	answer := ui.answers[0]
	ui.answers = ui.answers[1:]
	return answer, answer != ""
}

func (ui *fakeReportUI) choose(question string, options []SelectionOption) string {
	ui.asked = append(ui.asked, question)
	if len(ui.choices) == 0 {
		return ""
	}
	choice := ui.choices[0]
	ui.choices = ui.choices[1:]
	return choice
}

func (ui *fakeReportUI) say(message string) { ui.said = append(ui.said, message) }

func (ui *fakeReportUI) link(config *Config) bool {
	ui.linked++
	if ui.linkOK {
		saveDiscordLink(config, discordLink{Token: "fresh"})
	}
	return ui.linkOK
}

func stubClipboard(t *testing.T, fail bool) *string {
	t.Helper()
	copied := new(string)
	previous := clipboardWriteAll
	clipboardWriteAll = func(text string) error {
		if fail {
			return errors.New("no clipboard")
		}
		*copied = text
		return nil
	}
	t.Cleanup(func() { clipboardWriteAll = previous })
	return copied
}

type receivedReport struct {
	auth, title, description, diag, logName string
	log                                     []byte
}

func reportServer(t *testing.T, status int) (*httptest.Server, chan receivedReport) {
	t.Helper()
	got := make(chan receivedReport, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/report" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		rep := receivedReport{
			auth:        r.Header.Get("Authorization"),
			title:       r.FormValue("title"),
			description: r.FormValue("description"),
			diag:        r.FormValue("diag"),
		}
		if file, header, err := r.FormFile("log"); err == nil {
			rep.logName = header.Filename
			rep.log, _ = io.ReadAll(file)
			file.Close()
		}
		got <- rep
		w.WriteHeader(status)
		if status == http.StatusOK {
			json.NewEncoder(w).Encode(map[string]string{"url": "https://discord.com/channels/1/2"})
		} else {
			json.NewEncoder(w).Encode(map[string]string{"error": "nope"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func writeTestLog(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, LogFileName)
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i))
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReportSentWhenLinked(t *testing.T) {
	srv, got := reportServer(t, http.StatusOK)
	config := newDiscordTestConfig(t, srv.URL)
	saveDiscordLink(config, discordLink{Token: "tok"})
	opened := stubDiscordOpen(t)
	logPath := writeTestLog(t, config.StoragePath)

	ui := &fakeReportUI{answers: []string{"Episode won't play", "It stops at 0:00"}, choices: []string{"send"}}
	runReport(config, ui, logPath)

	rep := <-got
	if rep.auth != "Bearer tok" || rep.title != "Episode won't play" || rep.description != "It stops at 0:00" {
		t.Errorf("report = %+v", rep)
	}
	if !strings.Contains(rep.diag, runtime.GOOS+"/"+runtime.GOARCH) || !strings.Contains(rep.diag, "Last log lines") {
		t.Errorf("diag = %q", rep.diag)
	}
	if rep.logName != "otakase.log" || !bytes.Contains(rep.log, []byte("line xxx")) {
		t.Errorf("log %q (%d bytes)", rep.logName, len(rep.log))
	}
	if len(*opened) != 1 || (*opened)[0] != "https://discord.com/channels/1/2" {
		t.Errorf("opened %v", *opened)
	}
	if !strings.Contains(strings.Join(ui.said, "\n"), "https://discord.com/channels/1/2") {
		t.Errorf("said %v", ui.said)
	}
	// The confirmation shows where the log comes from.
	if !strings.Contains(strings.Join(ui.asked, "\n"), logPath) {
		t.Errorf("confirmation did not name the log: %v", ui.asked)
	}
}

func TestReportNotLinkedFallsBackToClipboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request without a link: %s", r.URL.Path)
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	opened := stubDiscordOpen(t)
	copied := stubClipboard(t, false)
	logPath := writeTestLog(t, config.StoragePath)

	ui := &fakeReportUI{answers: []string{"Crash", "On start"}, choices: []string{"copy"}}
	runReport(config, ui, logPath)

	if !strings.Contains(*copied, "Crash") || !strings.Contains(*copied, "On start") || !strings.Contains(*copied, logPath) || !strings.Contains(*copied, "```") {
		t.Errorf("clipboard = %q", *copied)
	}
	if len(*opened) != 1 || (*opened)[0] != DiscordInviteURL {
		t.Errorf("opened %v", *opened)
	}
}

func TestReportLinksFirstWhenAsked(t *testing.T) {
	srv, got := reportServer(t, http.StatusOK)
	config := newDiscordTestConfig(t, srv.URL)
	stubDiscordOpen(t)
	ui := &fakeReportUI{answers: []string{"T", "D"}, choices: []string{"link", "send"}, linkOK: true}
	runReport(config, ui, "")
	if ui.linked != 1 {
		t.Fatalf("link asked %d times", ui.linked)
	}
	if rep := <-got; rep.auth != "Bearer fresh" || rep.logName != "" {
		t.Errorf("report = %+v", rep)
	}
}

func TestReportRevokedTokenFallsBack(t *testing.T) {
	srv, _ := reportServer(t, http.StatusUnauthorized)
	config := newDiscordTestConfig(t, srv.URL)
	saveDiscordLink(config, discordLink{Token: "old"})
	stubDiscordOpen(t)
	copied := stubClipboard(t, false)
	ui := &fakeReportUI{answers: []string{"T", "D"}, choices: []string{"send"}}
	runReport(config, ui, "")
	if *copied == "" {
		t.Error("nothing copied after the bot refused the token")
	}
	if discordLinked(config) {
		t.Error("still linked after a 401")
	}
}

func TestReportCancelledSendsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request after cancelling: %s", r.URL.Path)
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	saveDiscordLink(config, discordLink{Token: "tok"})
	runReport(config, &fakeReportUI{answers: []string{""}}, "")
	runReport(config, &fakeReportUI{answers: []string{"T", "D"}}, "")
}

func TestReportFieldsAreClipped(t *testing.T) {
	srv, got := reportServer(t, http.StatusOK)
	config := newDiscordTestConfig(t, srv.URL)
	saveDiscordLink(config, discordLink{Token: "tok"})
	_, err := sendProblemReport(config, problemReport{
		Title:       strings.Repeat("t", 300),
		Description: strings.Repeat("d", 5000),
		Diag:        strings.Repeat("g", 9000),
		Log:         bytes.Repeat([]byte("l"), reportLogMax+10),
	})
	if err != nil {
		t.Fatal(err)
	}
	rep := <-got
	if len(rep.title) != reportTitleMax || len(rep.description) != reportDescriptionMax || len(rep.diag) != reportDiagMax || len(rep.log) != reportLogMax {
		t.Errorf("sizes title=%d description=%d diag=%d log=%d", len(rep.title), len(rep.description), len(rep.diag), len(rep.log))
	}
}

func TestCLIReportUI(t *testing.T) {
	lines := []string{"  My title \n", "\n", "2\n", "9\n"}
	var out bytes.Buffer
	ui := cliReportUI{
		in: func() (string, error) {
			if len(lines) == 0 {
				return "", io.EOF
			}
			line := lines[0]
			lines = lines[1:]
			return line, nil
		},
		out: &out,
	}
	if got, ok := ui.ask("Title?", "hint"); !ok || got != "My title" {
		t.Errorf("ask = %q, %v", got, ok)
	}
	if _, ok := ui.ask("Again?", ""); ok {
		t.Error("an empty answer did not cancel")
	}
	options := []SelectionOption{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}}
	if got := ui.choose("Pick", options); got != "b" {
		t.Errorf("choose = %q", got)
	}
	if got := ui.choose("Pick", options); got != "" {
		t.Errorf("out of range choice = %q", got)
	}
	if !strings.Contains(out.String(), "2) B") {
		t.Errorf("output = %q", out.String())
	}
}

func TestReadFileTailStartsAtALine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	os.WriteFile(path, []byte("first line\nsecond line\nthird\n"), 0o644)
	data, err := readFileTail(path, 15)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "third\n" {
		t.Errorf("tail = %q", data)
	}
	whole, _ := readFileTail(path, 1000)
	if !strings.HasPrefix(string(whole), "first line") {
		t.Errorf("whole = %q", whole)
	}
}

func TestPreservePreviousLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, LogFileName)
	os.WriteFile(logPath, []byte("from the last run\n"), 0o644)
	PreservePreviousLog(logPath)
	data, err := os.ReadFile(filepath.Join(dir, previousLogFileName))
	if err != nil || string(data) != "from the last run\n" {
		t.Fatalf("previous log = %q, %v", data, err)
	}

	previous := GetGlobalLogFile()
	t.Cleanup(func() { SetGlobalLogFile(previous) })
	SetGlobalLogFile(logPath)
	if got := reportLogPath(true); got != filepath.Join(dir, previousLogFileName) {
		t.Errorf("-report log = %q", got)
	}
	if got := reportLogPath(false); got != logPath {
		t.Errorf("menu report log = %q", got)
	}

	// An empty log does not replace the kept one.
	os.WriteFile(logPath, nil, 0o644)
	PreservePreviousLog(logPath)
	data, _ = os.ReadFile(filepath.Join(dir, previousLogFileName))
	if string(data) != "from the last run\n" {
		t.Errorf("previous log replaced by an empty one: %q", data)
	}
}
