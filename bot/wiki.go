package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
)

// A new support post gets links to the wiki sections that best match its
// title and question. The pages listed under "Using otakase" in the wiki
// sidebar are read from the repo once a day and split at their headings.
// With Ollama the sections are matched by meaning; without it, by the words
// they share with the question.

const wikiRaw = "https://raw.githubusercontent.com/TheXykril/otakase/main/wiki/"

// Below these scores a section isn't worth linking; both were tuned on
// sample support questions against the wiki. The semantic one is lower than
// the FAQ threshold because a question is compared with a section, not with
// another phrasing of the same question. Links after the first need
// wikiRunnerUp of the best score, so a clear answer comes alone.
const (
	wikiSemanticThreshold = 0.5
	wikiKeywordThreshold  = 0.7
	wikiRunnerUp          = 0.85
	wikiMaxLinks          = 3
)

type wikiSection struct {
	Page, PageTitle, Heading, Anchor string
	Text                             string // plain text, used for matching and the snippet

	terms map[string]bool
	head  map[string]bool // words in the page title and heading
	words int             // length, so long sections don't win on volume
	vecs  [][]float64     // one per embedTexts entry
}

func (s wikiSection) url() string {
	u := wikiURL + "/" + s.Page
	if s.Anchor != "" {
		u += "#" + s.Anchor
	}
	return u
}

func (s wikiSection) label() string {
	if s.Heading == "" {
		return s.PageTitle
	}
	return s.PageTitle + " › " + s.Heading
}

// embedTexts are what gets embedded: the headings alone say most about a
// section, and the start of its text covers what they leave out.
func (s wikiSection) embedTexts() []string {
	return []string{s.label(), clip(s.label()+". "+s.Text, 600)}
}

type wikiIndex struct {
	mu       sync.Mutex
	sections []wikiSection
	idf      map[string]float64
	avgWords float64
	cache    map[string][]float64 // embedded text → vector, kept across refreshes
}

// wikiLoop reads the wiki now and then once a day. Sections without vectors
// yet (Ollama not up) are retried hourly.
func (b *bot) wikiLoop() {
	for {
		wait := 24 * time.Hour
		pages, err := fetchWiki(httpClient, wikiRaw)
		if err != nil {
			log.Printf("wiki: %v", err)
			wait = time.Hour
		} else if !b.wiki.update(pages, b.sem) {
			wait = time.Hour
		}
		time.Sleep(wait)
	}
}

// update rebuilds the index from pages (name → markdown) and embeds the
// sections when Ollama is set up. It reports whether every section has its
// vectors, or Ollama isn't used at all.
func (w *wikiIndex) update(pages map[string]string, sm *semantic) bool {
	var secs []wikiSection
	names := make([]string, 0, len(pages))
	for name := range pages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		secs = append(secs, splitWiki(name, pages[name])...)
	}
	df := map[string]int{}
	words := 0
	for i := range secs {
		secs[i].terms = termSet(secs[i].label() + " " + secs[i].Text)
		secs[i].head = termSet(secs[i].label())
		secs[i].words = len(strings.Fields(secs[i].Text))
		words += secs[i].words
		for t := range secs[i].terms {
			df[t]++
		}
	}
	idf := map[string]float64{}
	for t, n := range df {
		idf[t] = math.Log(1 + float64(len(secs))/float64(n))
	}

	w.mu.Lock()
	cache := w.cache
	w.mu.Unlock()
	complete := true
	if sm != nil && sm.url != "" {
		next := map[string][]float64{}
		for i := range secs {
			for _, text := range secs[i].embedTexts() {
				v, ok := cache[text]
				if !ok && complete {
					// One at a time, like the FAQ examples.
					if e, err := sm.embed([]string{text}); err == nil {
						v, ok = e[0], true
					} else {
						log.Printf("wiki embed: %v", err)
						complete = false
					}
				}
				if !ok {
					complete = false
					break
				}
				next[text] = v
				secs[i].vecs = append(secs[i].vecs, v)
			}
		}
		cache = next
	}
	w.mu.Lock()
	w.sections, w.idf, w.cache = secs, idf, cache
	w.avgWords = float64(words) / float64(max(len(secs), 1))
	w.mu.Unlock()
	log.Printf("wiki: %d sections from %d pages", len(secs), len(pages))
	return complete
}

type wikiHit struct {
	wikiSection
	score float64
}

// match returns up to wikiMaxLinks sections for a question, best first, at
// most one per page. With Ollama it matches by meaning, otherwise by words.
func (w *wikiIndex) match(question string, sm *semantic) []wikiHit {
	w.mu.Lock()
	secs, idf, avg := w.sections, w.idf, w.avgWords
	w.mu.Unlock()
	if len(secs) == 0 || len(strings.TrimSpace(question)) < 12 {
		return nil
	}
	var hits []wikiHit
	threshold := wikiKeywordThreshold
	if q := semanticVec(question, secs, sm); q != nil {
		threshold = wikiSemanticThreshold
		for _, s := range secs {
			best := -1.0
			for _, v := range s.vecs {
				best = max(best, cosine(q, v))
			}
			hits = append(hits, wikiHit{s, best})
		}
	} else {
		q := termSet(question)
		for _, s := range secs {
			hits = append(hits, wikiHit{s, keywordScore(q, s, idf, avg)})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	var out []wikiHit
	seen := map[string]bool{}
	for _, h := range hits {
		if h.score < threshold || len(out) == wikiMaxLinks {
			break
		}
		if len(out) > 0 && h.score < out[0].score*wikiRunnerUp {
			break
		}
		if !seen[h.Page] {
			seen[h.Page] = true
			out = append(out, h)
		}
	}
	return out
}

// semanticVec embeds the question when every section has its vectors.
func semanticVec(question string, secs []wikiSection, sm *semantic) []float64 {
	if sm == nil || sm.url == "" {
		return nil
	}
	for _, s := range secs {
		if len(s.vecs) == 0 {
			return nil
		}
	}
	v, err := sm.embed([]string{clip(question, 600)})
	if err != nil {
		log.Printf("wiki match: %v", err)
		return nil
	}
	return v[0]
}

// keywordScore is the share of the question's word weight found in the
// section, words in the headings counting double and the text's words
// counting less the longer the section is. It needs two words in common, so
// one shared word like "episode" isn't enough.
func keywordScore(q map[string]bool, s wikiSection, idf map[string]float64, avgWords float64) float64 {
	long := 1.0
	if avgWords > 0 {
		long = math.Sqrt(max(float64(s.words)/avgWords, 1))
	}
	var total, found float64
	matched := 0
	for t := range q {
		wt, ok := idf[t]
		if !ok {
			continue // not in the wiki at all, says nothing either way
		}
		total += wt
		switch {
		case s.head[t]:
			matched++
			found += 2 * wt
		case s.terms[t]:
			matched++
			found += wt / long
		}
	}
	if matched < 2 || total == 0 {
		return 0
	}
	return found / total
}

// wikiEmbed is the "From the wiki" card for a support post, or nil.
func (b *bot) wikiEmbed(title, question string) *discordgo.MessageEmbed {
	hits := b.wiki.match(strings.TrimSpace(title+"\n"+question), b.sem)
	if len(hits) == 0 {
		return nil
	}
	var lines []string
	for _, h := range hits {
		lines = append(lines, fmt.Sprintf("**[%s](%s)**\n%s", h.label(), h.url(), clip(h.Text, 140)))
	}
	return &discordgo.MessageEmbed{
		Title:       "From the wiki",
		Description: strings.Join(lines, "\n\n") + "\n\nIf one of these fixes it, press **Mark solved**.",
		Color:       shu,
	}
}

var sidebarLinkRe = regexp.MustCompile(`\]\(([A-Za-z0-9-]+)\)`)

// sidebarPages lists the pages under "Using otakase" in _Sidebar.md.
func sidebarPages(sidebar string) []string {
	var pages []string
	in := false
	for _, line := range strings.Split(sidebar, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "**") && !strings.HasPrefix(t, "**[") {
			in = strings.Contains(strings.ToLower(t), "using otakase")
			continue
		}
		if m := sidebarLinkRe.FindStringSubmatch(t); in && m != nil {
			pages = append(pages, m[1])
		}
	}
	return pages
}

func fetchWiki(c *http.Client, base string) (map[string]string, error) {
	get := func(name string) (string, error) {
		resp, err := c.Get(base + name + ".md")
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("%s: %s", name, resp.Status)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return string(data), err
	}
	sidebar, err := get("_Sidebar")
	if err != nil {
		return nil, err
	}
	pages := map[string]string{}
	for _, name := range sidebarPages(sidebar) {
		md, err := get(name)
		if err != nil {
			log.Printf("wiki: %v", err)
			continue
		}
		pages[name] = md
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("no pages under \"Using otakase\" in the sidebar")
	}
	return pages, nil
}

var (
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*$`)
	mdLinkRe   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdMarkupRe = regexp.MustCompile("[*_`>|]+")
)

// splitWiki cuts a page into sections at its ## and ### headings. Text
// before the first one belongs to the page itself.
func splitWiki(page, md string) []wikiSection {
	pageTitle := strings.ReplaceAll(page, "-", " ")
	var secs []wikiSection
	cur := wikiSection{Page: page}
	var body []string
	anchors := map[string]int{}
	flush := func() {
		cur.Text = plainText(strings.Join(body, "\n"))
		if cur.Text != "" || cur.Heading != "" {
			secs = append(secs, cur)
		}
		body = nil
	}
	fence := false
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			continue
		}
		m := headingRe.FindStringSubmatch(line)
		if fence || m == nil {
			body = append(body, line)
			continue
		}
		text := plainText(m[2])
		if len(m[1]) == 1 {
			pageTitle = text
			continue
		}
		flush()
		a := wikiAnchor(text)
		if n := anchors[a]; n > 0 {
			anchors[a]++
			a = fmt.Sprintf("%s-%d", a, n)
		} else {
			anchors[a] = 1
		}
		cur = wikiSection{Page: page, Heading: text, Anchor: a}
	}
	flush()
	for i := range secs {
		secs[i].PageTitle = pageTitle
	}
	return secs
}

// plainText drops markdown markup and folds whitespace.
func plainText(md string) string {
	s := mdLinkRe.ReplaceAllString(md, "$1")
	s = mdMarkupRe.ReplaceAllString(s, " ")
	s = strings.NewReplacer("<br>", " ", "---", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// wikiAnchor is GitHub's heading anchor: lower case, spaces to dashes,
// other punctuation dropped.
func wikiAnchor(h string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(h) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			sb.WriteRune(r)
		case r == ' ':
			sb.WriteByte('-')
		}
	}
	return sb.String()
}

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`the and for are but not you your yours can cant could
		does doesnt did didnt dont was were has have had this that these those with from into
		its it's then than there their them they what when where which who why how all any
		some just also get got out about after before again only very too more most such
		will wont would should here been being our off now one two help please anyone thanks
		thank hello issue problem otakase otk work works working every time ask asks`) {
		stopWords[w] = true
	}
}

// termSet is the set of meaningful words in s, lightly stemmed so "devices"
// and "device" or "casting" and "cast" meet.
func termSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(strings.ReplaceAll(s, "'", "")), -1) {
		if len(w) < 3 || stopWords[w] {
			continue
		}
		out[stem(w)] = true
	}
	return out
}

func stem(w string) string {
	for _, suf := range []string{"ing", "ed", "s"} {
		if len(w) > len(suf)+3 && strings.HasSuffix(w, suf) {
			return w[:len(w)-len(suf)]
		}
	}
	return w
}
