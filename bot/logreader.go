package main

import (
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// A log or error pasted or attached in a support post is scanned for errors
// otakase is known to print, and the post gets the matching fixes. Several
// patterns are the retired command-line otakase's own wording; those answers
// point people at the app. Plain questions are left to the FAQ replies.

type logHint struct {
	Key   string
	Title string
	Fix   string
	re    *regexp.Regexp
}

var logHints = []logHint{
	{"mpv", "mpv is missing",
		"This log is from the old command-line otakase, which is no longer updated. Switch to the app (`/install`): it has its own player and needs nothing else.",
		regexp.MustCompile(`(?i)exec: \\?"mpv(\.exe)?\\?": executable file not found|fallback to mpv failed|Failed to resolve media player|(?m:^mpv: not found\s*$)`)},
	{"mpv-socket", "mpv started but otakase couldn't talk to it",
		"This log is from the old command-line otakase, which is no longer updated. Switch to the app (`/install`): it has its own player and needs nothing else.",
		regexp.MustCompile(`(?i)Failed to connect to MPV socket|Failed to start mpv|timed out waiting for mpv response`)},
	{"no-providers", "No sources are enabled",
		"This log is from the old command-line otakase, which is no longer updated. Switch to the app (`/install`): it has its own player and needs nothing else. The app always uses every source.",
		regexp.MustCompile(`(?i)No providers are currently enabled`)},
	{"blocked", "A source blocked or rate-limited you",
		"The site answered with a block (Cloudflare, DDoS-Guard or too many requests). Wait a few minutes and try again; Otakase falls back to other sources. A VPN can also trip this.",
		regexp.MustCompile(`(?i)Cloudflare challenge|Too many requests|failed with status (429|403)\b|DDoS-Guard`)},
	{"no-stream", "No source gave a playable stream",
		"Streaming sites change and go down often. Run `/provider-status` here to see how many answer, try another copy or source from the player, or try again later.",
		regexp.MustCompile(`(?i)No episode links found|failed to get episode links|No alternative provider could start playback|No other copy could start playback|No more (providers|sources) left to try|Playback failed to start( with|\. Trying another copy)|None of the servers for .* could be played`)},
	{"network", "Network problem reaching a source",
		"Otakase couldn't connect. Check your internet and DNS, and turn off any proxy or VPN to test. A certificate error usually means a wrong system clock or a filtering antivirus.",
		regexp.MustCompile(`(?i)\bno such host\b|dial tcp [^\n]*(i/o timeout|connection refused|network is unreachable)|TLS handshake timeout|x509: certificate|context deadline exceeded`)},
	{"anilist-token", "AniList sign-in expired",
		"Sign in to AniList again under **Accounts** in the app.",
		regexp.MustCompile(`(?i)invalid[ _]token|AniList token invalid|Failed to get user data`)},
	{"mal-credentials", "MyAnimeList isn't set up",
		"MyAnimeList sync needs your own MAL app credentials. The steps are on the wiki's Tracking page: " + wikiURL + "/Tracking",
		regexp.MustCompile(`(?i)MyAnimeList sync needs MAL application credentials`)},
	{"wrong-match", "The show wasn't found or matched wrong",
		"Titles differ between your tracker and the sources. Use **Wrong show?** on the show's page in the app to pick the right match.",
		regexp.MustCompile(`(?i)Could not find the selected anime|Wrong match is common|Failed to update (provider mapping|the match)`)},
	{"rofi", "rofi is missing",
		"This log is from the old command-line otakase, which is no longer updated. Switch to the app (`/install`): it has its own player and needs nothing else.",
		regexp.MustCompile(`(?i)exec: \\?"rofi\\?": executable file not found|failed to run Rofi`)},
	{"ffmpeg", "ffmpeg is missing",
		"Downloading and casting with subtitles on a computer need `ffmpeg`. Install it from your package manager (Windows: `winget install ffmpeg`).",
		regexp.MustCompile(`(?i)Downloading needs ffmpeg|exec: \\?"ffmpeg(\.exe)?\\?": executable file not found|cast: ffmpeg failed`)},
	{"cast", "No cast device answered",
		"Usually the computer's firewall drops the TV's reply. See " + wikiURL + "/Casting",
		regexp.MustCompile(`(?i)No cast devices found|cast: discovery failed|cast: cannot reach Kodi`)},
}

// maxLogHints keeps the reply short when a log is full of errors.
const maxLogHints = 3

// diagnoseLog returns the known problems in a log, most specific first.
func diagnoseLog(text string) []logHint {
	var found []logHint
	for _, h := range logHints {
		if h.re.MatchString(text) {
			found = append(found, h)
			if len(found) == maxLogHints {
				break
			}
		}
	}
	return found
}

var logMarkerRe = regexp.MustCompile("(?i)\\[LOG\\] \\d{4}/|otakase-debug\\.log|```|\\.go:\\d+:|^\\s*error:|otakase diagnostics")

// looksLikeLog tells pasted log output from someone describing a problem.
func looksLikeLog(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if logMarkerRe.MatchString(line) {
			return true
		}
	}
	return false
}

// logAttachment reports whether an attachment is a text log worth fetching.
func logAttachment(a *discordgo.MessageAttachment) bool {
	name := strings.ToLower(a.Filename)
	text := strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".txt") || strings.HasPrefix(a.ContentType, "text/")
	return text && a.Size > 0 && a.Size <= 8<<20
}

// logTail is how much of an attached log is read: errors are near the end.
const logTail = 1 << 20

func fetchLog(url string) string {
	resp, err := httpClient.Get(url)
	if err != nil {
		log.Printf("log reader: %v", err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if len(raw) > logTail {
		raw = raw[len(raw)-logTail:]
	}
	return string(raw)
}

// logReader remembers which fixes a post already got.
type logReader struct {
	mu   sync.Mutex
	sent map[string]bool
}

func newLogReader() *logReader { return &logReader{sent: map[string]bool{}} }

// fresh drops hints the thread was already given and marks the rest given.
func (r *logReader) fresh(thread string, hints []logHint) []logHint {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sent) > 5000 {
		r.sent = map[string]bool{}
	}
	var out []logHint
	for _, h := range hints {
		if k := thread + "/" + h.Key; !r.sent[k] {
			r.sent[k] = true
			out = append(out, h)
		}
	}
	return out
}

// readSupportLog handles a message in a support post that carries a log. It
// reports whether it took the message, so the FAQ replies stay out of it.
func (b *bot) readSupportLog(m *discordgo.MessageCreate) bool {
	if b.cfg.SupportForum == "" {
		return false
	}
	var files []string
	for _, a := range m.Attachments {
		if logAttachment(a) {
			files = append(files, a.URL)
		}
	}
	if len(files) == 0 && !looksLikeLog(m.Content) {
		return false
	}
	th, err := b.s.State.Channel(m.ChannelID)
	if err != nil {
		th, err = b.s.Channel(m.ChannelID)
	}
	if err != nil || th.ParentID != b.cfg.SupportForum {
		return false
	}
	go func() {
		text := m.Content
		for _, u := range files {
			text += "\n" + fetchLog(u)
		}
		if _, said := mentionedVersion(m.Content); len(files) > 0 && !said {
			b.checkVersionIn(m, text) // a long diagnostics block arrives as a file
		}
		hints := b.logs.fresh(m.ChannelID, diagnoseLog(text))
		if len(hints) == 0 {
			return
		}
		reply := logReply(hints)
		reply.Reference = m.Reference()
		msg, err := b.s.ChannelMessageSendComplex(m.ChannelID, reply)
		if err != nil {
			log.Printf("log reader: %v", err)
			return
		}
		var keys []string
		for _, h := range hints {
			keys = append(keys, "log:"+h.Key)
		}
		b.trackAnswer(msg, keys, m.Content)
	}()
	return true
}

func logReply(hints []logHint) *discordgo.MessageSend {
	e := &discordgo.MessageEmbed{
		Title:  "Spotted in your log",
		Color:  shu,
		Footer: &discordgo.MessageEmbedFooter{Text: "Not it? Someone will take a look."},
	}
	for _, h := range hints {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: h.Title, Value: h.Fix})
	}
	return &discordgo.MessageSend{
		Embeds:          []*discordgo.MessageEmbed{e},
		Components:      append(linkButtons("Troubleshooting", wikiURL+"/Troubleshooting"), answerRow()),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
}
