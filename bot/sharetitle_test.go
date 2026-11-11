package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestShareTitleChoiceDMAndAppItem(t *testing.T) {
	series := watchShow{ID: watchTitleID("tt0903747"), Title: "Breaking Bad", Year: 2008, Format: "TV", Cover: "https://m.media-amazon.com/images/M/x.jpg"}
	film := watchShow{ID: watchTitleID("tt0133093"), Title: "The Matrix", Year: 1999, Format: "MOVIE", Episodes: 1}
	if c := filmChoice(series); c.Name != "Breaking Bad (2008) · Series" || c.Value != "tt0903747:show" || !pickedFilmRe.MatchString(c.Value.(string)) {
		t.Errorf("series choice %+v", c)
	}
	if c := filmChoice(film); c.Value != "tt0133093:movie" {
		t.Errorf("film choice %+v", c)
	}
	dm := shareTitleDM("Ann", series, "watch it")
	if e := dm.Embeds[0]; e.URL != "https://www.imdb.com/title/tt0903747/" || e.Thumbnail == nil || !strings.Contains(e.Description, "> watch it") {
		t.Errorf("dm %+v", e)
	}
	item := shareTitleItem("Ann", film, "")
	b, _ := json.Marshal(item)
	if item.Kind != "queue" || item.AniListID != 0 || !strings.Contains(string(b), `"imdb":"tt0133093"`) || item.Key != "movie" {
		t.Errorf("item %s", b)
	}
}
