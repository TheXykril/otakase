package internal

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/theme"
)

// "Otakase: The Movie" is a short credits roll over a plain background: the
// logo, then the viewer's own stats scrolling past like the end of a film.
// It is found by searching for "otakase" when adding a show, and plays on its
// own when the viewer passes an episode milestone (milestones.go).
//
// There is no video file. mpv draws a solid colour through lavfi and the
// credits are an ASS subtitle written for the occasion, so nothing ships with
// the binary and the text always fits whatever mpv window it lands in.

const (
	otakaseMovieKey   = "otakase-movie"
	otakaseMovieTitle = "Otakase: The Movie"

	// The subtitle is laid out on a 1080p canvas; mpv scales it to the window.
	movieWidth  = 1920
	movieHeight = 1080

	// movieLogoSeconds is how long the logo holds the screen before the roll.
	movieLogoSeconds = 6
	// movieScrollSpeed is how fast the credits rise, in canvas pixels a second.
	movieScrollSpeed = 85
	// movieEndCardSeconds is the closing card after the roll has gone.
	movieEndCardSeconds = 5
)

// otakaseLogoGlyphs are the letters of the logo, six rows each.
var otakaseLogoGlyphs = map[rune][6]string{
	'O': {" ██████╗ ", "██╔═══██╗", "██║   ██║", "██║   ██║", "╚██████╔╝", " ╚═════╝ "},
	'T': {"████████╗", "╚══██╔══╝", "   ██║   ", "   ██║   ", "   ██║   ", "   ╚═╝   "},
	'A': {" █████╗ ", "██╔══██╗", "███████║", "██╔══██║", "██║  ██║", "╚═╝  ╚═╝"},
	'K': {"██╗  ██╗", "██║ ██╔╝", "█████╔╝ ", "██╔═██╗ ", "██║  ██╗", "╚═╝  ╚═╝"},
	'S': {"███████╗", "██╔════╝", "███████╗", "╚════██║", "███████║", "╚══════╝"},
	'E': {"███████╗", "██╔════╝", "█████╗  ", "██╔══╝  ", "███████╗", "╚══════╝"},
}

// otakaseLogo is the word OTAKASE in block letters, one string per row.
func otakaseLogo() []string {
	rows := make([]string, 6)
	for _, letter := range "OTAKASE" {
		glyph := otakaseLogoGlyphs[letter]
		for i := range rows {
			rows[i] += glyph[i]
		}
	}
	return rows
}

// isOtakaseMovieQuery reports whether a search is the one that finds the movie.
func isOtakaseMovieQuery(query string) bool {
	return strings.EqualFold(strings.TrimSpace(query), AppName)
}

// otakaseMovieOption is the search result the movie hides behind.
func otakaseMovieOption() SelectionOption {
	return SelectionOption{
		Key:   otakaseMovieKey,
		Label: otakaseMovieTitle + " (2026) · 1 episode · 10/10",
		Title: otakaseMovieTitle,
	}
}

// creditLine is one entry of the roll: a small role above a large name.
type creditLine struct {
	Role string
	Name string
}

// movieCredits turns the viewer's stats into the roll. milestone is the
// episode count being celebrated, 0 when the movie was found by searching.
func movieCredits(stats watchStats, viewer string, milestone int, now time.Time) []creditLine {
	if strings.TrimSpace(viewer) == "" {
		viewer = "You"
	}
	credits := []creditLine{{Role: "Starring", Name: viewer}}
	if milestone > 0 {
		credits = append(credits, creditLine{Role: "Achievement", Name: formatStatsCount(milestone) + " episodes watched"})
	}

	if totals := stats.AniList; totals != nil {
		credits = append(credits,
			creditLine{"Episodes watched", formatStatsCount(totals.EpisodesWatched)},
			creditLine{"Time watched", formatWatchMinutes(totals.MinutesWatched)},
			creditLine{"Anime on your list", formatStatsCount(totals.Count)},
		)
		if totals.MeanScore > 0 {
			credits = append(credits, creditLine{"Mean score", strconv.FormatFloat(totals.MeanScore, 'f', 1, 64)})
		}
		if len(totals.Genres) > 0 {
			names := make([]string, 0, 3)
			for _, genre := range totals.Genres {
				if len(names) == 3 {
					break
				}
				names = append(names, genre.Name)
			}
			credits = append(credits, creditLine{"Top genres", strings.Join(names, " · ")})
		}
		if title, episodes := statsMostBinged(stats.Activities); title != "" {
			credits = append(credits, creditLine{"Most binged lately", fmt.Sprintf("%s (%s)", title, formatStatsEpisodes(episodes))})
		}
		if _, longest := statsStreaks(stats.Activities, now); longest > 1 {
			credits = append(credits, creditLine{"Longest streak", formatStatsDays(longest)})
		}
	}
	if totals := stats.MyAnimeList; totals != nil {
		credits = append(credits,
			creditLine{"Episodes watched", formatStatsCount(totals.Episodes)},
			creditLine{"Time watched", formatWatchMinutes(int(math.Round(totals.DaysWatched * 24 * 60)))},
			creditLine{"Anime on your list", formatStatsCount(totals.Items)},
			creditLine{"Completed", formatStatsCount(totals.Completed)},
		)
		if totals.MeanScore > 0 {
			credits = append(credits, creditLine{"Mean score", strconv.FormatFloat(totals.MeanScore, 'f', 2, 64)})
		}
		if totals.Rewatched > 0 {
			credits = append(credits, creditLine{"Rewatches", formatStatsCount(totals.Rewatched)})
		}
	}

	return append(credits,
		creditLine{"Filmed on location", "Your couch"},
		creditLine{"Catering", "Whatever was in the fridge"},
		creditLine{"Skipped openings", "Too many to count"},
		creditLine{"", "Thanks for watching."},
	)
}

// assColor writes "#rrggbb" the way ASS wants it: &HBBGGRR&.
func assColor(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return "&HFFFFFF&"
	}
	return "&H" + strings.ToUpper(hex[4:6]+hex[2:4]+hex[0:2]) + "&"
}

// assText escapes text for an ASS event: braces open override blocks, a
// backslash starts a tag, and runs of spaces must stay hard to keep the logo's
// columns where they belong.
func assText(text string) string {
	text = strings.NewReplacer(`\`, `\\`, "{", "(", "}", ")").Replace(text)
	return strings.ReplaceAll(text, " ", `\h`)
}

// assTime writes seconds as ASS's h:mm:ss.cc.
func assTime(seconds float64) string {
	centis := int(math.Round(seconds * 100))
	return fmt.Sprintf("%d:%02d:%02d.%02d", centis/360000, centis/6000%60, centis/100%60, centis%100)
}

// movieScript is the whole credits roll as an ASS file, and how long the
// video must run to show all of it.
func movieScript(credits []creditLine, subtitle string, palette theme.Palette, font string) (string, float64) {
	const (
		logoSize = 34
		roleSize = 34
		nameSize = 58
		// lineGap is the space under each name, as a share of its size.
		lineGap = 1.9
	)
	accent, text, muted := assColor(palette.Accent), assColor(palette.BrightForeground), assColor(palette.Muted)

	var script strings.Builder
	fmt.Fprintf(&script, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 2\nScaledBorderAndShadow: yes\n\n", movieWidth, movieHeight)
	script.WriteString("[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(&script, "Style: Logo,%s,%d,%s,%s,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,5,0,0,0,1\n", font, logoSize, accent, accent)
	fmt.Fprintf(&script, "Style: Credit,%s,%d,%s,%s,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,8,0,0,0,1\n\n", font, nameSize, text, text)
	script.WriteString("[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	event := func(start, end float64, style, text string) {
		fmt.Fprintf(&script, "Dialogue: 0,%s,%s,%s,,0,0,0,,%s\n", assTime(start), assTime(end), style, text)
	}

	logo := make([]string, 0, 6)
	for _, row := range otakaseLogo() {
		logo = append(logo, assText(row))
	}
	logoBlock := strings.Join(logo, `\N`)

	// The opening: the logo, with the subtitle beneath it.
	event(0, movieLogoSeconds, "Logo", `{\fad(1200,800)}`+logoBlock)
	event(1.2, movieLogoSeconds, "Credit", fmt.Sprintf(`{\an5\pos(%d,%d)\fs%d\c%s\fad(800,800)}%s`, movieWidth/2, movieHeight/2+170, roleSize, muted, assText(subtitle)))

	// The roll: one block rising from below the frame until it has left the
	// top. Its height is estimated from the font sizes, which is all libass
	// needs to know for \move to carry it fully off screen.
	var roll strings.Builder
	height := 0.0
	for i, credit := range credits {
		if i > 0 {
			roll.WriteString(`\N`)
		}
		if credit.Role != "" {
			fmt.Fprintf(&roll, `{\fs%d\c%s}%s\N`, roleSize, muted, assText(credit.Role))
			height += roleSize * 1.25
		}
		fmt.Fprintf(&roll, `{\fs%d\c%s}%s\N`, nameSize, text, assText(credit.Name))
		height += nameSize * lineGap
	}
	travel := float64(movieHeight) + height + 80
	rollSeconds := travel / movieScrollSpeed
	rollStart := float64(movieLogoSeconds)
	rollEnd := rollStart + rollSeconds
	event(rollStart, rollEnd, "Credit", fmt.Sprintf(`{\move(%d,%d,%d,%d)}%s`,
		movieWidth/2, movieHeight+40, movieWidth/2, -int(height)-40, roll.String()))

	// The closing card: the logo once more, smaller, and a goodbye.
	end := rollEnd + movieEndCardSeconds
	event(rollEnd, end, "Logo", fmt.Sprintf(`{\fs22\pos(%d,%d)\fad(800,1000)}%s`, movieWidth/2, movieHeight/2-40, logoBlock))
	event(rollEnd+0.6, end, "Credit", fmt.Sprintf(`{\an5\pos(%d,%d)\fs%d\c%s\fad(800,1000)}%s`, movieWidth/2, movieHeight/2+120, roleSize+6, text, assText("See you next episode.")))
	return script.String(), end
}

// movieViewerName is the name the roll stars: the tracker account when there
// is one, otherwise whoever is logged in to the computer.
func movieViewerName(user *User) string {
	if user != nil && strings.TrimSpace(user.Username) != "" {
		return user.Username
	}
	for _, key := range []string{"USER", "USERNAME"} {
		if name := strings.TrimSpace(os.Getenv(key)); name != "" {
			return name
		}
	}
	return ""
}

// PlayOtakaseMovie plays the credits roll and returns once it is closed.
// milestone is the episode count being celebrated, or 0.
func PlayOtakaseMovie(config *Config, user *User, milestone int) {
	palette := theme.Builtin()
	if config == nil || !config.RofiSelection {
		printOtakaseLogo(palette)
	}

	var stats watchStats
	if statsTrackerKey(config) != TrackingRemoteNone {
		fetched, err := fetchWatchStats(config)
		if err != nil {
			Log(fmt.Sprintf("Otakase movie: no stats: %v", err))
		} else {
			stats = fetched
		}
	}

	subtitle := "presents"
	if milestone > 0 {
		subtitle = formatStatsCount(milestone) + " episodes. Roll the credits."
	}
	credits := movieCredits(stats, movieViewerName(user), milestone, time.Now())
	if err := playCreditsInMPV(credits, subtitle, palette); err != nil {
		Log(fmt.Sprintf("Otakase movie: %v", err))
		if config == nil || !config.RofiSelection {
			printMovieCredits(credits)
		}
	}
}

// playCreditsInMPV writes the script and plays it over a plain background,
// waiting for the window to close.
func playCreditsInMPV(credits []creditLine, subtitle string, palette theme.Palette) error {
	binary, err := findMPV()
	if err != nil {
		return fmt.Errorf("mpv not found: %w", err)
	}
	dir, err := os.MkdirTemp("", AppName+"-movie-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	script, seconds := movieScript(credits, subtitle, palette, theme.MonospaceFont())
	scriptPath := filepath.Join(dir, "credits.ass")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return err
	}

	background := strings.TrimPrefix(palette.Background, "#")
	source := fmt.Sprintf("av://lavfi:color=c=0x%s:s=%dx%d:r=30:d=%.1f", background, movieWidth, movieHeight, seconds+0.5)
	args := []string{
		"--no-terminal", "--force-window=yes", "--keep-open=no",
		"--no-resume-playback", "--save-position-on-quit=no",
		"--osd-level=0", "--sub-ass-override=no", "--sub-visibility=yes",
		"--sub-file=" + scriptPath,
		"--title=" + otakaseMovieTitle, "--force-media-title=" + otakaseMovieTitle,
		source,
	}
	return exec.Command(binary, args...).Run()
}

// printOtakaseLogo prints the logo in the terminal, in the accent colour.
func printOtakaseLogo(palette theme.Palette) {
	r, g, b := hexRGB(palette.Accent)
	fmt.Println()
	for _, row := range otakaseLogo() {
		fmt.Printf("  \033[38;2;%d;%d;%dm%s\033[0m\n", r, g, b, row)
	}
	fmt.Println()
}

// printMovieCredits is the roll for a terminal without mpv to show it in.
func printMovieCredits(credits []creditLine) {
	for _, credit := range credits {
		if credit.Role != "" {
			fmt.Printf("  %s: %s\n", credit.Role, credit.Name)
		} else {
			fmt.Printf("  %s\n", credit.Name)
		}
	}
	fmt.Println()
}

// hexRGB splits "#rrggbb" into its channels, white when it is not one.
func hexRGB(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil || len(hex) != 6 {
		return 255, 255, 255
	}
	return int(value >> 16 & 0xff), int(value >> 8 & 0xff), int(value & 0xff)
}
