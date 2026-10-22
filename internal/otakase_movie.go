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

// "Otakase: The Movie" is a short film about the viewer: their numbers
// counted up, a conversation about them in the manner of a long game's
// ending, a wall of their favourite shows, the crew, and the red seal anime
// end cards close on. It is found by searching for "otakase" when adding a
// show, and plays on its own when the viewer passes an episode milestone
// (milestones.go).
//
// There is no video file. mpv draws the background and lays the covers over
// it with lavfi, the rest of the picture is an ASS subtitle written for the
// occasion (otakase_movie_script.go, saying what otakase_movie_story.go
// writes), and the music is synthesised (otakase_movie_music.go), so nothing
// ships with the binary and the text always fits whatever window it lands in.

const (
	otakaseMovieKey   = "otakase-movie"
	otakaseMovieTitle = "Otakase: The Movie"

	// The subtitle is laid out on a 1080p canvas; mpv scales it to the window.
	movieWidth  = 1920
	movieHeight = 1080
	// movieBars is the height of each letterbox bar.
	movieBars = 120

	// movieWallSeconds is how long the favourites wall holds.
	movieWallSeconds = 8
	// movieEndCardSeconds is the closing card after the roll has gone.
	movieEndCardSeconds = 8

	// movieValueRunes keeps a long show title on its half of the screen.
	movieValueRunes = 34

	// The wall's covers, in canvas pixels.
	movieCoverWidth  = 250
	movieCoverHeight = 354
	movieCoverGap    = 44
	movieCoverTop    = 300
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

// movieCoverX is the left edge of cover i of n on the wall.
func movieCoverX(i, n int) int {
	width := n*movieCoverWidth + (n-1)*movieCoverGap
	return (movieWidth-width)/2 + i*(movieCoverWidth+movieCoverGap)
}

// moviePicture is one image the film shows, placed and timed by the script:
// the box it fills on the canvas and when it fades in and out.
type moviePicture struct {
	X, Y, Width, Height int
	In, Out             float64
}

// movieFilterGraph is the lavfi graph the picture is built on: background in
// on the first video label, one image per later label, the finished frame
// out on output. The background gets a faint grain and a vignette; each
// image is cut to its box and fades in and out on its own beat.
func movieFilterGraph(background string, images []string, output string, pictures []moviePicture) string {
	var graph strings.Builder
	fmt.Fprintf(&graph, "%snoise=alls=7:allf=t,vignette=PI/4[base0]", background)
	images = images[:min(len(images), len(pictures))]
	for i, image := range images {
		picture := pictures[i]
		fmt.Fprintf(&graph, ";%sscale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,format=yuva420p,loop=loop=-1:size=1,setpts=N/30/TB,trim=duration=%.2f,fade=t=in:st=%.2f:d=0.4:alpha=1,fade=t=out:st=%.2f:d=0.6:alpha=1[image%d]",
			image, picture.Width, picture.Height, picture.Width, picture.Height, picture.Out+0.5, picture.In, picture.Out-0.6, i)
		next := fmt.Sprintf("[base%d]", i+1)
		if i == len(images)-1 {
			next = output
		}
		fmt.Fprintf(&graph, ";[base%d][image%d]overlay=x=%d:y=%d:eof_action=pass:enable='between(t,%.2f,%.2f)'%s",
			i, i, picture.X, picture.Y, picture.In, picture.Out, next)
	}
	if len(images) == 0 {
		fmt.Fprintf(&graph, ";[base0]null%s", output)
	}
	return graph.String()
}

// movieViewerName is the name the roll stars: the tracker account when there
// is one, otherwise whoever is logged in to the computer.
func movieViewerName(user *User, extras movieExtras) string {
	if strings.TrimSpace(extras.Name) != "" {
		return extras.Name
	}
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
	terminal := config == nil || !config.RofiSelection
	if terminal {
		printOtakaseLogo(palette)
	}

	var stats watchStats
	var extras movieExtras
	if statsTrackerKey(config) != TrackingRemoteNone {
		endBusy := BeginBusy(config, "Developing the film")
		fetched, err := fetchWatchStats(config)
		if err != nil {
			Log(fmt.Sprintf("Otakase movie: no stats: %v", err))
		} else {
			stats = fetched
		}
		if extras, err = fetchMovieExtras(config); err != nil {
			Log(fmt.Sprintf("Otakase movie: no extras: %v", err))
		}
		endBusy()
	}

	facts := gatherMovieFacts(stats, extras, movieViewerName(user, extras), milestone, time.Now())
	tagline := "The movie"
	if milestone > 0 {
		tagline = formatStatsCount(milestone) + " episodes"
	}
	// A new draw of wordings every time it plays.
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1))
	if err := playMovieFilm(facts, tagline, extras.Favourites, palette, rng); err != nil {
		Log(fmt.Sprintf("Otakase movie: %v", err))
		if terminal {
			printMovieStory(facts, rng)
		}
	}
}

// playMovieFilm writes the picture and the music and plays them, waiting for
// the window to close.
func playMovieFilm(facts movieFacts, tagline string, wall []movieFavourite, palette theme.Palette, rng *rand.Rand) error {
	binary, err := findMPV()
	if err != nil {
		return fmt.Errorf("mpv not found: %w", err)
	}
	dir, err := os.MkdirTemp("", AppName+"-movie-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	script, timeline := movieScript(facts, tagline, wall, palette, theme.MonospaceFont(), "sans-serif", rng)
	scriptPath := filepath.Join(dir, "credits.ass")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return err
	}
	musicPath := filepath.Join(dir, "music.wav")
	if err := os.WriteFile(musicPath, movieSoundtrack(timeline.Score), 0o600); err != nil {
		return err
	}

	// mpv numbers tracks from 1: the background is vid1, each cover after it.
	args := []string{
		"--no-terminal", "--force-window=yes", "--keep-open=no",
		"--no-resume-playback", "--save-position-on-quit=no",
		"--osd-level=0", "--sub-ass-override=no", "--sub-visibility=yes",
		"--sub-file=" + scriptPath, "--audio-file=" + musicPath,
		"--title=" + otakaseMovieTitle, "--force-media-title=" + otakaseMovieTitle,
	}
	// The images go in the order the script placed them: the wall's
	// covers, then the characters.
	var paths, images []string
	for _, favourite := range wall {
		paths = append(paths, favourite.Path)
	}
	for _, character := range facts.Extras.Characters {
		paths = append(paths, character.Path)
	}
	for i, path := range paths {
		args = append(args, "--external-file="+path)
		images = append(images, fmt.Sprintf("[vid%d]", i+2))
	}
	args = append(args,
		"--lavfi-complex="+movieFilterGraph("[vid1]", images, "[vo]", timeline.Pictures)+";[aid1]anull[ao]",
		movieBackground(palette, timeline.End),
	)
	return exec.Command(binary, args...).Run()
}

// movieBackground is the lavfi source mpv plays under everything: the
// palette's background colour, for seconds.
func movieBackground(palette theme.Palette, seconds float64) string {
	return fmt.Sprintf("av://lavfi:color=c=0x%s:s=%dx%d:r=30:d=%.1f",
		strings.TrimPrefix(palette.Background, "#"), movieWidth, movieHeight, seconds+0.5)
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

// printMovieStory is the film for a terminal without mpv to show it in.
func printMovieStory(facts movieFacts, rng *rand.Rand) {
	for _, card := range movieStatCards(facts, rng) {
		fmt.Printf("  %s: %s %s\n    %s\n", card.Label, formatStatsCount(card.Value), card.Unit, card.Quip)
	}
	fmt.Println()
	for _, line := range moviePoem(facts, rng) {
		indent := "  "
		if line.Voice == 1 {
			indent = "    "
		}
		fmt.Println(indent + line.Text)
	}
	fmt.Println()
	for _, badge := range movieAchievements(facts, rng) {
		fmt.Printf("  * %s: %s\n", badge.Name, badge.Detail)
	}
	fmt.Println()
	title, episode, teaser := movieNextTime(facts, rng)
	fmt.Printf("  Next time: %s, %s. %s\n\n", title, episode, teaser)
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
