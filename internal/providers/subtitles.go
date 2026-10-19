package providers

import (
	"sort"
	"strings"
)

// subtitleLanguages maps every spelling hosts use for a language onto one
// canonical name. The first entry of each list is what mpv's --slang wants.
var subtitleLanguages = map[string][]string{
	"english":    {"en", "eng", "english"},
	"japanese":   {"ja", "jpn", "japanese"},
	"spanish":    {"es", "spa", "spanish", "español", "espanol", "latino"},
	"portuguese": {"pt", "por", "portuguese", "português", "portugues", "pt-br", "ptbr"},
	"french":     {"fr", "fre", "fra", "french", "français", "francais"},
	"german":     {"de", "ger", "deu", "german", "deutsch"},
	"italian":    {"it", "ita", "italian", "italiano"},
	"russian":    {"ru", "rus", "russian"},
	"arabic":     {"ar", "ara", "arabic"},
	"indonesian": {"id", "ind", "indonesian", "bahasa indonesia"},
	"malay":      {"ms", "may", "msa", "malay"},
	"thai":       {"th", "tha", "thai"},
	"vietnamese": {"vi", "vie", "vietnamese"},
	"turkish":    {"tr", "tur", "turkish"},
	"polish":     {"pl", "pol", "polish"},
	"chinese":    {"zh", "chi", "zho", "chinese"},
	"korean":     {"ko", "kor", "korean"},
	"hindi":      {"hi", "hin", "hindi"},
	"lithuanian": {"lt", "lit", "lithuanian", "lietuvių", "lietuviu"},
}

// CanonicalLanguage turns a code or name ("eng", "English", "pt-BR",
// "Portuguese - Brazilian") into one lowercase name ("english", "portuguese").
// Anything it does not know comes back lowercased and trimmed, so two unknown
// spellings still compare equal when they are the same word.
func CanonicalLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	for name, spellings := range subtitleLanguages {
		for _, spelling := range spellings {
			if value == spelling {
				return name
			}
		}
	}
	// Labels carry more than the language: "English [CC]", "Portuguese -
	// Brazilian", "Spanish (Latin America)". The first word decides.
	first := strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '(' || r == '[' || r == ','
	})
	if len(first) > 0 && first[0] != value {
		if name := CanonicalLanguage(first[0]); name != "" {
			if _, known := subtitleLanguages[name]; known {
				return name
			}
		}
	}
	return value
}

// MPVLanguageCodes is the --slang value for a language: every code mpv may
// find in a track's language tag, most common first.
func MPVLanguageCodes(language string) string {
	name := CanonicalLanguage(language)
	spellings, ok := subtitleLanguages[name]
	if !ok {
		return name
	}
	codes := make([]string, 0, len(spellings))
	for _, spelling := range spellings {
		if len(spelling) <= 3 {
			codes = append(codes, spelling)
		}
	}
	return strings.Join(codes, ",")
}

// SubtitleLanguageNames lists the languages known by name, sorted.
func SubtitleLanguageNames() []string {
	names := make([]string, 0, len(subtitleLanguages))
	for name := range subtitleLanguages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ThreeLetterCode is a language's ISO 639-2 code, as OpenSubtitles names
// languages: "eng", "lit". An unknown language gives "".
func ThreeLetterCode(language string) string {
	for _, spelling := range subtitleLanguages[CanonicalLanguage(language)] {
		if len(spelling) == 3 {
			return spelling
		}
	}
	return ""
}

// TrackLanguage is the canonical language of a track, read from its language
// field and, when that is empty, from its label.
func TrackLanguage(track SubtitleTrack) string {
	if language := CanonicalLanguage(track.Language); language != "" {
		return language
	}
	return CanonicalLanguage(track.Label)
}

// PickSubtitle chooses the track in the wanted language, or fallback when no
// track is in it. A plain track wins over a closed-caption or signs-only one
// in the same language, since those are the variants people pick on purpose.
func PickSubtitle(tracks []SubtitleTrack, wanted, fallback string) string {
	wanted = CanonicalLanguage(wanted)
	if wanted == "" {
		return fallback
	}
	best := ""
	for _, track := range tracks {
		if strings.TrimSpace(track.URL) == "" || TrackLanguage(track) != wanted {
			continue
		}
		if !isSpecialSubtitle(track) {
			return track.URL
		}
		if best == "" {
			best = track.URL
		}
	}
	if best != "" {
		return best
	}
	return fallback
}

// isSpecialSubtitle reports a track made for a narrower purpose than plain
// dialogue: closed captions, or signs and songs only.
func isSpecialSubtitle(track SubtitleTrack) bool {
	label := strings.ToLower(track.Label + " " + track.Language)
	for _, marker := range []string{"[cc]", "(cc)", " cc", "sdh", "signs", "forced"} {
		if strings.Contains(label, marker) {
			return true
		}
	}
	return false
}
