package main

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
)

// Emoji riddles and title scramble, played in the Activity. A riddle is a
// show's title told in emojis; members add their own with /riddle-add
// (they can't win their own). A scramble is a popular show's title with
// each word's letters shuffled.

// riddle is an emoji riddle. Built-in ones have ids starting with "s".
type riddle struct {
	ID     string   `json:"id"`
	Emojis string   `json:"emojis"`
	Name   string   `json:"name"`
	Titles []string `json:"titles"`
	URL    string   `json:"url,omitempty"`
	By     string   `json:"by,omitempty"` // Discord user id of whoever added it
}

// builtinRiddles are the riddles the bot starts with. Titles are every
// name that counts as right.
var builtinRiddles = []riddle{
	{ID: "s1", Emojis: "🧱👹⚔️🏰", Name: "Attack on Titan", Titles: []string{"Attack on Titan", "Shingeki no Kyojin"}},
	{ID: "s2", Emojis: "📓💀🍎", Name: "Death Note", Titles: []string{"Death Note"}},
	{ID: "s3", Emojis: "🏴‍☠️👒🍖🌊", Name: "One Piece", Titles: []string{"One Piece"}},
	{ID: "s4", Emojis: "🍥🦊🥷", Name: "Naruto", Titles: []string{"Naruto"}},
	{ID: "s5", Emojis: "🛁👧🐉🏮", Name: "Spirited Away", Titles: []string{"Spirited Away", "Sen to Chihiro no Kamikakushi"}},
	{ID: "s6", Emojis: "☄️🔄👦👧", Name: "Your Name", Titles: []string{"Your Name", "Your Name.", "Kimi no Na wa"}},
	{ID: "s7", Emojis: "👹🗡️🌊🔥", Name: "Demon Slayer", Titles: []string{"Demon Slayer", "Demon Slayer: Kimetsu no Yaiba", "Kimetsu no Yaiba"}},
	{ID: "s8", Emojis: "🦸🏫💥", Name: "My Hero Academia", Titles: []string{"My Hero Academia", "Boku no Hero Academia"}},
	{ID: "s9", Emojis: "⚗️👬🦾🛡️", Name: "Fullmetal Alchemist: Brotherhood", Titles: []string{"Fullmetal Alchemist: Brotherhood", "Fullmetal Alchemist", "Hagane no Renkinjutsushi"}},
	{ID: "s10", Emojis: "🤞👿🏫", Name: "Jujutsu Kaisen", Titles: []string{"Jujutsu Kaisen"}},
	{ID: "s11", Emojis: "⛓️🪚👨🐶", Name: "Chainsaw Man", Titles: []string{"Chainsaw Man"}},
	{ID: "s12", Emojis: "🕵️🔫👧🥜", Name: "Spy x Family", Titles: []string{"Spy x Family", "Spy Family"}},
	{ID: "s13", Emojis: "👊🥚🦲", Name: "One Punch Man", Titles: []string{"One Punch Man", "One-Punch Man"}},
	{ID: "s14", Emojis: "🏐🐦‍⬛🦊", Name: "Haikyu!!", Titles: []string{"Haikyu!!", "Haikyuu", "Haikyu"}},
	{ID: "s15", Emojis: "🚀🤠🎷", Name: "Cowboy Bebop", Titles: []string{"Cowboy Bebop"}},
	{ID: "s16", Emojis: "⏳📱🧪🍌", Name: "Steins;Gate", Titles: []string{"Steins;Gate", "Steins Gate"}},
	{ID: "s17", Emojis: "👁️♟️👑🤖", Name: "Code Geass", Titles: []string{"Code Geass", "Code Geass: Lelouch of the Rebellion"}},
	{ID: "s18", Emojis: "🤖👦😢🌅", Name: "Neon Genesis Evangelion", Titles: []string{"Neon Genesis Evangelion", "Evangelion", "Shinseiki Evangelion"}},
	{ID: "s19", Emojis: "🎣🧒⚡🃏", Name: "Hunter x Hunter", Titles: []string{"Hunter x Hunter", "Hunter Hunter"}},
	{ID: "s20", Emojis: "🧝⏳💐🧙", Name: "Frieren: Beyond Journey's End", Titles: []string{"Frieren: Beyond Journey's End", "Frieren", "Sousou no Frieren"}},
	{ID: "s21", Emojis: "💯🧑‍🦱🔮", Name: "Mob Psycho 100", Titles: []string{"Mob Psycho 100", "Mob Psycho"}},
	{ID: "s22", Emojis: "💀🔁🏠👧", Name: "Re:Zero", Titles: []string{"Re:Zero", "Re:Zero - Starting Life in Another World", "Re Zero"}},
	{ID: "s23", Emojis: "✉️🦾💌", Name: "Violet Evergarden", Titles: []string{"Violet Evergarden"}},
	{ID: "s24", Emojis: "🎻🎹🌸😭", Name: "Your Lie in April", Titles: []string{"Your Lie in April", "Shigatsu wa Kimi no Uso"}},
	{ID: "s25", Emojis: "🤟🧏‍♀️🌉", Name: "A Silent Voice", Titles: []string{"A Silent Voice", "Koe no Katachi"}},
	{ID: "s26", Emojis: "🎸😰🎤", Name: "Bocchi the Rock!", Titles: []string{"Bocchi the Rock!", "Bocchi the Rock"}},
	{ID: "s27", Emojis: "⭐🎤👶👁️", Name: "Oshi no Ko", Titles: []string{"Oshi no Ko", "[Oshi no Ko]"}},
	{ID: "s28", Emojis: "🪨🧪🌍", Name: "Dr. Stone", Titles: []string{"Dr. Stone", "Dr Stone"}},
	{ID: "s29", Emojis: "🏠👧👦🚪🌲", Name: "The Promised Neverland", Titles: []string{"The Promised Neverland", "Yakusoku no Neverland"}},
	{ID: "s30", Emojis: "⛵🪓❄️🌾", Name: "Vinland Saga", Titles: []string{"Vinland Saga"}},
	{ID: "s31", Emojis: "🌙👧💫🐱", Name: "Sailor Moon", Titles: []string{"Sailor Moon", "Bishoujo Senshi Sailor Moon"}},
	{ID: "s32", Emojis: "❤️🧠⚔️🏫", Name: "Kaguya-sama: Love is War", Titles: []string{"Kaguya-sama: Love is War", "Kaguya-sama", "Love is War"}},
	{ID: "s33", Emojis: "🕳️⬇️🧒🤖", Name: "Made in Abyss", Titles: []string{"Made in Abyss"}},
	{ID: "s34", Emojis: "✋👾🧑", Name: "Parasyte", Titles: []string{"Parasyte", "Parasyte: The Maxim", "Kiseijuu"}},
	{ID: "s35", Emojis: "🌲🐻🚌☔", Name: "My Neighbor Totoro", Titles: []string{"My Neighbor Totoro", "Tonari no Totoro"}},
	{ID: "s36", Emojis: "🏰🚶‍♂️🔥👵", Name: "Howl's Moving Castle", Titles: []string{"Howl's Moving Castle", "Howl no Ugoku Shiro"}},
	{ID: "s37", Emojis: "🐺👸🌳", Name: "Princess Mononoke", Titles: []string{"Princess Mononoke", "Mononoke Hime"}},
	{ID: "s38", Emojis: "👻👽👵👦", Name: "Dandadan", Titles: []string{"Dandadan", "Dan Da Dan"}},
	{ID: "s39", Emojis: "🗡️🚪⬆️👤", Name: "Solo Leveling", Titles: []string{"Solo Leveling", "Ore dake Level Up na Ken"}},
	{ID: "s40", Emojis: "⚽🔒👁️", Name: "Blue Lock", Titles: []string{"Blue Lock"}},
	{ID: "s41", Emojis: "🍜☕👁️🩸", Name: "Tokyo Ghoul", Titles: []string{"Tokyo Ghoul"}},
	{ID: "s42", Emojis: "🎮⚔️🔒🖥️", Name: "Sword Art Online", Titles: []string{"Sword Art Online"}},
	{ID: "s43", Emojis: "🐉🟠🔢🐲", Name: "Dragon Ball", Titles: []string{"Dragon Ball", "Dragon Ball Z", "Dragon Ball Super"}},
	{ID: "s44", Emojis: "⚡🐭🧢", Name: "Pokémon", Titles: []string{"Pokémon", "Pokemon"}},
	{ID: "s45", Emojis: "🐯🐉💕", Name: "Toradora!", Titles: []string{"Toradora!", "Toradora"}},
	{ID: "s46", Emojis: "🧙‍♀️💧🧝💥", Name: "KonoSuba", Titles: []string{"KonoSuba", "KonoSuba: God's Blessing on This Wonderful World!", "Kono Subarashii Sekai ni Shukufuku wo!"}},
}

// chatRounds remembers the riddles used lately, so they don't repeat.
type chatRounds struct {
	mu     sync.Mutex
	recent []string // riddle ids used lately
}

// riddles are the built-in riddles staff didn't turn off, then the added ones.
func riddles(d *storeData) []riddle {
	var out []riddle
	for _, r := range builtinRiddles {
		if !slices.Contains(d.RiddlesOff, r.ID) {
			out = append(out, r)
		}
	}
	return append(out, d.Riddles...)
}

// ── Scramble ──

// scrambleWord shuffles a word's letters so it reads differently, keeping
// it as is when it can't (like "aa").
func scrambleWord(w string, intn func(int) int) string {
	r := []rune(strings.ToUpper(w))
	if len(r) < 2 {
		return string(r)
	}
	orig := string(r)
	for range 10 {
		for k := len(r) - 1; k > 0; k-- {
			j := intn(k + 1)
			r[k], r[j] = r[j], r[k]
		}
		if string(r) != orig {
			break
		}
	}
	return string(r)
}

var scrambleOK = regexp.MustCompile(`^[A-Za-z0-9 ]+$`)

// scrambleable reports whether a title makes a fair scramble: plain
// letters, one to three words, 4 to 20 letters.
func scrambleable(name string) bool {
	if !scrambleOK.MatchString(name) {
		return false
	}
	words := strings.Fields(name)
	letters := len(strings.ReplaceAll(name, " ", ""))
	return len(words) >= 1 && len(words) <= 3 && letters >= 4 && letters <= 20
}

// scrambleTitle shuffles every word of a title, like "TCNAKA NO TAINT".
func scrambleTitle(name string, intn func(int) int) string {
	var out []string
	for _, w := range strings.Fields(name) {
		out = append(out, strings.Join(strings.Split(scrambleWord(w, intn), ""), " "))
	}
	return strings.Join(out, "   ")
}

// scrambleHint is each word with its first letter and length.
func scrambleHint(name string) string {
	var out []string
	for _, w := range strings.Fields(name) {
		r := []rune(strings.ToUpper(w))
		out = append(out, string(r[0])+strings.Repeat(" _", len(r)-1))
	}
	return strings.Join(out, "   ")
}

// ── Rounds ──

// pickRiddle chooses a riddle not used lately.
func (b *bot) pickRiddle() (riddle, bool) {
	var all []riddle
	b.store.view(func(d *storeData) { all = riddles(d) })
	if len(all) == 0 {
		return riddle{}, false
	}
	c := &b.chat
	c.mu.Lock()
	defer c.mu.Unlock()
	var choices []riddle
	for _, r := range all {
		if !slices.Contains(c.recent, r.ID) {
			choices = append(choices, r)
		}
	}
	if len(choices) == 0 {
		choices, c.recent = all, nil
	}
	r := choices[rand.IntN(len(choices))]
	c.recent = append(c.recent, r.ID)
	if len(c.recent) > len(all)/2 {
		c.recent = c.recent[1:]
	}
	return r, true
}

// ── Adding riddles ──

// riddleEmojis reports whether text is a fair riddle: no letters or digits
// (so the title can't be spelled out) and at least two symbols.
func riddleEmojis(text string) bool {
	n := 0
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			return false
		case unicode.IsSpace(r) || r == '‍' || r == '️':
		default:
			n++
		}
	}
	return n >= 2
}

const riddleShowQuery = `query ($id: Int) {
  Media(id: $id, type: ANIME) { id siteUrl isAdult synonyms title { romaji english native } }
}`

// maxRiddles caps how many riddles members can add.
const maxRiddles = 500

func (b *bot) addRiddle(i *discordgo.InteractionCreate) {
	opts := optionMap(i)
	emojis := strings.TrimSpace(opts["emojis"].StringValue())
	m := pickedShowRe.FindStringSubmatch(strings.TrimSpace(opts["anime"].StringValue()))
	if m == nil {
		b.respond(i, true, errorEmbed("Pick the anime from the suggestions as you type."))
		return
	}
	if !riddleEmojis(emojis) {
		b.respond(i, true, errorEmbed("Use emojis only (at least two), no letters or numbers."))
		return
	}
	id, _ := strconv.Atoi(m[1])
	var r struct {
		Data struct {
			Media *struct {
				ID       int      `json:"id"`
				SiteURL  string   `json:"siteUrl"`
				IsAdult  bool     `json:"isAdult"`
				Synonyms []string `json:"synonyms"`
				Title    struct{ Romaji, English, Native string }
			} `json:"Media"`
		} `json:"data"`
	}
	if err := anilistPost("", riddleShowQuery, map[string]any{"id": id}, &r); err != nil || r.Data.Media == nil {
		b.respond(i, true, errorEmbed("Couldn't find that show on AniList. Try again."))
		return
	}
	md := r.Data.Media
	if md.IsAdult {
		b.respond(i, true, errorEmbed("Pick a show for everyone."))
		return
	}
	rd := riddle{ID: fmt.Sprintf("m%d-%d", md.ID, time.Now().Unix()), Emojis: emojis, URL: md.SiteURL, By: i.Member.User.ID,
		Name: md.Title.English}
	if rd.Name == "" {
		rd.Name = md.Title.Romaji
	}
	for _, t := range append([]string{md.Title.English, md.Title.Romaji, md.Title.Native}, md.Synonyms...) {
		if strings.TrimSpace(t) != "" {
			rd.Titles = append(rd.Titles, t)
		}
	}
	full := false
	b.store.update(func(d *storeData) {
		if full = len(d.Riddles) >= maxRiddles; !full {
			d.Riddles = append(d.Riddles, rd)
		}
	})
	if full {
		b.respond(i, true, errorEmbed("There are too many riddles. Ask staff to clear some on the dashboard."))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Title: "Riddle added",
		Description: fmt.Sprintf("%s → **%s**\n\nIt can come up in the Activity's emoji riddles now. You can't win your own riddle.", emojis, rd.Name)})
}

// removeRiddle drops an added riddle or turns a built-in one off.
func (st *store) removeRiddle(id string) (riddle, bool) {
	var gone riddle
	var found bool
	st.update(func(d *storeData) {
		for k, r := range d.Riddles {
			if r.ID == id {
				gone, found = r, true
				d.Riddles = slices.Delete(d.Riddles, k, k+1)
				return
			}
		}
		for _, r := range builtinRiddles {
			if r.ID == id && !slices.Contains(d.RiddlesOff, id) {
				gone, found = r, true
				d.RiddlesOff = append(d.RiddlesOff, id)
			}
		}
	})
	return gone, found
}
