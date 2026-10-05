package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// When a local Ollama with a small embedding model is reachable, questions
// that the word patterns miss are matched by meaning against example
// phrasings: "my player won't open" finds the mpv answer. The bot still only
// sends its own fixed answers. Without Ollama nothing changes.

// intentExamples are phrasings per FAQ key, plus "down" for the source check.
var intentExamples = map[string][]string{
	"mpv": {
		"playback never starts", "the player doesn't open", "mpv not found",
		"video won't play", "nothing happens when I pick an episode", "player closes right away",
	},
	"nothing-found": {
		"no results when I search", "it can't find the anime", "search shows nothing",
		"no episodes found for this show", "the show I want isn't there",
	},
	"icons": {
		"menu icons show as boxes", "weird squares instead of icons", "icons look broken in the menu",
	},
	"sync": {
		"my anilist progress didn't update", "episodes aren't tracked on myanimelist",
		"watch progress doesn't sync", "anilist login stopped working",
		"my list didn't update after watching", "finished anime isn't marked completed on my list",
		"watched episodes don't show on my list",
	},
	"cast": {
		"casting can't find my tv", "chromecast doesn't show up", "how do I cast to my tv",
		"kodi isn't detected",
	},
	"install": {
		"how do I install otakase", "how do I update to the new version", "how to install on windows",
		"install on linux", "how do I upgrade",
	},
	"dev-builds": {
		"can I try new features early", "where do I get test builds", "is there a beta version",
	},
	"down": {
		"is otakase down", "are the sources broken right now", "is it down for everyone",
		"nothing works today", "did a provider die", "is the site down",
		"everything is broken today",
	},
}

// questionRe keeps the model to messages that look like questions or
// problem reports, so ordinary chat is never sent to it.
var questionRe = regexp.MustCompile(`(?i)\?|^(how|why|what|where|can|does|do|is|are|anyone|help)\b|\b(not|never|doesn'?t|don'?t|didn'?t|isn'?t|won'?t|can'?t|error|broken|fails?|stuck)\b`)

type semantic struct {
	url, model string
	threshold  float64

	mu       sync.Mutex
	ready    bool
	examples []semanticExample
}

type semanticExample struct {
	intent string
	vec    []float64
}

var ollamaClient = &http.Client{Timeout: 30 * time.Second}

func (sm *semantic) post(path string, body, out any) error {
	data, _ := json.Marshal(body)
	resp, err := ollamaClient.Post(strings.TrimRight(sm.url, "/")+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("ollama %s: %s %s", path, resp.Status, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (sm *semantic) embed(texts []string) ([][]float64, error) {
	var r struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := sm.post("/api/embed", map[string]any{"model": sm.model, "input": texts}, &r); err != nil {
		return nil, err
	}
	if len(r.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d texts", len(r.Embeddings), len(texts))
	}
	return r.Embeddings, nil
}

// start pulls the model if needed and embeds the examples, retrying in the
// background until Ollama answers.
func (sm *semantic) start() {
	if sm.url == "" {
		return
	}
	for wait := 30 * time.Second; ; wait = min(wait*2, 30*time.Minute) {
		if err := sm.load(); err == nil {
			log.Printf("semantic matching on (%s via %s)", sm.model, sm.url)
			return
		} else {
			log.Printf("semantic matching not ready: %v", err)
		}
		time.Sleep(wait)
	}
}

func (sm *semantic) load() error {
	pull := &http.Client{Timeout: 20 * time.Minute}
	data, _ := json.Marshal(map[string]any{"model": sm.model, "stream": false})
	resp, err := pull.Post(strings.TrimRight(sm.url, "/")+"/api/pull", "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("pulling %s: %s", sm.model, resp.Status)
	}
	var texts, intents []string
	for intent, list := range intentExamples {
		for _, t := range list {
			texts = append(texts, t)
			intents = append(intents, intent)
		}
	}
	// One at a time: some Ollama builds fail batched input for BERT-style
	// models like all-minilm.
	var vecs [][]float64
	for _, t := range texts {
		v, err := sm.embed([]string{t})
		if err != nil {
			return err
		}
		vecs = append(vecs, v[0])
	}
	examples := make([]semanticExample, len(vecs))
	for i, v := range vecs {
		examples[i] = semanticExample{intent: intents[i], vec: v}
	}
	sm.mu.Lock()
	sm.examples, sm.ready = examples, true
	sm.mu.Unlock()
	return nil
}

// match returns the intent closest in meaning to text, if close enough.
func (sm *semantic) match(text string) (string, bool) {
	sm.mu.Lock()
	ready, examples := sm.ready, sm.examples
	sm.mu.Unlock()
	if !ready || len(text) < 12 || len(text) > 400 || !questionRe.MatchString(text) {
		return "", false
	}
	vecs, err := sm.embed([]string{text})
	if err != nil {
		log.Printf("semantic: %v", err)
		return "", false
	}
	return bestIntent(vecs[0], examples, sm.threshold)
}

func bestIntent(v []float64, examples []semanticExample, threshold float64) (string, bool) {
	best, score := "", -1.0
	for _, e := range examples {
		if s := cosine(v, e.vec); s > score {
			best, score = e.intent, s
		}
	}
	return best, score >= threshold
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
