package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAniListAuthURL(t *testing.T) {
	got := anilistAuthURL("123")
	if got != "https://anilist.co/api/v2/oauth/authorize?client_id=123&redirect_uri=https%3A%2F%2Fanilist.co%2Fapi%2Fv2%2Foauth%2Fpin&response_type=code" {
		t.Errorf("auth url: %s", got)
	}
}

func TestAniListExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		if req["grant_type"] != "authorization_code" || req["client_id"] != "123" || req["client_secret"] != "s" ||
			req["redirect_uri"] != anilistPinURL || req["code"] != "good" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_request"}`))
			return
		}
		w.Write([]byte(`{"token_type":"Bearer","access_token":"tok"}`))
	}))
	defer srv.Close()
	old := anilistTokenURL
	anilistTokenURL = srv.URL
	defer func() { anilistTokenURL = old }()

	if tok, err := anilistExchange("123", "s", "good"); err != nil || tok != "tok" {
		t.Errorf("exchange: %q %v", tok, err)
	}
	if _, err := anilistExchange("123", "s", "bad"); err == nil {
		t.Error("bad code accepted")
	}
}

func TestAniListLookups(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]any
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch {
		case strings.Contains(req.Query, "Viewer"):
			gotAuth = r.Header.Get("Authorization")
			if gotAuth != "Bearer good" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"data":{"Viewer":{"id":7,"name":"Xy"}}}`))
		case strings.Contains(req.Query, "mediaList"):
			w.Write([]byte(`{"data":{"Page":{"mediaList":[{"progress":3,"media":{"siteUrl":"u","episodes":12,"isAdult":false,"title":{"romaji":"Sousou no Frieren","english":"Frieren"}}}]}}}`))
		case req.Variables["name"] == "missing":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"data":{"User":null}}`))
		default:
			w.Write([]byte(`{"data":{"User":{"id":7,"name":"Xy","statistics":{"anime":{"episodesWatched":500}}}}}`))
		}
	}))
	defer srv.Close()
	old := anilistAPI
	anilistAPI = srv.URL
	defer func() { anilistAPI = old }()

	if u, err := anilistViewer("good"); err != nil || u.ID != 7 {
		t.Errorf("viewer: %+v %v", u, err)
	}
	if _, err := anilistViewer("bad"); err == nil {
		t.Error("bad token accepted")
	}
	if u, err := anilistUserByName("Xy"); err != nil || u.Name != "Xy" {
		t.Errorf("by name: %+v %v", u, err)
	}
	if _, err := anilistUserByName("missing"); err == nil {
		t.Error("missing user found")
	}
	if u, err := anilistProfile(7); err != nil || u.Statistics.Anime.EpisodesWatched != 500 {
		t.Errorf("profile: %+v %v", u, err)
	}
	if w, err := anilistWatching(7); err != nil || len(w) != 1 || w[0].Progress != 3 {
		t.Errorf("watching: %+v %v", w, err)
	}
}

func TestProfileEmbed(t *testing.T) {
	var u anilistUser
	u.Name = "Xy"
	u.Statistics.Anime.EpisodesWatched = 1234
	u.Statistics.Anime.MinutesWatched = 2880
	for _, g := range []string{"Action", "Hentai", "Drama", "Comedy", "Romance"} {
		u.Statistics.Anime.Genres = append(u.Statistics.Anime.Genres, struct {
			Genre string `json:"genre"`
			Count int    `json:"count"`
		}{Genre: g})
	}
	twelve := 12
	var safe, adult watchingEntry
	safe.Progress = 5
	safe.Media.Episodes = &twelve
	safe.Media.SiteURL = "https://anilist.co/anime/1"
	safe.Media.Title.Romaji = "Sousou no Frieren"
	adult.Media.IsAdult = true
	adult.Media.Title.Romaji = "Hidden"

	e := profileEmbed("42", u, []watchingEntry{adult, safe}, true)
	text := e.Description
	for _, f := range e.Fields {
		text += "\n" + f.Name + ": " + f.Value
	}
	for _, want := range []string{"<@42>", "[Sousou no Frieren](https://anilist.co/anime/1) · ep 5/12", "1234", "2.0 days", "Action, Drama, Comedy"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	for _, bad := range []string{"Hidden", "Hentai", "Mean score"} {
		if strings.Contains(text, bad) {
			t.Errorf("unexpected %q in\n%s", bad, text)
		}
	}
	if strings.Contains(e.Footer.Text, "not verified") {
		t.Error("verified link marked unverified")
	}
	if e := profileEmbed("42", u, nil, false); !strings.Contains(e.Fields[0].Value, "Nothing") || !strings.Contains(e.Footer.Text, "not verified") {
		t.Errorf("empty/unverified: %+v %s", e.Fields[0], e.Footer.Text)
	}
}

func TestAniListLinkStored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	openStore(path).update(func(d *storeData) { d.AniList["u"] = anilistLink{ID: 7, Name: "Xy", Verified: true} })
	if l := openStore(path).data.AniList["u"]; l.ID != 7 || !l.Verified {
		t.Errorf("reloaded: %+v", l)
	}
}

func TestProfileCardFor(t *testing.T) {
	var u anilistUser
	u.Name = "Xy"
	u.CreatedAt = 1700343386
	u.Statistics.Anime.MinutesWatched = 2880
	u.Statistics.Anime.Statuses = append(u.Statistics.Anime.Statuses, struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}{"COMPLETED", 169})
	for _, g := range []string{"Action", "Hentai", "Drama"} {
		u.Statistics.Anime.Genres = append(u.Statistics.Anime.Genres, struct {
			Genre string `json:"genre"`
			Count int    `json:"count"`
		}{Genre: g, Count: 3})
	}
	twelve := 12
	var safe, adult watchingEntry
	safe.Progress = 5
	safe.Media.Episodes = &twelve
	safe.Media.Title.Romaji = "Sousou no Frieren"
	adult.Media.IsAdult = true

	c := profileCardFor(u, []watchingEntry{adult, safe}, false, false)
	if len(c.Shows) != 1 || c.Shows[0].Title != "Sousou no Frieren" || c.Shows[0].Episodes != 12 {
		t.Errorf("shows = %+v", c.Shows)
	}
	if len(c.Genres) != 2 || c.Genres[1].Name != "Drama" {
		t.Errorf("genres = %+v", c.Genres)
	}
	if c.Since != 2023 || c.Completed != 169 || c.Days != 2 || !strings.Contains(c.Note, "not verified") {
		t.Errorf("card = %+v", c)
	}
}

func TestRenderProfileCard(t *testing.T) {
	cover := image.NewRGBA(image.Rect(0, 0, 46, 65))
	draw.Draw(cover, cover.Bounds(), image.NewUniform(color.RGBA{200, 80, 50, 255}), image.Point{}, draw.Src)
	c := profileCard{Name: "Xy", Since: 2023, Score: 84.7, Anime: 216, Episodes: 2414, Days: 39.7, Avatar: cover, Banner: cover, Hero: cover,
		Shows:  []cardShow{{Title: "A very long title that has to wrap onto two lines and then some", Progress: 3, Episodes: 12, Cover: cover}, {Title: "No cover"}},
		Genres: []cardGenre{{"Action", 10}, {"Drama", 5}}, Note: "/link-anilist to get yours"}
	full, err := renderProfileCard(c)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(full))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != cardWidth*cardScale {
		t.Errorf("width = %d", img.Bounds().Dx())
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Error("corner not rounded")
	}
	empty, err := renderProfileCard(profileCard{Name: "Xy", Hidden: true})
	if err != nil {
		t.Fatal(err)
	}
	small, _ := png.Decode(bytes.NewReader(empty))
	if small.Bounds().Dy() >= img.Bounds().Dy() {
		t.Errorf("empty card %d tall, full %d", small.Bounds().Dy(), img.Bounds().Dy())
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 2414: "2,414", 1234567: "1,234,567"} {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %q", n, got)
		}
	}
}

func TestLastWatchedLine(t *testing.T) {
	for l, want := range map[lastWatched]string{
		{Title: "Slime", Progress: "21"}:      "Last watched **Slime** ep 21",
		{Title: "Uglymug", Progress: "6 - 7"}: "Last watched **Uglymug** eps 6–7",
		{Title: "Frieren"}:                    "Finished **Frieren**",
	} {
		if got := l.line(true); got != want {
			t.Errorf("line = %q, want %q", got, want)
		}
	}
}
