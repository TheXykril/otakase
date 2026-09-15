package internal

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/thexykril/otakase/internal/theme"
)

// Tab is one category along the top of the menu. Tabs are the tracker's own
// list statuses -- watching, planning, completed -- so switching one changes
// which list is shown rather than navigating anywhere.
type Tab struct {
	Key   string
	Label string
}

// FooterAction is one entry in the bar along the bottom: the things you do to
// the list rather than a list you look at.
type FooterAction struct {
	Key   string
	Label string
	Hint  string
}

// menuLayout holds the parts of the menu that surround the list. A zero value
// means no tabs, no footer and no side pane, which renders exactly as the menu
// did before any of this existed -- so every existing caller is untouched.
type menuLayout struct {
	tabs      []Tab
	activeTab int
	footer    []FooterAction
	pane      bool
}

func (l menuLayout) hasTabs() bool   { return len(l.tabs) > 0 }
func (l menuLayout) hasFooter() bool { return len(l.footer) > 0 }

// activeTabKey is what the caller needs to know to load the right list.
func (l menuLayout) activeTabKey() string {
	if !l.hasTabs() || l.activeTab < 0 || l.activeTab >= len(l.tabs) {
		return ""
	}
	return l.tabs[l.activeTab].Key
}

// cycleTab moves by delta and wraps, so Tab from the last tab reaches the first
// rather than stopping.
func (l menuLayout) cycleTab(delta int) menuLayout {
	if !l.hasTabs() {
		return l
	}
	count := len(l.tabs)
	l.activeTab = ((l.activeTab+delta)%count + count) % count
	return l
}

var (
	tabActiveStyle   lipgloss.Style
	tabInactiveStyle lipgloss.Style
	tabBarStyle      lipgloss.Style
	footerKeyStyle   lipgloss.Style
	footerTextStyle  lipgloss.Style
	paneBorderStyle  lipgloss.Style
	paneTitleStyle   lipgloss.Style
	paneMetaStyle    lipgloss.Style
)

// applyLayoutTheme rebuilds the surrounding chrome from the palette. Every
// colour here comes from the active theme -- on Omarchy that is the desktop's
// own -- so nothing is hardcoded and a theme change carries through.
func applyLayoutTheme(palette theme.Palette) {
	color := func(value string) lipgloss.Color { return lipgloss.Color(value) }

	tabActiveStyle = lipgloss.NewStyle().
		Foreground(color(palette.SelectionForeground())).
		Background(color(palette.SelectionBackground())).
		Bold(true).
		Padding(0, 2)

	tabInactiveStyle = lipgloss.NewStyle().
		Foreground(color(palette.Muted)).
		Padding(0, 2)

	tabBarStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false).
		BorderForeground(color(palette.Muted))

	footerKeyStyle = lipgloss.NewStyle().
		Foreground(color(palette.Accent)).
		Bold(true)

	footerTextStyle = lipgloss.NewStyle().
		Foreground(color(palette.Muted))

	paneBorderStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderTop(false).
		BorderBottom(false).
		BorderRight(false).
		BorderForeground(color(palette.Muted)).
		Padding(0, 2)

	paneTitleStyle = lipgloss.NewStyle().
		Foreground(color(palette.Foreground)).
		Bold(true)

	paneMetaStyle = lipgloss.NewStyle().
		Foreground(color(palette.Muted))
}

// renderTabBar draws the categories. It returns an empty string when there are
// no tabs, so a caller can always concatenate the result unconditionally.
func renderTabBar(layout menuLayout, width int) string {
	if !layout.hasTabs() {
		return ""
	}
	parts := make([]string, 0, len(layout.tabs))
	for i, tab := range layout.tabs {
		if i == layout.activeTab {
			parts = append(parts, tabActiveStyle.Render(tab.Label))
			continue
		}
		parts = append(parts, tabInactiveStyle.Render(tab.Label))
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	if width > 0 {
		return tabBarStyle.Width(width).Render(bar)
	}
	return tabBarStyle.Render(bar)
}

// renderFooter draws the actions bar: "↵ play  / search  c continue".
func renderFooter(layout menuLayout) string {
	if !layout.hasFooter() {
		return ""
	}
	parts := make([]string, 0, len(layout.footer))
	for _, action := range layout.footer {
		hint := action.Hint
		if hint == "" {
			hint = "·"
		}
		parts = append(parts, footerKeyStyle.Render(hint)+" "+footerTextStyle.Render(action.Label))
	}
	return strings.Join(parts, footerTextStyle.Render("   "))
}

// renderSidePane draws the detail column for the highlighted entry: its poster
// when the terminal can draw one, then the title and whatever else is known.
//
// The poster is passed in already rendered rather than fetched here, because
// drawing is a pure function of state and fetching is not: a view that reached
// for the network would stall the interface every time the selection moved.
func renderSidePane(title, poster string, meta []string, width, height int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	if poster != "" {
		b.WriteString(poster)
		b.WriteString("\n\n")
	}
	if title != "" {
		b.WriteString(paneTitleStyle.Render(truncate(title, width-4)))
		b.WriteString("\n")
	}
	for _, line := range meta {
		if line == "" {
			continue
		}
		b.WriteString(paneMetaStyle.Render(truncate(line, width-4)))
		b.WriteString("\n")
	}
	style := paneBorderStyle.Width(width)
	if height > 0 {
		style = style.Height(height)
	}
	return style.Render(b.String())
}

// truncate shortens to fit, marking that it did so. Width is counted in runes
// rather than bytes, or a title with any non-ASCII character in it -- which is
// most of them here -- would be cut in the wrong place.
func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

// Tab labels are deliberately shorter than the menu entries they replace.
// A tab bar is horizontal and every column spent on "Currently Watching"
// is one the next category does not get.
var menuCategoryLabels = map[string]string{
	"CURRENT":    "Watching",
	"PLANNING":   "Planning",
	"COMPLETED":  "Completed",
	"PAUSED":     "On Hold",
	"DROPPED":    "Dropped",
	"REWATCHING": "Rewatching",
	// AniList calls rewatching REPEATING, and both spellings reach this code.
	"REPEATING": "Rewatching",
	"ALL":       "All",
	"UNTRACKED": "Untracked",
}

// menuActions are the entries that do something rather than show a list. The
// hint is the key that triggers them from the footer.
var menuActions = map[string]FooterAction{
	"CONTINUE_LAST":  {Key: "CONTINUE_LAST", Label: "continue", Hint: "c"},
	"UPDATE":         {Key: "UPDATE", Label: "update", Hint: "u"},
	"REMAP_PROVIDER": {Key: "REMAP_PROVIDER", Label: "remap", Hint: "r"},
	"TRACKER":        {Key: "TRACKER", Label: "tracker", Hint: "t"},
	"PROVIDER":       {Key: "PROVIDER", Label: "provider", Hint: "p"},
}

// SplitMenuOrder divides a MenuOrder setting into the categories that become
// tabs and the actions that become footer entries, keeping the order the user
// wrote in each case.
//
// MenuOrder mixes two kinds of thing that used to sit in one list: lists you
// look at, and things you do. Rather than introduce a second setting and a
// migration to fill it, the one setting keeps its name and is read as both.
// A key belonging to neither group is ignored rather than guessed at.
func SplitMenuOrder(menuOrder string) ([]Tab, []FooterAction) {
	tabs := []Tab{}
	actions := []FooterAction{}
	seenTab := map[string]bool{}
	seenAction := map[string]bool{}

	for _, raw := range strings.Split(menuOrder, ",") {
		key := strings.ToUpper(strings.TrimSpace(raw))
		if key == "" {
			continue
		}
		if label, ok := menuCategoryLabels[key]; ok {
			if !seenTab[label] {
				seenTab[label] = true
				tabs = append(tabs, Tab{Key: key, Label: label})
			}
			continue
		}
		if action, ok := menuActions[key]; ok {
			if !seenAction[key] {
				seenAction[key] = true
				actions = append(actions, action)
			}
		}
	}
	return tabs, actions
}

// attachDetailPane switches the detail pane on when there is something worth
// putting in it.
//
// It is decided from the options rather than configured, because the answer is
// already knowable: a menu whose entries carry cover art in a terminal that can
// draw pictures has a pane worth showing, and one without either does not. A
// setting would only ask the user to tell the program something it can see.
func attachDetailPane(model *Model) {
	if model == nil || len(model.allOptions) == 0 {
		return
	}
	config := GetGlobalConfig()
	if config != nil && !config.ImagePreview {
		return
	}
	withCovers := 0
	for _, option := range model.allOptions {
		if option.Thumbnail != "" {
			withCovers++
		}
	}
	if withCovers == 0 {
		return
	}
	protocol := DetectTerminalImageProtocol()
	if protocol == TerminalImageNone {
		return
	}

	// Sized for a poster in a pane a third of a normal terminal wide. The
	// protocols speak pixels, not cells, so this is deliberately generous --
	// scaling down is cheap and scaling up would blur.
	source := NewPosterSource(protocol, 320, 480)
	model.layout.pane = true
	model.posterOf = source.Poster
}

// attachCategoryTabs gives a menu its tab bar, when the caller supplied the
// categories and a way to load one.
//
// The bar is only worth drawing for more than one category, and only when
// switching actually leads somewhere, so both the list and the loader are
// required. The active category is matched by key rather than assumed to be
// first, or opening "Completed" would draw "Watching" as selected.
func attachCategoryTabs(model *Model, refresh *SelectionRefreshConfig) {
	if model == nil || refresh == nil || refresh.LoadCategory == nil || len(refresh.Categories) < 2 {
		return
	}
	model.layout.tabs = refresh.Categories
	model.layout.activeTab = 0
	for i, tab := range refresh.Categories {
		if strings.EqualFold(tab.Key, refresh.ActiveCategory) {
			model.layout.activeTab = i
			break
		}
	}
	model.loadTab = refresh.LoadCategory
}
