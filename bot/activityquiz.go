package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Question games and the letter Wordle. A quiz is ten multiple-choice
// questions made from the show pool (which studio, what year, who is a
// main character…); two truths, one lie is five shows with three facts each,
// one of them false. Played alone they are boards like the other puzzles;
// in a party everyone answers the same question at once (activitysocial.go).

const (
	quizLen    = 10
	quizNeed   = 7
	truthsLen  = 5
	truthsNeed = 4
)

// quizQ is one question. Answer is the index of the right option.
type quizQ struct {
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
	Cover   int      `json:"cover,omitempty"` // a show's cover to show with it
	Answer  int      `json:"-"`
	Explain string   `json:"-"`
}

// pickN picks n different items from s.
func pickN[T any](s []T, n int, intn func(int) int) []T {
	c := slices.Clone(s)
	shuffle(c, intn)
	return c[:min(n, len(c))]
}

// options mixes the right answer among the wrong ones and returns the
// question with the right index.
func quizOptions(q quizQ, right string, wrong []string, intn func(int) int) quizQ {
	q.Options = append([]string{right}, wrong...)
	shuffle(q.Options, intn)
	q.Answer = slices.Index(q.Options, right)
	return q
}

// otherValues are up to n values of key across the pool, unlike skip.
func otherValues(pool []guessShow, key func(guessShow) string, skip []string, n int, intn func(int) int) []string {
	seen := map[string]bool{}
	for _, s := range skip {
		seen[s] = true
	}
	var all []string
	for _, s := range pool {
		if v := key(s); v != "" && !seen[v] && !pzAdultWord.MatchString(v) {
			seen[v] = true
			all = append(all, v)
		}
	}
	return pickN(all, n, intn)
}

// distinctShows are n shows whose key all differ.
func distinctShows(pool []guessShow, key func(guessShow) string, n int, intn func(int) int) []guessShow {
	idx := intn(len(pool))
	seen := map[string]bool{}
	var out []guessShow
	for k := range pool {
		s := pool[(idx+k*7919)%len(pool)]
		if v := key(s); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, s)
			if len(out) == n {
				break
			}
		}
	}
	return out
}

var quizEpisodes = []int{1, 6, 10, 11, 12, 13, 24, 25, 26, 37, 48, 50, 51, 64, 75}

// quizMakers each try to make one kind of question about s.
var quizMakers = []func(pool []guessShow, s guessShow, intn func(int) int) (quizQ, bool){
	// Which studio made it?
	func(pool []guessShow, s guessShow, intn func(int) int) (quizQ, bool) {
		if s.Studio == "" {
			return quizQ{}, false
		}
		wrong := otherValues(pool, func(x guessShow) string { return x.Studio }, []string{s.Studio}, 3, intn)
		if len(wrong) < 3 {
			return quizQ{}, false
		}
		return quizOptions(quizQ{Prompt: "Which studio made " + s.Name + "?", Cover: s.ID}, s.Studio, wrong, intn), true
	},
	// What year did it first air?
	func(pool []guessShow, s guessShow, intn func(int) int) (quizQ, bool) {
		if s.Year == 0 {
			return quizQ{}, false
		}
		years := map[int]bool{s.Year: true}
		for len(years) < 4 {
			years[s.Year+intn(13)-6] = true
		}
		var opts []int
		for y := range years {
			opts = append(opts, y)
		}
		sort.Ints(opts)
		q := quizQ{Prompt: "When did " + s.Name + " first air?", Cover: s.ID}
		for i, y := range opts {
			q.Options = append(q.Options, fmt.Sprint(y))
			if y == s.Year {
				q.Answer = i
			}
		}
		return q, true
	},
	// Who is a main character?
	func(pool []guessShow, s guessShow, intn func(int) int) (quizQ, bool) {
		var lead string
		for _, l := range s.Leads {
			if !strings.Contains(normWords(s.Name), normWords(l)) {
				lead = l
				break
			}
		}
		if lead == "" {
			return quizQ{}, false
		}
		wrong := otherValues(pool, func(x guessShow) string {
			if x.ID == s.ID || len(x.Leads) == 0 {
				return ""
			}
			return x.Leads[0]
		}, s.Leads, 3, intn)
		if len(wrong) < 3 {
			return quizQ{}, false
		}
		return quizOptions(quizQ{Prompt: "Who is a main character in " + s.Name + "?", Cover: s.ID}, lead, wrong, intn), true
	},
	// How many episodes?
	func(pool []guessShow, s guessShow, intn func(int) int) (quizQ, bool) {
		if s.Format != "TV" || s.Episodes < 2 {
			return quizQ{}, false
		}
		var wrong []string
		for _, n := range pickN(quizEpisodes, len(quizEpisodes), intn) {
			if n != s.Episodes && len(wrong) < 3 && n > 1 {
				wrong = append(wrong, fmt.Sprint(n))
			}
		}
		q := quizOptions(quizQ{Prompt: "How many episodes does " + s.Name + " have?", Cover: s.ID}, fmt.Sprint(s.Episodes), wrong, intn)
		return q, true
	},
	// Which genre isn't its?
	func(pool []guessShow, s guessShow, intn func(int) int) (quizQ, bool) {
		var its []string
		for _, g := range s.Genres {
			if !pzAdultWord.MatchString(g) && !slices.Contains(its, g) {
				its = append(its, g)
			}
		}
		if len(its) < 3 {
			return quizQ{}, false
		}
		odd := otherValues(pool, func(x guessShow) string {
			if len(x.Genres) == 0 {
				return ""
			}
			return x.Genres[intn(len(x.Genres))]
		}, s.Genres, 1, intn)
		if len(odd) == 0 {
			return quizQ{}, false
		}
		q := quizOptions(quizQ{Prompt: "Which genre is NOT one of " + s.Name + "'s?", Cover: s.ID}, odd[0], pickN(its, 3, intn), intn)
		q.Explain = "Its genres: " + strings.Join(its, ", ")
		return q, true
	},
}

// quizAbout are questions over several shows, without one cover.
var quizAbout = []func(pool []guessShow, intn func(int) int) (quizQ, bool){
	// Which aired first?
	func(pool []guessShow, intn func(int) int) (quizQ, bool) {
		shows := distinctShows(pool, func(x guessShow) string {
			if x.Year == 0 {
				return ""
			}
			return fmt.Sprint(x.Year)
		}, 4, intn)
		if len(shows) < 4 {
			return quizQ{}, false
		}
		first := shows[0]
		var names, wrong, years []string
		for _, x := range shows {
			if x.Year < first.Year {
				first = x
			}
		}
		for _, x := range shows {
			names = append(names, x.Name)
			years = append(years, fmt.Sprint(x.Name, " ", x.Year))
			if x.ID != first.ID {
				wrong = append(wrong, x.Name)
			}
		}
		q := quizOptions(quizQ{Prompt: "Which of these aired first?"}, first.Name, wrong, intn)
		q.Explain = strings.Join(years, " · ")
		return q, true
	},
	// Which is based on a light novel (or a manga, an original…)?
	func(pool []guessShow, intn func(int) int) (quizQ, bool) {
		srcs := []string{"LIGHT_NOVEL", "ORIGINAL", "VIDEO_GAME", "WEB_NOVEL", "MANGA"}
		src := srcs[intn(len(srcs))]
		var yes, no []guessShow
		for _, x := range pool {
			switch {
			case x.Source == src:
				yes = append(yes, x)
			case x.Source != "" && dailySources[x.Source] != "":
				no = append(no, x)
			}
		}
		if len(yes) == 0 || len(no) < 3 {
			return quizQ{}, false
		}
		right := yes[intn(len(yes))]
		var wrong []string
		for _, x := range pickN(no, 3, intn) {
			wrong = append(wrong, x.Name)
		}
		prompt := "Which of these is based on a " + strings.ToLower(dailySources[src]) + "?"
		if src == "ORIGINAL" {
			prompt = "Which of these is an original story, not adapted from anything?"
		}
		return quizOptions(quizQ{Prompt: prompt}, right.Name, wrong, intn), true
	},
	// Which did this studio make?
	func(pool []guessShow, intn func(int) int) (quizQ, bool) {
		s := pool[intn(len(pool))]
		if s.Studio == "" {
			return quizQ{}, false
		}
		var no []guessShow
		for _, x := range pool {
			if x.Studio != "" && x.Studio != s.Studio {
				no = append(no, x)
			}
		}
		var wrong []string
		for _, x := range distinctShows(no, func(x guessShow) string { return x.Studio }, 3, intn) {
			wrong = append(wrong, x.Name)
		}
		if len(wrong) < 3 {
			return quizQ{}, false
		}
		return quizOptions(quizQ{Prompt: "Which of these did " + s.Studio + " make?"}, s.Name, wrong, intn), true
	},
	// Which scores highest?
	func(pool []guessShow, intn func(int) int) (quizQ, bool) {
		shows := distinctShows(pool, func(x guessShow) string {
			if x.Score == 0 {
				return ""
			}
			return fmt.Sprint(x.Score / 3)
		}, 4, intn)
		if len(shows) < 4 {
			return quizQ{}, false
		}
		best := shows[0]
		for _, x := range shows {
			if x.Score > best.Score {
				best = x
			}
		}
		var wrong, scores []string
		for _, x := range shows {
			scores = append(scores, fmt.Sprintf("%s %d%%", x.Name, x.Score))
			if x.ID != best.ID {
				wrong = append(wrong, x.Name)
			}
		}
		q := quizOptions(quizQ{Prompt: "Which of these has the highest AniList score?"}, best.Name, wrong, intn)
		q.Explain = strings.Join(scores, " · ")
		return q, true
	},
}

// newQuiz makes n questions of mixed kinds about different shows.
func newQuiz(pool []guessShow, n int, intn func(int) int) ([]quizQ, error) {
	if len(pool) < 20 {
		return nil, fmt.Errorf("pool too small")
	}
	var qs []quizQ
	used := map[int]bool{}
	kinds := len(quizMakers) + len(quizAbout)
	for tries := 0; len(qs) < n && tries < n*40; tries++ {
		k := (len(qs) + intn(kinds)) % kinds
		var q quizQ
		var ok bool
		if k < len(quizMakers) {
			s := pool[intn(len(pool))]
			if used[s.ID] {
				continue
			}
			if q, ok = quizMakers[k](pool, s, intn); ok {
				used[s.ID] = true
			}
		} else {
			q, ok = quizAbout[k-len(quizMakers)](pool, intn)
		}
		if ok && len(q.Options) == 4 && q.Answer >= 0 && !slices.ContainsFunc(qs, func(o quizQ) bool { return o.Prompt == q.Prompt }) {
			qs = append(qs, q)
		}
	}
	if len(qs) < n {
		return nil, fmt.Errorf("couldn't make questions")
	}
	return qs, nil
}

// ── Two truths, one lie ──

// truthFact is something true about s and a false twin of it.
type truthFact func(pool []guessShow, s guessShow, intn func(int) int) (truth, lie string, ok bool)

var truthFacts = []truthFact{
	func(pool []guessShow, s guessShow, intn func(int) int) (string, string, bool) {
		if s.Year == 0 {
			return "", "", false
		}
		off := 1 + intn(4)
		if intn(2) == 0 {
			off = -off
		}
		return fmt.Sprint("It first aired in ", s.Year), fmt.Sprint("It first aired in ", s.Year+off), true
	},
	func(pool []guessShow, s guessShow, intn func(int) int) (string, string, bool) {
		other := otherValues(pool, func(x guessShow) string { return x.Studio }, []string{s.Studio}, 1, intn)
		if s.Studio == "" || len(other) == 0 {
			return "", "", false
		}
		return "It was made by " + s.Studio, "It was made by " + other[0], true
	},
	func(pool []guessShow, s guessShow, intn func(int) int) (string, string, bool) {
		say := func(src string) string {
			if src == "ORIGINAL" {
				return "It's an original story, not adapted"
			}
			return "It's based on a " + strings.ToLower(dailySources[src])
		}
		if dailySources[s.Source] == "" {
			return "", "", false
		}
		var others []string
		for _, x := range []string{"MANGA", "LIGHT_NOVEL", "ORIGINAL", "VIDEO_GAME", "WEB_NOVEL"} {
			if x != s.Source {
				others = append(others, x)
			}
		}
		return say(s.Source), say(others[intn(len(others))]), true
	},
	func(pool []guessShow, s guessShow, intn func(int) int) (string, string, bool) {
		if s.Format != "TV" || s.Episodes < 2 {
			return "", "", false
		}
		n := quizEpisodes[intn(len(quizEpisodes))]
		for n == s.Episodes || n < 2 {
			n = quizEpisodes[intn(len(quizEpisodes))]
		}
		return fmt.Sprint("It has ", s.Episodes, " episodes"), fmt.Sprint("It has ", n, " episodes"), true
	},
	func(pool []guessShow, s guessShow, intn func(int) int) (string, string, bool) {
		var its []string
		for _, g := range s.Genres {
			if !pzAdultWord.MatchString(g) {
				its = append(its, g)
			}
		}
		odd := otherValues(pool, func(x guessShow) string {
			if len(x.Genres) == 0 {
				return ""
			}
			return x.Genres[0]
		}, s.Genres, 1, intn)
		if len(its) == 0 || len(odd) == 0 {
			return "", "", false
		}
		return "One of its genres is " + its[intn(len(its))], "One of its genres is " + odd[0], true
	},
	func(pool []guessShow, s guessShow, intn func(int) int) (string, string, bool) {
		var lead string
		for _, l := range s.Leads {
			if !strings.Contains(normWords(s.Name), normWords(l)) {
				lead = l
				break
			}
		}
		other := otherValues(pool, func(x guessShow) string {
			if x.ID == s.ID || len(x.Leads) == 0 {
				return ""
			}
			return x.Leads[0]
		}, s.Leads, 1, intn)
		if lead == "" || len(other) == 0 {
			return "", "", false
		}
		return lead + " is a main character", other[0] + " is a main character", true
	},
	func(pool []guessShow, s guessShow, intn func(int) int) (string, string, bool) {
		if s.Score == 0 {
			return "", "", false
		}
		off := 5 + intn(8)
		if s.Score+off > 95 || (s.Score-off >= 40 && intn(2) == 0) {
			off = -off
		}
		return fmt.Sprintf("Its AniList score is %d%%", s.Score), fmt.Sprintf("Its AniList score is %d%%", s.Score+off), true
	},
}

// newTruths makes n shows with two truths and a lie each.
func newTruths(pool []guessShow, n int, intn func(int) int) ([]quizQ, error) {
	var qs []quizQ
	used := map[int]bool{}
	for tries := 0; len(qs) < n && tries < n*40; tries++ {
		s := pool[intn(len(pool))]
		if used[s.ID] {
			continue
		}
		type fact struct{ truth, lie string }
		var facts []fact
		for _, k := range pickN([]int{0, 1, 2, 3, 4, 5, 6}, 7, intn) {
			if t, l, ok := truthFacts[k](pool, s, intn); ok {
				facts = append(facts, fact{t, l})
			}
			if len(facts) == 3 {
				break
			}
		}
		if len(facts) < 3 {
			continue
		}
		used[s.ID] = true
		lie := intn(3)
		q := quizQ{Prompt: "Which one is the lie about " + s.Name + "?", Cover: s.ID, Answer: lie, Explain: "The truth: " + facts[lie].truth}
		for i, f := range facts {
			if i == lie {
				q.Options = append(q.Options, f.lie)
			} else {
				q.Options = append(q.Options, f.truth)
			}
		}
		qs = append(qs, q)
	}
	if len(qs) < n {
		return nil, fmt.Errorf("couldn't make facts")
	}
	return qs, nil
}

// ── A set of questions played alone ──

type qset struct {
	game  string
	qs    []quizQ
	picks []int
	right int
	need  int
	done  bool
	won   bool
}

func (q *qset) clone() actPuzzle {
	n := *q
	n.picks = slices.Clone(q.picks)
	return &n
}

func (q *qset) result() (bool, bool) { return q.done, q.won }

func (q *qset) move(m pzMove) error {
	if q.done {
		return nil
	}
	if m.GiveUp {
		q.done = true
		return nil
	}
	cur := q.qs[len(q.picks)]
	if m.Cell < 0 || m.Cell >= len(cur.Options) {
		return errPz("pick an answer")
	}
	q.picks = append(q.picks, m.Cell)
	if m.Cell == cur.Answer {
		q.right++
	}
	if len(q.picks) == len(q.qs) {
		q.done, q.won = true, q.right >= q.need
	}
	return nil
}

// quizLast is a question with its answer shown.
type quizLast struct {
	quizQ
	Answer  int    `json:"answer"`
	Pick    int    `json:"pick"`
	Explain string `json:"explain,omitempty"`
}

type qsetView struct {
	Game   string     `json:"game"`
	Index  int        `json:"index"` // questions answered
	Total  int        `json:"total"`
	Right  int        `json:"right"`
	Need   int        `json:"need"`
	Q      *quizQ     `json:"q,omitempty"`
	Last   *quizLast  `json:"last,omitempty"`
	Review []quizLast `json:"review,omitempty"` // every question, once done
	Done   bool       `json:"done"`
	Won    bool       `json:"won"`
}

func (q *qset) view() any {
	v := qsetView{Game: q.game, Index: len(q.picks), Total: len(q.qs), Right: q.right, Need: q.need, Done: q.done, Won: q.won}
	shown := func(i int) quizLast {
		p := -1
		if i < len(q.picks) {
			p = q.picks[i]
		}
		return quizLast{quizQ: q.qs[i], Answer: q.qs[i].Answer, Pick: p, Explain: q.qs[i].Explain}
	}
	if n := len(q.picks); n > 0 {
		l := shown(n - 1)
		v.Last = &l
	}
	if q.done {
		for i := range q.qs {
			v.Review = append(v.Review, shown(i))
		}
	} else {
		c := q.qs[len(q.picks)]
		v.Q = &c
	}
	return v
}

// ── Letter Wordle ──

const wordleTries = 6

var wordleSkip = map[string]bool{"season": true, "movie": true, "final": true, "story": true, "about": true, "their": true,
	"there": true, "these": true, "where": true, "which": true, "other": true, "after": true, "under": true, "with": true,
	"from": true, "into": true, "part": true, "that": true, "this": true, "what": true, "your": true, "when": true}

type wordleRow struct {
	Word  string   `json:"word"`
	Marks []string `json:"marks"` // yes, near or no per letter
}

type wordle struct {
	mode string // title or character
	word string // lower-case a–z
	ans  guessShow
	rows []wordleRow
	done bool
	won  bool
}

// wordleWords are the words of s fit for a Wordle: 4 to 7 plain letters.
func wordleWords(text string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(accents.Replace(text)), func(r rune) bool { return r < 'a' || r > 'z' }) {
		if len(w) >= 4 && len(w) <= 7 && !wordleSkip[w] {
			out = append(out, w)
		}
	}
	return out
}

func newWordle(pool []guessShow, mode string, intn func(int) int) (*wordle, error) {
	if mode != "character" {
		mode = "title"
	}
	type pickT struct {
		word string
		s    guessShow
	}
	var choices []pickT
	for _, s := range pool {
		if mode == "title" {
			// Only names written in English letters, so the word is the
			// one people know.
			if hangmanFold(s.Name) == "" {
				continue
			}
			for _, w := range wordleWords(s.Name) {
				choices = append(choices, pickT{w, s})
			}
			continue
		}
		for _, l := range s.Leads {
			if strings.Contains(normWords(s.Name), normWords(l)) {
				continue
			}
			if ws := wordleWords(l); len(ws) > 0 {
				choices = append(choices, pickT{ws[0], s})
			}
		}
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("no words")
	}
	p := choices[intn(len(choices))]
	return &wordle{mode: mode, word: p.word, ans: p.s}, nil
}

// wordleMarks colours a guess: right place, in the word, or not in it.
func wordleMarks(guess, word string) []string {
	marks := make([]string, len(guess))
	left := map[byte]int{}
	for i := range word {
		if guess[i] == word[i] {
			marks[i] = "yes"
		} else {
			left[word[i]]++
		}
	}
	for i := range guess {
		if marks[i] != "" {
			continue
		}
		if left[guess[i]] > 0 {
			marks[i] = "near"
			left[guess[i]]--
		} else {
			marks[i] = "no"
		}
	}
	return marks
}

func (w *wordle) clone() actPuzzle {
	n := *w
	n.rows = slices.Clone(w.rows)
	return &n
}

func (w *wordle) result() (bool, bool) { return w.done, w.won }

func (w *wordle) move(m pzMove) error {
	if w.done {
		return nil
	}
	if m.GiveUp {
		w.done = true
		return nil
	}
	g := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(accents.Replace(m.Text)), " ", ""))
	if len(g) != len(w.word) || strings.Trim(g, "abcdefghijklmnopqrstuvwxyz") != "" {
		return errPz(fmt.Sprintf("Use %d letters", len(w.word)))
	}
	for _, r := range w.rows {
		if r.Word == g {
			return errPz("Already tried that")
		}
	}
	w.rows = append(w.rows, wordleRow{Word: g, Marks: wordleMarks(g, w.word)})
	w.won = g == w.word
	w.done = w.won || len(w.rows) >= wordleTries
	return nil
}

type wordleView struct {
	Mode   string            `json:"mode"`
	Len    int               `json:"len"`
	Max    int               `json:"max"`
	Rows   []wordleRow       `json:"rows"`
	Keys   map[string]string `json:"keys"` // best colour per letter tried
	Hint   string            `json:"hint"`
	Done   bool              `json:"done"`
	Won    bool              `json:"won"`
	Word   string            `json:"word,omitempty"`
	Answer *activityHit      `json:"answer,omitempty"`
}

func (w *wordle) view() any {
	v := wordleView{Mode: w.mode, Len: len(w.word), Max: wordleTries, Rows: append([]wordleRow{}, w.rows...), Keys: map[string]string{},
		Done: w.done, Won: w.won}
	rank := map[string]int{"no": 1, "near": 2, "yes": 3}
	for _, r := range w.rows {
		for i, m := range r.Marks {
			l := string(r.Word[i])
			if rank[m] > rank[v.Keys[l]] {
				v.Keys[l] = m
			}
		}
	}
	v.Hint = "A word from an anime title"
	if w.mode == "character" {
		v.Hint = "A main character's name"
	}
	if len(w.rows) >= 3 || w.done {
		if w.mode == "character" {
			v.Hint += " · from " + w.ans.Name
		} else {
			var bits []string
			if f := dailyFormats[w.ans.Format]; f != "" {
				bits = append(bits, f)
			}
			if w.ans.Year > 0 {
				bits = append(bits, fmt.Sprint(w.ans.Year))
			}
			if len(w.ans.Genres) > 0 {
				bits = append(bits, strings.Join(w.ans.Genres[:min(2, len(w.ans.Genres))], ", "))
			}
			v.Hint += " · " + strings.Join(bits, " · ")
		}
	}
	if w.done {
		v.Word = w.word
		v.Answer = &activityHit{ID: w.ans.ID, Name: w.ans.Name, URL: w.ans.URL}
	}
	return v
}
