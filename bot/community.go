package main

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/bwmarrin/discordgo"
)

// Helpers shared by the spoiler guard, the guessing game and levels: what
// kind of channel a message is in, and title normalising.

// chatPlace describes where a message was posted.
type chatPlace struct {
	Name    string // channel name without its prefix, lower case; a thread's parent
	Staff   bool   // mod-chat, bot-log and other staff channels
	Support bool   // the support forum and its posts
	Bots    bool   // bot command channels
}

// channel finds a channel in the state, asking Discord when it isn't there.
func (b *bot) channel(id string) *discordgo.Channel {
	if c, err := b.s.State.Channel(id); err == nil {
		return c
	}
	c, err := b.s.Channel(id)
	if err != nil {
		return nil
	}
	_ = b.s.State.ChannelAdd(c)
	return c
}

// place says what kind of channel id is. Threads count as their parent.
func (b *bot) place(id string) chatPlace {
	c := b.channel(id)
	if c == nil {
		return chatPlace{Staff: true} // unknown: leave it alone
	}
	if c.IsThread() && c.ParentID != "" {
		if p := b.channel(c.ParentID); p != nil {
			c = p
		}
	}
	p := chatPlace{Name: strings.ToLower(channelBase(c.Name))}
	p.Support = c.ID == b.cfg.SupportForum || p.Name == "support"
	p.Staff = c.ID == b.cfg.ModChannel || c.ID == b.cfg.LogChannel || staffName(p.Name)
	if !p.Staff && c.ParentID != "" {
		if cat := b.channel(c.ParentID); cat != nil {
			p.Staff = staffName(strings.ToLower(channelBase(cat.Name)))
		}
	}
	p.Bots = botsName(p.Name)
	return p
}

func staffName(n string) bool {
	for _, w := range []string{"mod", "staff", "admin", "bot-log", "logs", "maintainer"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

func botsName(n string) bool {
	return n == "bots" || n == "bot" || strings.Contains(n, "bot-command") || strings.Contains(n, "bot-spam") ||
		strings.Contains(n, "commands")
}

// chatName reports whether a channel is for chatting about anime or
// anything: general, anime, off-topic and the like.
func chatName(n string) bool {
	for _, w := range []string{"general", "anime", "chat", "off-topic", "offtopic", "lounge", "seasonal", "discussion", "manga", "memes"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// accents folds the accented letters romaji and English titles use.
var accents = strings.NewReplacer(
	"ā", "a", "á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a",
	"ē", "e", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"ī", "i", "í", "i", "ì", "i", "î", "i", "ï", "i",
	"ō", "o", "ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o", "ø", "o",
	"ū", "u", "ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "×", "x", "'", "", "’", "", "‘", "",
)

// normWords lower-cases s, folds accents and turns punctuation into spaces.
func normWords(s string) string {
	s = accents.Replace(strings.ToLower(s))
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

var (
	seasonWordRe  = regexp.MustCompile(`\b((the )?final season|season \w+|\d+(st|nd|rd|th) season|part \w+|cour \w+|s\d+|\d+(st|nd|rd|th) (part|cour))\b`)
	trailingNumRe = regexp.MustCompile(` (\d+|ii|iii|iv|v|vi)$`)
)

// normTitle is a title as members would type it: lower case, no
// punctuation, no "the", no season or part numbers.
func normTitle(s string) string {
	s = normWords(s)
	s = seasonWordRe.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	s = trailingNumRe.ReplaceAllString(s, "")
	var words []string
	for _, w := range strings.Fields(s) {
		if w != "the" {
			words = append(words, w)
		}
	}
	return strings.Join(words, " ")
}

// mainTitle is the part of a title before a subtitle ("Frieren: Beyond
// Journey's End" → "Frieren"), or "" when there is no subtitle.
func mainTitle(s string) string {
	for _, sep := range []string{":", " - ", " – ", " -", "～", "~"} {
		if k := strings.Index(s, sep); k > 0 {
			return strings.TrimSpace(s[:k])
		}
	}
	return ""
}
