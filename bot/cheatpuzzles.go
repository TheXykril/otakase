package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// The puzzles behind the cheat code (cheathunt.go). Each hunt is made fresh
// for the member who asked: one of five kinds, three steps each, built from
// the games' pool of popular shows, so the shows, words, ciphers and the
// final question change every time and nobody can solve one from memory.
//
//	1. An opening that depends on the kind (a coded message, a studio
//	   acrostic, a timeline, a character, a letter grid). Its answer is
//	   typed in Otakase.
//	2. A coded message, often keyed by the first answer, that says what to
//	   /whisper to the bot in Discord.
//	3. What the bot whispers back: a question about one more show, looked up
//	   on AniList or MyAnimeList and answered in Otakase.
//
// The answers come from the same AniList data the shows were picked from,
// so the bot can check them without asking anyone.

// huntStep is one step of a hunt. A step is answered in the app (Answers)
// or with /whisper in Discord (Whisper), never both.
type huntStep struct {
	Text    string   `json:"text"`
	Answers []string `json:"answers,omitempty"` // as huntNorm makes them
	Whisper string   `json:"whisper,omitempty"` // as huntNorm makes it
	Links   []string `json:"links,omitempty"`   // pages the app can open
}

type huntPuzzle struct {
	Kind  string     `json:"kind"`
	Steps []huntStep `json:"steps"`
}

// huntKinds are the five puzzles, by the name the app shows.
var huntKinds = []struct {
	name  string
	first func(pool []guessShow, intn func(int) int) (huntStep, string, error)
}{
	{"Intercepted transmission", huntTransmission},
	{"Studio acrostic", huntAcrostic},
	{"Lost timeline", huntTimeline},
	{"Missing person", huntCharacter},
	{"Letter grid", huntGrid},
}

var errHuntPool = errors.New("not enough shows to build a puzzle")

// Words for the whispers and the hidden words. Only A-Z.
var (
	huntAdjectives = []string{"SILENT", "CRIMSON", "HOLLOW", "GOLDEN", "SLEEPING", "WANDERING", "BROKEN", "PALE",
		"HIDDEN", "FALLEN", "ENDLESS", "FROZEN", "VELVET", "QUIET", "SCARLET", "MIDNIGHT", "PAPER", "IRON", "LONELY", "ASHEN"}
	huntNouns = []string{"KITSUNE", "LANTERN", "TORII", "KATANA", "RONIN", "SAKURA", "TANUKI", "SHRINE", "CRANE",
		"DRAGON", "SHOGUN", "YOKAI", "HANABI", "ORIGAMI", "TEAPOT", "TENGU", "KAPPA", "DARUMA", "FERRY", "LOTUS",
		"PAGODA", "SCROLL", "BONSAI", "MOCHI", "RAMEN", "KOTATSU", "UMBRELLA", "COMET", "MIRROR", "PUPPET"}
)

// newHuntPuzzle builds a hunt of a random kind.
func newHuntPuzzle(pool []guessShow, intn func(int) int) (huntPuzzle, error) {
	// A kind the pool can't feed (a pool missing studios, say) gives way to
	// the next one.
	start := intn(len(huntKinds))
	var err error
	for k := range huntKinds {
		kind := huntKinds[(start+k)%len(huntKinds)]
		var p huntPuzzle
		if p, err = buildHunt(kind.name, kind.first, pool, intn); err == nil {
			return p, nil
		}
	}
	return huntPuzzle{}, err
}

func buildHunt(name string, first func([]guessShow, func(int) int) (huntStep, string, error), pool []guessShow, intn func(int) int) (huntPuzzle, error) {
	s1, key, err := first(pool, intn)
	if err != nil {
		return huntPuzzle{}, err
	}
	s2 := huntWhisperStep(key, intn)
	s3, err := huntFinal(pool, intn)
	if err != nil {
		return huntPuzzle{}, err
	}
	s3.Text = "The bot leans in and whispers:\n\n" + s3.Text
	return huntPuzzle{Kind: name, Steps: []huntStep{s1, s2, s3}}, nil
}

// ── Answers ──

// huntNorm is an answer as compared: lower case letters and digits only, so
// "A-1 Pictures", "a1 pictures" and "A1PICTURES" are the same.
func huntNorm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// studioAnswers accepts a studio with or without a leading "Studio".
func studioAnswers(name string) []string {
	out := []string{huntNorm(name)}
	if rest, ok := strings.CutPrefix(out[0], "studio"); ok && rest != "" {
		out = append(out, rest)
	}
	return out
}

// ── Ciphers ──

// Plain texts are upper case; ciphers change A-Z and keep everything else.

func shiftLetters(s string, by func(k int) int) string {
	var b strings.Builder
	k := 0
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			b.WriteRune('A' + rune(((int(r-'A')+by(k))%26+26)%26))
			k++
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func caesar(s string, shift int) string { return shiftLetters(s, func(int) int { return shift }) }

// vigenere shifts each letter by the next letter of key (A = 0). key holds
// only A-Z and is not empty.
func vigenere(s, key string) string {
	return shiftLetters(s, func(k int) int { return int(key[k%len(key)] - 'A') })
}

func atbash(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return 'Z' - (r - 'A')
		}
		return r
	}, s)
}

// a1z26 writes letters as their place in the alphabet, words apart with " / ".
func a1z26(s string) string {
	var words []string
	for _, w := range strings.Fields(s) {
		var nums []string
		for _, r := range w {
			if r >= 'A' && r <= 'Z' {
				nums = append(nums, strconv.Itoa(int(r-'A')+1))
			} else {
				nums = append(nums, string(r))
			}
		}
		words = append(words, strings.Join(nums, "-"))
	}
	return strings.Join(words, " / ")
}

func reverse(s string) string {
	r := []rune(s)
	slices.Reverse(r)
	return string(r)
}

// encodeOpen hides plain with a cipher that needs no key, and says how in a
// hint that names no cipher outright.
func encodeOpen(plain string, intn func(int) int) (coded, hint string) {
	switch intn(4) {
	case 0:
		return caesar(plain, 3+intn(21)), "Every letter took the same number of steps forward through the alphabet. How many is for you to find."
	case 1:
		return atbash(plain), "A is Z. B is Y. The rest follows."
	case 2:
		return a1z26(plain), "A is 1. Z is 26."
	default:
		return atbash(reverse(plain)), "Hold it up to a mirror first. Then A is Z."
	}
}

// encodeKeyed hides plain with the answer just given as the key: a Vigenère
// key when it has letters, a Caesar shift when it is a number. With no
// usable key it falls back to encodeOpen.
func encodeKeyed(plain, key string, intn func(int) int) (coded, hint string) {
	letters := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - 'a' + 'A'
		}
		return -1
	}, key)
	if letters != "" && intn(3) > 0 {
		return vigenere(plain, letters), "Your last answer is the key: under each letter of the message write the key, again and again (letters only). A key letter A moves nothing, B moves one step forward, and so on. Undo it."
	}
	if letters != "" && len(letters)%26 != 0 {
		return caesar(plain, len(letters)), "Every letter moved forward as many steps as your last answer has letters."
	}
	if n, err := strconv.Atoi(key); err == nil && n > 0 {
		sum := 0
		for _, d := range key {
			sum += int(d - '0')
		}
		if sum%26 != 0 {
			return caesar(plain, sum), "Every letter moved forward as many steps as the digits of your last answer add up to."
		}
	}
	return encodeOpen(plain, intn)
}

// huntWhisperStep is the second step: a coded message saying what to whisper.
func huntWhisperStep(key string, intn func(int) int) huntStep {
	phrase := huntAdjectives[intn(len(huntAdjectives))] + " " + huntNouns[intn(len(huntNouns))]
	coded, hint := encodeKeyed("WHISPER "+phrase+" TO THE BOT", key, intn)
	return huntStep{
		Text:    "A second message arrived:\n\n    " + coded + "\n\n" + hint + "\n\nThis step is not answered here.",
		Whisper: huntNorm(phrase),
	}
}

// ── First steps ──

func anilistURL(id int) string { return fmt.Sprintf("https://anilist.co/anime/%d", id) }
func malURL(id int) string     { return fmt.Sprintf("https://myanimelist.net/anime/%d", id) }

// shuffled is a shuffled copy of the shows that pass keep.
func shuffled(pool []guessShow, keep func(guessShow) bool, intn func(int) int) []guessShow {
	var out []guessShow
	for _, s := range pool {
		if keep(s) {
			out = append(out, s)
		}
	}
	for k := len(out) - 1; k > 0; k-- {
		j := intn(k + 1)
		out[k], out[j] = out[j], out[k]
	}
	return out
}

// huntTransmission: a coded note names a show; the answer is its studio.
func huntTransmission(pool []guessShow, intn func(int) int) (huntStep, string, error) {
	shows := shuffled(pool, func(s guessShow) bool { return s.Studio != "" && huntNorm(s.Studio) != "" }, intn)
	if len(shows) == 0 {
		return huntStep{}, "", errHuntPool
	}
	s := shows[0]
	coded, hint := encodeOpen(fmt.Sprintf("FIND ANILIST ANIME %d AND NAME ITS MAIN STUDIO", s.ID), intn)
	return huntStep{
		Text:    "Something intercepted a message:\n\n    " + coded + "\n\n" + hint,
		Answers: studioAnswers(s.Studio),
	}, huntNorm(s.Studio), nil
}

// studioInitial is the first A-Z letter of a studio's name, 0 when none.
func studioInitial(name string) byte {
	for _, r := range strings.ToUpper(name) {
		if r >= 'A' && r <= 'Z' {
			return byte(r)
		}
		if unicode.IsLetter(r) {
			return 0
		}
	}
	return 0
}

// huntAcrostic: the first letters of some shows' studios spell a word.
func huntAcrostic(pool []guessShow, intn func(int) int) (huntStep, string, error) {
	byLetter := map[byte][]guessShow{}
	for _, s := range shuffled(pool, func(s guessShow) bool { return studioInitial(s.Studio) != 0 }, intn) {
		c := studioInitial(s.Studio)
		byLetter[c] = append(byLetter[c], s)
	}
	var words []string
	for _, w := range huntNouns {
		if len(w) < 5 || len(w) > 7 {
			continue
		}
		need := map[byte]int{}
		ok := true
		for k := range len(w) {
			need[w[k]]++
			if len(byLetter[w[k]]) < need[w[k]] {
				ok = false
			}
		}
		if ok {
			words = append(words, w)
		}
	}
	if len(words) == 0 {
		return huntStep{}, "", errHuntPool
	}
	word := words[intn(len(words))]
	used := map[byte]int{}
	var lines, links []string
	for k := range len(word) {
		s := byLetter[word[k]][used[word[k]]]
		used[word[k]]++
		lines = append(lines, fmt.Sprintf("%d. %s", k+1, anilistURL(s.ID)))
		links = append(links, anilistURL(s.ID))
	}
	return huntStep{
		Text: "A list was left behind:\n\n" + strings.Join(lines, "\n") +
			"\n\nEach show has a main studio. Their first letters, in this order, spell a word.",
		Answers: []string{huntNorm(word)},
		Links:   links,
	}, huntNorm(word), nil
}

var seasonOrder = map[string]int{"WINTER": 0, "SPRING": 1, "SUMMER": 2, "FALL": 3}

// huntTimeline: lettered shows, put in the order they first aired, spell a word.
func huntTimeline(pool []guessShow, intn func(int) int) (huntStep, string, error) {
	var words []string
	for _, w := range huntNouns {
		if len(w) == 5 || len(w) == 6 {
			words = append(words, w)
		}
	}
	word := words[intn(len(words))]
	// Two years apart at least, so a show that MyAnimeList dates a few
	// weeks off from AniList can't swap places.
	var picked []guessShow
	for _, s := range shuffled(pool, func(s guessShow) bool { return s.MalID > 0 && s.Year > 0 }, intn) {
		if !slices.ContainsFunc(picked, func(p guessShow) bool { return p.Year-s.Year < 2 && s.Year-p.Year < 2 }) {
			picked = append(picked, s)
		}
		if len(picked) == len(word) {
			break
		}
	}
	if len(picked) < len(word) {
		return huntStep{}, "", errHuntPool
	}
	slices.SortFunc(picked, func(a, b guessShow) int { return a.Year - b.Year })
	type row struct {
		letter byte
		show   guessShow
	}
	rows := make([]row, len(word))
	for k := range rows {
		rows[k] = row{word[k], picked[k]}
	}
	for k := len(rows) - 1; k > 0; k-- {
		j := intn(k + 1)
		rows[k], rows[j] = rows[j], rows[k]
	}
	var lines, links []string
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf("[%c] %s", r.letter, malURL(r.show.MalID)))
		links = append(links, malURL(r.show.MalID))
	}
	return huntStep{
		Text: "Someone shuffled the timeline:\n\n" + strings.Join(lines, "\n") +
			"\n\nPut the shows in the order they first aired, oldest first, and read their letters.",
		Answers: []string{huntNorm(word)},
		Links:   links,
	}, huntNorm(word), nil
}

// huntCharacter: a main character's name; the answer is the year their
// show began. Only a character who leads one show in the pool, so the year
// is certain.
func huntCharacter(pool []guessShow, intn func(int) int) (huntStep, string, error) {
	shows := map[string]int{}
	for _, s := range pool {
		for _, l := range s.Leads {
			shows[huntNorm(l)]++
		}
	}
	for _, s := range shuffled(pool, func(s guessShow) bool { return s.Year > 0 && len(s.Leads) > 0 }, intn) {
		lead := s.Leads[intn(len(s.Leads))]
		if shows[huntNorm(lead)] != 1 || huntNorm(lead) == "" {
			continue
		}
		year := strconv.Itoa(s.Year)
		return huntStep{
			Text: "A missing person report:\n\n    " + lead +
				"\n\nThey are a main character in a show on AniList. Find their best-known show. Answer: the year it began.",
			Answers: []string{year},
		}, year, nil
	}
	return huntStep{}, "", errHuntPool
}

// huntGridSize is the letter grid's width and height.
const huntGridSize = 9

var gridDirections = [][2]int{{0, 1}, {1, 0}, {1, 1}, {-1, 1}, {0, -1}, {-1, 0}, {-1, -1}, {1, -1}}

// huntGrid: one word hides in a grid of letters, maybe backwards or diagonal.
func huntGrid(pool []guessShow, intn func(int) int) (huntStep, string, error) {
	var words []string
	for _, w := range huntNouns {
		if len(w) >= 5 && len(w) <= huntGridSize {
			words = append(words, w)
		}
	}
	for range 50 {
		word := words[intn(len(words))]
		grid, ok := placeWord(word, intn)
		if !ok || countWord(grid, word) != 1 {
			continue
		}
		var lines []string
		for _, row := range grid {
			lines = append(lines, "    "+strings.Join(strings.Split(string(row), ""), " "))
		}
		return huntStep{
			Text: "A page of letters was found:\n\n" + strings.Join(lines, "\n") +
				"\n\nOne word hides here in a straight line: across, down or diagonal, maybe backwards.",
			Answers: []string{huntNorm(word)},
		}, huntNorm(word), nil
	}
	return huntStep{}, "", errHuntPool
}

func placeWord(word string, intn func(int) int) ([][]byte, bool) {
	n := huntGridSize
	grid := make([][]byte, n)
	for r := range grid {
		grid[r] = make([]byte, n)
		for c := range grid[r] {
			grid[r][c] = byte('A' + intn(26))
		}
	}
	d := gridDirections[intn(len(gridDirections))]
	// The start must leave room for the word in direction d.
	var rows, cols []int
	for k := range n {
		if end := k + d[0]*(len(word)-1); end >= 0 && end < n {
			rows = append(rows, k)
		}
		if end := k + d[1]*(len(word)-1); end >= 0 && end < n {
			cols = append(cols, k)
		}
	}
	if len(rows) == 0 || len(cols) == 0 {
		return nil, false
	}
	r, c := rows[intn(len(rows))], cols[intn(len(cols))]
	for k := range len(word) {
		grid[r+d[0]*k][c+d[1]*k] = word[k]
	}
	return grid, true
}

// countWord is how many times word reads in grid, every direction counted.
func countWord(grid [][]byte, word string) int {
	n, found := len(grid), 0
	for r := range n {
		for c := range n {
			for _, d := range gridDirections {
				k := 0
				for ; k < len(word); k++ {
					rr, cc := r+d[0]*k, c+d[1]*k
					if rr < 0 || rr >= n || cc < 0 || cc >= n || grid[rr][cc] != word[k] {
						break
					}
				}
				if k == len(word) {
					found++
				}
			}
		}
	}
	// A palindrome reads the same both ways from both ends.
	if word == reverse(word) {
		found /= 2
	}
	return found
}

// ── Last step ──

// huntFinal is the question the bot whispers: one more show, looked up on
// AniList or MyAnimeList.
func huntFinal(pool []guessShow, intn func(int) int) (huntStep, error) {
	start := intn(3)
	for k := range 3 {
		switch (start + k) % 3 {
		case 0:
			for _, s := range shuffled(pool, func(s guessShow) bool { return s.MalID > 0 && s.Episodes > 0 && s.Year > 0 }, intn) {
				return huntStep{
					Text: "\"" + malURL(s.MalID) + "\n\nHow many episodes, and the year it began? Answer like: 12 2020\"\n\nAnswer it in Otakase.",
					Answers: []string{
						huntNorm(fmt.Sprintf("%d %d", s.Episodes, s.Year)),
					},
					Links: []string{malURL(s.MalID)},
				}, nil
			}
		case 1:
			for _, s := range shuffled(pool, func(s guessShow) bool { _, ok := seasonOrder[s.Season]; return ok && s.Year > 0 }, intn) {
				return huntStep{
					Text:    "\"" + anilistURL(s.ID) + "\n\nWhich season did it begin in? Answer like: spring 2014\"\n\nAnswer it in Otakase.",
					Answers: []string{huntNorm(fmt.Sprintf("%s %d", s.Season, s.Year))},
					Links:   []string{anilistURL(s.ID)},
				}, nil
			}
		default:
			for _, s := range shuffled(pool, func(s guessShow) bool { return huntNorm(s.Studio) != "" }, intn) {
				return huntStep{
					Text:    "\"" + anilistURL(s.ID) + "\n\nWho made it? Name its main studio.\"\n\nAnswer it in Otakase.",
					Answers: studioAnswers(s.Studio),
					Links:   []string{anilistURL(s.ID)},
				}, nil
			}
		}
	}
	return huntStep{}, errHuntPool
}
