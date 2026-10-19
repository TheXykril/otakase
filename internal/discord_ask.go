package internal

import (
	"fmt"
	"strings"
	"sync"

	"github.com/thexykril/otakase/internal/icons"
)

// Asking the server: when no source has a show, or every one fails for it,
// the recovery menus offer "Ask the Otakase Discord where to watch this". A
// linked app makes a support post through the report endpoint, titled
// "Where to watch <show>?", with what was tried and the app details (no
// log); otherwise the question is copied and the support forum opened, as
// reports do. Offered once per show per run.

const askServerKey = "ask-discord"

var askedServer = struct {
	sync.Mutex
	m map[string]bool
}{m: map[string]bool{}}

func askServerShowKey(show string) string { return strings.ToLower(strings.TrimSpace(show)) }

// withAskServerOption adds the row to a recovery menu, before its Back row,
// when Discord is on and this show was not asked about yet.
func withAskServerOption(config *Config, options []SelectionOption, show string) []SelectionOption {
	key := askServerShowKey(show)
	if key == "" || !discordEnabled(config) {
		return options
	}
	askedServer.Lock()
	asked := askedServer.m[key]
	askedServer.Unlock()
	if asked {
		return options
	}
	row := SelectionOption{Key: askServerKey, Label: "Ask the Otakase Discord where to watch this", Icon: icons.OpenLink}
	if n := len(options); n > 0 && options[n-1].Key == "back" {
		return append(append(options[:n-1:n-1], row), options[n-1])
	}
	return append(options, row)
}

// askServerText is the support post's title and description.
func askServerText(show string, anilistID int, tried string) (title, description string) {
	show = strings.TrimSpace(show)
	title = clipRunes("Where to watch "+show+"?", reportTitleMax)
	var b strings.Builder
	fmt.Fprintf(&b, "Otakase couldn't find a source that plays %s.", show)
	if anilistID > 0 {
		fmt.Fprintf(&b, "\nAniList: https://anilist.co/anime/%d", anilistID)
	}
	if tried = strings.TrimSpace(tried); tried != "" {
		b.WriteString("\n\nWhat was tried:\n```\n" + clipRunes(tried, 2000) + "\n```")
	}
	b.WriteString("\n\nDoes anyone know a source that has it, or another name to search for?")
	return title, b.String()
}

// askServerWhereToWatch makes the support post (or copies it and opens the
// forum) and does not offer it again for this show.
func askServerWhereToWatch(config *Config, show string, anilistID int, tried string) {
	askedServer.Lock()
	askedServer.m[askServerShowKey(show)] = true
	askedServer.Unlock()
	title, description := askServerText(show, anilistID, tried)
	// The app details carry the end of the log; the log itself stays here.
	report := newProblemReport(config, title, description, "")
	report.Diag = reportDiagnostics(config, reportLogPath(false))
	said := false
	EnterAppScreen()
	deliverReport(config, menuReportUI{config: config, said: &said}, report)
	if said {
		pauseAfterNotice(config)
	}
}
