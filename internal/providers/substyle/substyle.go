// Package substyle settles how subtitles are delivered in sub mode: as a
// separate file the player loads (soft) or burned into the video (hard).
package substyle

import (
	"errors"
	"strings"
	"sync"

	"github.com/thexykril/otakase/internal/providerhost"
)

const (
	soft = "soft"
	hard = "hard"
	ask  = "ask"
)

// The answer given at a prompt holds for the rest of the run, so the next
// episode does not ask again.
var (
	mu      sync.Mutex
	session string
)

func normalize(style string) string {
	switch s := strings.ToLower(strings.TrimSpace(style)); s {
	case soft, hard:
		return s
	}
	return ask
}

// preference is the style in force: the live setting first, then an answer
// given earlier this run, then the caller's configured value.
func preference(configured string) string {
	if providerhost.CurrentSubStyle != nil {
		if live := normalize(providerhost.CurrentSubStyle()); live != ask {
			return live
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if session != "" {
		return session
	}
	return normalize(configured)
}

func remember(style string) {
	if style != soft && style != hard {
		return
	}
	mu.Lock()
	session = style
	mu.Unlock()
	if providerhost.PersistSubStylePreference != nil {
		_ = providerhost.PersistSubStylePreference(style)
	}
}

func say(msg string) {
	if providerhost.Out != nil {
		providerhost.Out(msg)
	}
}

// Choose picks soft or hard subtitle delivery for sub-mode playback.
//
// A stated preference is honoured when the episode comes that way. Wanting
// soft but having only hard plays hard unasked; wanting hard but having only
// soft asks first. With no preference the user picks when there is a choice.
func Choose(hasSoft, hasHard bool, configured string) (string, error) {
	pref := preference(configured) // soft, hard or ask
	switch {
	case pref == soft && hasSoft, pref == hard && hasHard:
		return pref, nil
	case pref == soft && hasHard:
		return hard, nil
	case pref == hard && hasSoft:
		return offerSoft()
	case pref == ask && hasSoft && hasHard:
		return askBoth()
	case pref == ask && hasSoft:
		say("Using soft subs with external subtitles.")
		remember(soft)
		return soft, nil
	case pref == ask && hasHard:
		return hard, nil
	}
	return "", errors.New("no sub streams found")
}

// askBoth lets the user pick when the episode comes both ways. Without a way
// to prompt, soft wins: it keeps the subtitles switchable.
func askBoth() (string, error) {
	if providerhost.PromptSelect == nil {
		remember(soft)
		return soft, nil
	}
	say("Both soft-sub and hard-sub streams are available.")
	picked, err := providerhost.PromptSelect([]providerhost.PromptOption{
		{Key: soft, Label: "Soft sub (external subtitles)"},
		{Key: hard, Label: "Hard sub (burned-in subtitles)"},
	})
	if err != nil {
		return "", err
	}
	style := soft
	if picked.Key == hard {
		style = hard
	}
	remember(style)
	return style, nil
}

// offerSoft asks a user who wants hard subs whether soft ones will do, since
// that is all the episode has.
func offerSoft() (string, error) {
	if providerhost.PromptSelect == nil {
		remember(soft)
		return soft, nil
	}
	say("Only soft subs with external subtitles are available.")
	picked, err := providerhost.PromptSelect([]providerhost.PromptOption{
		{Key: soft, Label: "Use soft subs (external subtitles)"},
		{Key: "cancel", Label: "Cancel"}, // anything but soft refuses
	})
	if err != nil {
		return "", err
	}
	if picked.Key != soft {
		return "", errors.New("no hard-sub streams found")
	}
	remember(soft)
	return soft, nil
}

// ResetForTest forgets the answer given earlier in the run, so each test
// starts with nothing remembered.
func ResetForTest() {
	mu.Lock()
	session = ""
	mu.Unlock()
}
