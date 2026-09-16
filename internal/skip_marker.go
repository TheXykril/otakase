package internal

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Keys the marker binds in the player. They are Alt combinations because mpv
// leaves those alone: a plain letter would take over a key that already does
// something, and the viewer did not ask to lose it.
var skipMarkerBindings = []struct {
	Key     string
	Action  string
	Purpose string
}{
	{"Alt+o", "op", "mark the opening"},
	{"Alt+e", "ed", "mark the ending"},
	{"Alt+s", "submit", "send what is marked to AniSkip"},
	{"Alt+r", "reset", "forget what is marked"},
	{"Alt+u", "upvote", "agree with the skip you were given"},
	{"Alt+d", "downvote", "report the skip you were given as wrong"},
}

const (
	skipMarkerMessage = "otakase-skip"

	// How long a submission stays armed after the first Alt+s. Long enough to
	// read what is about to be sent, short enough that it cannot be confirmed
	// by a keypress meant for something else minutes later.
	skipConfirmWindow = 10 * time.Second
)

// skipMarker collects skip times from the viewer while they watch, and offers
// them to AniSkip once they say so.
//
// Marking is free and local; nothing leaves the machine until Alt+s is pressed
// twice, with what will be sent on screen in between. That matters because
// these entries are public: every player reading AniSkip inherits whatever this
// sends, so an accidental keypress must not be able to publish anything.
type skipMarker struct {
	socket      string
	storagePath string
	malID       int
	episode     int
	applied     SkipIDs

	mu         sync.Mutex
	opStart    *float64
	opEnd      *float64
	edStart    *float64
	edEnd      *float64
	confirmAt  time.Time
	submitting bool
}

var (
	activeSkipMarkerMu sync.Mutex
	activeSkipMarker   *skipMarker
)

// StartSkipMarker binds the marker's keys in the player and answers them until
// playback ends.
//
// One marker serves the whole session. Starting a second for the next episode
// would leave both listening to the same socket, and every keypress would be
// answered twice.
func StartSkipMarker(config *CurdConfig, anime *Anime, socket string, applied SkipIDs, done <-chan struct{}) {
	if config == nil || anime == nil || !config.ContributeSkipTimes {
		return
	}
	if socket == "" || socket == "android-intent" {
		return
	}

	activeSkipMarkerMu.Lock()
	if activeSkipMarker != nil && activeSkipMarker.socket == socket {
		activeSkipMarkerMu.Unlock()
		UpdateSkipMarker(anime, applied)
		return
	}
	activeSkipMarkerMu.Unlock()

	marker := &skipMarker{
		socket:      socket,
		storagePath: config.StoragePath,
		malID:       anime.MalId,
		episode:     anime.Ep.Number,
		applied:     applied,
	}

	for _, binding := range skipMarkerBindings {
		command := fmt.Sprintf("script-message %s %s", skipMarkerMessage, binding.Action)
		if _, err := MPVSendCommand(socket, []interface{}{"keybind", binding.Key, command}); err != nil {
			Log(fmt.Sprintf("skip marker: could not bind %s: %v", binding.Key, err))
			return
		}
	}

	activeSkipMarkerMu.Lock()
	activeSkipMarker = marker
	activeSkipMarkerMu.Unlock()

	go func() {
		listenMPVClientMessages(socket, done, func(args []string) {
			if len(args) < 2 || args[0] != skipMarkerMessage {
				return
			}
			marker.handle(args[1])
		})
		activeSkipMarkerMu.Lock()
		if activeSkipMarker == marker {
			activeSkipMarker = nil
		}
		activeSkipMarkerMu.Unlock()
	}()
}

// UpdateSkipMarker points the marker at the episode now playing, and forgets
// marks made against the last one -- they describe a different episode, and
// submitting them under this one would file times for the wrong thing.
func UpdateSkipMarker(anime *Anime, applied SkipIDs) {
	if anime == nil {
		return
	}

	activeSkipMarkerMu.Lock()
	marker := activeSkipMarker
	activeSkipMarkerMu.Unlock()
	if marker == nil {
		return
	}

	marker.mu.Lock()
	defer marker.mu.Unlock()
	marker.malID = anime.MalId
	marker.episode = anime.Ep.Number
	marker.applied = applied
	marker.opStart, marker.opEnd, marker.edStart, marker.edEnd = nil, nil, nil, nil
	marker.confirmAt = time.Time{}
}

func (m *skipMarker) handle(action string) {
	switch action {
	case "op", "ed":
		m.mark(action)
	case "reset":
		m.reset()
	case "submit":
		m.submit()
	case "upvote", "downvote":
		m.vote(action)
	}
}

// mark records where the viewer is: the start of a span the first time, the end
// the second.
func (m *skipMarker) mark(kind string) {
	position, err := mpvFloatProperty(m.socket, "time-pos")
	if err != nil {
		m.say("Could not read the position from the player")
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.confirmAt = time.Time{}

	start, end := &m.opStart, &m.opEnd
	label := "Opening"
	if kind == "ed" {
		start, end = &m.edStart, &m.edEnd
		label = "Ending"
	}

	switch {
	case *start == nil:
		*start = &position
		*end = nil
		m.say(fmt.Sprintf("%s starts at %s — press again at its end", label, FormatSkipTimestamp(position)))
	case position <= **start:
		// Marking an end before the start is a mis-press, and the useful
		// reading of it is "I meant to start here".
		*start = &position
		*end = nil
		m.say(fmt.Sprintf("%s starts at %s — press again at its end", label, FormatSkipTimestamp(position)))
	default:
		*end = &position
		m.say(fmt.Sprintf("%s %s–%s · alt+s to send it", label,
			FormatSkipTimestamp(**start), FormatSkipTimestamp(position)))
	}
}

func (m *skipMarker) reset() {
	m.mu.Lock()
	m.opStart, m.opEnd, m.edStart, m.edEnd = nil, nil, nil, nil
	m.confirmAt = time.Time{}
	m.mu.Unlock()
	m.say("Marks cleared")
}

// submit asks the first time and sends the second.
func (m *skipMarker) submit() {
	m.mu.Lock()
	if m.submitting {
		m.mu.Unlock()
		return
	}

	submissions, length, err := m.pending()
	if err != nil {
		m.confirmAt = time.Time{}
		m.mu.Unlock()
		m.say(err.Error())
		return
	}

	armed := !m.confirmAt.IsZero() && time.Since(m.confirmAt) < skipConfirmWindow
	if !armed {
		m.confirmAt = time.Now()
		m.mu.Unlock()
		m.say("Send " + describeSubmissions(submissions) + " to AniSkip? alt+s again to confirm")
		return
	}

	m.confirmAt = time.Time{}
	m.submitting = true
	m.mu.Unlock()

	// Sending happens off the player's event loop: the keypress must not wait
	// on a request, and the reply lands on screen when it arrives.
	go func() {
		sent, failures := 0, []string{}
		for _, submission := range submissions {
			submission.EpisodeLength = length
			if _, err := SubmitSkipTime(m.storagePath, submission); err != nil {
				Log(fmt.Sprintf("skip marker: submitting %s: %v", submission.SkipType, err))
				failures = append(failures, err.Error())
				continue
			}
			sent++
		}

		m.mu.Lock()
		m.submitting = false
		if len(failures) == 0 {
			m.opStart, m.opEnd, m.edStart, m.edEnd = nil, nil, nil, nil
		}
		m.mu.Unlock()

		switch {
		case len(failures) == 0:
			m.say(fmt.Sprintf("Thank you — %d skip time%s sent to AniSkip", sent, plural(sent)))
		case sent > 0:
			m.say(fmt.Sprintf("Sent %d, but: %s", sent, failures[0]))
		default:
			m.say("Not sent: " + failures[0])
		}
	}()
}

// pending turns the marks into submissions, or explains why it cannot.
// It is called with the lock held.
func (m *skipMarker) pending() ([]SkipSubmission, float64, error) {
	length, err := mpvFloatProperty(m.socket, "duration")
	if err != nil || length <= 0 {
		return nil, 0, fmt.Errorf("the episode length is unknown, and AniSkip needs it")
	}

	submissions := []SkipSubmission{}
	for _, pair := range []struct {
		skipType   string
		start, end *float64
	}{
		{"op", m.opStart, m.opEnd},
		{"ed", m.edStart, m.edEnd},
	} {
		if pair.start == nil || pair.end == nil {
			continue
		}
		submission := SkipSubmission{
			MalID:         m.malID,
			Episode:       m.episode,
			SkipType:      pair.skipType,
			StartTime:     *pair.start,
			EndTime:       *pair.end,
			EpisodeLength: length,
		}
		if err := submission.Validate(); err != nil {
			return nil, 0, fmt.Errorf("%s", err.Error())
		}
		submissions = append(submissions, submission)
	}

	if len(submissions) == 0 {
		return nil, 0, fmt.Errorf("nothing marked yet — alt+o at the start and end of the opening")
	}
	return submissions, length, nil
}

// vote agrees or disagrees with the entry the player was given.
func (m *skipMarker) vote(voteType string) {
	position, err := mpvFloatProperty(m.socket, "time-pos")
	if err != nil {
		m.say("Could not read the position from the player")
		return
	}

	// Which entry the viewer means is the one they are at, or the one they just
	// watched the player skip -- asking would mean a menu over the video.
	skipID, label := m.entryNear(position)
	if skipID == "" {
		m.say("Nothing here came from AniSkip, so there is nothing to vote on")
		return
	}

	go func() {
		if err := VoteSkipTime(skipID, voteType); err != nil {
			Log(fmt.Sprintf("skip marker: %s %s: %v", voteType, skipID, err))
			m.say("Vote not sent: " + err.Error())
			return
		}
		if voteType == "upvote" {
			m.say("Agreed with the " + label + " times")
			return
		}
		m.say("Reported the " + label + " times as wrong · alt+o to mark the right ones")
	}()
}

// entryNear picks the AniSkip entry the given position belongs to, allowing for
// the viewer reacting a little after the skip happened.
func (m *skipMarker) entryNear(position float64) (string, string) {
	const reactionWindow = 30.0

	anime := GetGlobalAnime()
	if anime == nil {
		return "", ""
	}
	for _, candidate := range []struct {
		id    string
		span  Skip
		label string
	}{
		{m.applied.Op, anime.Ep.SkipTimes.Op, "opening"},
		{m.applied.Ed, anime.Ep.SkipTimes.Ed, "ending"},
	} {
		if candidate.id == "" || !usableSpan(candidate.span) {
			continue
		}
		if position >= float64(candidate.span.Start)-reactionWindow &&
			position <= float64(candidate.span.End)+reactionWindow {
			return candidate.id, candidate.label
		}
	}
	return "", ""
}

func (m *skipMarker) say(text string) {
	mpvShowText(m.socket, text, 4*time.Second)
	Log("skip marker: " + text)
}

func describeSubmissions(submissions []SkipSubmission) string {
	parts := make([]string, 0, len(submissions))
	for _, submission := range submissions {
		name := "opening"
		if strings.HasSuffix(submission.SkipType, "ed") {
			name = "ending"
		}
		parts = append(parts, fmt.Sprintf("%s %s–%s", name,
			FormatSkipTimestamp(submission.StartTime), FormatSkipTimestamp(submission.EndTime)))
	}
	return strings.Join(parts, " and ")
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
