package internal

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/thexykril/otakase/internal/icons"
)

// rofi's toolbar: the row of buttons above the main list that stands in for
// the terminal's tabs and bottom bar. Each button is a rofi custom keybinding,
// so clicking it and pressing its key end the menu the same way, with an exit
// code saying which (kb-custom-1 exits with 10, kb-custom-2 with 11, ...).
// Buttons need rofi 1.7 or later.

// rofiCustomExitBase is the exit code of kb-custom-1.
const rofiCustomExitBase = 10

type rofiButton struct {
	Key   string
	Label string
	// Binding is the rofi key that presses the button, empty for click only.
	Binding string
	// Hint is the key as the button shows it, ^k, so the keys are learnt by
	// looking rather than from the README.
	Hint string
	// On marks a toggle that is switched on, drawn in the accent colour.
	On bool
}

type rofiToolbar struct {
	buttons []rofiButton
	// keys are the custom bindings in kb-custom order: every button's, then
	// the ones with no button of their own, like Tab for the next list.
	keys []rofiButton
}

// rofiKeyName turns a footer hint, ctrl+k, into rofi's spelling, Control+k.
func rofiKeyName(hint string) string {
	if rest, ok := strings.CutPrefix(hint, "ctrl+"); ok {
		return "Control+" + rest
	}
	return hint
}

// mainRofiToolbar builds the toolbar for the main list from its lists and
// actions, in the order MenuActions gives them.
func mainRofiToolbar(config *Config, tabs []Tab, actions []FooterAction) rofiToolbar {
	var bar rofiToolbar
	if len(tabs) > 1 {
		// Tab moves to the next list without the menu; the button is where
		// the lists are, so it is where that key is shown.
		bar.buttons = append(bar.buttons, rofiButton{Key: listsMenuKey, Label: toolbarButtonLabel(listsMenuKey, config), Hint: "Tab"})
	}
	for _, action := range actions {
		// The continue rows lead the Watching list, so a button for them
		// would only repeat them; with the rows off it is the way back to the
		// last show.
		if action.Key == "CONTINUE_LAST" && continueRowsOn(config) {
			continue
		}
		bar.buttons = append(bar.buttons, rofiButton{
			Key:     action.Key,
			Label:   toolbarButtonLabel(action.Key, config),
			Binding: rofiKeyName(action.Hint),
			Hint:    shortKeyLabel(action.Hint),
			On:      action.Key == "CAST" && config != nil && config.CastToDevice,
		})
	}
	bar.keys = append(bar.keys, bar.buttons...)
	if len(tabs) > 1 {
		bar.keys = append(bar.keys, rofiButton{Key: nextListKey, Binding: "Tab"})
	}
	return bar
}

// toolbarButtonLabel is a button's text: its icon, then its name, or for cast
// the state it is in.
func toolbarButtonLabel(key string, config *Config) string {
	label := toolbarButtonLabels[key]
	if key == "CAST" {
		// The device name is the one part of the bar whose length nobody
		// chose; a long one would push the last buttons off the edge.
		label = truncate(castMenuLabel(config), 24)
	}
	return toolbarIconPrefix(toolbarButtonIcon(key, config)) + label
}

// rofiDefaultsTakenByToolbar frees the keys the toolbar uses from what rofi's
// defaults do with them, all of them editing or completion shortcuts: a key
// bound twice makes rofi refuse to start. Escape still cancels.
var rofiDefaultsTakenByToolbar = []string{
	"-kb-remove-to-eol", "", // Control+k
	"-kb-move-end", "", // Control+e
	"-kb-remove-to-sol", "", // Control+u
	"-kb-mode-complete", "", // Control+l
	"-kb-cancel", "Escape,Control+bracketleft", // was also Control+g
	"-kb-element-next", "", // Tab
}

// args are the rofi arguments that draw the toolbar and bind its keys.
// children is the theme's mainbox with the toolbar placed in it.
func (bar rofiToolbar) args(children []string) []string {
	if len(bar.buttons) == 0 && len(bar.keys) == 0 {
		return nil
	}
	args := append([]string{}, rofiDefaultsTakenByToolbar...)
	for i, key := range bar.keys {
		if key.Binding != "" {
			args = append(args, fmt.Sprintf("-kb-custom-%d", i+1), key.Binding)
		}
	}

	// One declaration per line: rofi's lexer reads a quoted string to the last
	// quote on its line, so a content and an action on one line run together.
	var theme strings.Builder
	theme.WriteString("mainbox {\n  children: [ " + strings.Join(children, ", ") + " ];\n}\n")
	names := make([]string, 0, len(bar.buttons)+1)
	for i := range bar.buttons {
		names = append(names, fmt.Sprintf("button-%d", i+1))
	}
	names = append(names, "button-quit")
	theme.WriteString("box-toolbar {\n  orientation: horizontal;\n  expand: false;\n  spacing: 3px;\n  background-color: transparent;\n  children: [ " + strings.Join(names, ", ") + " ];\n}\n")
	for i, button := range bar.buttons {
		writeRofiButton(&theme, names[i], button.Label, button.Hint, fmt.Sprintf("kb-custom-%d", i+1), button.On, false)
	}
	writeRofiButton(&theme, "button-quit", toolbarQuitLabel(), "Esc", "kb-cancel", false, true)
	// Wide enough for the default bar with its keys and a cast device name;
	// the cards are sized for their lists, not for a row of buttons.
	theme.WriteString("window {\n  width: 1200px;\n}\n")
	return append(args, "-theme-str", theme.String())
}

func toolbarQuitLabel() string {
	return toolbarIconPrefix(optionIcon(SelectionOption{Key: "-1"})) + "Quit"
}

// toolbarIconPrefix is an icon and one space: a button is short, and the two
// spaces a row's icon gets would make the bar wider than its window.
func toolbarIconPrefix(icon icons.Icon) string {
	if prefix := icon.String(); prefix != "" {
		return strings.TrimRight(prefix, " ") + " "
	}
	return ""
}

// rofiButtonMarkup is a button's label with its key after it, dimmed: the
// label says what it does, the key is the shortcut to it. On a button that is
// on, filled with the accent, the theme's dim colour may not show, so the key
// is faded from the button's own text instead.
func rofiButtonMarkup(label, hint string, on bool) string {
	markup := escapePango(label)
	if hint == "" {
		return markup
	}
	if on || rofiMetaColor == "" {
		return markup + ` <span alpha="55%">` + escapePango(hint) + `</span>`
	}
	return markup + fmt.Sprintf(` <span foreground='%s'>`, rofiMetaColor) + escapePango(hint) + `</span>`
}

func writeRofiButton(w *strings.Builder, name, label, hint, action string, on, muted bool) {
	background, text := "@sel-fill", "@text"
	if on {
		background, text = "@accent", "@base"
	}
	if muted {
		background, text = "transparent", "@muted"
	}
	fmt.Fprintf(w, "%s {\n", name)
	fmt.Fprintf(w, "  content: \"%s\";\n", rofiThemeString(rofiButtonMarkup(label, hint, on)))
	fmt.Fprintf(w, "  markup: true;\n")
	fmt.Fprintf(w, "  action: \"%s\";\n", action)
	fmt.Fprintf(w, "  expand: false;\n  padding: 3px 6px;\n  border-radius: 12px;\n  cursor: pointer;\n")
	fmt.Fprintf(w, "  background-color: %s;\n  text-color: %s;\n}\n", background, text)
}

// rofiThemeString escapes text for a quoted rasi string.
func rofiThemeString(text string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(text)
}

// pressed reports which button or key ended the menu, from rofi's exit.
func (bar rofiToolbar) pressed(err error) (SelectionOption, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return SelectionOption{}, false
	}
	index := exitErr.ExitCode() - rofiCustomExitBase
	if index < 0 || index >= len(bar.keys) {
		return SelectionOption{}, false
	}
	key := bar.keys[index]
	return SelectionOption{Key: key.Key, Label: key.Label}, true
}

// rofiPlaceholder is the search box's hint, naming the list on screen since
// the theme draws no prompt.
func rofiPlaceholder(listName string) []string {
	if strings.TrimSpace(listName) == "" {
		return nil
	}
	return []string{"-theme-str", fmt.Sprintf("entry {\n  placeholder: \"Search %s…\";\n}", rofiThemeString(listName))}
}
