package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestNormTitle(t *testing.T) {
	cases := map[string]string{
		"Frieren: Beyond Journey's End":             "frieren beyond journeys end",
		"Sousou no Frieren 2nd Season":              "sousou no frieren",
		"Attack on Titan: The Final Season":         "attack on titan",
		"Kusuriya no Hitorigoto Season 2":           "kusuriya no hitorigoto",
		"The Apothecary Diaries":                    "apothecary diaries",
		"Mob Psycho 100 III":                        "mob psycho 100",
		"Kaguya-sama: Love is War -Ultra Romantic-": "kaguya sama love is war ultra romantic",
		"Pokémon":                          "pokemon",
		"Dr. STONE: SCIENCE FUTURE Part 2": "dr stone science future",
	}
	for in, want := range cases {
		if got := normTitle(in); got != want {
			t.Errorf("normTitle(%q) = %q, want %q", in, got, want)
		}
	}
	if got := mainTitle("Frieren: Beyond Journey's End"); got != "Frieren" {
		t.Errorf("mainTitle = %q", got)
	}
	if got := mainTitle("Dandadan"); got != "" {
		t.Errorf("mainTitle without subtitle = %q", got)
	}
}

var testSpoilerTitles = spoilerTitles([]string{
	"Frieren: Beyond Journey's End Season 2", "Sousou no Frieren 2nd Season",
	"DAN DA DAN Season 2", "The Apothecary Diaries Season 2", "Kusuriya no Hitorigoto 2nd Season",
	"Monster", "ONE PIECE", "Solo Leveling -ReAwakening-", "Chainsaw Man", "K",
})

func TestSpoilerTitles(t *testing.T) {
	for _, bad := range []string{"monster", "k", "solo"} {
		for _, got := range testSpoilerTitles {
			if got == bad {
				t.Errorf("%q should be too generic to look for", bad)
			}
		}
	}
	for _, want := range []string{"frieren", "dan da dan", "apothecary diaries", "one piece", "solo leveling"} {
		found := false
		for _, got := range testSpoilerTitles {
			found = found || got == want
		}
		if !found {
			t.Errorf("missing %q in %v", want, testSpoilerTitles)
		}
	}
}

func TestSpoilerHit(t *testing.T) {
	positives := []string{
		"omg in frieren himmel dies in the flashback??",
		"can't believe the Dandadan finale ended like that",
		"Turns out the masked guy in Apothecary Diaries is her dad",
		"Frieren ep 7 when Fern finally snapped, that scene was insane",
		"so in one piece the old man betrayed everyone",
		"chainsaw man last episode was brutal",
		"Kusuriya no Hitorigoto: the reveal about Jinshi though",
	}
	for _, text := range positives {
		if _, ok := spoilerHit(text, testSpoilerTitles); !ok {
			t.Errorf("missed spoiler: %q", text)
		}
	}
	negatives := []string{
		"just started watching frieren, it's so cozy",
		"when does the dandadan finale air in europe?",
		"the frieren ending song is so good",
		"I died laughing at the new one piece episode",
		"||frieren himmel dies|| no spoilers here",
		"frieren ||himmel dies in the flashback||",
		"is the apothecary diaries episode 5 out yet?",
		"anyone know where to watch chainsaw man dubbed?",
		"he dies at the end of the movie, so sad",           // no show named
		"the monster in my game died",                       // generic title not looked for
		"frieren ep 3 is out",                               // episode with no event
		"https://example.com/frieren-finale-dies is a link", // only in a link
		"Death Note is a classic, one piece is better",
	}
	for _, text := range negatives {
		if title, ok := spoilerHit(text, testSpoilerTitles); ok {
			t.Errorf("false spoiler (%s): %q", title, text)
		}
	}
}

func TestSpoilerNagCooldown(t *testing.T) {
	var g spoilerGuard
	now := time.Now()
	if !g.nagOK("u", now) {
		t.Fatal("first nudge refused")
	}
	if g.nagOK("u", now.Add(9*time.Minute)) {
		t.Fatal("nudged twice within 10 minutes")
	}
	if !g.nagOK("other", now.Add(time.Minute)) {
		t.Fatal("other member refused")
	}
	if !g.nagOK("u", now.Add(11*time.Minute)) {
		t.Fatal("refused after the cooldown")
	}
}

func TestSpoilerRepost(t *testing.T) {
	if got := spoilerRepost("he ||dies|| at the end"); got != "||he dies at the end||\n-# (spoiler hidden by bot)" {
		t.Errorf("repost = %q", got)
	}
}

func TestSeasonOf(t *testing.T) {
	s, y, ps, py := seasonAndPrev(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
	if s != "WINTER" || y != 2026 || ps != "FALL" || py != 2025 {
		t.Errorf("January: %s %d, %s %d", s, y, ps, py)
	}
	s, y, ps, py = seasonAndPrev(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	if s != "FALL" || y != 2026 || ps != "SUMMER" || py != 2026 {
		t.Errorf("October: %s %d, %s %d", s, y, ps, py)
	}
}

func TestGuessMatches(t *testing.T) {
	frieren := []string{"Frieren: Beyond Journey's End", "Sousou no Frieren", "葬送のフリーレン", "Frieren at the Funeral"}
	aot := []string{"Attack on Titan", "Shingeki no Kyojin", "進撃の巨人", "AoT", "SnK"}
	dandadan := []string{"DAN DA DAN", "Dandadan", "ダンダダン"}
	jjk := []string{"JUJUTSU KAISEN Season 2", "Jujutsu Kaisen 2nd Season", "呪術廻戦 第2期", "JJK"}
	cases := []struct {
		guess  string
		titles []string
		want   bool
	}{
		{"frieren", frieren, true},
		{"FRIEREN!!", frieren, true},
		{"sousou no frieren", frieren, true},
		{"sosou no frieren", frieren, true}, // typo
		{"frieren beyond journeys end", frieren, true},
		{"葬送のフリーレン", frieren, true},
		{"attack on titan", aot, true},
		{"attack on titan season 3", aot, true},
		{"aot", aot, true},
		{"snk", aot, true},
		{"shingeki", aot, false},
		{"dan da dan", dandadan, true},
		{"dandadan", dandadan, true},
		{"jujutsu kaisen", jjk, true},
		{"jujutsu kaisen 2", jjk, true},
		{"jjk", jjk, true},
		{"naruto", frieren, false},
		{"fr", frieren, false},
		{"the", aot, false},
		{"attack", aot, false},
		{"jujutsu", jjk, false},
	}
	for _, c := range cases {
		if got := guessMatches(c.guess, c.titles); got != c.want {
			t.Errorf("guessMatches(%q, %v) = %v, want %v", c.guess, c.titles[0], got, c.want)
		}
	}
	if guessMatches("boruto", []string{"Naruto"}) {
		t.Error("boruto shouldn't count for Naruto")
	}
	if !guessMatches("my hero academia", []string{"My Hero Academia Season 7", "Boku no Hero Academia 7"}) {
		t.Error("my hero academia should match season 7")
	}
}

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"", "", 0}, {"abc", "abc", 0}, {"kitten", "sitting", 3}, {"frieren", "freiren", 2}, {"", "abc", 3}}
	for _, c := range cases {
		if got := editDistance(c.a, c.b); got != c.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestTitleBlanks(t *testing.T) {
	cases := map[string]string{
		"frieren":                 "F _ _ _ _ _ _",
		`"Oshi no Ko"`:            `" O _ _ _   N _   K _ "`,
		"Mob Psycho 100":          "M _ _   P _ _ _ _ _   1 0 0",
		"Re:ZERO -Starting Life-": "R _ : _ _ _ _   - S _ _ _ _ _ _ _   L _ _ _ -",
	}
	for in, want := range cases {
		if got := titleBlanks(in); got != want {
			t.Errorf("titleBlanks(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGuessClues(t *testing.T) {
	s := guessShow{Name: "Frieren", Titles: []string{"Frieren", "Sousou no Frieren"}, Format: "TV", Season: "FALL", Year: 2023,
		Genres: []string{"Adventure", "Drama", "Fantasy", "Slice of Life"}, Studio: "Madhouse", Episodes: 28,
		Source: "MANGA", Score: 91, Tags: []string{"Elf", "Travel"}, Leads: []string{"Frieren", "Fern"}, Rank: 40}
	pool := infoClues(s)
	if len(pool) != 9 {
		t.Fatalf("infoClues = %d clues, want 9: %v", len(pool), pool)
	}
	if guessLead(s) != "Fern" {
		t.Errorf("guessLead = %q, want Fern (Frieren gives the title away)", guessLead(s))
	}
	// No shuffle: the first three of the pool, hardest first, then the
	// title with first letters.
	none := func(int) int { return 0 }
	got := guessClues(s, none)
	if len(got) != guessInfoClues+1 || len(got) > len(guessBlocks) {
		t.Fatalf("guessClues = %q", got)
	}
	if got[len(got)-1] != "Title: `F _ _ _ _ _ _`" {
		t.Errorf("title clue = %q", got[len(got)-1])
	}
	// Different picks give different rounds.
	seen := map[string]bool{}
	for seed := range 20 {
		r := rand.New(rand.NewPCG(uint64(seed), 1))
		seen[strings.Join(guessClues(s, r.IntN), "|")] = true
	}
	if len(seen) < 5 {
		t.Errorf("only %d different rounds in 20", len(seen))
	}
	// A show with little data still gets the title clue.
	got = guessClues(guessShow{Name: "Your Name", Format: "MOVIE", Episodes: 1}, none)
	if len(got) != 2 || got[0] != "**Movie**" {
		t.Errorf("guessClues(movie) = %q", got)
	}
}

func TestVowelBlanks(t *testing.T) {
	if got := vowelBlanks("Attack on Titan"); got != "a _ _ a _ _   o _   _ i _ a _" {
		t.Errorf("got %q", got)
	}
}

func TestGuessLeaders(t *testing.T) {
	week := weekKey(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	if week != "2026-W42" {
		t.Errorf("weekKey = %q", week)
	}
	scores := map[string]guessScore{
		"a": {Total: 10, Week: 1, WeekOf: week},
		"b": {Total: 3, Week: 3, WeekOf: week},
		"c": {Total: 20, Week: 5, WeekOf: "2026-W40"},
	}
	weekly := guessLeaders(scores, week, true)
	if len(weekly) != 2 || weekly[0].User != "b" || weekly[1].User != "a" {
		t.Errorf("weekly = %+v", weekly)
	}
	all := guessLeaders(scores, week, false)
	if len(all) != 3 || all[0].User != "c" || all[2].User != "b" {
		t.Errorf("all time = %+v", all)
	}
}

func TestPixelate(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 230, 345))
	for y := range 345 {
		for x := range 230 {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 0x80, 0xff})
		}
	}
	data, err := pixelate(img, 7)
	if err != nil {
		t.Fatal(err)
	}
	out, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := out.Bounds(); b.Dx() != 300 || b.Dy() != 426 {
		t.Errorf("size %v", b)
	}
	// Big squares: neighbouring pixels inside a block are the same.
	if out.At(1, 1) != out.At(20, 20) {
		t.Error("not pixelated")
	}
}

func TestLevelCurve(t *testing.T) {
	if xpToNext(0) != 100 || xpToNext(1) != 155 || xpToNext(5) != 475 {
		t.Errorf("curve: %d %d %d", xpToNext(0), xpToNext(1), xpToNext(5))
	}
	cases := []struct{ total, level, into, need int }{
		{0, 0, 0, 100},
		{99, 0, 99, 100},
		{100, 1, 0, 155},
		{254, 1, 154, 155},
		{255, 2, 0, 220},
	}
	for _, c := range cases {
		l, in, n := levelFor(c.total)
		if l != c.level || in != c.into || n != c.need {
			t.Errorf("levelFor(%d) = %d %d %d, want %d %d %d", c.total, l, in, n, c.level, c.into, c.need)
		}
	}
}

func TestXPCooldown(t *testing.T) {
	var x xpTracker
	now := time.Now()
	if !x.earn("u", now) {
		t.Fatal("first message earned nothing")
	}
	if x.earn("u", now.Add(59*time.Second)) {
		t.Fatal("earned twice within a minute")
	}
	if !x.earn("u", now.Add(61*time.Second)) {
		t.Fatal("nothing after the cooldown")
	}
}

func TestXPRanked(t *testing.T) {
	list := xpRanked(map[string]int{"a": 50, "b": 500, "c": 0, "d": 50})
	if len(list) != 3 || list[0].User != "b" || list[1].User != "a" || list[2].User != "d" {
		t.Errorf("ranked = %+v", list)
	}
	if got := xpBar(50, 100, 10); got != "▰▰▰▰▰▱▱▱▱▱" {
		t.Errorf("bar = %q", got)
	}
}

func TestRenderRankCard(t *testing.T) {
	data, err := renderRankCard(rankCard{Name: "Xykril", Level: 7, Rank: 3, Into: 120, Need: 445, Total: 2400,
		Avatar: image.NewRGBA(image.Rect(0, 0, 64, 64))})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != cardWidth*cardScale {
		t.Errorf("width %d", img.Bounds().Dx())
	}
	if dir := os.Getenv("OTAKASE_CARD_OUT"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "rank.png"), data, 0o644)
	}
}

func TestSelfRolesCard(t *testing.T) {
	roles := []*discordgo.Role{
		{ID: "1", Name: "linux"}, {ID: "2", Name: "Windows"}, {ID: "3", Name: "macOS"},
		{ID: "4", Name: "Release pings"}, {ID: "5", Name: "Tester"}, {ID: "6", Name: "Watch party pings"},
		{ID: "7", Name: "Moderator"},
	}
	found := findSelfRoles(roles)
	if len(found) != 6 || found["Linux"].ID != "1" {
		t.Fatalf("found = %v", found)
	}
	data, _ := json.Marshal(selfRolesCard(found))
	s := string(data)
	for _, id := range []string{"selfrole:1", "selfrole:6"} {
		if !strings.Contains(s, id) {
			t.Errorf("card misses %s", id)
		}
	}
	if strings.Contains(s, "selfrole:7") {
		t.Error("card offers a staff role")
	}
	// Six buttons need two rows of at most five.
	box := selfRolesCard(found)[0].(discordgo.Container)
	rows := 0
	for _, c := range box.Components {
		if r, ok := c.(discordgo.ActionsRow); ok {
			rows++
			if len(r.Components) > 5 {
				t.Errorf("row of %d buttons", len(r.Components))
			}
		}
	}
	if rows != 2 {
		t.Errorf("%d rows", rows)
	}
	// Missing roles are left off.
	delete(found, "Tester")
	if strings.Contains(jsonText(selfRolesCard(found)), "Tester") {
		t.Error("missing role on the card")
	}
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestCommunityStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	st := openStore(path)
	st.update(func(d *storeData) {
		d.SpoilerGuard = spoilerHide
		d.Guess = map[string]guessScore{"u": {Total: 2, Week: 1, WeekOf: "2026-W42"}}
		d.XP = map[string]int{"u": 340}
		d.RolesCard = &rolesCard{Channel: "c", Message: "m"}
	})
	got := openStore(path)
	if got.data.SpoilerGuard != spoilerHide || got.data.Guess["u"].Total != 2 || got.data.XP["u"] != 340 || got.data.RolesCard.Message != "m" {
		t.Errorf("round trip lost data: %+v", got.data)
	}
}
