package internal

import (
	"strings"
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/theme"
)

// Every row of the logo has to be the same width, or the letters shear.
func TestTheLogoRowsLineUp(t *testing.T) {
	rows := otakaseLogo()
	width := len([]rune(rows[0]))
	for i, row := range rows {
		if got := len([]rune(row)); got != width {
			t.Fatalf("row %d is %d wide, row 0 is %d", i, got, width)
		}
	}
}

func TestOnlyOtakaseFindsTheMovie(t *testing.T) {
	for _, query := range []string{"otakase", "  OTAKASE ", "Otakase"} {
		if !isOtakaseMovieQuery(query) {
			t.Errorf("%q did not find the movie", query)
		}
	}
	for _, query := range []string{"otaku", "otakase 2", ""} {
		if isOtakaseMovieQuery(query) {
			t.Errorf("%q found the movie", query)
		}
	}
}

// The credits star the viewer and carry their real numbers.
func TestTheCreditsCarryTheStats(t *testing.T) {
	stats := watchStats{AniList: &aniListStatsTotals{
		Count: 120, EpisodesWatched: 1500, MinutesWatched: 36000,
		Genres: []statCount{{"Action", 50}, {"Comedy", 40}, {"Drama", 30}, {"Horror", 1}},
	}}
	credits := movieCredits(stats, "Xy", 1000, time.Now())
	text := ""
	for _, credit := range credits {
		text += credit.Role + "=" + credit.Name + "\n"
	}
	for _, want := range []string{"Starring=Xy", "Episodes watched=1,500", "Top genres=Action · Comedy · Drama", "Achievement=1,000 episodes watched"} {
		if !strings.Contains(text, want) {
			t.Errorf("credits miss %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Horror") {
		t.Error("more than three genres listed")
	}
}

// No tracker still makes a film: it just has less in it.
func TestTheCreditsNeedNoTracker(t *testing.T) {
	credits := movieCredits(watchStats{}, "", 0, time.Now())
	if credits[0].Name != "You" {
		t.Fatalf("nameless viewer starred as %q", credits[0].Name)
	}
}

// A brace in a show title would open an ASS override block and swallow the
// rest of the line.
func TestTheScriptEscapesTitles(t *testing.T) {
	script, seconds := movieScript([]creditLine{{"Most binged lately", `Re:{Zero} \N`}}, "presents", theme.Builtin(), "monospace")
	if strings.Contains(script, "{Zero}") || !strings.Contains(script, `\\N`) {
		t.Fatalf("title not escaped:\n%s", script)
	}
	if seconds < movieLogoSeconds+movieEndCardSeconds {
		t.Fatalf("film too short: %.1fs", seconds)
	}
}

func TestAssColorIsBlueGreenRed(t *testing.T) {
	if got := assColor("#d2492f"); got != "&H2F49D2&" {
		t.Fatalf("got %s", got)
	}
}
