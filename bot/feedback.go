package main

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// "Did this help?": the bot's automatic answers (FAQ replies, wiki links on
// new support posts, log fixes, the update reminder) carry Helped and Didn't
// help buttons. Each person votes once per answer. Didn't help tells staff in
// the bot log, and /answer-stats lists the answers that miss most often.

// answerMsg is one answer the bot posted, by message id in the store.
type answerMsg struct {
	Channel  string    `json:"channel"`
	Keys     []string  `json:"keys"` // what was answered, like "faq:mpv" or "log:no-stream"
	Question string    `json:"question,omitempty"`
	At       time.Time `json:"at"`
	Voters   []string  `json:"voters,omitempty"`
}

type answerTally struct {
	Helped    int `json:"helped"`
	NotHelped int `json:"not_helped"`
}

// Answers older than answerKeep, or past the newest answersKept, stop taking
// votes, so the store stays small. The tallies are kept.
const (
	answerKeep  = 60 * 24 * time.Hour
	answersKept = 2000
)

// answerRow is the Helped / Didn't help buttons.
func answerRow() discordgo.ActionsRow {
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Helped", Emoji: &discordgo.ComponentEmoji{Name: "👍"}, Style: discordgo.SecondaryButton, CustomID: "ans-up"},
		discordgo.Button{Label: "Didn't help", Emoji: &discordgo.ComponentEmoji{Name: "👎"}, Style: discordgo.SecondaryButton, CustomID: "ans-down"},
	}}
}

// answerRows is answerRow when on is true, else nothing.
func answerRows(on bool) []discordgo.MessageComponent {
	if !on {
		return nil
	}
	return []discordgo.MessageComponent{answerRow()}
}

// trackAnswer remembers an answer the bot posted so its votes can be counted.
func (b *bot) trackAnswer(msg *discordgo.Message, keys []string, question string) {
	if msg == nil || len(keys) == 0 {
		return
	}
	now := time.Now().UTC()
	b.store.update(func(d *storeData) {
		if d.Answers == nil {
			d.Answers = map[string]answerMsg{}
		}
		d.Answers[msg.ID] = answerMsg{Channel: msg.ChannelID, Keys: keys, Question: clip(strings.TrimSpace(question), 400), At: now}
		pruneAnswers(d.Answers, now)
	})
}

// pruneAnswers drops answers too old to vote on, and the oldest past the cap.
func pruneAnswers(a map[string]answerMsg, now time.Time) {
	for id, m := range a {
		if now.Sub(m.At) > answerKeep {
			delete(a, id)
		}
	}
	if len(a) <= answersKept {
		return
	}
	ids := make([]string, 0, len(a))
	for id := range a {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return a[ids[i]].At.Before(a[ids[j]].At) })
	for _, id := range ids[:len(ids)-answersKept] {
		delete(a, id)
	}
}

type voteResult int

const (
	voteCounted voteResult = iota
	voteAgain
	voteUnknown
)

// recordAnswerVote counts userID's vote on the answer in message msgID.
func recordAnswerVote(d *storeData, msgID, userID string, helped bool) (answerMsg, voteResult) {
	a, ok := d.Answers[msgID]
	if !ok {
		return a, voteUnknown
	}
	if slices.Contains(a.Voters, userID) {
		return a, voteAgain
	}
	a.Voters = append(a.Voters, userID)
	d.Answers[msgID] = a
	if d.AnswerStats == nil {
		d.AnswerStats = map[string]answerTally{}
	}
	for _, k := range a.Keys {
		t := d.AnswerStats[k]
		if helped {
			t.Helped++
		} else {
			t.NotHelped++
		}
		d.AnswerStats[k] = t
	}
	return a, voteCounted
}

// answerLabel says in words which answer a key is.
func answerLabel(key string) string {
	kind, name, _ := strings.Cut(key, ":")
	switch kind {
	case "faq":
		if f, ok := faqByKey(name); ok {
			return "FAQ: " + f.Question
		}
		return "FAQ: " + name
	case "wiki":
		page, anchor, _ := strings.Cut(name, "#")
		label := "Wiki: " + strings.ReplaceAll(page, "-", " ")
		if anchor != "" {
			label += " › " + strings.ReplaceAll(anchor, "-", " ")
		}
		return label
	case "log":
		for _, h := range logHints {
			if h.Key == name {
				return "Log fix: " + h.Title
			}
		}
		return "Log fix: " + name
	case "version":
		return "Update reminder"
	}
	return key
}

// answerKeysText lists answer keys in words, linking wiki sections.
func answerKeysText(keys []string) string {
	var out []string
	for _, k := range keys {
		label := answerLabel(k)
		if page, ok := strings.CutPrefix(k, "wiki:"); ok {
			label = fmt.Sprintf("[%s](%s/%s)", label, wikiURL, page)
		}
		out = append(out, label)
	}
	return strings.Join(out, "\n")
}

var wikiLinkRe = regexp.MustCompile(`\]\(` + regexp.QuoteMeta(wikiURL) + `/([^)\s]+)\)`)

// wikiAnswerKeys are the wiki sections a "From the wiki" card links.
func wikiAnswerKeys(e *discordgo.MessageEmbed) []string {
	if e == nil {
		return nil
	}
	var keys []string
	for _, m := range wikiLinkRe.FindAllStringSubmatch(e.Description, -1) {
		keys = append(keys, "wiki:"+m[1])
	}
	return keys
}

// voteAnswer handles the Helped and Didn't help buttons.
func (b *bot) voteAnswer(i *discordgo.InteractionCreate, helped bool) {
	var a answerMsg
	var res voteResult
	b.store.update(func(d *storeData) { a, res = recordAnswerVote(d, i.Message.ID, i.Member.User.ID, helped) })
	switch res {
	case voteUnknown:
		b.respond(i, true, errorEmbed("This answer is too old to vote on."))
		return
	case voteAgain:
		b.respond(i, true, errorEmbed("You already voted on this answer. Thanks!"))
		return
	}
	if helped {
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Thanks, glad it helped!"})
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu,
		Description: "Thanks for telling us. Staff will see it; describing the problem a bit more here helps someone answer."})
	question := a.Question
	if question == "" {
		question = "(no text)"
	}
	b.staffNote(&discordgo.MessageEmbed{
		Title: "An answer didn't help",
		URL:   messageLink(b.cfg.GuildID, i.ChannelID, i.Message.ID),
		Color: 0x8A8276,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Answer", Value: clip(answerKeysText(a.Keys), 1000)},
			{Name: "Question", Value: "```\n" + truncate(question, 900) + "\n```"},
			{Name: "Voted by", Value: "<@" + i.Member.User.ID + ">", Inline: true},
			{Name: "Where", Value: "<#" + i.ChannelID + ">", Inline: true},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

// answerStatsEmbed lists the answers with the most Didn't help votes.
func answerStatsEmbed(stats map[string]answerTally) *discordgo.MessageEmbed {
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		x, y := stats[keys[i]], stats[keys[j]]
		if x.NotHelped != y.NotHelped {
			return x.NotHelped > y.NotHelped
		}
		if x.Helped != y.Helped {
			return x.Helped < y.Helped
		}
		return keys[i] < keys[j]
	})
	var lines []string
	for k, key := range keys {
		if k == 15 {
			break
		}
		t := stats[key]
		lines = append(lines, fmt.Sprintf("**%s** · didn't help %d · helped %d", clip(answerLabel(key), 80), t.NotHelped, t.Helped))
	}
	if len(lines) == 0 {
		lines = []string{"No votes yet."}
	}
	return &discordgo.MessageEmbed{Title: "Answers that miss most", Description: strings.Join(lines, "\n"), Color: shu,
		Footer: &discordgo.MessageEmbedFooter{Text: "Votes on the bot's automatic answers"}}
}

func (b *bot) answerStats(i *discordgo.InteractionCreate) {
	var e *discordgo.MessageEmbed
	b.store.view(func(d *storeData) { e = answerStatsEmbed(d.AnswerStats) })
	b.respond(i, true, e)
}

// postQuestion is a support post's title and first message.
func postQuestion(s *discordgo.Session, th *discordgo.Channel) string {
	q := th.Name
	// A forum post's first message has the same id as the post.
	if m, err := s.ChannelMessage(th.ID, th.ID); err == nil && m.Content != "" {
		q += "\n" + m.Content
	}
	return q
}
