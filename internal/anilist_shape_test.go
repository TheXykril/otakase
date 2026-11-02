package internal

import (
	"encoding/json"
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
