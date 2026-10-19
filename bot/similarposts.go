package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
)

// A new support post gets links to earlier posts that look like the same
// problem, solved ones first. Posts are compared by meaning with Ollama when
// it runs, else by the words they share, which only counts clear overlaps.

const (
	similarKeep   = 300  // support posts remembered
	similarShown  = 3    // links shown under a new post
	similarCosine = 0.75 // embedding similarity that counts as the same problem
	// Without embeddings two posts must share this many keywords, and this
	// much of their keywords overall.
	similarWords     = 4
	similarWordScore = 0.5
	similarEvery     = 30 * time.Minute
	similarTextLen   = 500
)

// supportPost is one support post in the similar posts index.
type supportPost struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Text     string    `json:"text,omitempty"` // the first message, shortened
	Vec      packedVec `json:"vec,omitempty"`
	Solved   bool      `json:"solved,omitempty"`
	SolvedAt time.Time `json:"solved_at,omitzero"`
}

// packedVec is an embedding scaled to bytes, which keeps the store small.
// Cosine similarity doesn't depend on the scale, so little is lost.
type packedVec []int8

func packVec(v []float64) packedVec {
	var top float64
	for _, x := range v {
		top = max(top, math.Abs(x))
	}
	if top == 0 {
		return nil
	}
	out := make(packedVec, len(v))
	for k, x := range v {
		out[k] = int8(math.Round(x / top * 127))
	}
	return out
}

func (p packedVec) floats() []float64 {
	out := make([]float64, len(p))
	for k, x := range p {
		out[k] = float64(x)
	}
	return out
}

func (p packedVec) MarshalJSON() ([]byte, error) {
	raw := make([]byte, len(p))
	for k, x := range p {
		raw[k] = byte(x)
	}
	return json.Marshal(base64.StdEncoding.EncodeToString(raw))
}

func (p *packedVec) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	*p = make(packedVec, len(raw))
	for k, x := range raw {
		(*p)[k] = int8(x)
	}
	return nil
}

// similarStop are words too common in support posts to say what they're about.
var similarStop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`the and for are but not you your yours with this that these those from have has had
		was were will would can could should does doesnt did didnt dont cant isnt wont arent wasnt havent ive its
		what when where which who why how there here then than them they their just also any some
		all get got getting into out about after before again still even only very really much more most
		one two any anyone anybody someone something anything nothing everything please thanks thank hello
		help helps need needs want wants try tried trying use used using work works working worked
		otakase issue issues problem problems error errors bug bugs same like know getting other way
		hey hi pls plz since because while same now today yesterday time times`) {
		similarStop[w] = true
	}
}

// keywords are the words of text that say what it's about.
func keywords(text string) map[string]bool {
	out := map[string]bool{}
	text = strings.NewReplacer("'", "", "’", "").Replace(strings.ToLower(text))
	for _, w := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(w) < 3 || similarStop[w] {
			continue
		}
		if len(w) > 4 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
			w = w[:len(w)-1]
		}
		out[w] = true
	}
	return out
}

// wordOverlap is how much two keyword sets share, 0 to 1, and how many
// words they share.
func wordOverlap(a, b map[string]bool) (float64, int) {
	if len(a) == 0 || len(b) == 0 {
		return 0, 0
	}
	shared := 0
	for w := range a {
		if b[w] {
			shared++
		}
	}
	return float64(shared) / math.Sqrt(float64(len(a))*float64(len(b))), shared
}

func similarText(title, text string) string {
	return strings.TrimSpace(title + ". " + clip(strings.TrimSpace(text), similarTextLen))
}

// findSimilar returns up to three earlier posts that look like the same
// problem, solved ones first, then the closest. vec may be nil.
func findSimilar(posts []supportPost, selfID string, vec []float64, title, text string) []supportPost {
	words := keywords(title + " " + text)
	type hit struct {
		p     supportPost
		score float64
	}
	var hits []hit
	for _, p := range posts {
		if p.ID == selfID {
			continue
		}
		if vec != nil && len(p.Vec) > 0 {
			if s := cosine(vec, p.Vec.floats()); s >= similarCosine {
				hits = append(hits, hit{p, s})
			}
			continue
		}
		if s, n := wordOverlap(words, keywords(p.Title+" "+p.Text)); n >= similarWords && s >= similarWordScore {
			hits = append(hits, hit{p, s})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].p.Solved != hits[b].p.Solved {
			return hits[a].p.Solved
		}
		return hits[a].score > hits[b].score
	})
	var out []supportPost
	for k := 0; k < len(hits) && k < similarShown; k++ {
		out = append(out, hits[k].p)
	}
	return out
}

func similarEmbed(posts []supportPost) *discordgo.MessageEmbed {
	lines := []string{"These posts look similar:"}
	for _, p := range posts {
		line := "- " + postLink(p.ID)
		if p.Solved {
			line += " · solved"
		}
		lines = append(lines, line)
	}
	return &discordgo.MessageEmbed{Description: strings.Join(lines, "\n"), Color: shu,
		Footer: &discordgo.MessageEmbedFooter{Text: "The answer might already be there."}}
}

// snowflakeNewer reports whether Discord id a was made after b.
func snowflakeNewer(a, b string) bool {
	x, _ := strconv.ParseUint(a, 10, 64)
	y, _ := strconv.ParseUint(b, 10, 64)
	return x > y
}

// addSupportPost puts p in the index, replacing an older copy, and keeps
// the newest posts.
func addSupportPost(posts []supportPost, p supportPost) []supportPost {
	out := posts[:0:0]
	for _, q := range posts {
		if q.ID != p.ID {
			out = append(out, q)
		}
	}
	out = append(out, p)
	sort.SliceStable(out, func(a, b int) bool { return snowflakeNewer(out[a].ID, out[b].ID) })
	if len(out) > similarKeep {
		out = out[:similarKeep]
	}
	return out
}

// canEmbed reports whether Ollama is up for embeddings.
func (sm *semantic) canEmbed() bool {
	if sm == nil || sm.url == "" {
		return false
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.ready
}

func (sm *semantic) embedOne(text string) []float64 {
	if !sm.canEmbed() {
		return nil
	}
	v, err := sm.embed([]string{text})
	if err != nil {
		log.Printf("similar posts: %v", err)
		return nil
	}
	return v[0]
}

// onSupportPostSimilar links a new support post to earlier ones like it and
// adds it to the index.
func (b *bot) onSupportPostSimilar(s *discordgo.Session, t *discordgo.ThreadCreate) {
	if !t.NewlyCreated || t.ParentID != b.cfg.SupportForum || b.cfg.SupportForum == "" || t.OwnerID == s.State.User.ID {
		return
	}
	// After the greeting, which waits two seconds for the first message.
	time.Sleep(4 * time.Second)
	text := ""
	// A forum post's first message has the same id as the post.
	if m, err := s.ChannelMessage(t.ID, t.ID); err == nil {
		text = m.Content
	}
	vec := b.sem.embedOne(similarText(t.Name, text))
	var posts []supportPost
	b.store.view(func(d *storeData) { posts = append(posts, d.SupportPosts...) })
	if hits := findSimilar(posts, t.ID, vec, t.Name, text); len(hits) > 0 {
		if _, err := s.ChannelMessageSendComplex(t.ID, &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{similarEmbed(hits)}, AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Printf("similar posts: %v", err)
		}
	}
	p := supportPost{ID: t.ID, Title: t.Name, Text: clip(text, similarTextLen), Vec: packVec(vec)}
	b.store.update(func(d *storeData) { d.SupportPosts = addSupportPost(d.SupportPosts, p) })
}

// noteSolved marks a post solved in the index.
func (b *bot) noteSolved(threadID string) {
	b.store.update(func(d *storeData) {
		for k := range d.SupportPosts {
			if d.SupportPosts[k].ID == threadID {
				d.SupportPosts[k].Solved, d.SupportPosts[k].SolvedAt = true, time.Now()
			}
		}
	})
}

// supportThreads lists the support forum's posts: the open ones and up to
// pages×100 recently archived ones, newest first. Posts the bot made are
// left out.
func (b *bot) supportThreads(pages int) ([]*discordgo.Channel, error) {
	active, err := b.s.GuildThreadsActive(b.cfg.GuildID)
	if err != nil {
		return nil, err
	}
	var out []*discordgo.Channel
	seen := map[string]bool{}
	add := func(th *discordgo.Channel) {
		if th.ParentID == b.cfg.SupportForum && th.OwnerID != b.s.State.User.ID && !seen[th.ID] {
			seen[th.ID] = true
			out = append(out, th)
		}
	}
	for _, th := range active.Threads {
		add(th)
	}
	var before *time.Time
	for page := 0; page < pages; page++ {
		list, err := b.s.ThreadsArchived(b.cfg.SupportForum, before, 100)
		if err != nil {
			return nil, err
		}
		for _, th := range list.Threads {
			add(th)
			if th.ThreadMetadata != nil {
				at := th.ThreadMetadata.ArchiveTimestamp
				before = &at
			}
		}
		if !list.HasMore || len(list.Threads) == 0 || before == nil {
			break
		}
	}
	sort.SliceStable(out, func(a, c int) bool { return snowflakeNewer(out[a].ID, out[c].ID) })
	return out, nil
}

func (b *bot) similarLoop() {
	if b.cfg.SupportForum == "" {
		return
	}
	time.Sleep(2 * time.Minute) // after startup, gently
	for {
		if err := b.syncSimilarIndex(); err != nil {
			b.jobFailed("Similar posts index", err)
		} else {
			b.jobOK("Similar posts index")
		}
		time.Sleep(similarEvery)
	}
}

// syncSimilarIndex fills the index from the forum (first message of posts
// it hasn't seen, one per second), updates solved flags and embeds posts
// that were added while Ollama was off.
func (b *bot) syncSimilarIndex() error {
	forum, err := b.s.Channel(b.cfg.SupportForum)
	if err != nil {
		return err
	}
	solvedTag := supportSolvedTag(forum)
	threads, err := b.supportThreads(3)
	if err != nil {
		return err
	}
	if len(threads) > similarKeep {
		threads = threads[:similarKeep]
	}
	known := map[string]bool{}
	b.store.update(func(d *storeData) {
		byID := map[string]*discordgo.Channel{}
		for _, th := range threads {
			byID[th.ID] = th
		}
		for k := range d.SupportPosts {
			p := &d.SupportPosts[k]
			known[p.ID] = true
			th := byID[p.ID]
			if th == nil || solvedTag == "" {
				continue
			}
			solved := contains(th.AppliedTags, solvedTag)
			if solved && !p.Solved {
				p.SolvedAt = time.Now()
				if th.ThreadMetadata != nil && th.ThreadMetadata.Archived {
					p.SolvedAt = th.ThreadMetadata.ArchiveTimestamp
				}
			}
			p.Solved = solved
		}
	})

	var batch []supportPost
	flush := func() {
		if len(batch) == 0 {
			return
		}
		b.store.update(func(d *storeData) {
			for _, p := range batch {
				d.SupportPosts = addSupportPost(d.SupportPosts, p)
			}
		})
		batch = nil
	}
	for _, th := range threads {
		if known[th.ID] {
			continue
		}
		text := ""
		if m, err := b.s.ChannelMessage(th.ID, th.ID); err == nil {
			text = m.Content
		}
		p := supportPost{ID: th.ID, Title: th.Name, Text: clip(text, similarTextLen)}
		if solvedTag != "" && contains(th.AppliedTags, solvedTag) {
			p.Solved = true
			if th.ThreadMetadata != nil {
				p.SolvedAt = th.ThreadMetadata.ArchiveTimestamp
			}
		}
		p.Vec = packVec(b.sem.embedOne(similarText(p.Title, p.Text)))
		batch = append(batch, p)
		if len(batch) == 25 {
			flush()
		}
		time.Sleep(time.Second)
	}
	flush()

	if !b.sem.canEmbed() {
		return nil
	}
	var missing []supportPost
	b.store.view(func(d *storeData) {
		for _, p := range d.SupportPosts {
			if len(p.Vec) == 0 {
				missing = append(missing, p)
			}
		}
	})
	vecs := map[string]packedVec{}
	save := func() {
		if len(vecs) == 0 {
			return
		}
		b.store.update(func(d *storeData) {
			for k := range d.SupportPosts {
				if v, ok := vecs[d.SupportPosts[k].ID]; ok {
					d.SupportPosts[k].Vec = v
				}
			}
		})
		vecs = map[string]packedVec{}
	}
	for _, p := range missing {
		v := b.sem.embedOne(similarText(p.Title, p.Text))
		if v == nil {
			break // Ollama went away; next round
		}
		vecs[p.ID] = packVec(v)
		if len(vecs) == 25 {
			save()
		}
		time.Sleep(200 * time.Millisecond)
	}
	save()
	return nil
}
