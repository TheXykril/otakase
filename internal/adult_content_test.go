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
