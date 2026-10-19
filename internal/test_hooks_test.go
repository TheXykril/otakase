package internal

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// withPromptSelect answers every menu (ordered or not) with pick for the
// rest of the test.
func withPromptSelect(t *testing.T, pick func([]SelectionOption) (SelectionOption, error)) {
	t.Helper()
	savedSelect, savedOrdered := promptSelect, promptSelectOrdered
	promptSelect, promptSelectOrdered = pick, pick
	t.Cleanup(func() {
		promptSelect, promptSelectOrdered = savedSelect, savedOrdered
	})
}

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w

	done := make(chan []byte)
	go func() {
		var out bytes.Buffer
		_, _ = io.Copy(&out, r)
		done <- out.Bytes()
	}()

	func() {
		defer func() { os.Stdout = saved }()
		fn()
	}()
	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe: %v", err)
	}
	out := <-done
	_ = r.Close()
	return string(out)
}

// scriptedProvider serves canned episode lists and stream links per audio
// mode and records which modes were asked for.
type scriptedProvider struct {
	episodes    map[string][]string
	episodeErrs map[string]error
	links       map[string][]string
	linkErrs    map[string]error

	episodeCalls []string
	linkCalls    []string
}

func (s *scriptedProvider) Name() string { return "scripted" }

func (s *scriptedProvider) SearchAnime(string, string) ([]SelectionOption, error) {
	return nil, nil
}

func (s *scriptedProvider) EpisodesList(_ string, mode string) ([]string, error) {
	mode = normalizeTranslationType(mode)
	s.episodeCalls = append(s.episodeCalls, mode)
	if err, ok := s.episodeErrs[mode]; ok && err != nil {
		return nil, err
	}
	if eps, ok := s.episodes[mode]; ok {
		return eps, nil
	}
	return []string{"1"}, nil
}

func (s *scriptedProvider) GetEpisodeURL(cfg Config, _ string, _ int) ([]string, error) {
	mode := normalizeTranslationType(cfg.SubOrDub)
	s.linkCalls = append(s.linkCalls, mode)
	if err, ok := s.linkErrs[mode]; ok && err != nil {
		return nil, err
	}
	return s.links[mode], nil
}

func useProvider(t *testing.T, p Provider) {
	t.Helper()
	saved := CurrentProvider
	CurrentProvider = p
	t.Cleanup(func() { CurrentProvider = saved })
}

func useConfig(t *testing.T, cfg *Config) {
	t.Helper()
	saved := GetGlobalConfig()
	SetGlobalConfig(cfg)
	t.Cleanup(func() { SetGlobalConfig(saved) })
}
