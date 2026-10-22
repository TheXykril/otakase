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
	credits := movieCredits(stats, movieExtras{Studios: []string{"MAPPA"}}, "Xy", 1000, time.Now())
	text := ""
	for _, credit := range credits {
		text += credit.Role + "=" + credit.Name + "\n"
	}
	for _, want := range []string{"Starring=Xy", "Episodes watched=1,500", "Top genres=Action · Comedy · Drama", "Achievement=1,000 episodes", "Animation by=MAPPA", "As a full-time job=15 work weeks"} {
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
	credits := movieCredits(watchStats{}, movieExtras{}, "", 0, time.Now())
	if credits[1].Name != "You" {
		t.Fatalf("nameless viewer starred as %q", credits[1].Name)
	}
	for _, credit := range credits {
		if credit.Heading == "The numbers" {
			t.Fatal("a numbers section with no numbers in it")
		}
	}
}

// A brace in a show title would open an ASS override block and swallow the
// rest of the line.
func TestTheScriptEscapesTitles(t *testing.T) {
	script, timeline := movieScript([]creditLine{{Role: "Most binged lately", Name: `Re:{Zero} \N`}}, movieOpening{Presenter: "Xy", Tagline: "The movie"}, nil, theme.Builtin(), "monospace", "sans-serif")
	if strings.Contains(script, "{Zero}") || !strings.Contains(script, `\\N`) {
		t.Fatalf("title not escaped:\n%s", script)
	}
	if timeline.End < movieTitleSeconds+movieEndCardSeconds || timeline.WallEnd != timeline.WallStart {
		t.Fatalf("timeline without a wall: %+v", timeline)
	}
}

func TestAssColorIsBlueGreenRed(t *testing.T) {
	if got := assColor("#d2492f"); got != "&H2F49D2&" {
		t.Fatalf("got %s", got)
	}
}

// A title longer than its half of the screen is cut, not run off the edge.
func TestLongTitlesAreClipped(t *testing.T) {
	long := strings.Repeat("x", 80)
	if got := []rune(ellipsize(long, movieValueRunes)); len(got) != movieValueRunes || got[len(got)-1] != '…' {
		t.Fatalf("clipped to %q", string(got))
	}
	if ellipsize("Frieren", movieValueRunes) != "Frieren" {
		t.Fatal("a short title was changed")
	}
}

// The soundtrack is a valid WAV of the asked length.
func TestTheSoundtrackIsAWav(t *testing.T) {
	wav := movieSoundtrack(2, 1)
	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatal("not a WAV header")
	}
	if want := 44 + 2*2*movieSampleRate; len(wav) != want {
		t.Fatalf("%d bytes, want %d", len(wav), want)
	}
}

// Each cover gets its own place in the graph, and the last one is the output.
func TestTheGraphLaysOutEveryCover(t *testing.T) {
	timeline := movieTimeline{WallStart: 40, WallEnd: 48, Stamp: 49, End: 56}
	graph := movieFilterGraph("[vid1]", []string{"[vid2]", "[vid3]"}, "[vo]", timeline)
	for _, want := range []string{"[vid2]scale", "[vid3]scale", "[base1][cover1]overlay", "[vo]"} {
		if !strings.Contains(graph, want) {
			t.Errorf("graph misses %q:\n%s", want, graph)
		}
	}
	if !strings.HasSuffix(movieFilterGraph("[vid1]", nil, "[vo]", timeline), "null[vo]") {
		t.Error("no covers left the graph without an output")
	}
}

// The covers sit centred, side by side.
func TestTheWallIsCentred(t *testing.T) {
	left := movieCoverX(0, 3)
	right := movieCoverX(2, 3) + movieCoverWidth
	if left != movieWidth-right {
		t.Fatalf("wall runs %d to %d", left, right)
	}
}
