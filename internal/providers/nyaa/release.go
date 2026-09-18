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
	batchMarkers = regexp.MustCompile(`(?i)\b(batch|complete|season\s*\d+\s*(complete)?|\d{1,3}\s*[-~]\s*\d{1,3})\b`)
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
	bracketed    = regexp.MustCompile(`[\[(][^\])]*[\])]`)
	episodeTail  = regexp.MustCompile(`(?i)\s-\s\d{1,4}(v\d)?\s*$|\s(?i:S\d{1,2}E\d{1,4}).*$`)
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
