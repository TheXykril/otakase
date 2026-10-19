package internal

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestGetAnilistUserIDNamesTheMissingField(t *testing.T) {
	cases := map[string]string{
		`{}`:                               "data field missing",
		`{"data":{}}`:                      "Viewer field missing",
		`{"data":{"Viewer":{"name":"x"}}}`: "id field missing",
		`{"data":{"Viewer":{"id":4}}}`:     "name field missing",
		`{"data":{"Viewer":{"id":4,"name":"x"}}}`: "",
	}
	for answer, wantErr := range cases {
		fakeAniList(t, answerAlways(answer))
		id, name, err := GetAnilistUserID("token")
		if wantErr == "" {
			if err != nil || id != 4 || name != "x" {
				t.Errorf("%s: got (%d, %q, %v)", answer, id, name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%s: err = %v, want it to mention %q", answer, err, wantErr)
		}
	}
}

func TestGetAnimeSequelShapeErrors(t *testing.T) {
	for answer, wantErr := range map[string]string{
		`{}`:                                    "data field missing",
		`{"data":{}}`:                           "Media field missing",
		`{"data":{"Media":{"relations":null}}}`: "",
	} {
		fakeAniList(t, answerAlways(answer))
		sequels, err := GetAnimeSequel(1, "")
		if wantErr == "" {
			if err != nil || sequels != nil {
				t.Errorf("%s: got (%v, %v), want no sequels and no error", answer, sequels, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%s: err = %v, want %q", answer, err, wantErr)
		}
	}
}

func TestAddAniListAnimeToListSendsTheStatus(t *testing.T) {
	calls := fakeAniList(t, answerAlways(`{"data":{"SaveMediaListEntry":{"id":1}}}`))
	if err := AddAniListAnimeToList(12, "PLANNING", "token"); err != nil {
		t.Fatalf("AddAniListAnimeToList: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("%d requests, want 1", len(*calls))
	}
	vars := (*calls)[0].Variables
	if vars["status"] != "PLANNING" || vars["mediaId"] != float64(12) {
		t.Errorf("variables = %v", vars)
	}

	fakeAniList(t, func(aniListCall, int) (int, string) { return http.StatusInternalServerError, "down" })
	err := AddAniListAnimeToWatchingList(12, "token")
	if err == nil || !strings.HasPrefix(err.Error(), "failed to add anime: ") {
		t.Errorf("err = %v, want the add-anime prefix", err)
	}
}

func TestEncodeGraphQLBody(t *testing.T) {
	body, err := encodeGraphQL("q", nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded["query"]) != `"q"` || string(decoded["variables"]) != "null" {
		t.Errorf("body = %s", body)
	}
}

func TestMediaDisplayTitleFallbacks(t *testing.T) {
	cfg := &Config{}
	for want, media := range map[string]Media{
		"Romaji":        {ID: 1, Title: AnimeTitle{Romaji: "Romaji", English: "English"}},
		"Native":        {ID: 1, Title: AnimeTitle{Japanese: "Native"}},
		"7":             {ID: 7},
		"Unknown Anime": {},
	} {
		if got := mediaDisplayTitle(media, cfg); got != want {
			t.Errorf("mediaDisplayTitle(%+v) = %q, want %q", media, got, want)
		}
	}
}
