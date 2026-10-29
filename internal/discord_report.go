package internal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// Reporting a problem: `otakase -report` or "Report a problem" in the
// Community menu. A linked app posts it to the support forum through the bot,
// as the linked member; otherwise the report is copied to the clipboard and
// the forum opened, to paste it there.

const (
	reportTitleMax       = 100
	reportDescriptionMax = 4000
	reportDiagMax        = 8000
	reportLogMax         = 1 << 20
	reportLogTailLines   = 30

	// previousLogFileName keeps the log of the run before this one. The log is
	// emptied at every launch, so without it `otakase -report`, being a launch
	// of its own, would only ever attach its own few lines.
	previousLogFileName = "otakase-debug.previous.log"
)

// discordUploadTimeout bounds sending a report. It is longer than the other
// requests because it carries up to a megabyte of log, and it only ever runs
// when the user has asked to send one, never during startup or playback.
var discordUploadClient = &http.Client{Timeout: 20 * time.Second}

// PreservePreviousLog copies a non-empty log aside before it is emptied for
// this run, as otakase-debug.previous.log beside it.
func PreservePreviousLog(logFile string) {
	info, err := os.Stat(logFile)
	if err != nil || info.Size() == 0 {
		return
	}
	data, err := readFileTail(logFile, reportLogMax)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(filepath.Dir(logFile), previousLogFileName), data, 0o600)
}

// reportLogPath is the log a report attaches: this run's when the report is
// made from inside otakase, the previous run's for `otakase -report`.
func reportLogPath(previousRun bool) string {
	current := GetGlobalLogFile()
	if previousRun && current != "" {
		previous := filepath.Join(filepath.Dir(current), previousLogFileName)
		if info, err := os.Stat(previous); err == nil && info.Size() > 0 {
			return previous
		}
	}
	return current
}

// readFileTail reads at most max bytes from the end of a file, starting at a
// whole line when it had to cut.
func readFileTail(path string, max int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	cut := info.Size() > max
	if cut {
		if _, err := file.Seek(info.Size()-max, io.SeekStart); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, max))
	if err != nil {
		return nil, err
	}
	if cut {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return data, nil
}

func lastLines(data []byte, n int) string {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for i, line := range lines {
		if len([]rune(line)) > 200 {
			lines[i] = string([]rune(line)[:200]) + "…"
		}
	}
	return strings.Join(lines, "\n")
}

// reportDiagnostics is the block of app details sent with a report. It is a
// variable so the fuller `otakase -diag` block can stand in for this one.
var reportDiagnostics = fallbackDiagnosticsText

// UseDiagnosticsFor makes reports carry the `otakase -diag` block for the
// given config file.
func UseDiagnosticsFor(version, configPath string) {
	reportDiagnostics = func(_ *Config, logPath string) string {
		return Diagnostics(version, configPath, logPath)
	}
}

// fallbackDiagnosticsText collects what a report needs to be acted on: the
// version, the system, the settings that change how playback works, and the
// end of the log.
func fallbackDiagnosticsText(config *Config, logPath string) string {
	var b strings.Builder
	version := Version()
	if isDevBuildVersion(version) {
		version += " (dev build)"
	}
	fmt.Fprintf(&b, "%s %s\n", DisplayName, version)
	fmt.Fprintf(&b, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if config != nil {
		menus := "terminal"
		if config.RofiSelection {
			menus = "rofi"
		}
		fmt.Fprintf(&b, "Player: %s · Tracker: %s · Menus: %s\n",
			config.Player, config.TrackingRemote, menus)
		fmt.Fprintf(&b, "Sub/dub: %s · Quality: %s · Cast: %t\n", config.SubOrDub, config.Quality, config.CastToDevice)
	}
	if logPath != "" {
		fmt.Fprintf(&b, "Log: %s\n", logPath)
		if data, err := readFileTail(logPath, 64<<10); err == nil && len(bytes.TrimSpace(data)) > 0 {
			b.WriteString("\nLast log lines:\n")
			b.WriteString(lastLines(data, reportLogTailLines))
			b.WriteString("\n")
		}
	}
	return clipRunes(b.String(), reportDiagMax)
}

func clipRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

type problemReport struct {
	Title       string
	Description string
	Diag        string
	LogPath     string
	Log         []byte
}

// clipboardText is the report as it is pasted into a forum post by hand.
func (r problemReport) clipboardText() string {
	var b strings.Builder
	b.WriteString(r.Title + "\n\n")
	b.WriteString(r.Description + "\n\n")
	b.WriteString("```\n" + strings.TrimSpace(r.Diag) + "\n```\n")
	if r.LogPath != "" {
		b.WriteString("\nLog file (attach it to the post): " + r.LogPath + "\n")
	}
	return b.String()
}

// sendProblemReport posts the report through the bot and returns the post's
// link.
func sendProblemReport(config *Config, report problemReport) (string, error) {
	token := loadDiscordLink(config).Token
	if token == "" {
		return "", errDiscordUnauthorized
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	fields := [][2]string{
		{"title", clipRunes(report.Title, reportTitleMax)},
		{"description", clipRunes(report.Description, reportDescriptionMax)},
		{"diag", clipRunes(report.Diag, reportDiagMax)},
	}
	for _, field := range fields {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return "", err
		}
	}
	if len(report.Log) > 0 {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="log"; filename="otakase.log"`)
		header.Set("Content-Type", "text/plain; charset=utf-8")
		part, err := form.CreatePart(header)
		if err != nil {
			return "", err
		}
		logData := report.Log
		if len(logData) > reportLogMax {
			logData = logData[len(logData)-reportLogMax:]
		}
		if _, err := part.Write(logData); err != nil {
			return "", err
		}
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	var answer struct {
		URL string `json:"url"`
	}
	if _, err := discordDoWith(discordUploadClient, context.Background(), config, http.MethodPost, "/v1/report", token, form.FormDataContentType(), &body, &answer); err != nil {
		return "", err
	}
	return answer.URL, nil
}

// reportUI is how a report asks its questions: plain lines for
// `otakase -report`, otakase's menus from inside it.
type reportUI interface {
	// ask returns the answer, or false when the question was backed out of.
	ask(question, hint string) (string, bool)
	// choose returns the chosen option's key, or "" for none.
	choose(question string, options []SelectionOption) string
	say(message string)
	// link links the app to Discord and reports whether it now is.
	link(config *Config) bool
}

func runReport(config *Config, ui reportUI, logPath string) {
	title, ok := ui.ask("Report a problem: a short title", `For example "Episode 5 of Frieren won't play" (up to 100 characters)`)
	if !ok {
		return
	}
	description, ok := ui.ask("What happened, and what did you expect?", "One line is fine; app details and the log are added for you.")
	if !ok {
		return
	}
	deliverReport(config, ui, newProblemReport(config, title, description, logPath))
}

// newProblemReport is a report with the app details and, when logPath is
// set, the log.
func newProblemReport(config *Config, title, description, logPath string) problemReport {
	report := problemReport{
		Title:       clipRunes(strings.TrimSpace(title), reportTitleMax),
		Description: clipRunes(strings.TrimSpace(description), reportDescriptionMax),
		Diag:        reportDiagnostics(config, logPath),
		LogPath:     logPath,
	}
	if logPath != "" {
		if data, err := readFileTail(logPath, reportLogMax); err == nil {
			report.Log = data
		}
	}
	return report
}

// deliverReport sends a report through the bot once the user agrees,
// linking first if they want to, or copies it and opens the support forum.
func deliverReport(config *Config, ui reportUI, report problemReport) {
	linked := discordLinked(config)
	if !linked && discordEnabled(config) {
		choice := ui.choose("Reports are posted to the support forum from your linked Discord account, and this app is not linked yet.", []SelectionOption{
			{Key: "link", Label: "Link Discord, then send", Icon: icons.Tracker},
			{Key: "copy", Label: "Copy the report and open the support forum", Icon: icons.OpenLink},
		})
		switch choice {
		case "link":
			if !ui.link(config) {
				return
			}
			linked = true
		case "copy":
			reportFallback(ui, report)
			return
		default:
			return
		}
	}
	if !linked {
		reportFallback(ui, report)
		return
	}

	summary := fmt.Sprintf("This is sent to the Otakase support forum:\n  Title: %s\n  Description: %s\n  App details: %s\n  Log: %s",
		report.Title, truncate(report.Description, 60), strings.SplitN(report.Diag, "\n", 2)[0], reportLogSummary(report))
	choice := ui.choose(summary, []SelectionOption{
		{Key: "send", Label: "Send the report · " + reportLogSummary(report), Icon: icons.Yes},
		{Key: "copy", Label: "Copy it instead and open the support forum", Icon: icons.OpenLink},
	})
	switch choice {
	case "send":
	case "copy":
		reportFallback(ui, report)
		return
	default:
		return
	}
	ui.say("Sending the report…")
	url, err := sendProblemReport(config, report)
	if err != nil {
		Log(fmt.Sprintf("Report: sending failed: %v", err))
		if errors.Is(err, errDiscordUnauthorized) {
			ui.say("This app's Discord link was removed, so the report was not sent. Link again from the Community menu or with otakase -discord-link.")
		} else {
			ui.say("Could not send the report: " + err.Error())
		}
		reportFallback(ui, report)
		return
	}
	if url == "" {
		ui.say("Report sent to the support forum.")
		return
	}
	ui.say("Report posted: " + url)
	if err := discordOpenURL(url); err != nil {
		Log(fmt.Sprintf("Report: opening the post: %v", err))
	}
}

func reportLogSummary(report problemReport) string {
	if report.LogPath == "" || len(report.Log) == 0 {
		return "no log"
	}
	return fmt.Sprintf("%s (%s)", report.LogPath, formatByteSize(len(report.Log)))
}

func formatByteSize(n int) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.Itoa(n/(1<<10)) + " KB"
	default:
		return strconv.Itoa(n) + " bytes"
	}
}

// reportFallback copies the report and opens the support forum, for posting
// it by hand.
func reportFallback(ui reportUI, report problemReport) {
	if err := clipboardWriteAll(report.clipboardText()); err != nil {
		Log(fmt.Sprintf("Report: could not copy to the clipboard: %v", err))
		ui.say("Could not copy the report. Open a post in the support forum at " + DiscordInviteURL + " and attach the log: " + report.LogPath)
	} else {
		message := "Copied the report to your clipboard. Paste it into a new post in the support forum"
		if report.LogPath != "" {
			message += " and attach the log: " + report.LogPath
		}
		ui.say(message + ".")
	}
	if err := discordOpenURL(DiscordInviteURL); err != nil {
		Log(fmt.Sprintf("Report: opening the forum: %v", err))
		ui.say("Open " + DiscordInviteURL + " in your browser.")
	}
}

// cliReportUI asks on plain terminal lines.
type cliReportUI struct {
	in  func() (string, error)
	out io.Writer
}

func newCLIReportUI() cliReportUI {
	return cliReportUI{
		in:  func() (string, error) { return stdinLineReader().ReadString('\n') },
		out: os.Stdout,
	}
}

func (ui cliReportUI) ask(question, hint string) (string, bool) {
	fmt.Fprintln(ui.out, question)
	if hint != "" {
		fmt.Fprintln(ui.out, "  "+hint)
	}
	fmt.Fprint(ui.out, "> ")
	line, err := ui.in()
	line = strings.TrimSpace(line)
	if err != nil && line == "" {
		return "", false
	}
	return line, line != ""
}

func (ui cliReportUI) choose(question string, options []SelectionOption) string {
	fmt.Fprintln(ui.out)
	fmt.Fprintln(ui.out, question)
	for i, option := range options {
		fmt.Fprintf(ui.out, "  %d) %s\n", i+1, option.Label)
	}
	fmt.Fprint(ui.out, "Choose a number (enter to cancel): ")
	line, _ := ui.in()
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(options) {
		return ""
	}
	return options[n-1].Key
}

func (ui cliReportUI) say(message string) { fmt.Fprintln(ui.out, message) }

func (ui cliReportUI) link(config *Config) bool {
	if err := RunDiscordLinkCLI(config); err != nil {
		fmt.Fprintln(ui.out, "Linking failed:", err)
		return false
	}
	return discordLinked(config)
}

// menuReportUI asks with otakase's own prompts and menus.
type menuReportUI struct {
	config *Config
	said   *bool
}

func (ui menuReportUI) ask(question, hint string) (string, bool) {
	value, cancelled, err := promptCancelable(ui.config, "Report", question, hint)
	if err != nil {
		Log(fmt.Sprintf("Report: prompt: %v", err))
		return "", false
	}
	return value, !cancelled && strings.TrimSpace(value) != ""
}

func (ui menuReportUI) choose(question string, options []SelectionOption) string {
	var selected SelectionOption
	var err error
	if ui.config != nil && ui.config.RofiSelection {
		selected, err = RofiSelectWithMessage(options, false, "Report", escapePango(question))
	} else {
		// The terminal menu has no message area; its rows say what they do.
		selected, err = promptSelectOrdered(options)
	}
	if err != nil {
		Log(fmt.Sprintf("Report: menu: %v", err))
		return ""
	}
	selected = NormalizeSelectionKey(selected)
	if SelectionMeansQuit(selected) {
		Exit(nil)
	}
	if SelectionMeansBack(selected) {
		return ""
	}
	return selected.Key
}

func (ui menuReportUI) say(message string) {
	Out(message)
	if ui.said != nil {
		*ui.said = true
	}
}

func (ui menuReportUI) link(config *Config) bool {
	EnterAppScreen()
	return linkDiscordFromMenu(config)
}

// reportFromMenu is "Report a problem" in the Community menu.
func reportFromMenu(config *Config) {
	said := false
	runReport(config, menuReportUI{config: config, said: &said}, reportLogPath(false))
	if said {
		pauseAfterNotice(config)
	}
}

// pauseAfterNotice keeps a message on the terminal until it has been read:
// the menu drawn next would cover it. rofi shows messages as notifications,
// which stay by themselves.
func pauseAfterNotice(config *Config) {
	if config == nil || config.RofiSelection || castWindowNonInteractive() {
		return
	}
	Out("Press Enter to go back.")
	AwaitEnter()
}

// RunReportCLI is `otakase -report`.
func RunReportCLI(config *Config) {
	runReport(config, newCLIReportUI(), reportLogPath(true))
}
