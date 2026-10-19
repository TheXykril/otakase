package providers_test

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/thexykril/otakase/internal/loadproviders"
	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
)

// The live tests here hit real hosts and are opt-in, one variable each. The
// edge-case run, for one, is
//	OTAKASE_LIVE_EDGE=1 go test ./internal/providers -run TestLiveProviderEdgeFlows -v -timeout 45m

// requireLive skips unless env is set. Outside the app nothing hands the
// providers an HTTP client, so a default one is set here.
func requireLive(t testing.TB, env, reason string) {
	t.Helper()
	if os.Getenv(env) == "" {
		t.Skipf("set %s=1 to run %s", env, reason)
	}
	if providerhost.HTTPClient == nil {
		providerhost.HTTPClient = func() *http.Client { return http.DefaultClient }
	}
}

// liveCase is a search a user might type and a word the right show's title
// has in it.
type liveCase struct {
	name        string
	query       string
	wantInTitle string
}

var edgeAnimeCases = []liveCase{
	// punctuation as users type it
	{name: "colon_apostrophe", query: "frieren beyond journey's end", wantInTitle: "frieren"},
	{name: "colon_short", query: "re:zero", wantInTitle: "re:zero"},
	{name: "colon_spaced", query: "re zero", wantInTitle: "re:zero"},
	{name: "semicolon", query: "steins;gate", wantInTitle: "stein"},
	{name: "slash", query: "fate/stay night", wantInTitle: "fate"},
	{name: "bang", query: "k-on!", wantInTitle: "k-on"},
	{name: "double_bang", query: "love live superstar", wantInTitle: "love live"},
	{name: "apostrophe_full", query: "JoJo's Bizarre Adventure", wantInTitle: "jojo"},
	{name: "apostrophe_stripped", query: "jojos bizarre adventure", wantInTitle: "jojo"},

	// romaji and English titles of the same show
	{name: "romaji_snk", query: "shingeki no kyojin", wantInTitle: "attack on titan"},
	{name: "english_aot", query: "attack on titan", wantInTitle: "attack on titan"},
	{name: "romaji_mha", query: "boku no hero academia", wantInTitle: "hero academia"},
	{name: "english_mha", query: "my hero academia", wantInTitle: "hero academia"},
	{name: "romaji_frieren", query: "sousou no frieren", wantInTitle: "frieren"},
	{name: "kimetsu", query: "kimetsu no yaiba", wantInTitle: "demon"},
	{name: "demon_slayer", query: "demon slayer", wantInTitle: "demon"},

	// short, numeric and abbreviated
	{name: "numeric_86", query: "86", wantInTitle: "86"},
	{name: "two_digit", query: "91 days", wantInTitle: "91 days"},
	{name: "three_letters", query: "mha", wantInTitle: "hero"},
	{name: "abbr_sao", query: "sao", wantInTitle: "sword art"},
	{name: "abbr_danmachi", query: "danmachi", wantInTitle: "danmachi"},
	{name: "abbr_csm", query: "chainsaw man", wantInTitle: "chainsaw"},
	{name: "x_hxh", query: "hunter x hunter", wantInTitle: "hunter"},
	{name: "x_spy", query: "spy x family", wantInTitle: "spy"},

	// long titles cut short
	{name: "long_magirevo", query: "magical revolution reincarnated princess", wantInTitle: "magical revolution"},
	{name: "long_slime", query: "that time i got reincarnated as a slime", wantInTitle: "slime"},
	{name: "long_apothecary", query: "apothecary diaries", wantInTitle: "apothecary"},
	{name: "bleach_tybw", query: "bleach thousand year blood war", wantInTitle: "bleach"},
	{name: "evangelion", query: "neon genesis evangelion", wantInTitle: "evangelion"},
	{name: "witch_hat", query: "witch hat atelier", wantInTitle: "witch"},

	// crowded result lists
	{name: "ambiguous_monogatari", query: "monogatari", wantInTitle: "monogatari"},
	{name: "ambiguous_fate", query: "fate", wantInTitle: "fate"},
	{name: "ambiguous_gundam", query: "gundam", wantInTitle: "gundam"},
	{name: "single_naruto", query: "naruto", wantInTitle: "naruto"},
	{name: "single_bleach", query: "bleach", wantInTitle: "bleach"},
	{name: "long_runner", query: "one piece", wantInTitle: "one piece"},

	// seasons, spin-offs and similar titles
	{name: "season_noise", query: "frieren season 2", wantInTitle: "frieren"},
	{name: "cour_alias", query: "mushoku tensei jobless reincarnation", wantInTitle: "mushoku"},
	{name: "spinoff_mini", query: "frieren marumaru mahou", wantInTitle: "frieren"},
	{name: "dungeon_meshi", query: "dungeon meshi", wantInTitle: "dungeon"},
	{name: "delicious_in_dungeon", query: "delicious in dungeon", wantInTitle: "dungeon"},

	// films and recent shows
	{name: "movie_your_name", query: "your name", wantInTitle: "your name"},
	{name: "movie_suzume", query: "suzume", wantInTitle: "suzume"},
	{name: "oshi_no_ko", query: "oshi no ko", wantInTitle: "oshi"},
	{name: "bocchi", query: "bocchi the rock", wantInTitle: "bocchi"},
}

// edgeProvider is a registered provider driven through its public methods,
// the way the app drives it.
type edgeProvider struct {
	name     string
	provider providers.Provider
}

func liveProvider(name string) edgeProvider {
	provider, err := providers.New(name)
	if err != nil {
		panic(err)
	}
	return edgeProvider{name: name, provider: provider}
}

// edgeProviders are the hosts the edge cases are run against.
func edgeProviders() []edgeProvider {
	return []edgeProvider{liveProvider("anipub"), liveProvider("anineko")}
}

func (p edgeProvider) search(query, mode string) ([]providers.SelectionOption, error) {
	return p.provider.SearchAnime(query, mode)
}

func (p edgeProvider) episodes(showID, mode string) ([]string, error) {
	return p.provider.EpisodesList(showID, mode)
}

func (p edgeProvider) streams(showID, mode string, epNo int) ([]string, error) {
	config := providers.PlaybackConfig{SubOrDub: mode}
	if resolver, ok := p.provider.(providers.HintResolver); ok {
		links, _, err := resolver.GetEpisodeURLForModeWithHints(config, showID, epNo, mode)
		return links, err
	}
	return p.provider.GetEpisodeURL(config, showID, epNo)
}

// flowResult is how far one search got: a result, the right show, its
// episode list, a stream for episode 1.
type flowResult struct {
	provider, caseName, query               string
	searchOK, matchOK, episodesOK, streamOK bool
	searchErr, episodesErr, streamErr       string
	pickedTitle, pickedKey                  string
	epCount                                 int
	searchTime, flowTime                    time.Duration
}

// pickEdgeResult picks the first result whose title or label contains want,
// or the first result with ok false.
func pickEdgeResult(options []providers.SelectionOption, want string) (providers.SelectionOption, bool) {
	if len(options) == 0 {
		return providers.SelectionOption{}, false
	}
	want = strings.ToLower(strings.TrimSpace(want))
	for _, option := range options {
		if strings.Contains(strings.ToLower(option.Title), want) || strings.Contains(strings.ToLower(option.Label), want) {
			return option, true
		}
	}
	return options[0], false
}

// runEdgeCase runs a search and, when it finds anything, the episode list and
// episode 1's stream of the picked result, all in sub.
func runEdgeCase(p edgeProvider, tc liveCase) (out flowResult) {
	out = flowResult{provider: p.name, caseName: tc.name, query: tc.query}

	start := time.Now()
	options, err := p.search(tc.query, "sub")
	out.searchTime = time.Since(start)
	switch {
	case err != nil:
		out.searchErr = err.Error()
		return out
	case len(options) == 0:
		out.searchErr = "no results"
		return out
	}
	out.searchOK = true

	picked, matched := pickEdgeResult(options, tc.wantInTitle)
	out.matchOK, out.pickedKey, out.pickedTitle = matched, picked.Key, picked.Title
	if out.pickedTitle == "" {
		out.pickedTitle = picked.Label
	}
	if picked.Key == "" {
		return out
	}

	start = time.Now()
	defer func() { out.flowTime = time.Since(start) }()
	episodes, err := p.episodes(picked.Key, "sub")
	switch {
	case err != nil:
		out.episodesErr = err.Error()
		return out
	case len(episodes) == 0:
		out.episodesErr = "no episodes"
		return out
	}
	out.episodesOK, out.epCount = true, len(episodes)

	links, err := p.streams(picked.Key, "sub", 1)
	switch {
	case err != nil:
		out.streamErr = err.Error()
	case len(links) == 0:
		out.streamErr = "no stream url"
	default:
		out.streamOK = true
	}
	return out
}

// flowTally counts how many cases got through each step.
type flowTally struct{ search, match, episodes, stream, total int }

func (c *flowTally) add(res flowResult) {
	c.total++
	for _, step := range []struct {
		ok    bool
		count *int
	}{{res.searchOK, &c.search}, {res.matchOK, &c.match}, {res.episodesOK, &c.episodes}, {res.streamOK, &c.stream}} {
		if step.ok {
			*step.count++
		}
	}
}

func (c flowTally) String() string {
	return fmt.Sprintf("search %d/%d match %d/%d episodes %d/%d stream %d/%d",
		c.search, c.total, c.match, c.total, c.episodes, c.total, c.stream, c.total)
}

func TestLiveProviderEdgeFlows(t *testing.T) {
	requireLive(t, "OTAKASE_LIVE_EDGE", "live provider edge-case tests")

	var runs []flowResult
	tallies := map[string]*flowTally{}
	for _, p := range edgeProviders() {
		tallies[p.name] = &flowTally{}
		for _, tc := range edgeAnimeCases {
			res := runEdgeCase(p, tc)
			runs = append(runs, res)
			tallies[p.name].add(res)
			t.Log(edgeStatusLine(res))
			time.Sleep(edgePause)
		}
	}

	slices.SortFunc(runs, func(a, b flowResult) int {
		return strings.Compare(a.provider+"/"+a.caseName, b.provider+"/"+b.caseName)
	})
	t.Log(edgeSummary(runs))
	issues := 0
	for _, res := range runs {
		if !res.searchOK || !res.matchOK || !res.streamOK {
			issues++
		}
	}
	for name, tally := range tallies {
		t.Logf("%s: %s", name, tally)
	}

	// These must play on every edge provider; the rest are only reported.
	for _, caseName := range []string{"english_aot", "single_naruto", "demon_slayer", "dungeon_meshi", "romaji_frieren"} {
		for _, res := range runs {
			if res.caseName == caseName && !res.streamOK {
				t.Errorf("critical case %s/%s must stream: search=%v episodes=%v stream_err=%s",
					res.provider, caseName, res.searchOK, res.episodesOK, res.streamErr)
			}
		}
	}
	if issues > 0 {
		t.Logf("%d edge-case issues logged (see above)", issues)
	}
}

// edgePause spaces requests to the same host out a little.
const edgePause = 150 * time.Millisecond

// edgeLineFormat is one case's line in the log and the summary.
const edgeLineFormat = "[%s] %-22s %-28s search=%s match=%s eps=%v stream=%s (%dms+%dms) picked=%q"

// edgeStatusLine reports how far one case got.
func edgeStatusLine(res flowResult) string {
	matchMark := "ok"
	switch {
	case !res.searchOK:
		matchMark = "fail"
	case !res.matchOK:
		matchMark = "weak"
	}
	streamMark := "-"
	switch {
	case res.streamOK:
		streamMark = "ok"
	case res.searchOK:
		streamMark = "fail"
	}
	return fmt.Sprintf(edgeLineFormat,
		res.provider, res.caseName, res.query,
		boolMark(res.searchOK), matchMark, res.episodesOK, streamMark,
		res.searchTime.Milliseconds(), res.flowTime.Milliseconds(), truncate(res.pickedTitle, 48))
}

// edgeSummary lists every result again, grouped by provider.
func edgeSummary(results []flowResult) string {
	var sb strings.Builder
	sb.WriteString("\n=== provider edge-case summary ===\n")
	for i, res := range results {
		if i == 0 || results[i-1].provider != res.provider {
			fmt.Fprintf(&sb, "\n-- %s --\n", res.provider)
		}
		sb.WriteString(edgeStatusLine(res) + "\n")
	}
	return sb.String()
}

func boolMark(ok bool) string {
	mark := "FAIL"
	if ok {
		mark = "ok"
	}
	return mark
}

func truncate(s string, max int) string {
	if s = strings.TrimSpace(s); len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}
