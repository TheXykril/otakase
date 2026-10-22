package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The cheat code: ↑↑↓↓←→←→BA on the main list in the terminal, or
// uuddlrlrba typed into rofi's main list. It opens a puzzle hunt the Discord
// bot makes for the linked member (bot/cheathunt.go). The bot holds the
// puzzle and its answers; this side only shows each step and sends what the
// viewer types. Solving it shows a one-time code to redeem in Discord.

// cheatCodeKey is what the main list returns when the code is entered.
const cheatCodeKey = "CHEAT_CODE"

// konamiKeys is the code as Bubble Tea names the keys.
var konamiKeys = []string{"up", "up", "down", "down", "left", "right", "left", "right", "b", "a"}

// konamiTracker remembers the last keys pressed on a menu.
type konamiTracker struct{ keys []string }

// press records key and reports whether it finished the code.
func (k *konamiTracker) press(key string) bool {
	k.keys = append(k.keys, strings.ToLower(key))
	if len(k.keys) > len(konamiKeys) {
		k.keys = k.keys[len(k.keys)-len(konamiKeys):]
	}
	if len(k.keys) != len(konamiKeys) {
		return false
	}
	for i, want := range konamiKeys {
		if k.keys[i] != want {
			return false
		}
	}
	k.keys = nil
	return true
}

// isKonamiText reports whether text typed into rofi is the code spelt out:
// uuddlrlrba, in any case, with or without spaces.
func isKonamiText(text string) bool {
	text = strings.ToLower(strings.Join(strings.Fields(text), ""))
	return text == "uuddlrlrba" || text == "↑↑↓↓←→←→ba"
}

func init() {
	homeMenuActions[cheatCodeKey] = func(m *homeMenu) menuOutcome {
		return openPage(func() { RunCheatCode(m.config) })
	}
}

// cheatView is the bot's answer, see bot/cheathunt.go.
type cheatView struct {
	Correct   bool      `json:"correct"`
	Status    string    `json:"status"` // puzzle, found, wait or gone
	Kind      string    `json:"kind"`
	Step      int       `json:"step"`
	Steps     int       `json:"steps"`
	Text      string    `json:"text"`
	Links     []string  `json:"links"`
	Where     string    `json:"where"` // app or discord
	WrongLeft int       `json:"wrong_left"`
	Code      string    `json:"code"`
	Redeemed  bool      `json:"redeemed"`
	Until     time.Time `json:"until"`
	Message   string    `json:"message"`
}

// cheatHTTPClient waits longer than the other bot requests: the first hunt
// after the bot starts may wait for its show list.
var cheatHTTPClient = &http.Client{Timeout: 30 * time.Second}

func cheatRequest(config *Config, path string, in any) (cheatView, error) {
	var v cheatView
	data, err := json.Marshal(in)
	if err != nil {
		return v, err
	}
	_, err = discordDoWith(cheatHTTPClient, context.Background(), config, http.MethodPost, path,
		loadDiscordLink(config).Token, "application/json", bytes.NewReader(data), &v)
	return v, err
}

// cheatPage asks for one line under a block of text. It is the prompt's
// frame with room for a puzzle; unlike the prompt, an empty Enter is not
// leaving (Esc is), so a step done in Discord can be checked again.
var cheatPage = func(config *Config, title, body, note, hint string) (answer string, leave bool) {
	if config != nil && config.RofiSelection {
		return cheatRofiPage(title, body, note)
	}
	field := textinput.New()
	field.Prompt = "› "
	field.CharLimit = 200
	field.Focus()
	finished, err := tea.NewProgram(cheatPageModel{input: field, title: title, body: body, note: note, hint: hint}).Run()
	if err != nil {
		Log(fmt.Sprintf("Cheat code: %v", err))
		return "", true
	}
	page, ok := finished.(cheatPageModel)
	if !ok || page.leave {
		return "", true
	}
	return strings.TrimSpace(page.input.Value()), false
}

type cheatPageModel struct {
	input                   textinput.Model
	title, body, note, hint string
	leave                   bool
}

func (m cheatPageModel) Init() tea.Cmd { return textinput.Blink }

func (m cheatPageModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := message.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			m.leave = true
			return m, tea.Quit
		case tea.KeyEnter:
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(message)
	return m, cmd
}

func (m cheatPageModel) View() string {
	width := lipgloss.Width(m.title)
	for _, line := range strings.Split(m.body, "\n") {
		width = max(width, lipgloss.Width(line))
	}
	var b strings.Builder
	b.WriteString(renderBreadcrumb("Cheat code"))
	b.WriteString("\n")
	b.WriteString(renderRule(min(width+4, 80)))
	b.WriteString("\n\n")
	b.WriteString(paneTitleStyle.Render(m.title))
	b.WriteString("\n\n")
	b.WriteString(m.body)
	b.WriteString("\n\n")
	if m.note != "" {
		b.WriteString(filterLabelStyle.Render(m.note))
		b.WriteString("\n")
	}
	if m.hint != "" {
		b.WriteString(footerTextStyle.Render(m.hint))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(m.input.View())
	b.WriteString("\n")
	return b.String()
}

// cheatRofiPage is cheatPage in rofi: the text above an entry, in a fixed
// width font so grids line up.
func cheatRofiPage(title, body, note string) (string, bool) {
	defer suspendBusy()()
	mesg := "<b>" + escapePango(title) + "</b>\n\n<tt>" + escapePango(body) + "</tt>"
	if note != "" {
		mesg += "\n\n<i>" + escapePango(note) + "</i>"
	}
	theme := filepath.Join(GetStoragePath(), "userinput.rasi")
	out, err := exec.Command("rofi", "-dmenu", "-theme", theme, "-p", "Answer", "-mesg", mesg).Output()
	if err != nil {
		// Dismissed: leaving.
		return "", true
	}
	return strings.TrimSpace(string(out)), false
}

// cheatOpenCommand reads "open" or "open 2" typed at a step: which link to
// open, from 1.
func cheatOpenCommand(answer string) (int, bool) {
	fields := strings.Fields(strings.ToLower(answer))
	if len(fields) == 0 || fields[0] != "open" || len(fields) > 2 {
		return 0, false
	}
	if len(fields) == 1 {
		return 1, true
	}
	n, err := strconv.Atoi(fields[1])
	return n, err == nil && n > 0
}

func cheatStepTitle(v cheatView, first bool) string {
	title := fmt.Sprintf("%s · step %d of %d", v.Kind, v.Step, v.Steps)
	if first {
		title = "CHEAT ACTIVATED · " + title
	}
	return title
}

func cheatHint(v cheatView) string {
	var parts []string
	if v.Where == "discord" {
		parts = append(parts, "Do it in Discord, then press Enter here to check")
	} else {
		parts = append(parts, "Type your answer")
	}
	switch len(v.Links) {
	case 0:
	case 1:
		parts = append(parts, "open opens the link")
	default:
		parts = append(parts, fmt.Sprintf("open 1-%d opens a link", len(v.Links)))
	}
	parts = append(parts, "Esc leaves; enter the code again to come back")
	return strings.Join(parts, " · ")
}

// cheatEndText is what a hunt that is not running says.
func cheatEndText(v cheatView) (title, body string) {
	until := ""
	if !v.Until.IsZero() {
		until = v.Until.Local().Format("Monday 2 January, 15:04")
	}
	switch v.Status {
	case "found":
		if v.Redeemed {
			return "Code redeemed", fmt.Sprintf("This week's code (%s) has been redeemed.\n\nThe next hunt opens %s.", v.Code, until)
		}
		return "CODE FOUND", fmt.Sprintf("    %s\n\n%s\n\nRedeem it in Discord: /redeem code:%s", v.Code, cmpOr(v.Message, "It works once."), v.Code)
	case "wait":
		return "The trail went cold", cmpOr(v.Message, "Too many wrong answers.") + "\n\nTry again after " + until + "."
	}
	return "Nothing here", cmpOr(v.Message, "The hunt is over. Enter the code again for a new one.")
}

func cmpOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// RunCheatCode is what the cheat code opens.
func RunCheatCode(config *Config) {
	Log("Cheat code entered")
	const leaveHint = "Press Enter or Esc to go back"
	if !discordEnabled(config) {
		cheatPage(config, "CHEAT ACTIVATED", "...but nothing answers. The Discord features are off (DiscordBotURL is empty).", "", leaveHint)
		return
	}
	if !discordLinked(config) {
		cheatPage(config, "CHEAT ACTIVATED", "Something stirs, but it doesn't know who you are yet.\n\nLink Otakase to Discord first (Community, then Link to Discord), then enter the code again.", "", leaveHint)
		return
	}
	endBusy := BeginBusy(config, "Waking something up")
	v, err := cheatRequest(config, "/v1/cheat", struct{}{})
	endBusy()
	first := true
	note := ""
	for {
		if err != nil {
			Log(fmt.Sprintf("Cheat code: %v", err))
			msg := "Nothing answers right now. Try again later."
			var apiErr *discordAPIError
			if errors.As(err, &apiErr) && apiErr.Message != "" {
				msg = "Nothing answers right now: " + apiErr.Message + "."
			}
			if errors.Is(err, errDiscordUnauthorized) {
				msg = "This app is no longer linked to Discord. Link it again from Community, then enter the code again."
			}
			cheatPage(config, "CHEAT ACTIVATED", msg, "", leaveHint)
			return
		}
		if v.Status != "puzzle" {
			title, body := cheatEndText(v)
			if first && v.Status == "found" && !v.Redeemed {
				title = "CHEAT ACTIVATED · " + title
			}
			cheatPage(config, title, body, note, leaveHint)
			return
		}

		answer, leave := cheatPage(config, cheatStepTitle(v, first), v.Text, note, cheatHint(v))
		first = false
		if leave {
			return
		}
		note = ""
		if n, ok := cheatOpenCommand(answer); ok {
			if n > len(v.Links) {
				note = "There is no such link."
			} else if err := discordOpenURL(v.Links[n-1]); err != nil {
				note = "Could not open a browser: " + v.Links[n-1]
			}
			continue
		}
		if strings.TrimSpace(answer) == "" || !strings.ContainsFunc(answer, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			// An empty Enter checks whether a step done in Discord went through.
			v, err = cheatRequest(config, "/v1/cheat", struct{}{})
			if err == nil && v.Status == "puzzle" && v.Where == "discord" && strings.TrimSpace(answer) == "" {
				note = "Nothing yet."
			}
			continue
		}
		v, err = cheatRequest(config, "/v1/cheat/answer", map[string]string{"answer": answer})
		if err == nil {
			switch {
			case v.Correct && v.Status == "puzzle":
				note = "Correct."
			case !v.Correct:
				note = v.Message
				if v.Status == "puzzle" && v.WrongLeft > 0 && v.WrongLeft <= 5 {
					note += fmt.Sprintf(" (%d wrong answers left on this trail)", v.WrongLeft)
				}
			}
		}
	}
}
