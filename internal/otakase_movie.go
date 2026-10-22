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
// title card, credits that roll like the end of a film, a wall of their
// favourite shows, and the red seal anime end cards close on. It is found by
// searching for "otakase" when adding a show, and plays on its own when the
// viewer passes an episode milestone (milestones.go).
//
// There is no video file. mpv draws the background and lays the covers over
// it with lavfi, the rest of the picture is an ASS subtitle written for the
// occasion, and the music is synthesised (otakase_movie_music.go), so nothing
// ships with the binary and the text always fits whatever window it lands in.

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

// creditLine is one row of the roll: a role and its name side by side, or a
// section heading on its own when Heading is set.
type creditLine struct {
	Heading string
	Role    string
	Name    string
}

// movieCredits turns the viewer's stats into the roll. milestone is the
// episode count being celebrated, 0 when the movie was found by searching.
func movieCredits(stats watchStats, extras movieExtras, viewer string, milestone int, now time.Time) []creditLine {
	if strings.TrimSpace(viewer) == "" {
		viewer = "You"
	}
	credits := []creditLine{{Heading: "The cast"}, {Role: "Starring", Name: viewer}}
	if milestone > 0 {
		credits = append(credits, creditLine{Role: "Achievement", Name: formatStatsCount(milestone) + " episodes"})
	}
	if !extras.MemberSince.IsZero() {
		credits = append(credits, creditLine{Role: "Watching since", Name: extras.MemberSince.Format("January 2006")})
	}

	numbers := []creditLine{}
	minutes := 0
	if totals := stats.AniList; totals != nil {
		minutes = totals.MinutesWatched
		numbers = append(numbers,
			creditLine{Role: "Episodes watched", Name: formatStatsCount(totals.EpisodesWatched)},
			creditLine{Role: "Time watched", Name: formatWatchMinutes(totals.MinutesWatched)},
			creditLine{Role: "Anime on your list", Name: formatStatsCount(totals.Count)},
		)
		for _, status := range totals.Statuses {
			if status.Name == "COMPLETED" {
				numbers = append(numbers, creditLine{Role: "Completed", Name: formatStatsCount(status.Count)})
			}
		}
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
		minutes = int(math.Round(totals.DaysWatched * 24 * 60))
		numbers = append(numbers,
			creditLine{Role: "Episodes watched", Name: formatStatsCount(totals.Episodes)},
			creditLine{Role: "Time watched", Name: formatWatchMinutes(minutes)},
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
	// A 40-hour week is 2400 minutes.
	if weeks := minutes / 2400; weeks >= 1 {
		name := strconv.Itoa(weeks) + " work weeks"
		if weeks == 1 {
			name = "1 work week"
		}
		numbers = append(numbers, creditLine{Role: "As a full-time job", Name: name})
	}
	if len(numbers) > 0 {
		credits = append(credits, creditLine{Heading: "The numbers"})
		credits = append(credits, numbers...)
	}

	credits = append(credits, creditLine{Heading: "The crew"})
	for i, studio := range extras.Studios {
		role := ""
		if i == 0 {
			role = "Animation by"
		}
		credits = append(credits, creditLine{Role: role, Name: studio})
	}
	if extras.VoiceActor != "" {
		credits = append(credits, creditLine{Role: "Voice you hear most", Name: extras.VoiceActor})
	}
	if len(extras.Tags) > 0 {
		credits = append(credits, creditLine{Role: "Recurring themes", Name: strings.Join(extras.Tags, " · ")})
	}
	if extras.StartYear > 0 {
		credits = append(credits, creditLine{Role: "Favourite year", Name: strconv.Itoa(extras.StartYear)})
	}
	return append(credits,
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

// movieTimeline is when the film's parts happen, in seconds.
type movieTimeline struct {
	WallStart, WallEnd float64
	Stamp              float64
	End                float64
}

// movieCoverX is the left edge of cover i of n on the wall.
func movieCoverX(i, n int) int {
	width := n*movieCoverWidth + (n-1)*movieCoverGap
	return (movieWidth-width)/2 + i*(movieCoverWidth+movieCoverGap)
}

// movieScript is the whole film's picture, less the covers themselves, as an
// ASS file, and when its parts happen.
func movieScript(credits []creditLine, opening movieOpening, wall []movieFavourite, palette theme.Palette, logoFont, textFont string) (string, movieTimeline) {
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
	fmt.Fprintf(&script, "Style: Seal,serif,250,%s,%s,&H00000000,&H00000000,1,0,0,0,100,100,0,0,1,0,0,5,0,0,0,1\n", accent, accent)
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
	rollEnd := rollStart + float64(movieHeight+rollHeight+100)/movieScrollSpeed
	var timeline movieTimeline
	timeline.WallStart, timeline.WallEnd = rollEnd, rollEnd
	if len(wall) > 0 {
		timeline.WallEnd = rollEnd + movieWallSeconds
	}
	cardStart := timeline.WallEnd
	timeline.Stamp = cardStart + 1.2
	timeline.End = cardStart + movieEndCardSeconds

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
	rise := movieHeight + rollHeight + 200
	for _, credit := range credits {
		if credit.Heading != "" {
			y += rowHeight
			heading := strings.ToUpper(credit.Heading)
			event(2, rollStart, rollEnd, "Text", fmt.Sprintf(`{\move(%d,%d,%d,%d)\fs%d\fsp14\c%s}%s`,
				centreX, y, centreX, y-rise, headingSize, accent, assText(heading)))
			// A short rule either side of the heading.
			half := len([]rune(heading))*(headingSize*6/10+14)/2 + 40
			for _, side := range []int{-1, 1} {
				x := centreX + side*(half+60)
				event(2, rollStart, rollEnd, "Draw", fmt.Sprintf(`{\an5\move(%d,%d,%d,%d)\c%s\alpha&H60&\p1}m 0 0 l 120 0 120 2 0 2{\p0}`,
					x, y, x, y-rise, accent))
			}
			y += rowHeight
			continue
		}
		if credit.Role != "" {
			event(2, rollStart, rollEnd, "Text", fmt.Sprintf(`{\an6\move(%d,%d,%d,%d)\fs%d\c%s}%s`,
				centreX-gutter, y, centreX-gutter, y-rise, roleSize, muted, assText(ellipsize(credit.Role, movieValueRunes))))
		}
		event(2, rollStart, rollEnd, "Text", fmt.Sprintf(`{\an4\move(%d,%d,%d,%d)\fs%d\c%s}%s`,
			centreX+gutter, y, centreX+gutter, y-rise, nameSize, text, assText(ellipsize(credit.Name, movieValueRunes))))
		y += rowHeight
	}

	// The favourites wall: the covers come from lavfi (movieFilterGraph); this
	// draws the heading, a thin frame round each cover and its title.
	if len(wall) > 0 {
		start, end := timeline.WallStart, timeline.WallEnd
		event(2, start, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs%d\fsp14\c%s\fad(600,700)}%s`, centreX, movieCoverTop-62, headingSize, accent, assText("YOUR FAVOURITES")))
		for i, favourite := range wall {
			x := movieCoverX(i, len(wall))
			in := 0.6 + float64(i)*0.35
			event(3, start+in, end, "Draw", fmt.Sprintf(`{\an7\pos(%d,%d)\1a&HFF&\bord2\3c%s\fad(500,700)\p1}m 0 0 l %d 0 %d %d 0 %d{\p0}`,
				x, movieCoverTop, accent, movieCoverWidth, movieCoverWidth, movieCoverHeight, movieCoverHeight))
			event(2, start+in+0.2, end, "Text", fmt.Sprintf(`{\an8\pos(%d,%d)\fs22\c%s\fad(500,700)}%s`,
				x+movieCoverWidth/2, movieCoverTop+movieCoverHeight+24, text, assText(ellipsize(favourite.Title, 21))))
		}
	}

	// Closing card: the seal. 終, "the end", stamped in the accent colour the
	// way anime end cards close, with OTAKASE and a goodbye beneath it.
	stamp := timeline.Stamp
	sealY := centreY - 70
	land := `\frz-6\fscx260\fscy260\alpha&HFF&\t(0,40,\alpha&H00&)\t(0,170,\fscx100\fscy100)\fad(0,1200)`
	// A filled square in the accent with the character cut out of it in the
	// background colour, the way a seal prints.
	event(3, stamp, timeline.End, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s\blur0.8%s\p1}m 12 0 l 318 0 b 325 0 330 5 330 12 l 330 318 b 330 325 325 330 318 330 l 12 330 b 5 330 0 325 0 318 l 0 12 b 0 5 5 0 12 0{\p0}`,
		centreX, sealY, accent, land))
	event(4, stamp, timeline.End, "Seal", fmt.Sprintf(`{\pos(%d,%d)\c%s%s}終`, centreX, sealY+6, assColor(palette.Background), land))
	// The ink spreads a little after it lands.
	event(2, stamp+0.15, timeline.End, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s\alpha&HB0&\blur22\frz-6\t(0,700,\fscx122\fscy122\alpha&HF0&)\fad(0,1200)\p1}m 0 0 l 330 0 330 330 0 330{\p0}`,
		centreX, sealY, accent))
	event(2, stamp+0.9, timeline.End, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs%d\fsp20\c%s\fad(700,1200)}%s`, centreX, centreY+170, headingSize, text, assText("OTAKASE")))
	event(2, stamp+1.5, timeline.End, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs%d\c%s\fad(700,1200)}%s`, centreX, centreY+222, roleSize-4, muted, assText("Mata ne.")))

	// Petals drifting down through the whole film, behind everything.
	petals := rand.New(rand.NewPCG(4, 12))
	for i := 0; i < 26; i++ {
		start := petals.Float64() * (timeline.End - 4)
		life := 11 + petals.Float64()*8
		x := petals.IntN(movieWidth)
		drift := petals.IntN(500) - 150
		scale := 60 + petals.IntN(80)
		spin := petals.IntN(720) - 360
		event(0, start, math.Min(timeline.End, start+life), "Draw", fmt.Sprintf(`{\an5\move(%d,%d,%d,%d)\fscx%d\fscy%d\frz%d\t(\frz%d)\c%s\alpha&H%02X&\blur1.5\fad(800,800)\p1}m 0 0 b 10 -14 26 -10 30 0 b 26 10 10 14 0 0{\p0}`,
			x, movieBars-40, x+drift, movieHeight-movieBars+40, scale, scale, petals.IntN(360), spin, accent, 0x70+petals.IntN(0x50)))
	}

	// Letterbox bars, over everything.
	bar := fmt.Sprintf(`m 0 0 l %d 0 %d %d 0 %d`, movieWidth, movieWidth, movieBars, movieBars)
	event(9, 0, timeline.End, "Draw", `{\pos(0,0)\c&H000000&\p1}`+bar+`{\p0}`)
	event(9, 0, timeline.End, "Draw", fmt.Sprintf(`{\pos(0,%d)\c&H000000&\p1}%s{\p0}`, movieHeight-movieBars, bar))
	return script.String(), timeline
}

// movieFilterGraph is the lavfi graph the picture is built on: background in
// on the first video label, one cover per later label, the finished frame out
// on output. The background gets a faint grain and a vignette; each cover is
// cut to the wall's shape and fades in and out on its own beat.
func movieFilterGraph(background string, covers []string, output string, timeline movieTimeline) string {
	var graph strings.Builder
	fmt.Fprintf(&graph, "%snoise=alls=7:allf=t,vignette=PI/4[base0]", background)
	for i, cover := range covers {
		in := timeline.WallStart + 0.6 + float64(i)*0.35
		out := timeline.WallEnd - 0.7
		fmt.Fprintf(&graph, ";%sscale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,format=yuva420p,loop=loop=-1:size=1,setpts=N/30/TB,trim=duration=%.2f,fade=t=in:st=%.2f:d=0.5:alpha=1,fade=t=out:st=%.2f:d=0.7:alpha=1[cover%d]",
			cover, movieCoverWidth, movieCoverHeight, movieCoverWidth, movieCoverHeight, timeline.WallEnd+0.5, in, out, i)
		next := fmt.Sprintf("[base%d]", i+1)
		if i == len(covers)-1 {
			next = output
		}
		fmt.Fprintf(&graph, ";[base%d][cover%d]overlay=x=%d:y=%d:eof_action=pass:enable='between(t,%.2f,%.2f)'%s",
			i, i, movieCoverX(i, len(covers)), movieCoverTop, in, timeline.WallEnd, next)
	}
	if len(covers) == 0 {
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

	viewer := movieViewerName(user, extras)
	opening := movieOpening{Presenter: viewer, Tagline: "The movie"}
	if milestone > 0 {
		opening.Tagline = formatStatsCount(milestone) + " episodes"
	}
	credits := movieCredits(stats, extras, viewer, milestone, time.Now())
	if err := playMovieFilm(credits, opening, extras.Favourites, palette); err != nil {
		Log(fmt.Sprintf("Otakase movie: %v", err))
		if terminal {
			printMovieCredits(credits)
		}
	}
}

// playMovieFilm writes the picture and the music and plays them, waiting for
// the window to close.
func playMovieFilm(credits []creditLine, opening movieOpening, wall []movieFavourite, palette theme.Palette) error {
	binary, err := findMPV()
	if err != nil {
		return fmt.Errorf("mpv not found: %w", err)
	}
	dir, err := os.MkdirTemp("", AppName+"-movie-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	script, timeline := movieScript(credits, opening, wall, palette, theme.MonospaceFont(), "sans-serif")
	scriptPath := filepath.Join(dir, "credits.ass")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return err
	}
	musicPath := filepath.Join(dir, "music.wav")
	if err := os.WriteFile(musicPath, movieSoundtrack(timeline.End, timeline.Stamp), 0o600); err != nil {
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
	covers := make([]string, 0, len(wall))
	for i, favourite := range wall {
		args = append(args, "--external-file="+favourite.Path)
		covers = append(covers, fmt.Sprintf("[vid%d]", i+2))
	}
	args = append(args,
		"--lavfi-complex="+movieFilterGraph("[vid1]", covers, "[vo]", timeline)+";[aid1]anull[ao]",
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
