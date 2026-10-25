package internal

import (
	"os"
	"path/filepath"
	"testing"
)

// Rows as each version of the history file wrote them, all in one file: a
// file that was appended to across versions holds a mix.
const mixedHistory = "1,abc,3,100,5\n" +
	"2,def,4,200,Old Name\n" +
	"3,ghi,5,300,1400,Six Col\n" +
	"4,kickassanime::jkl,6,400,1420,kickassanime,\"Name, with \"\"quotes\"\"\"\n"

// writeHistoryFixture puts contents in a history file of its own and makes
// GetAnimeName independent of whatever config another test left behind.
func writeHistoryFixture(t *testing.T, contents string) string {
	t.Helper()
	previous := GetGlobalConfig()
	SetGlobalConfig(nil)
	t.Cleanup(func() { SetGlobalConfig(previous) })

	path := filepath.Join(t.TempDir(), "history.csv")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write history fixture: %v", err)
	}
	return path
}

func readHistoryFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	return string(data)
}

func TestParseAnimeRowLayouts(t *testing.T) {
	tests := []struct {
		name         string
		row          []string
		duration     int
		title        string
		providerName string
	}{
		{"five columns with a name", []string{"123", "p", "4", "55", "Legacy Title"}, 0, "Legacy Title", "allanime"},
		{"five columns with a duration", []string{"123", "p", "4", "55", "24"}, 24, "123", "allanime"},
		{"five columns, padded number is a name", []string{"123", "p", "4", "55", " 24"}, 0, " 24", "allanime"},
		{"six columns", []string{"123", "p", "4", "55", " 24 ", "Six"}, 24, "Six", "allanime"},
		{"seven columns", []string{"123", "p", "4", "55", "24", "kickassanime", "Current"}, 24, "Current", "kickassanime"},
		{"unreadable duration", []string{"123", "p", "4", "55", "n/a", "kickassanime", "Current"}, 0, "Current", "kickassanime"},
		{"padded required numbers", []string{" 123", "p", "4 ", " 55 ", "24", "kickassanime", "Current"}, 24, "Current", "kickassanime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAnimeRow(tt.row)
			if got == nil {
				t.Fatalf("row %q did not parse", tt.row)
			}
			if got.AnilistId != 123 || got.ProviderId != "p" || got.Ep.Number != 4 || got.Ep.Player.PlaybackTime != 55 {
				t.Fatalf("required columns misread: %+v", got)
			}
			if got.Ep.Duration != tt.duration || got.Title.Romaji != tt.title || got.Title.English != tt.title || got.ProviderName != tt.providerName {
				t.Fatalf("got duration %d, title %q, provider %q; want %d, %q, %q",
					got.Ep.Duration, got.Title.Romaji, got.ProviderName, tt.duration, tt.title, tt.providerName)
			}
		})
	}

	for _, row := range [][]string{
		{"123", "p", "4", "55"},
		{"bad-id", "p", "4", "55", "24", "anikoto", "Broken"},
		{"123", "p", "four", "55", "24", "anikoto", "Broken"},
		{"123", "p", "4", "", "24", "anikoto", "Broken"},
	} {
		if got := parseAnimeRow(row); got != nil {
			t.Errorf("row %q should be rejected, got %+v", row, got)
		}
	}
}

func TestGetAnimeNameWithoutGlobalConfig(t *testing.T) {
	previous := GetGlobalConfig()
	SetGlobalConfig(nil)
	t.Cleanup(func() { SetGlobalConfig(previous) })

	if name := GetAnimeName(Anime{Title: AnimeTitle{English: "English Title", Romaji: "Romaji Title"}}); name != "English Title" {
		t.Fatalf("no config should mean English, got %q", name)
	}
	if name := GetAnimeName(Anime{Title: AnimeTitle{Romaji: "Romaji Title"}}); name != "Romaji Title" {
		t.Fatalf("a missing English title should fall back to romaji, got %q", name)
	}
	if name := GetAnimeName(Anime{AnilistId: 42}); name != "42" {
		t.Fatalf("an untitled show should be named by its id, got %q", name)
	}
	if name := GetAnimeName(Anime{}); name != "" {
		t.Fatalf("nothing to name a show by should give an empty name, got %q", name)
	}
}

func TestHistoryReadsEveryOldRowLayout(t *testing.T) {
	path := writeHistoryFixture(t, mixedHistory)

	rows := LocalGetAllAnime(path)
	if len(rows) != 4 {
		t.Fatalf("read %d rows, want 4: %+v", len(rows), rows)
	}
	if rows[0].Ep.Duration != 5 || GetAnimeName(rows[0]) != "1" {
		t.Errorf("five-column duration row misread: %+v", rows[0])
	}
	if GetAnimeName(rows[1]) != "Old Name" || rows[1].ProviderName != "allanime" {
		t.Errorf("five-column name row misread: %+v", rows[1])
	}
	if rows[2].Ep.Duration != 1400 || GetAnimeName(rows[2]) != "Six Col" {
		t.Errorf("six-column row misread: %+v", rows[2])
	}
	if rows[3].ProviderId != "kickassanime::jkl" || GetAnimeName(rows[3]) != `Name, with "quotes"` {
		t.Errorf("seven-column row misread: %+v", rows[3])
	}
}

// One update rewrites the whole file in the current layout. The bytes are
// pinned because existing history files are read back by this same code.
func TestHistoryRoundTripsThroughAnUpdate(t *testing.T) {
	path := writeHistoryFixture(t, mixedHistory)

	if err := LocalUpdateAnime(path, 3, "ghi", 6, 10, 1400, "Six Col", "anizone"); err != nil {
		t.Fatalf("update: %v", err)
	}

	want := "1,abc,3,100,5,allanime,1\n" +
		"2,def,4,200,0,allanime,Old Name\n" +
		"3,ghi,6,10,1400,anizone,Six Col\n" +
		"4,jkl,6,400,1420,kickassanime,\"Name, with \"\"quotes\"\"\"\n"
	if got := readHistoryFile(t, path); got != want {
		t.Fatalf("history after update:\n%s\nwant:\n%s", got, want)
	}

	// Reading the rewritten file back gives the same shows.
	rows := LocalGetAllAnime(path)
	if len(rows) != 4 || rows[2].Ep.Number != 6 || rows[2].ProviderName != "anizone" || GetAnimeName(rows[3]) != `Name, with "quotes"` {
		t.Fatalf("rewritten history read back wrong: %+v", rows)
	}
	if err := LocalUpdateAnime(path, 3, "ghi", 6, 10, 1400, "Six Col", "anizone"); err != nil {
		t.Fatalf("second update: %v", err)
	}
	if got := readHistoryFile(t, path); got != want {
		t.Fatalf("an identical update changed the file:\n%s", got)
	}
}

func TestLocalUpdateAnimeMatchesOnProviderID(t *testing.T) {
	path := writeHistoryFixture(t, "4,jkl,6,400,1420,kickassanime,Show\n")

	// A qualified id names the same row as the bare one on file.
	if err := LocalUpdateAnime(path, 4, "kickassanime::jkl", 7, 5, 1420, "Show", "kickassanime"); err != nil {
		t.Fatalf("update: %v", err)
	}
	// Same show, another provider: a row of its own.
	if err := LocalUpdateAnime(path, 4, "xyz", 2, 0, 0, "", "anizone"); err != nil {
		t.Fatalf("add: %v", err)
	}

	want := "4,jkl,7,5,1420,kickassanime,Show\n" +
		"4,xyz,2,0,0,anizone,4\n"
	if got := readHistoryFile(t, path); got != want {
		t.Fatalf("history:\n%s\nwant:\n%s", got, want)
	}
}

func TestLocalUpdateAnimeStartsAMissingFile(t *testing.T) {
	previous := GetGlobalConfig()
	SetGlobalConfig(nil)
	t.Cleanup(func() { SetGlobalConfig(previous) })
	path := filepath.Join(t.TempDir(), "nested", "history.csv")

	if rows := LocalGetAllAnime(path); len(rows) != 0 {
		t.Fatalf("a missing history should read as empty, got %+v", rows)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("reading should have created the file: %v", err)
	}
	if err := LocalUpdateAnime(path, 9, "id", 1, 2, 3, "Nine", "anizone"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := readHistoryFile(t, path); got != "9,id,1,2,3,anizone,Nine\n" {
		t.Fatalf("history: %q", got)
	}
}

func TestHistoryRewriteKeepsModeAndLeavesNoTemporaryFile(t *testing.T) {
	path := writeHistoryFixture(t, mixedHistory)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := LocalUpdateAnime(path, 1, "abc", 4, 0, 5, "One", "allanime"); err != nil {
		t.Fatalf("update: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("history mode is %o after a rewrite, want 600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("history directory holds %d files after a rewrite, want 1", len(entries))
	}
}

func TestLocalDeleteAnimeLeavesOtherRowsAsWritten(t *testing.T) {
	path := writeHistoryFixture(t, "x,bad,1,1,1\n"+mixedHistory)

	LocalDeleteAnime(path, 2, "def")
	LocalDeleteAnime(path, 4, "jkl") // the file has it qualified
	LocalDeleteAnime(path, 3, "other-provider")

	// Old short rows stay short, and the unreadable row goes.
	want := "1,abc,3,100,5\n" +
		"3,ghi,5,300,1400,Six Col\n"
	if got := readHistoryFile(t, path); got != want {
		t.Fatalf("history after delete:\n%s\nwant:\n%s", got, want)
	}

	LocalDeleteAnime(path, 1, "abc")
	LocalDeleteAnime(path, 3, "ghi")
	if got := readHistoryFile(t, path); got != "" {
		t.Fatalf("deleting every row should leave an empty file, got %q", got)
	}
}

func TestLocalDeleteAnimeOnAMissingFileCreatesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.csv")
	LocalDeleteAnime(path, 1, "abc")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("delete created a history file: %v", err)
	}
}

func TestLocalRemapAnimeProviderMovesEveryRowOfTheShow(t *testing.T) {
	path := writeHistoryFixture(t, "7,old,2,90,1400,anikoto,Seven\n"+
		"7,old,1,30,1400,anikoto,Seven\n"+
		"7,other,1,10,1400,anizone,Seven\n"+
		"8,eight,1,1,1,anikoto,Eight\n")

	if err := LocalRemapAnimeProvider(path, 7, "kickassanime", "kickassanime::new", "", 3, 50, 1410); err != nil {
		t.Fatalf("remap: %v", err)
	}
	if err := LocalRemapAnimeProvider(path, 9, "kickassanime", "nine", "Nine", 1, 0, 0); err != nil {
		t.Fatalf("remap of a show with no history: %v", err)
	}

	want := "7,new,3,50,1410,kickassanime,Seven\n" +
		"7,new,3,50,1410,kickassanime,Seven\n" +
		"7,other,1,10,1400,anizone,Seven\n" +
		"8,eight,1,1,1,anikoto,Eight\n" +
		"9,nine,1,0,0,kickassanime,Nine\n"
	if got := readHistoryFile(t, path); got != want {
		t.Fatalf("history after remap:\n%s\nwant:\n%s", got, want)
	}
}

func TestLocalFindAnimePicksTheFurthestRow(t *testing.T) {
	rows := []Anime{
		{AnilistId: 1, ProviderId: "a"},
		{AnilistId: 1, ProviderId: "b"},
		{AnilistId: 2, ProviderId: "c"},
		{AnilistId: 3, ProviderId: "a"},
	}
	rows[0].Ep.Number, rows[0].Ep.Player.PlaybackTime = 5, 10
	rows[1].Ep.Number, rows[1].Ep.Player.PlaybackTime = 5, 20
	rows[2].Ep.Number = 9
	rows[3].Ep.Number = 6

	if got := LocalFindAnime(rows, 1, ""); got != &rows[1] {
		t.Fatalf("same episode should go to the later position, got %+v", got)
	}
	if got := LocalFindAnime(rows, 1, "a"); got != &rows[3] {
		t.Fatalf("a provider id match should count too, got %+v", got)
	}
	if got := LocalFindAnime(rows, 4, ""); got != nil {
		t.Fatalf("no match should be nil, got %+v", got)
	}
}
