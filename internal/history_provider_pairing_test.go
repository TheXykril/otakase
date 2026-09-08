package internal

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The provider id written to the watch history and the provider name written
// beside it are a pair: the id is meaningless to any other host. Six call sites
// wrote anime.ProviderId alongside GetProvider().Name(), which is the first
// *configured* provider, not the one that actually served the episode.
//
// Playing an episode that anipub could not serve but anineko could stored
// anineko's slug under the name "anipub", and every later run then handed that
// slug straight back to anipub:
//
//	anipub episode: invalid anipub show id "rich-girl-caretaker-im-secretly-..."
//
// which took anipub out of the running for that show permanently. The pairing
// cannot be checked at runtime -- a wrong name is still a valid string -- so it
// is pinned here at the call sites instead.
func TestHistoryWritesPairTheIDWithItsOwnProvider(t *testing.T) {
	source, err := os.ReadFile("../cmd/curd/main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	// LocalUpdateAnime calls are single-line at every current call site.
	call := regexp.MustCompile(`LocalUpdateAnime\([^\n]*`)
	var offenders []string
	for _, line := range call.FindAllString(string(source), -1) {
		if strings.Contains(line, "GetProvider().Name()") {
			offenders = append(offenders, strings.TrimSpace(line))
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("%d history write(s) pair a provider id with the default provider's name "+
			"instead of CurrentAnimeProviderName(&anime):\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
