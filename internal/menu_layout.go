package internal

import (
	"fmt"
	"strconv"
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
	// Count is how many entries the category holds. Shown beside the label,
	// because a bare word reads as decoration while a number reads as a view
	// of something that exists.
	Count int
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

	crumbAppStyle  lipgloss.Style
	crumbSepStyle  lipgloss.Style
	crumbViewStyle lipgloss.Style
	ruleStyle      lipgloss.Style
	keyBadgeStyle  lipgloss.Style
	noticeStyle    lipgloss.Style
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

	// No border of its own: the frame draws one rule, under the header, and a
	// second one here would sit at a different width and read as a mistake.
	tabBarStyle = lipgloss.NewStyle()

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

	// A breadcrumb says where you are without spending a line on a title bar.
	crumbAppStyle = lipgloss.NewStyle().
		Foreground(color(palette.Accent)).
		Bold(true)
	crumbSepStyle = lipgloss.NewStyle().Foreground(color(palette.Muted))
	crumbViewStyle = lipgloss.NewStyle().Foreground(color(palette.Muted))

	ruleStyle = lipgloss.NewStyle().Foreground(color(palette.Muted))

	// A key drawn as a badge reads as something to press. The same words in
	// running text read as a sentence about the program.
	keyBadgeStyle = lipgloss.NewStyle().
		Foreground(color(palette.Background)).
		Background(color(palette.Muted)).
		Bold(true).
		Padding(0, 1)

	noticeStyle = lipgloss.NewStyle().
		Foreground(color(palette.Yellow)).
		Align(lipgloss.Center)
}

// Below this the menu cannot be drawn usefully: the list has no room and the
// chrome would take every row. Saying so beats rendering something unreadable
// and leaving the user to guess whether it is broken.
const (
	minMenuWidth  = 36
	minMenuHeight = 10
)

// renderBreadcrumb draws "Otakase › Watching".
func renderBreadcrumb(section string) string {
	crumb := crumbAppStyle.Render(DisplayName)
	if section != "" {
		crumb += crumbSepStyle.Render(" › ") + crumbViewStyle.Render(section)
	}
	return crumb
}

// renderRule draws the line under the header, the width of the content.
func renderRule(width int) string {
	if width <= 0 {
		return ""
	}
	return ruleStyle.Render(strings.Repeat("─", width))
}

// renderKeyHints draws the bar of key badges along the bottom.
func renderKeyHints(hints []keyHint, width int) string {
	if len(hints) == 0 {
		return ""
	}
	parts := make([]string, 0, len(hints))
	for _, hint := range hints {
		parts = append(parts, keyBadgeStyle.Render(hint.Key)+" "+footerTextStyle.Render(hint.Label))
	}
	bar := strings.Join(parts, "  ")
	if width > lipgloss.Width(bar) {
		return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(bar)
	}
	return bar
}

// keyHint is one key and what it does.
type keyHint struct {
	Key   string
	Label string
}

// renderTooSmallNotice replaces the menu when the terminal cannot hold it.
func renderTooSmallNotice(width, height int) string {
	message := fmt.Sprintf("%s needs at least %d \u00d7 %d cells\n"+
		"This terminal is %d \u00d7 %d\nResize to continue.",
		DisplayName, minMenuWidth, minMenuHeight, width, height)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, noticeStyle.Render(message))
}

// renderTabBar draws the categories. It returns an empty string when there are
// no tabs, so a caller can always concatenate the result unconditionally.
func renderTabBar(layout menuLayout, width int) string {
	if !layout.hasTabs() {
		return ""
	}
	parts := make([]string, 0, len(layout.tabs))
	for i, tab := range layout.tabs {
		label := tab.Label
		if tab.Count > 0 {
			label += "  " + strconv.Itoa(tab.Count)
		}
		if i == layout.activeTab {
			parts = append(parts, tabActiveStyle.Render(label))
			continue
		}
		parts = append(parts, tabInactiveStyle.Render(label))
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	// Never narrower than the tabs themselves, or the rule cuts through them.
	if width > lipgloss.Width(bar) {
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
func renderSidePane(title string, meta []string, width, height int) string {
	if width <= 0 {
		return ""
	}
	// Wrap rather than truncate. The pane exists precisely because the row is
	// clipped, so clipping again here would make it useless.
	inner := width - 4
	if inner < 8 {
		return ""
	}
	wrap := lipgloss.NewStyle().Width(inner)

	var b strings.Builder
	if title != "" {
		b.WriteString(wrap.Inherit(paneTitleStyle).Render(title))
		b.WriteString("\n")
	}
	for _, line := range meta {
		if line == "" {
			continue
		}
		b.WriteString("\n")
		b.WriteString(wrap.Inherit(paneMetaStyle).Render(line))
	}
	style := paneBorderStyle.Width(width)
	if height > 0 {
		// Match the list, so the divider runs the height of the menu instead of
		// stopping after a line or two and looking like a mistake.
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

// menuCategoryLabels maps a MenuOrder key to a tab.
//
// A key belongs here only if getEntriesByCategory can return a list for it.
// Anything else looks like a category and is not one: UNTRACKED reads as a
// list but runs a search-and-watch flow, and as a tab it produced an empty
// list with no way to reach the thing it actually does.
//
// The labels are deliberately shorter than the menu entries they replace. A tab
// bar is horizontal, and every column spent on "Currently Watching" is one the
// next category does not get.
var menuCategoryLabels = map[string]string{
	"CURRENT":    "Watching",
	"PLANNING":   "Planning",
	"COMPLETED":  "Completed",
	"PAUSED":     "On Hold",
	"DROPPED":    "Dropped",
	"REWATCHING": "Rewatching",
	"ALL":        "All",
}

// canonicalCategoryKey maps a spelling onto the one the list filter answers to.
// AniList calls rewatching REPEATING and the documented config key is
// REWATCHING; only the latter is a case in getEntriesByCategory, so a tab keyed
// REPEATING would show nothing.
func canonicalCategoryKey(key string) string {
	if key == "REPEATING" {
		return "REWATCHING"
	}
	return key
}

// menuActions are the entries that do something rather than show a list. The
// hint is the key that triggers them from the footer.
var menuActions = map[string]FooterAction{
	"UNTRACKED":      {Key: "UNTRACKED", Label: "untracked", Hint: "n"},
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
		key = canonicalCategoryKey(key)
		if label, ok := menuCategoryLabels[key]; ok {
			if !seenTab[key] {
				seenTab[key] = true
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

// attachDetailPane switches on the column beside the list.
//
// It shows text only. Posters were tried here and removed: a terminal image
// protocol draws outside the text flow, occupies no cells, and is not erased by
// a repaint, so Bubble Tea -- which repaints by counting lines and moving the
// cursor up -- miscounted every frame, stacked them down the screen, and left
// every poster ever drawn on screen at once. Making that work needs the
// alternate screen, an explicit delete-images sequence each frame, and the
// terminal's cell size to reserve the space. rofi already draws the poster grid
// properly, so the terminal has not been left without one.
func attachDetailPane(model *Model) {
	if model == nil || len(model.allOptions) == 0 {
		return
	}
	model.layout.pane = true
	model.metaOf = paneDetails
	Log(fmt.Sprintf("Detail pane: on, %d entries", len(model.allOptions)))
}

// paneDetails is what is known about an entry beyond its title. The label
// already carries the counts and the airing note, so the pane repeats neither;
// it shows the parts a truncated row cannot.
func paneDetails(option SelectionOption) []string {
	details := []string{}
	if option.HasNewEpisodes {
		details = append(details, "new episode")
	}
	// The label is the title plus its counts and airing note. Showing it whole
	// beneath the title repeats the title, and at pane width the repetition is
	// all that fits -- so only the part the title does not already say is kept.
	if extra := labelBeyondTitle(option); extra != "" {
		details = append(details, extra)
	}
	return details
}

// labelBeyondTitle strips the leading title from a label, returning what the
// title does not already convey.
func labelBeyondTitle(option SelectionOption) string {
	label := strings.TrimSpace(option.Label)
	title := strings.TrimSpace(option.Title)
	if label == "" || label == title {
		return ""
	}
	if title != "" && strings.HasPrefix(label, title) {
		rest := strings.TrimSpace(strings.TrimPrefix(label, title))
		return strings.TrimSpace(strings.TrimPrefix(rest, "·"))
	}
	return label
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
		if refresh != nil && len(refresh.Categories) < 2 {
			Log(fmt.Sprintf("Category tabs: off, %d category in MenuOrder", len(refresh.Categories)))
		}
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
	Log(fmt.Sprintf("Category tabs: on, %d tabs, %q active", len(refresh.Categories), model.layout.activeTabKey()))
}
