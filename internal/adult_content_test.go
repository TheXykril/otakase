package internal

import "testing"

func TestHideAdultEntries(t *testing.T) {
	entries := []Entry{
		{Media: Media{ID: 1}},
		{Media: Media{ID: 2, IsAdult: true}},
	}
	if got := hideAdultEntries(entries, &Config{AdultContent: false}); len(got) != 1 || got[0].Media.ID != 1 {
		t.Fatalf("AdultContent=false: got %+v, want only show 1", got)
	}
	if got := hideAdultEntries(entries, &Config{AdultContent: true}); len(got) != 2 {
		t.Fatalf("AdultContent=true: got %d entries, want 2", len(got))
	}
}

func TestParseAnimeListReadsAdultFlag(t *testing.T) {
	data := map[string]interface{}{"data": map[string]interface{}{"MediaListCollection": map[string]interface{}{
		"lists": []interface{}{map[string]interface{}{
			"name": "Planning",
			"entries": []interface{}{
				map[string]interface{}{"id": float64(1), "status": "PLANNING",
					"media": map[string]interface{}{"id": float64(10), "isAdult": true}},
			},
		}},
	}}}
	if list := ParseAnimeList(data); len(list.Planning) != 1 || !list.Planning[0].Media.IsAdult {
		t.Fatalf("parsed = %+v, want an adult entry", list.Planning)
	}
}

// The flag belongs to the show, so a merge that keeps the other tracker's
// entry must not lose it.
func TestMergeKeepsAdultFlag(t *testing.T) {
	merged := mergeEntryMetadata(Entry{Media: Media{ID: 1}}, Entry{Media: Media{ID: 1, IsAdult: true}})
	if !merged.Media.IsAdult {
		t.Fatal("merge dropped the adult flag")
	}
}

// sukebei only carries adult titles, so it is in the provider stack only
// while AdultContent is on.
func TestSukebeiFollowsAdultContent(t *testing.T) {
	previous := GetGlobalConfig()
	t.Cleanup(func() { SetGlobalConfig(previous) })

	SetGlobalConfig(&Config{AdultContent: false})
	if ProviderEnabled("sukebei") {
		t.Fatal("sukebei enabled with AdultContent=false")
	}
	for _, name := range defaultEnabledProviderStack() {
		if name == "sukebei" {
			t.Fatal("sukebei in the default stack with AdultContent=false")
		}
	}

	SetGlobalConfig(&Config{AdultContent: true})
	if !ProviderEnabled("sukebei") {
		t.Fatalf("sukebei disabled with AdultContent=true: %s", ProviderDisabledReason("sukebei"))
	}
	if !ProviderEnabled("nyaa") {
		t.Fatal("nyaa should stay enabled")
	}
}

// With AdultContent on, an adult show is looked for on sukebei alone, and any
// other show never on sukebei.
func TestProviderNamesForShowRoutesAdultShows(t *testing.T) {
	previous := GetGlobalConfig()
	t.Cleanup(func() { SetGlobalConfig(previous) })
	config := &Config{AdultContent: true}
	SetGlobalConfig(config)

	adult := &Anime{IsAdult: true}
	if got := providerNamesForShow(config, adult); len(got) != 1 || got[0] != "sukebei" {
		t.Fatalf("adult show providers = %v, want [sukebei]", got)
	}
	for _, name := range providerNamesForShow(config, &Anime{}) {
		if name == "sukebei" {
			t.Fatal("a general show was routed to sukebei")
		}
	}
	// A stack written without sukebei still sends adult shows there.
	config.Provider = `["anikoto","nyaa"]`
	if got := providerNamesForShow(config, adult); len(got) != 1 || got[0] != "sukebei" {
		t.Fatalf("adult show providers with a custom stack = %v, want [sukebei]", got)
	}
	// A stored mapping onto a general host is not trusted for an adult show.
	if providerSuitsShow(config, adult, "anikoto") || !providerSuitsShow(config, adult, "sukebei") {
		t.Fatal("providerSuitsShow wrong for an adult show")
	}

	// AdultContent off: nothing changes, the adult flag is ignored.
	config.AdultContent = false
	config.Provider = ""
	for _, name := range providerNamesForShow(config, adult) {
		if name == "sukebei" {
			t.Fatal("sukebei used with AdultContent=false")
		}
	}
	if !providerSuitsShow(config, adult, "anikoto") {
		t.Fatal("with AdultContent=false a general host should suit any show")
	}
}
