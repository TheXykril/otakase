package internal

import (
	"math/rand/v2"
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

func testFacts() movieFacts {
	stats := watchStats{AniList: &aniListStatsTotals{
		Count: 120, EpisodesWatched: 1500, MinutesWatched: 36000, MeanScore: 82,
		Genres:   []statCount{{"Action", 50}, {"Comedy", 40}, {"Drama", 30}, {"Horror", 1}},
		Statuses: []statCount{{"COMPLETED", 90}},
	}}
	extras := movieExtras{Studios: []string{"MAPPA"}, VoiceActor: "Kana Hanazawa", MemberSince: time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC)}
	return gatherMovieFacts(stats, extras, "Xy", 1000, time.Now())
}

// The numbers are the viewer's own, at most four cards of them.
func TestTheCardsCarryTheStats(t *testing.T) {
	cards := movieStatCards(testFacts(), rand.New(rand.NewPCG(1, 2)))
	if len(cards) == 0 || len(cards) > 4 {
		t.Fatalf("%d cards", len(cards))
	}
	if cards[0].Value != 1500 || cards[1].Value != 600 || cards[2].Value != 90 {
		t.Fatalf("cards %+v", cards)
	}
}

// The conversation uses the viewer's name and numbers, and is not word for
// word the same every time.
func TestThePoemIsTheirsAndVaries(t *testing.T) {
	facts := testFacts()
	join := func(seed uint64) string {
		var lines []string
		for _, line := range moviePoem(facts, rand.New(rand.NewPCG(seed, 9))) {
			lines = append(lines, line.Text)
		}
		return strings.Join(lines, "\n")
	}
	first := join(1)
	for _, want := range []string{"Xy", "1,500"} {
		if !strings.Contains(first, want) {
			t.Errorf("poem misses %q:\n%s", want, first)
		}
	}
	if lines := strings.Count(first, "\n") + 1; lines < 25 {
		t.Errorf("only %d lines of talk", lines)
	}
	varied, voiced := false, false
	for seed := uint64(2); seed < 20; seed++ {
		words := join(seed)
		varied = varied || words != first
		voiced = voiced || strings.Contains(words, "Kana Hanazawa")
	}
	if !varied {
		t.Error("every viewing had the same words")
	}
	if !voiced {
		t.Error("the voice actor never came up")
	}
}

// No tracker still makes a film: it just has less in it.
func TestTheFilmNeedsNoTracker(t *testing.T) {
	facts := gatherMovieFacts(watchStats{}, movieExtras{}, "", 0, time.Now())
	if facts.Viewer != "You" {
		t.Fatalf("nameless viewer is %q", facts.Viewer)
	}
	if cards := movieStatCards(facts, rand.New(rand.NewPCG(1, 2))); len(cards) != 0 {
		t.Fatalf("cards with no numbers: %+v", cards)
	}
	_, timeline := movieScript(facts, "The movie", nil, theme.Builtin(), "monospace", "sans-serif", rand.New(rand.NewPCG(1, 2)))
	if timeline.End <= 0 || len(timeline.Pictures) != 0 {
		t.Fatalf("timeline %+v", timeline)
	}
}

// A brace in a show title would open an ASS override block and swallow the
// rest of the line.
func TestTheScriptEscapesTitles(t *testing.T) {
	facts := testFacts()
	facts.Binged, facts.BingedCount = `Re:{Zero} \N`, 12
	script, _ := movieScript(facts, "The movie", nil, theme.Builtin(), "monospace", "sans-serif", rand.New(rand.NewPCG(1, 2)))
	if strings.Contains(script, "{Zero}") || !strings.Contains(script, `\\N`) {
		t.Fatal("title not escaped")
	}
}

// Every sound effect lands inside the film, and the beat plays somewhere.
func TestTheScoreFollowsThePicture(t *testing.T) {
	wall := []movieFavourite{{Title: "A"}, {Title: "B"}}
	facts := testFacts()
	facts.Extras.Characters = []movieCharacter{{Name: "Frieren", Series: "Frieren"}, {Name: "Fern"}, {Name: "Stark"}}
	_, timeline := movieScript(facts, "1,000 episodes", wall, theme.Builtin(), "monospace", "sans-serif", rand.New(rand.NewPCG(1, 2)))
	if len(timeline.Pictures) != 5 {
		t.Fatalf("%d pictures, want the 2 covers and 3 characters", len(timeline.Pictures))
	}
	for i, picture := range timeline.Pictures {
		if picture.In <= 0 || picture.Out <= picture.In || picture.Out > timeline.End {
			t.Errorf("picture %d shows %.1f to %.1f in a %.1fs film", i, picture.In, picture.Out, timeline.End)
		}
	}
	kinds := map[string]bool{}
	for _, cue := range timeline.Score.Cues {
		if cue.At < 0 || cue.At > timeline.End {
			t.Errorf("%s at %.1fs, film is %.1fs", cue.Kind, cue.At, timeline.End)
		}
		kinds[cue.Kind] = true
	}
	for _, kind := range []string{cueImpact, cueTick, cuePop, cueBell, cueWhoosh} {
		if !kinds[kind] {
			t.Errorf("no %s", kind)
		}
	}
	if len(timeline.Score.Drums) == 0 {
		t.Error("the beat never plays")
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
	wav := movieSoundtrack(movieScore{Length: 2, Cues: []movieCue{{At: 0.5, Kind: cueBell}, {At: 1, Kind: cueWhoosh, Length: 0.6}}})
	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatal("not a WAV header")
	}
	if want := 44 + 2*2*movieSampleRate; len(wav) != want {
		t.Fatalf("%d bytes, want %d", len(wav), want)
	}
}

// Each image gets its own place in the graph, and the last one is the output.
func TestTheGraphLaysOutEveryImage(t *testing.T) {
	pictures := []moviePicture{{X: 10, Y: 20, Width: 250, Height: 354, In: 40, Out: 48}, {X: 300, Y: 20, Width: 220, Height: 310, In: 50, Out: 56}}
	graph := movieFilterGraph("[vid1]", []string{"[vid2]", "[vid3]"}, "[vo]", pictures)
	for _, want := range []string{"[vid2]scale=250:354", "[vid3]scale=220:310", "[base1][image1]overlay=x=300", "[vo]"} {
		if !strings.Contains(graph, want) {
			t.Errorf("graph misses %q:\n%s", want, graph)
		}
	}
	if !strings.HasSuffix(movieFilterGraph("[vid1]", nil, "[vo]", nil), "null[vo]") {
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
