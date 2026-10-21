// Package nyaa resolves episodes from Nyaa's public torrent index.
//
// Every other provider otakase ships scrapes a streaming site, and those share a
// failure mode: they carry a show's back catalogue but lag or omit the episodes
// that aired this week -- exactly the ones a tracked "continue watching" list
// asks for. Fansub and web-rip releases are indexed within hours of broadcast,
// there is no anti-bot gate in front of the index, and the RSS feed is a stable
// documented interface rather than markup that changes shape.
package nyaa

import (
	"regexp"
	"strconv"
	"strings"
)

// Release is one indexed torrent.
type Release struct {
	Title    string
	InfoHash string
	Size     string
	Seeders  int
	Trusted  bool
	// Episode is the episode number parsed from Title, or 0 when the release is
	// a batch or the number could not be read.
	Episode int
	// Quality is the vertical resolution, e.g. 1080. Zero when unstated.
	Quality int
	// Group is the release group, e.g. "SubsPlease".
	Group string
}

var (
	// releaseGroup matches the "[Group]" prefix convention.
	releaseGroup = regexp.MustCompile(`^\[([^\]]+)\]`)
	// qualityPattern matches "1080p", "720p" and friends.
	qualityPattern = regexp.MustCompile(`(?i)\b(\d{3,4})p\b`)
	// seasonEpisode matches the "S01E10" form.
	seasonEpisode = regexp.MustCompile(`(?i)\bS\d{1,2}E(\d{1,4})\b`)
	// dashEpisode matches the fansub convention "Title - 10 (1080p)". The
	// trailing boundary matters: without it "Title - 10" also matches inside
	// "Title - 1080p" and every release becomes episode 108.
	dashEpisode = regexp.MustCompile(`\s-\s(\d{1,4})(?:v\d)?\s*(?:[\[(]|$)`)
	// batchMarkers identify a whole-season torrent. Those cannot answer "give me
	// episode 10" without downloading the entire season, so they are skipped.
	// "Season 2" alone is not one: weekly releases name their season too.
	batchMarkers = regexp.MustCompile(`(?i)\b(batch|complete|\d{1,3}\s*[-~]\s*\d{1,3})\b`)
)

// parseRelease reads what can be known from a release title.
func parseRelease(title string) Release {
	r := Release{Title: title}

	if m := releaseGroup.FindStringSubmatch(title); m != nil {
		r.Group = m[1]
	}
	if m := qualityPattern.FindStringSubmatch(title); m != nil {
		r.Quality, _ = strconv.Atoi(m[1])
	}

	// A batch has no single episode number, so leave Episode at zero and let the
	// caller drop it.
	if isBatch(title) {
		return r
	}

	// S01E10 is unambiguous, so it wins over the dash form.
	if m := seasonEpisode.FindStringSubmatch(title); m != nil {
		r.Episode, _ = strconv.Atoi(m[1])
		return r
	}
	if m := dashEpisode.FindStringSubmatch(stripQualityHints(title)); m != nil {
		r.Episode, _ = strconv.Atoi(m[1])
	}
	return r
}

// isBatch reports whether a release covers a range of episodes.
func isBatch(title string) bool {
	// A resolution such as "1080p" is not an episode range, and a checksum like
	// "[A87EB168]" can contain digits and a dash; neither should read as a batch.
	return batchMarkers.MatchString(stripQualityHints(title))
}

// stripQualityHints removes the substrings that most often produce false
// episode numbers: resolutions, bit depth, audio channel counts and the
// bracketed CRC32 checksum that ends most fansub filenames.
var (
	noisePatterns = regexp.MustCompile(`(?i)\b\d{3,4}p\b|\b\d{1,2}\s*bit\b|\bAAC\s*\d\.\d\b|\bEAC-?3\b|\bH\.?26[45]\b|\bx26[45]\b|\[[0-9A-F]{8}\]`)
)

func stripQualityHints(title string) string {
	return noisePatterns.ReplaceAllString(title, " ")
}

// seriesKey reduces a release title to something stable enough to group the
// releases of one show together: group prefix, episode number and all the
// bracketed technical tags removed.
var (
	bracketed = regexp.MustCompile(`[\[(][^\])]*[\])]`)
	// Everything after the episode number is the episode title or technical
	// tags left outside brackets ("Show S2 - 11 x265 10bit"), never the show.
	episodeTail  = regexp.MustCompile(`(?i)\s-\s\d{1,4}(v\d)?\b.*$|\s(?i:S\d{1,2}E\d{1,4}).*$`)
	multiSpaceRE = regexp.MustCompile(`\s+`)
)

func seriesKey(title string) string {
	s := releaseGroup.ReplaceAllString(title, " ")
	s = bracketed.ReplaceAllString(s, " ")
	// Alternate titles are appended after "|"; the first is the one searched for.
	if i := strings.Index(s, "|"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), ".mkv")
	s = episodeTail.ReplaceAllString(s, "")
	s = multiSpaceRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "-~:"))
}

// seasonMarker matches the ways a title names its season: "S2", "S02E05",
// "Season 2", "2nd Season", and the roman "II" to "IV" that sequels use (upper
// case only: lower-case "ii" is too often part of a word).
var seasonMarker = regexp.MustCompile(`(?i)\bS(\d{1,2})(?:E\d{1,4})?\b|\bseason\s*(\d{1,2})\b|\b(\d{1,2})(?:st|nd|rd|th)\s+season\b|(?-i:\b(II|III|IV)\b)`)

var romanSeasons = map[string]int{"II": 2, "III": 3, "IV": 4}

// releaseSeason is the season a title names, or 0 when it names none. Groups
// that number a sequel on from the first season ("Show - 24") name none, so 0
// means "unknown", not "first".
func releaseSeason(title string) int {
	m := seasonMarker.FindStringSubmatch(title)
	if m == nil {
		return 0
	}
	if m[4] != "" {
		return romanSeasons[m[4]]
	}
	for _, digits := range m[1:4] {
		if digits != "" {
			n, _ := strconv.Atoi(digits)
			return n
		}
	}
	return 0
}

// splitSeason takes the season out of a show name: "Dandadan 2nd Season" is
// "Dandadan" and 2. A name with no season is season 1.
func splitSeason(name string) (string, int) {
	season := releaseSeason(name)
	base := seasonMarker.ReplaceAllString(name, " ")
	base = multiSpaceRE.ReplaceAllString(base, " ")
	base = strings.TrimSpace(strings.Trim(strings.TrimSpace(base), "-~:|"))
	if base == "" {
		return strings.TrimSpace(name), 1
	}
	if season < 1 {
		season = 1
	}
	return base, season
}

// seasonName is how a show's key names a season: the first season is the
// bare name, any later one gets " S2".
func seasonName(base string, season int) string {
	if season <= 1 {
		return base
	}
	return base + " S" + strconv.Itoa(season)
}

// inSeason reports whether a release can belong to season. A release naming
// no season counts as the first: a sequel numbered on from it ("Show - 24")
// cannot be told apart from the first season without knowing its length, and
// asking for episode 5 of the second season must never play the first's.
func inSeason(release Release, season int) bool {
	got := releaseSeason(release.Title)
	if season <= 1 {
		return got <= 1
	}
	return got == season
}

var (
	// foreignMarkers name a language other than English for the audio or the
	// subtitles. sukebei indexes every language under one category, so its
	// best-seeded copy of an episode is often a Latin American dub or carries
	// Spanish subtitles only.
	foreignMarkers = regexp.MustCompile(`(?i)\b(latino|lat|esp|español|espanol|spanish|castellano|doblaje|pt[\s-]?br|portugu[eê]s|dublado|legendado|vostfr|french|fran[cç]ais|gersub|german|deutsch|italian|italiano|russian|arabic|chinese|mandarin|cantonese|indonesian|vietnamese|thai|big5|chs|cht)\b|简体|繁体|简繁|繁中|简中|内封|内嵌`)
	// englishMarkers say English is there after all.
	englishMarkers = regexp.MustCompile(`(?i)\b(eng|english)\b`)
	// rawMarker is an untranslated release: Japanese audio, no subtitles.
	rawMarker = regexp.MustCompile(`(?i)\braw\b`)
)

// notEnglish reports a release that carries neither English audio nor English
// subtitles: of no use to anyone otakase plays for.
func notEnglish(release Release) bool {
	if rawMarker.MatchString(release.Title) {
		return true
	}
	return foreignMarkers.MatchString(release.Title) && !englishMarkers.MatchString(release.Title)
}

// sameShowName compares two show names the way groups vary them: case,
// punctuation and spacing aside.
func sameShowName(a, b string) bool {
	return normalizeName(a) == normalizeName(b)
}

var nameNoise = regexp.MustCompile(`[^\p{L}\p{N}]+`)

func normalizeName(name string) string {
	return strings.TrimSpace(nameNoise.ReplaceAllString(strings.ToLower(name), " "))
}

// containsName reports whether a title mentions a show name, as whole words.
func containsName(title, name string) bool {
	name = normalizeName(name)
	return name != "" && strings.Contains(" "+normalizeName(title)+" ", " "+name+" ")
}

// extendsName reports a normalised name that is another one with more words
// after it: "sousou no frieren marumaru no mahou" extends "sousou no frieren".
func extendsName(name, of string) bool {
	of = normalizeName(of)
	return of != "" && name != of && strings.HasPrefix(name, of+" ")
}

// strayDash is a dash standing apart from a word, as in "Frieren- Beyond" or
// "Show - Subtitle".
var strayDash = regexp.MustCompile(`\s+-+\s*|\s*-+\s+|-+$`)

// searchName is a show name as the index can search for it. The index matches
// words as written, so "Journey's" must stay as it is, but a dash standing on
// its own reads as "without the next word" and finds nothing.
func searchName(name string) string {
	return strings.TrimSpace(multiSpaceRE.ReplaceAllString(strayDash.ReplaceAllString(name, " "), " "))
}

var (
	// alternateName is a name a release gives its show besides the one it is
	// filed under: in parentheses, or after a "|".
	alternateName = regexp.MustCompile(`\(([^)]*)\)|\|([^|\[(]*)`)
	// technicalTag is a parenthesised tag that is not a name at all.
	technicalTag = regexp.MustCompile(`(?i)\b(subs?|dub(bed)?|audio|web|web-?dl|webrip|bd|bdrip|hevc|avc|x26[45]|h\s?26[45]|aac|flac|opus|\d{3,4}p|batch|weekly|multi|dual|uncensored|ova|eng|english)\b`)
	letters      = regexp.MustCompile(`\p{L}`)
)

// alternateNames are the other names a release title gives its show,
// normalised, season taken out: "(Kaijuu 8-gou, Dual-Audio)" names
// "kaijuu 8 gou".
func alternateNames(title string) []string {
	var names []string
	for _, m := range alternateName.FindAllStringSubmatch(title, -1) {
		for _, part := range []string{m[1], m[2]} {
			part, _, _ = strings.Cut(part, ",")
			part, _, _ = strings.Cut(part, "|")
			part = strings.TrimSpace(part)
			if part == "" || technicalTag.MatchString(part) || len(letters.FindAllString(part, -1)) < 5 {
				continue
			}
			name, _ := splitSeason(part)
			if name = normalizeName(name); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}
