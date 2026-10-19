package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/mpv"
)

// Party extras in mpv, for the host and the members alike: the party's
// Discord chat and reactions on screen, and votes to skip or move on.
//
//	Alt+k  vote to skip (the opening or ending when it's known, else 85 s)
//	Alt+n  vote for the next episode
//	Alt+c  party chat on or off (on to begin with)
//
// The bot relays {"type":"react","emoji","name"} and {"type":"chat","name",
// "text"} from the party's voice channel chat. A key sends {"type":"vote",
// "what":"skip"|"next"}; the bot answers everyone with {"type":"votes",
// "what","votes","needed"}, and with {"type":"voted","what"} once more than
// half of the party agrees: the host's app skips or plays the next episode,
// and the members follow it as they follow everything else.

const (
	partyFXMessage    = "otakase-party"
	partyChatLines    = 5
	partyChatShowFor  = 45 * time.Second
	partyReactShowFor = 4 * time.Second
	partyReactsShown  = 6
	// partyOverlayID is this feature's osd-overlay; the id only has to be
	// unique within its own IPC connection.
	partyOverlayID = 41
	// partySkipBy is how far a skip goes when no opening or ending is known
	// where the host is: about an opening's length.
	partySkipBy = 85.0
)

var partyFXBindings = []struct{ Key, Action string }{
	{"Alt+k", "skip"},
	{"Alt+n", "next"},
	{"Alt+c", "chat"},
}

type partyChatLine struct {
	Name, Text string
	At         time.Time
}

type partyReaction struct {
	Name, Emoji string
	At          time.Time
}

// partyFX draws the chat and reactions in one mpv and answers its keys.
// It follows the player across episodes: attach it to each new socket.
type partyFX struct {
	send func(v any)
	// onVoted runs on the host's app when a vote passes.
	onVoted func(socket, what string)

	mu      sync.Mutex
	socket  string
	conn    net.Conn // kept open: mpv drops a client's overlays when it disconnects
	done    chan struct{}
	chatOff bool
	chat    []partyChatLine
	reacts  []partyReaction
	drawn   string
	resX    int
}

func newPartyFX(send func(v any)) *partyFX {
	return &partyFX{send: send, resX: 1280}
}

// attach binds the keys and opens the overlay in the mpv at socket, once
// per player.
func (f *partyFX) attach(socket string) {
	f.mu.Lock()
	if socket == f.socket || !usableSocket(socket) {
		f.mu.Unlock()
		return
	}
	f.detachLocked()
	f.mu.Unlock()

	for _, b := range partyFXBindings {
		command := fmt.Sprintf("script-message %s %s", partyFXMessage, b.Action)
		if _, err := MPVSendCommand(socket, []interface{}{"keybind", b.Key, command}); err != nil {
			Log("watch party keys: " + err.Error())
			return
		}
	}
	conn, err := mpv.Dial(socket)
	if err != nil {
		Log("watch party overlay: " + err.Error())
		return
	}
	go io.Copy(io.Discard, conn) // mpv's answers
	done := make(chan struct{})
	f.mu.Lock()
	f.socket, f.conn, f.done, f.drawn = socket, conn, done, ""
	f.resX = partyOverlayWidth(socket)
	f.mu.Unlock()

	go func() {
		listenMPVClientMessages(socket, done, func(args []string) {
			if len(args) >= 2 && args[0] == partyFXMessage {
				f.key(args[1])
			}
		})
		// The player is gone: the next attach starts over.
		f.mu.Lock()
		if f.done == done {
			f.detachLocked()
		}
		f.mu.Unlock()
	}()
	mpvOSD(socket, "Watch party · Alt+k vote skip · Alt+n vote next episode · Alt+c chat")
}

func (f *partyFX) detachLocked() {
	if f.done != nil {
		close(f.done)
		f.done = nil
	}
	if f.conn != nil {
		f.conn.Close()
		f.conn = nil
	}
	f.socket = ""
}

// close takes the overlay down and stops listening.
func (f *partyFX) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overlayLocked("")
	f.detachLocked()
}

// partyOverlayWidth is the overlay's width for a 720 high canvas in the
// player's shape.
func partyOverlayWidth(socket string) int {
	w, err1 := mpvFloatProperty(socket, "osd-width")
	h, err2 := mpvFloatProperty(socket, "osd-height")
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 1280
	}
	return int(720 * w / h)
}

// key answers one of the party's keys.
func (f *partyFX) key(action string) {
	f.mu.Lock()
	socket := f.socket
	f.mu.Unlock()
	switch action {
	case "skip", "next":
		f.send(map[string]string{"type": "vote", "what": action})
		mpvOSD(socket, "You voted to "+partyVoteWords(action))
	case "chat":
		f.mu.Lock()
		f.chatOff = !f.chatOff
		off := f.chatOff
		f.redrawLocked(time.Now())
		f.mu.Unlock()
		if off {
			mpvOSD(socket, "Party chat off · Alt+c to show it")
		} else {
			mpvOSD(socket, "Party chat on")
		}
	}
}

func partyVoteWords(what string) string {
	if what == "next" {
		return "play the next episode"
	}
	return "skip"
}

// handle takes the messages that are the extras' own and reports whether m
// was one.
func (f *partyFX) handle(m partyMessage) bool {
	now := time.Now()
	f.mu.Lock()
	socket := f.socket
	switch m.Type {
	case "react":
		f.reacts = append(f.reacts, partyReaction{Name: m.Name, Emoji: m.Emoji, At: now})
		if len(f.reacts) > partyReactsShown {
			f.reacts = f.reacts[len(f.reacts)-partyReactsShown:]
		}
		f.redrawLocked(now)
	case "chat":
		f.chat = append(f.chat, partyChatLine{Name: m.Name, Text: m.Text, At: now})
		if len(f.chat) > partyChatLines {
			f.chat = f.chat[len(f.chat)-partyChatLines:]
		}
		f.redrawLocked(now)
	case "votes", "voted":
	default:
		f.mu.Unlock()
		return false
	}
	onVoted := f.onVoted
	f.mu.Unlock()
	switch m.Type {
	case "votes":
		mpvOSD(socket, fmt.Sprintf("Vote to %s: %d of %d · Alt+%s to agree", partyVoteWords(m.What), m.Votes, m.Needed, partyVoteKey(m.What)))
	case "voted":
		mpvOSD(socket, "The party voted to "+partyVoteWords(m.What))
		if onVoted != nil && usableSocket(socket) {
			onVoted(socket, m.What)
		}
	}
	return true
}

func partyVoteKey(what string) string {
	if what == "next" {
		return "n"
	}
	return "k"
}

// tick lets old reactions and chat lines go.
func (f *partyFX) tick() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.redrawLocked(time.Now())
}

func (f *partyFX) redrawLocked(now time.Time) {
	f.overlayLocked(partyOverlayASS(f.chat, f.reacts, !f.chatOff, now, f.resX))
}

// overlayLocked shows text (ASS events) in the overlay, or takes it down
// when text is empty.
func (f *partyFX) overlayLocked(text string) {
	if f.conn == nil || text == f.drawn {
		return
	}
	command := []interface{}{"osd-overlay", partyOverlayID, "ass-events", text, f.resX, 720, 0, false, false}
	if text == "" {
		command = []interface{}{"osd-overlay", partyOverlayID, "none", ""}
	}
	line, _ := json.Marshal(map[string]any{"command": command})
	f.conn.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := f.conn.Write(append(line, '\n')); err != nil {
		Log("watch party overlay: " + err.Error())
		return
	}
	f.drawn = text
}

// partyOverlayASS draws the chat at the bottom left (when on) and the
// reactions rising on the right, on a canvas resX wide and 720 high.
// Chat lines last partyChatShowFor and reactions partyReactShowFor.
func partyOverlayASS(chat []partyChatLine, reacts []partyReaction, chatOn bool, now time.Time, resX int) string {
	var events []string
	if chatOn {
		var lines []string
		for _, l := range chat {
			if now.Sub(l.At) < partyChatShowFor {
				lines = append(lines, fmt.Sprintf("{\\b1}%s{\\b0}  %s", assEscape(l.Name), assEscape(l.Text)))
			}
		}
		if len(lines) > 0 {
			events = append(events, "{\\an1\\pos(24,600)\\fs22\\bord2\\shad0\\1c&HFFFFFF&\\3c&H000000&}"+strings.Join(lines, "\\N"))
		}
	}
	k := 0
	for _, r := range reacts {
		age := now.Sub(r.At)
		if age >= partyReactShowFor || age < 0 {
			continue
		}
		// Rises 80 px and fades over its time on screen.
		rise := int(80 * age / partyReactShowFor)
		alpha := int(255 * age / partyReactShowFor)
		y := 520 - 70*k - rise
		events = append(events, fmt.Sprintf("{\\an3\\pos(%d,%d)\\bord2\\shad0\\alpha&H%02X&}{\\fs44}%s{\\fs16}\\N%s",
			resX-30, y, alpha, assEscape(r.Emoji), assEscape(r.Name)))
		k++
	}
	return strings.Join(events, "\n")
}

// assEscape keeps text from the chat from being read as ASS tags or line
// breaks.
func assEscape(s string) string {
	s = strings.NewReplacer("\\", "\\⁠", "{", "\\{", "}", "\\}", "\n", " ", "\r", " ").Replace(s)
	return s
}

// partySkipTarget is where a skip vote takes the host at pos: the end of
// the opening or ending pos is in (or about to start), else partySkipBy
// further.
func partySkipTarget(pos float64, st SkipTimes) float64 {
	for _, s := range []Skip{st.Op, st.Ed} {
		if s.End > s.Start && pos >= float64(s.Start)-5 && pos < float64(s.End) {
			return float64(s.End)
		}
	}
	return pos + partySkipBy
}

// partyHostVoted does what the party voted for in the host's player.
func partyHostVoted(socket, what string) {
	switch what {
	case "skip":
		pos, err := mpvFloatProperty(socket, "time-pos")
		if err != nil {
			return
		}
		var st SkipTimes
		if a := GetGlobalAnime(); a != nil {
			st = a.Ep.SkipTimes
		}
		if _, err := MPVSendCommand(socket, []interface{}{"seek", partySkipTarget(pos, st), "absolute"}); err != nil {
			Log("watch party skip: " + err.Error())
		}
	case "next":
		// The end of the episode: what happens next is what always happens
		// when an episode ends, and the members follow the host there.
		if _, err := MPVSendCommand(socket, []interface{}{"seek", 100, "absolute-percent"}); err != nil {
			Log("watch party next: " + err.Error())
		}
	}
}
