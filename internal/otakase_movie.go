package internal

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/theme"
)

// "Otakase: The Movie" is a short film made of the viewer's own stats: a
// title card, credits that roll like the end of a film, and a "to be
// continued" to close. It is found by searching for "otakase" when adding a
// show, and plays on its own when the viewer passes an episode milestone
// (milestones.go).
//
// There is no video file. mpv draws the background through lavfi, the picture
// is an ASS subtitle written for the occasion, and the music is synthesised
// (otakase_movie_music.go), so nothing ships with the binary and the text
// always fits whatever window it lands in.

const (
	otakaseMovieKey   = "otakase-movie"
	otakaseMovieTitle = "Otakase: The Movie"

	// The subtitle is laid out on a 1080p canvas; mpv scales it to the window.
	movieWidth  = 1920
	movieHeight = 1080
	// movieBars is the height of each letterbox bar.
	movieBars = 120

	// movieTitleSeconds is when the credits start, after the title card.
	movieTitleSeconds = 11
	// movieScrollSpeed is how fast the credits rise, in canvas pixels a second.
	movieScrollSpeed = 80
	// movieEndCardSeconds is the closing card after the roll has gone.
	movieEndCardSeconds = 7

	// movieValueRunes keeps a long show title on its half of the screen.
	movieValueRunes = 34
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

// creditLine is one row of the roll: a role and its name side by side, or a
// section heading on its own when Heading is set.
type creditLine struct {
	Heading string
	Role    string
	Name    string
}

// movieCredits turns the viewer's stats into the roll. milestone is the
// episode count being celebrated, 0 when the movie was found by searching.
func movieCredits(stats watchStats, viewer string, milestone int, now time.Time) []creditLine {
	if strings.TrimSpace(viewer) == "" {
		viewer = "You"
	}
	credits := []creditLine{{Heading: "The cast"}, {Role: "Starring", Name: viewer}}
	if milestone > 0 {
		credits = append(credits, creditLine{Role: "Achievement", Name: formatStatsCount(milestone) + " episodes"})
	}

	numbers := []creditLine{}
	if totals := stats.AniList; totals != nil {
		numbers = append(numbers,
			creditLine{Role: "Episodes watched", Name: formatStatsCount(totals.EpisodesWatched)},
			creditLine{Role: "Time watched", Name: formatWatchMinutes(totals.MinutesWatched)},
			creditLine{Role: "Anime on your list", Name: formatStatsCount(totals.Count)},
		)
		if totals.MeanScore > 0 {
			numbers = append(numbers, creditLine{Role: "Mean score", Name: strconv.FormatFloat(totals.MeanScore, 'f', 1, 64)})
		}
		if len(totals.Genres) > 0 {
			names := make([]string, 0, 3)
			for _, genre := range totals.Genres {
				if len(names) == 3 {
					break
				}
				names = append(names, genre.Name)
			}
			numbers = append(numbers, creditLine{Role: "Top genres", Name: strings.Join(names, " · ")})
		}
		if title, episodes := statsMostBinged(stats.Activities); title != "" {
			numbers = append(numbers,
				creditLine{Role: "Most binged lately", Name: title},
				creditLine{Role: "", Name: formatStatsEpisodes(episodes)},
			)
		}
		if _, longest := statsStreaks(stats.Activities, now); longest > 1 {
			numbers = append(numbers, creditLine{Role: "Longest streak", Name: formatStatsDays(longest)})
		}
	}
	if totals := stats.MyAnimeList; totals != nil {
		numbers = append(numbers,
			creditLine{Role: "Episodes watched", Name: formatStatsCount(totals.Episodes)},
			creditLine{Role: "Time watched", Name: formatWatchMinutes(int(math.Round(totals.DaysWatched * 24 * 60)))},
			creditLine{Role: "Anime on your list", Name: formatStatsCount(totals.Items)},
			creditLine{Role: "Completed", Name: formatStatsCount(totals.Completed)},
		)
		if totals.MeanScore > 0 {
			numbers = append(numbers, creditLine{Role: "Mean score", Name: strconv.FormatFloat(totals.MeanScore, 'f', 2, 64)})
		}
		if totals.Rewatched > 0 {
			numbers = append(numbers, creditLine{Role: "Rewatches", Name: formatStatsCount(totals.Rewatched)})
		}
	}
	if len(numbers) > 0 {
		credits = append(credits, creditLine{Heading: "The numbers"})
		credits = append(credits, numbers...)
	}

	return append(credits,
		creditLine{Heading: "The crew"},
		creditLine{Role: "Filmed on location", Name: "Your couch"},
		creditLine{Role: "Catering", Name: "Whatever was in the fridge"},
		creditLine{Role: "Openings skipped", Name: "Too many to count"},
		creditLine{Role: "Sleep schedule", Name: "Lost in production"},
		creditLine{Role: "Special thanks", Name: "Every fansubber, ever"},
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

// ellipsize shortens text to at most n runes, ending in an ellipsis.
func ellipsize(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return strings.TrimSpace(string(runes[:n-1])) + "…"
}

// movieOpening is what the title card says.
type movieOpening struct {
	// Presenter is the name on the "presents" line.
	Presenter string
	// Tagline sits under the logo: "THE MOVIE", or the milestone.
	Tagline string
}

// movieScript is the whole film as an ASS file, and how long the video must
// run to show all of it.
func movieScript(credits []creditLine, opening movieOpening, palette theme.Palette, logoFont, textFont string) (string, float64) {
	const (
		roleSize    = 30
		nameSize    = 44
		headingSize = 30
		rowHeight   = 74
		// A heading takes the room of two rows: space above, then itself.
		headingHeight = 2 * rowHeight
		// The gutter either side of the centre line.
		gutter = 36
	)
	accent, text, muted := assColor(palette.Accent), assColor(palette.BrightForeground), assColor(palette.Muted)
	centreX, centreY := movieWidth/2, movieHeight/2

	var script strings.Builder
	fmt.Fprintf(&script, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 2\nScaledBorderAndShadow: yes\n\n", movieWidth, movieHeight)
	script.WriteString("[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(&script, "Style: Logo,%s,34,%s,%s,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,5,0,0,0,1\n", logoFont, accent, accent)
	fmt.Fprintf(&script, "Style: Text,%s,%d,%s,%s,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,5,0,0,0,1\n", textFont, nameSize, text, text)
	script.WriteString("Style: Draw,Arial,20,&H00FFFFFF,&H00FFFFFF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,7,0,0,0,1\n\n")
	script.WriteString("[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	event := func(layer int, start, end float64, style, body string) {
		fmt.Fprintf(&script, "Dialogue: %d,%s,%s,%s,,0,0,0,,%s\n", layer, assTime(start), assTime(end), style, body)
	}

	logo := make([]string, 0, 6)
	for _, row := range otakaseLogo() {
		logo = append(logo, assText(row))
	}
	logoBlock := strings.Join(logo, `\N`)

	// The roll's length decides the film's.
	rollHeight := 0
	for _, credit := range credits {
		if credit.Heading != "" {
			rollHeight += headingHeight
		} else {
			rollHeight += rowHeight
		}
	}
	rollStart := float64(movieTitleSeconds)
	rollSeconds := float64(movieHeight+rollHeight+100) / movieScrollSpeed
	rollEnd := rollStart + rollSeconds
	end := rollEnd + movieEndCardSeconds

	// Title card: who presents, then the logo with a soft glow behind it, an
	// accent rule drawn out beneath, and the tagline spaced wide.
	presenter := strings.ToUpper(opening.Presenter)
	if presenter == "" {
		presenter = "YOU"
	}
	event(2, 0.8, 4.2, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs%d\fsp6\c%s\fad(700,600)}%s`, centreX, centreY, roleSize, muted, assText(presenter+"  presents")))
	event(1, 4.6, movieTitleSeconds-0.4, "Logo", fmt.Sprintf(`{\pos(%d,%d)\blur14\alpha&H70&\fad(1400,700)}%s`, centreX, centreY-40, logoBlock))
	event(2, 4.6, movieTitleSeconds-0.4, "Logo", fmt.Sprintf(`{\pos(%d,%d)\fad(1400,700)}%s`, centreX, centreY-40, logoBlock))
	event(2, 5.8, movieTitleSeconds-0.4, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s\fscx0\t(0,900,\fscx100)\fad(0,700)\p1}m 0 0 l 1000 0 1000 4 0 4{\p0}`, centreX, centreY+95, accent))
	event(2, 6.4, movieTitleSeconds-0.4, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs%d\fsp18\c%s\fad(800,700)}%s`, centreX, centreY+150, headingSize+4, text, assText(strings.ToUpper(opening.Tagline))))

	// The roll: each row is its own event moving at the same speed, so roles
	// right-align against the centre line and names left-align after it, the
	// way film credits sit.
	y := movieHeight + 60
	for _, credit := range credits {
		if credit.Heading != "" {
			y += rowHeight
			heading := strings.ToUpper(credit.Heading)
			event(2, rollStart, rollEnd, "Text", fmt.Sprintf(`{\move(%d,%d,%d,%d)\fs%d\fsp14\c%s}%s`,
				centreX, y, centreX, y-movieHeight-rollHeight-200, headingSize, accent, assText(heading)))
			// A short rule either side of the heading.
			half := len([]rune(heading))*(headingSize*6/10+14)/2 + 40
			for _, side := range []int{-1, 1} {
				x := centreX + side*(half+60)
				event(2, rollStart, rollEnd, "Draw", fmt.Sprintf(`{\an5\move(%d,%d,%d,%d)\c%s\alpha&H60&\p1}m 0 0 l 120 0 120 2 0 2{\p0}`,
					x, y, x, y-movieHeight-rollHeight-200, accent))
			}
			y += rowHeight
			continue
		}
		top := y - movieHeight - rollHeight - 200
		if credit.Role != "" {
			event(2, rollStart, rollEnd, "Text", fmt.Sprintf(`{\an6\move(%d,%d,%d,%d)\fs%d\c%s}%s`,
				centreX-gutter, y, centreX-gutter, top, roleSize, muted, assText(ellipsize(credit.Role, movieValueRunes))))
		}
		event(2, rollStart, rollEnd, "Text", fmt.Sprintf(`{\an4\move(%d,%d,%d,%d)\fs%d\c%s}%s`,
			centreX+gutter, y, centreX+gutter, top, nameSize, text, assText(ellipsize(credit.Name, movieValueRunes))))
		y += rowHeight
	}

	// Closing card: the logo once more, small, and a "to be continued" arrow
	// sliding in from the left.
	event(1, rollEnd, end, "Logo", fmt.Sprintf(`{\pos(%d,%d)\fs20\blur10\alpha&H80&\fad(900,900)}%s`, centreX, centreY-60, logoBlock))
	event(2, rollEnd, end, "Logo", fmt.Sprintf(`{\pos(%d,%d)\fs20\fad(900,900)}%s`, centreX, centreY-60, logoBlock))
	arrowY := movieHeight - movieBars - 90
	event(3, rollEnd+1.4, end, "Draw", fmt.Sprintf(`{\an7\move(%d,%d,%d,%d,0,500)\c%s\fad(0,900)\p1}m 0 0 l 520 0 600 40 520 80 0 80 40 40{\p0}`,
		-640, arrowY-40, 90, arrowY-40, accent))
	event(4, rollEnd+1.4, end, "Text", fmt.Sprintf(`{\an4\move(%d,%d,%d,%d,0,500)\fs%d\b1\fsp4\c%s\fad(0,900)}%s`,
		-560, arrowY, 170, arrowY, headingSize, assColor(palette.DarkBackground), assText("TO BE CONTINUED")))

	// Petals drifting down through the whole film, behind everything.
	petals := rand.New(rand.NewPCG(4, 12))
	for i := 0; i < 26; i++ {
		start := petals.Float64() * (end - 4)
		life := 11 + petals.Float64()*8
		x := petals.IntN(movieWidth)
		drift := petals.IntN(500) - 150
		scale := 60 + petals.IntN(80)
		spin := petals.IntN(720) - 360
		event(0, start, math.Min(end, start+life), "Draw", fmt.Sprintf(`{\an5\move(%d,%d,%d,%d)\fscx%d\fscy%d\frz%d\t(\frz%d)\c%s\alpha&H%02X&\blur1.5\fad(800,800)\p1}m 0 0 b 10 -14 26 -10 30 0 b 26 10 10 14 0 0{\p0}`,
			x, movieBars-40, x+drift, movieHeight-movieBars+40, scale, scale, petals.IntN(360), spin, accent, 0x70+petals.IntN(0x50)))
	}

	// Letterbox bars, over everything, and a fade from black at the very start.
	bar := fmt.Sprintf(`m 0 0 l %d 0 %d %d 0 %d`, movieWidth, movieWidth, movieBars, movieBars)
	event(9, 0, end, "Draw", `{\pos(0,0)\c&H000000&\p1}`+bar+`{\p0}`)
	event(9, 0, end, "Draw", fmt.Sprintf(`{\pos(0,%d)\c&H000000&\p1}%s{\p0}`, movieHeight-movieBars, bar))
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

// PlayOtakaseMovie plays the film and returns once it is closed. milestone is
// the episode count being celebrated, or 0.
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

	viewer := movieViewerName(user)
	opening := movieOpening{Presenter: viewer, Tagline: "The movie"}
	if milestone > 0 {
		opening.Tagline = formatStatsCount(milestone) + " episodes"
	}
	credits := movieCredits(stats, viewer, milestone, time.Now())
	if err := playMovieFilm(credits, opening, palette); err != nil {
		Log(fmt.Sprintf("Otakase movie: %v", err))
		if config == nil || !config.RofiSelection {
			printMovieCredits(credits)
		}
	}
}

// playMovieFilm writes the picture and the music and plays them, waiting for
// the window to close.
func playMovieFilm(credits []creditLine, opening movieOpening, palette theme.Palette) error {
	binary, err := findMPV()
	if err != nil {
		return fmt.Errorf("mpv not found: %w", err)
	}
	dir, err := os.MkdirTemp("", AppName+"-movie-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	script, seconds := movieScript(credits, opening, palette, theme.MonospaceFont(), "sans-serif")
	scriptPath := filepath.Join(dir, "credits.ass")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return err
	}
	musicPath := filepath.Join(dir, "music.wav")
	if err := os.WriteFile(musicPath, movieSoundtrack(seconds, movieTitleSeconds), 0o600); err != nil {
		return err
	}

	args := []string{
		"--no-terminal", "--force-window=yes", "--keep-open=no",
		"--no-resume-playback", "--save-position-on-quit=no",
		"--osd-level=0", "--sub-ass-override=no", "--sub-visibility=yes",
		"--sub-file=" + scriptPath, "--audio-file=" + musicPath,
		"--title=" + otakaseMovieTitle, "--force-media-title=" + otakaseMovieTitle,
		movieBackground(palette, seconds),
	}
	return exec.Command(binary, args...).Run()
}

// movieBackground is the lavfi source mpv plays: the palette's background with
// a faint film grain and a vignette, for seconds.
func movieBackground(palette theme.Palette, seconds float64) string {
	background := strings.TrimPrefix(palette.Background, "#")
	return fmt.Sprintf("av://lavfi:color=c=0x%s:s=%dx%d:r=30:d=%.1f,noise=alls=7:allf=t,vignette=PI/4",
		background, movieWidth, movieHeight, seconds+0.5)
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
		switch {
		case credit.Heading != "":
			fmt.Printf("\n  %s\n", strings.ToUpper(credit.Heading))
		case credit.Role != "":
			fmt.Printf("  %s: %s\n", credit.Role, credit.Name)
		default:
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
