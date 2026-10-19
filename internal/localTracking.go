package internal

import (
	"bytes"
	"cmp"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The local history is a headerless CSV file, one row per show and provider:
//
//	anilist id, provider id, episode, position, duration, provider, name
//
// Older versions wrote shorter rows (see parseAnimeRow), and those are still
// read. Rows are only ever written in the full seven-column form.

// legacyHistoryProvider is the provider of every row written before the
// provider column existed.
const legacyHistoryProvider = "allanime"

// localProviderID strips a provider qualifier from a stored id.
//
// It used to also fold animepahe's per-session ids onto something stable, and
// took the provider name and title for that; no remaining provider hands out an
// id that changes between runs.
func localProviderID(providerID string) string {
	if _, raw, ok := ParseProviderQualifiedID(providerID); ok {
		return raw
	}
	return providerID
}

// readHistoryRows returns the raw rows of a history file. The field count is
// left unchecked: a file can hold rows from before a column was added, and
// encoding/csv would otherwise reject the whole file over one short row.
func readHistoryRows(r io.Reader) ([][]string, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	return reader.ReadAll()
}

// writeHistoryRows replaces the history file with rows. It goes through a
// temporary file so that a crash mid-write cannot cost the user their whole
// history, and keeps the mode of the file it replaces.
func writeHistoryRows(path string, rows [][]string) error {
	var buf bytes.Buffer
	if err := csv.NewWriter(&buf).WriteAll(rows); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	return writeFileAtomic(path, buf.Bytes(), perm)
}

func historyRecord(anime Anime) []string {
	return []string{
		strconv.Itoa(anime.AnilistId),
		localProviderID(anime.ProviderId),
		strconv.Itoa(anime.Ep.Number),
		strconv.Itoa(anime.Ep.Player.PlaybackTime),
		strconv.Itoa(anime.Ep.Duration),
		anime.ProviderName,
		GetAnimeName(anime),
	}
}

func writeHistory(path string, animeList []Anime) error {
	rows := make([][]string, len(animeList))
	for i, anime := range animeList {
		rows[i] = historyRecord(anime)
	}
	return writeHistoryRows(path, rows)
}

// sameHistoryRow reports whether row is the one kept for a show under a
// provider id that has already been through localProviderID.
func sameHistoryRow(row Anime, anilistID int, providerID string) bool {
	return row.AnilistId == anilistID && localProviderID(row.ProviderId) == providerID
}

// LocalDeleteAnime removes the rows of one show under one provider id.
func LocalDeleteAnime(databaseFile string, anilistID int, providerID string) {
	data, readErr := os.ReadFile(databaseFile)
	if readErr != nil {
		Out(fmt.Sprintf("Error opening file: %v", readErr))
		return
	}
	rows, parseErr := readHistoryRows(bytes.NewReader(data))
	if parseErr != nil {
		Out(fmt.Sprintf("Error reading file: %v", parseErr))
		return
	}

	target := localProviderID(providerID)
	// Survivors are written back exactly as read, so an old short row is not
	// upgraded as a side effect of deleting something else.
	survivors := slices.DeleteFunc(rows, func(row []string) bool {
		parsed := parseAnimeRow(row)
		if parsed == nil {
			Out(fmt.Sprintf("Skipping invalid local history row: %v", row))
			return true
		}
		return sameHistoryRow(*parsed, anilistID, target)
	})

	if writeErr := writeHistoryRows(databaseFile, survivors); writeErr != nil {
		Out(fmt.Sprintf("Error writing to file: %v", writeErr))
	}
}

// openHistory reads the rows of the history file, creating it (and its
// directory) empty when it is missing: the rest of the history code expects
// the file to be there after the first read. Failures are reported to the
// user and come back as ok=false.
func openHistory(path string) (rows [][]string, ok bool) {
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		Out(fmt.Sprintf("Error creating directory: %v", mkdirErr))
		return nil, false
	}
	file, openErr := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o644)
	if openErr != nil {
		Out(fmt.Sprintf("Error opening or creating file: %v", openErr))
		return nil, false
	}
	defer file.Close()

	rows, readErr := readHistoryRows(file)
	if readErr != nil {
		Out(fmt.Sprintf("Error reading file: %v", readErr))
		return nil, false
	}
	return rows, true
}

// LocalGetAllAnime reads every valid row of the history file. It never
// returns nil, even when the file cannot be read.
func LocalGetAllAnime(historyPath string) []Anime {
	shows := []Anime{}
	rows, ok := openHistory(historyPath)
	if !ok {
		return shows
	}
	for _, row := range rows {
		if parsed := parseAnimeRow(row); parsed != nil {
			shows = append(shows, *parsed)
		}
	}
	return shows
}

// parseAnimeRow reads one history row in any of the layouts it has had:
//
//	7 columns: id, provider id, episode, position, duration, provider, name
//	6 columns: id, provider id, episode, position, duration, name
//	5 columns: id, provider id, episode, position, then a duration or a name
//
// It returns nil for a row that cannot be used.
func parseAnimeRow(fields []string) *Anime {
	if len(fields) < 5 {
		Out(fmt.Sprintf("Invalid row format: %v", fields))
		return nil
	}

	// Columns that every layout has and that must be numbers.
	required := []struct {
		column int
		label  string
	}{{0, "AniList ID"}, {2, "episode"}, {3, "playback time"}}
	var numbers [3]int
	for i, want := range required {
		n, ok := parseRequiredLocalInt(fields[want.column], want.label, fields)
		if !ok {
			return nil
		}
		numbers[i] = n
	}

	parsed := &Anime{AnilistId: numbers[0], ProviderId: fields[1], ProviderName: legacyHistoryProvider}
	parsed.Ep.Number = numbers[1]
	parsed.Ep.Player.PlaybackTime = numbers[2]
	// A row with no name is shown by its AniList id.
	title := strconv.Itoa(parsed.AnilistId)

	switch len(fields) {
	case 5:
		// The fifth column held a duration in some versions and a name in
		// others. Deliberately untrimmed: " 24" was a name, not a number.
		if seconds, err := strconv.Atoi(fields[4]); err == nil {
			parsed.Ep.Duration = seconds
		} else {
			title = fields[4]
		}
	case 6:
		// Six columns predate the provider column too, so the provider stays
		// the legacy one; the row is remapped to a working provider the next
		// time it is played.
		parsed.Ep.Duration = parseLocalInt(fields[4])
		title = fields[5]
	default:
		parsed.Ep.Duration = parseLocalInt(fields[4])
		parsed.ProviderName, title = fields[5], fields[6]
	}

	parsed.Title.English, parsed.Title.Romaji = title, title
	return parsed
}

// parseLocalInt reads an optional number column; anything unreadable is zero.
func parseLocalInt(field string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(field))
	return n
}

// parseRequiredLocalInt reads a column the row is useless without, and says
// which one when it is not a number.
func parseRequiredLocalInt(column, label string, row []string) (int, bool) {
	n, convErr := strconv.Atoi(strings.TrimSpace(column))
	if convErr == nil {
		return n, true
	}
	Out(fmt.Sprintf("Invalid %s in local history row %v: %v", label, row, convErr))
	return 0, false
}

// GetAnimeName is the title to display for show: the configured language when it
// has one, then whichever title it does have, then its AniList id.
func GetAnimeName(show Anime) string {
	titles := show.Title
	if titles.English != "" && useEnglishAnimeNames(GetGlobalConfig()) {
		return titles.English
	}
	if known := cmp.Or(titles.Romaji, titles.English); known != "" {
		return known
	}
	if show.AnilistId == 0 {
		return ""
	}
	return strconv.Itoa(show.AnilistId)
}

// historyPosition is where a history row stands: provider, episode, position
// within it and its length.
type historyPosition struct {
	providerName, providerID string
	episode, seconds, length int
}

// apply moves row to p. The title is left to the caller.
func (p historyPosition) apply(row *Anime) {
	row.ProviderId = p.providerID
	row.ProviderName = p.providerName
	row.Ep.Number = p.episode
	row.Ep.Player.PlaybackTime = p.seconds
	row.Ep.Duration = p.length
}

func setHistoryTitle(row *Anime, name string) {
	row.Title.English, row.Title.Romaji = name, name
}

// LocalUpdateAnime records progress for one show under one provider id,
// adding a row when it has none yet, and rewrites the history file.
func LocalUpdateAnime(databaseFile string, anilistID int, providerID string, watchingEpisode int, playbackTime int, animeDuration int, animeName string, providerName string) error {
	at := historyPosition{
		providerName: providerName,
		providerID:   localProviderID(providerID),
		episode:      watchingEpisode,
		seconds:      playbackTime,
		length:       animeDuration,
	}
	shows := LocalGetAllAnime(databaseFile)

	idx := slices.IndexFunc(shows, func(row Anime) bool { return sameHistoryRow(row, anilistID, at.providerID) })
	if idx == -1 {
		shows = append(shows, Anime{AnilistId: anilistID})
		idx = len(shows) - 1
	}
	at.apply(&shows[idx])
	setHistoryTitle(&shows[idx], animeName)

	saveErr := writeHistory(databaseFile, shows)
	if saveErr != nil {
		Out(fmt.Sprintf("Error saving local history: %v", saveErr))
	}
	return saveErr
}

// LocalFindAnime returns the furthest-along row matching either id, or nil.
// An empty providerID matches on the AniList id alone.
func LocalFindAnime(animeList []Anime, anilistID int, providerID string) *Anime {
	var furthest *Anime
	for idx := range animeList {
		candidate := &animeList[idx]
		matches := candidate.AnilistId == anilistID ||
			(providerID != "" && candidate.ProviderId == providerID)
		if matches && (furthest == nil || historyRowAhead(candidate, furthest)) {
			furthest = candidate
		}
	}
	return furthest
}

// historyRowAhead orders rows by episode, then by position in it.
func historyRowAhead(a, b *Anime) bool {
	if a.Ep.Number == b.Ep.Number {
		return a.Ep.Player.PlaybackTime > b.Ep.Player.PlaybackTime
	}
	return a.Ep.Number > b.Ep.Number
}

// LocalRemapAnimeProvider moves a show's history onto another provider, or
// adds it when the history has no row for it.
//
// Every row the show has under its current provider moves, not just the
// furthest one: a duplicate left behind would still point at the old provider.
func LocalRemapAnimeProvider(historyPath string, anilistID int, providerName, providerID, title string, episode, position, duration int) error {
	to := historyPosition{
		providerName: providerName,
		providerID:   localProviderID(providerID),
		episode:      episode,
		seconds:      position,
		length:       duration,
	}
	shows := LocalGetAllAnime(historyPath)

	current := LocalFindAnime(shows, anilistID, "")
	if current == nil {
		added := Anime{AnilistId: anilistID}
		to.apply(&added)
		setHistoryTitle(&added, title)
		shows = append(shows, added)
	} else {
		from := localProviderID(current.ProviderId)
		for idx := range shows {
			if !sameHistoryRow(shows[idx], anilistID, from) {
				continue
			}
			to.apply(&shows[idx])
			// No title given keeps the one on record.
			if title != "" {
				setHistoryTitle(&shows[idx], title)
			}
		}
	}

	if saveErr := writeHistory(historyPath, shows); saveErr != nil {
		return fmt.Errorf("save local history: %w", saveErr)
	}
	return nil
}

// LocalFindEpisode returns the history row for one episode of one show.
//
// LocalFindAnime answers a different question: it returns the furthest-ahead row
// for the show, which is what "where is this show up to" needs. For "where in
// this episode was I" it is wrong, and wrong in a way that hides itself. A show
// watched through more than one provider has a row each, and if any of them is
// further ahead its position wins -- so a nine minute position in the episode
// about to play was replaced by a one second position in a later one, and the
// resume did nothing at all.
//
// The row written by providerName is preferred, because that is the one the
// position being looked for was written to, and a position from another provider
// is a position in a different encode of the episode.
func LocalFindEpisode(animeList []Anime, anilistID, episode int, providerName string) *Anime {
	providerName = strings.TrimSpace(strings.ToLower(providerName))

	var best *Anime
	bestProvider := false
	for i := range animeList {
		row := &animeList[i]
		if row.AnilistId != anilistID || row.Ep.Number != episode {
			continue
		}
		sameProvider := providerName != "" && strings.EqualFold(strings.TrimSpace(row.ProviderName), providerName)
		switch {
		case best == nil:
		case sameProvider && !bestProvider:
		case sameProvider == bestProvider && row.Ep.Player.PlaybackTime > best.Ep.Player.PlaybackTime:
		default:
			continue
		}
		best, bestProvider = row, sameProvider
	}
	return best
}
