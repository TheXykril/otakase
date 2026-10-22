package main

import (
	"regexp"
	"strings"
	"unicode"
)

// The time capsule filter runs on every message before it is sealed, so
// staff never have to approve anything. Three layers, cheapest first:
//
//  1. personal data: links, emails, phone and other long numbers,
//     mentions, invites, usernames with tags and street addresses;
//  2. a word list that sees through l33t ("sh1t"), stretched letters
//     ("fuuuck") and spaced-out spellings ("f u c k");
//  3. a meaning check with Ollama (capsule.go), for what the words miss.
//
// A blocked message is never stored: the sender is told why and can write
// it again.

const (
	capsuleMinRunes = 3
	capsuleMaxRunes = 300
)

var (
	capsuleLinkRe    = regexp.MustCompile(`(?i)\b(https?://|www\.)|\b[a-z0-9-]{2,}\.(com|net|org|io|gg|me|xyz|ru|co|tv|app|dev|link|ly|to|lt|uk|de|info|site|online|live|club|shop)\b`)
	capsuleEmailRe   = regexp.MustCompile(`(?i)[a-z0-9._%+-]+\s*(@|\(at\)|\[at\])\s*[a-z0-9-]+\s*(\.|\(dot\)|\[dot\])\s*[a-z]{2,}`)
	capsuleMentionRe = regexp.MustCompile(`<[@#][!&]?\d+>|@everyone|@here|(^|\s)@[\w.]{2,}`)
	capsuleInviteRe  = regexp.MustCompile(`(?i)discord(app)?\s*\.\s*(gg|com\s*/\s*invite)|\.gg/`)
	capsuleTagRe     = regexp.MustCompile(`\w#\d{4}\b`)
	capsuleDigitsRe  = regexp.MustCompile(`\d[\d\s().+/-]*\d`)
	capsuleAddressRe = regexp.MustCompile(`(?i)\b\d{1,5}[a-z]?\s+([\p{L}.'-]+\s+){0,3}(street|st|avenue|ave|road|rd|lane|ln|drive|dr|boulevard|blvd|court|ct|place|pl|gatve|gatvė|g)\b\.?`)
	capsuleSocialRe  = regexp.MustCompile(`(?i)\b(snap(chat)?|insta(gram)?|ig|tiktok|telegram|whatsapp|kik|discord|twitter|phone|number|address|password|passwd)\s*[:=]`)
)

// capsulePersonalReason says what personal data text holds, "" for none.
func capsulePersonalReason(text string) string {
	switch {
	case capsuleInviteRe.MatchString(text):
		return "Invites can't go in the time capsule."
	case capsuleEmailRe.MatchString(text):
		return "Email addresses can't go in the time capsule."
	case capsuleLinkRe.MatchString(text):
		return "Links can't go in the time capsule."
	case capsuleMentionRe.MatchString(text), capsuleTagRe.MatchString(text):
		return "Mentions and usernames can't go in the time capsule."
	case capsuleSocialRe.MatchString(text):
		return "Contact details and passwords can't go in the time capsule."
	case capsuleAddressRe.MatchString(text):
		return "Addresses can't go in the time capsule."
	}
	for _, run := range capsuleDigitsRe.FindAllString(text, -1) {
		// Years and episode counts are fine; phone, card and ID numbers
		// are not.
		digits := 0
		for _, r := range run {
			if unicode.IsDigit(r) {
				digits++
			}
		}
		if digits >= 7 {
			return "Phone numbers and other long numbers can't go in the time capsule."
		}
	}
	return ""
}

// capsuleLeet turns look-alike digits and symbols back into letters.
var capsuleLeet = strings.NewReplacer(
	"0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t", "8", "b", "9", "g",
	"@", "a", "$", "s", "!", "i", "|", "l", "+", "t", "€", "e",
)

// capsuleSqueeze drops repeated letters, so "fuuuck" and "fuck" compare
// equal (and "ass" reads "as": the lists are squeezed the same way).
func capsuleSqueeze(s string) string {
	var b strings.Builder
	var last rune
	for _, r := range s {
		if r != last {
			b.WriteRune(r)
		}
		last = r
	}
	return b.String()
}

// capsuleTokens is text as the word list sees it: lower case, accents
// folded, l33t undone, letters only. Runs of single letters ("f u c k",
// "f.u.c.k") are joined into one token.
func capsuleTokens(text string) []string {
	var raw []string
	for _, f := range strings.FieldsFunc(accents.Replace(strings.ToLower(text)), func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';' || r == ':' || r == '?' || r == '"' || r == '(' || r == ')'
	}) {
		// Pure numbers stay numbers ("2026" is a year, not a word).
		if strings.IndexFunc(f, unicode.IsLetter) < 0 {
			continue
		}
		// "great!" is excitement, not a hidden "i".
		f = capsuleLeet.Replace(strings.TrimRight(f, "!."))
		// Dots, dashes and stars inside a word split it into letters,
		// which the single-letter join below puts back together.
		raw = append(raw, strings.FieldsFunc(f, func(r rune) bool { return !unicode.IsLetter(r) })...)
	}
	var tokens []string
	var singles strings.Builder
	flush := func() {
		if singles.Len() > 1 {
			tokens = append(tokens, singles.String())
		}
		singles.Reset()
	}
	for _, w := range raw {
		if len([]rune(w)) == 1 {
			singles.WriteString(w)
			continue
		}
		flush()
		tokens = append(tokens, w)
	}
	flush()
	return tokens
}

// capsuleStems block any word they appear in. They are long or odd enough
// not to hide inside ordinary words.
var capsuleStems = []string{
	"fuck", "fck", "shit", "cunt", "faggot", "fagget", "retard", "whore", "slut", "bitch", "bastard", "wank",
	"twat", "pussy", "porn", "hentai", "dildo", "blowjob", "handjob", "jizz", "asshole", "motherf", "pedophil",
	"molest", "nazi", "hitler",
	// Lithuanian and Russian, common on the server.
	"pizd", "pyzd", "bybi", "nahui", "nachui", "naxui", "blyat", "bliat", "sukin", "yobany", "jeban",
}

// capsuleWords block only as whole words: inside other words they are
// harmless ("class", "cockpit", "grape", "therapist", "Niigata"). A
// stretched spelling ("aaass") counts, a plain word that only looks the
// same once squeezed ("as") does not.
var capsuleWords = []string{
	"ass", "asses", "arse", "dick", "dicks", "cock", "cocks", "cum", "tits", "titties", "boobs", "piss", "fag", "fags",
	"nigger", "niggers", "nigga", "niggas", "fuk", "fuking", "fukin", "rape", "raped", "raping", "rapist", "rapists",
	"kys", "kms", "spic", "chink", "kike", "tranny", "sex", "sexy", "nude", "nudes", "horny", "milf",
	"kurva", "kurwa", "blet", "chuj", "huj", "suka", "debil", "pidaras", "pyderas",
}

// capsulePhrases are whole phrases, matched on the tokens joined by spaces.
var capsulePhrases = []string{
	"kill yourself", "kill urself", "kill your self", "go die", "neck yourself", "end yourself", "hang yourself",
	"hope you die", "eik nachui", "eik nahui",
}

var capsuleWordSet, capsuleSqueezedWordSet = func() (map[string]bool, map[string]bool) {
	plain, squeezed := map[string]bool{}, map[string]bool{}
	for _, w := range capsuleWords {
		plain[w] = true
		squeezed[capsuleSqueeze(w)] = true
	}
	return plain, squeezed
}()

// capsuleWordReason says why the words of text are not fit, "" when they
// are.
func capsuleWordReason(text string) string {
	tokens := capsuleTokens(text)
	const reason = "Please keep the time capsule friendly: no swearing, insults or adult words."
	for _, t := range tokens {
		if squeezed := capsuleSqueeze(t); capsuleWordSet[t] || (squeezed != t && capsuleSqueezedWordSet[squeezed]) {
			return reason
		}
		for _, s := range capsuleStems {
			if strings.Contains(capsuleSqueeze(t), capsuleSqueeze(s)) {
				return reason
			}
		}
	}
	squeezed := make([]string, len(tokens))
	for i, t := range tokens {
		squeezed[i] = capsuleSqueeze(t)
	}
	joined := " " + strings.Join(squeezed, " ") + " "
	for _, p := range capsulePhrases {
		if strings.Contains(joined, " "+capsuleSqueezeWords(p)+" ") {
			return reason
		}
	}
	return ""
}

func capsuleSqueezeWords(p string) string {
	words := strings.Fields(p)
	for i, w := range words {
		words[i] = capsuleSqueeze(w)
	}
	return strings.Join(words, " ")
}

// capsuleTextReason runs the length, personal data and word checks: the
// ones that need no network. "" means the text may go on to the meaning
// check.
func capsuleTextReason(text string) string {
	n := len([]rune(text))
	switch {
	case n < capsuleMinRunes:
		return "That's a bit short for a time capsule. Write a few words."
	case n > capsuleMaxRunes:
		return "That's too long: keep it under 300 characters."
	}
	if r := capsulePersonalReason(text); r != "" {
		return r + " Keep it about anime and the people here, nothing that points to anyone."
	}
	return capsuleWordReason(text)
}
