package skiptimes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fake serves all three services and the two files the service downloads.
func fake(t *testing.T, aniskip, introdb, graphql string) (*Service, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/aniskip/v2/skip-times/", func(w http.ResponseWriter, r *http.Request) {
		if aniskip == "" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, aniskip)
	})
	mux.HandleFunc("/introdb", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tmdb_id") != "30991" || r.URL.Query().Get("season") != "1" {
			t.Errorf("TheIntroDB asked for %s", r.URL.RawQuery)
		}
		if introdb == "" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, introdb)
	})
	mux.HandleFunc("/lists", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{"anilist_id":1,"themoviedb_id":{"tv":30991},"season":{"tmdb":1}},
			{"anilist_id":2,"themoviedb_id":{"movie":5},"season":{}}]`)
	})
	mux.HandleFunc("/playground", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<script>headers: {"X-Client-ID": "playground-id-123"}</script>`)
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Client-ID") != "playground-id-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "findShowsByExternalId") {
			io.WriteString(w, `{"data":{"findShowsByExternalId":[{"id":"show-1"}]}}`)
			return
		}
		io.WriteString(w, graphql)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	s := New(dir, nil)
	s.aniSkipBase = srv.URL + "/aniskip/v2/skip-times"
	s.introDBBase = srv.URL + "/introdb"
	s.animeSkipBase = srv.URL + "/graphql"
	s.mapping = newTMDBMapping(filepath.Join(dir, "map.json"), srv.URL+"/lists", nil)
	s.ids = newAutoClientID(filepath.Join(dir, "id"), srv.URL+"/playground")
	return s, srv
}

func all() Options { return Options{IntroDB: true, AnimeSkipClientID: AutoClientID} }

func TestEachKindComesFromTheFirstServiceThatHasIt(t *testing.T) {
	s, _ := fake(t,
		`{"found":true,"results":[{"interval":{"startTime":80,"endTime":170},"skipType":"op","skipId":"a1","episodeLength":1420}]}`,
		`{"intro":[{"start_ms":10000,"end_ms":99000}],"credits":[{"start_ms":1300000,"end_ms":1390000}]}`,
		`{"data":{"findEpisodesByShowId":[{"number":"3","timestamps":[{"at":0,"type":{"name":"Recap"}},{"at":60,"type":{"name":"Canon"}}]}]}}`)
	spans, errs := s.Lookup(context.Background(), http.DefaultClient, Ref{MalID: 1, AniListID: 1, Episode: 3, Length: 1420}, all())
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	got := map[string]Span{}
	for _, sp := range spans {
		got[sp.Type] = sp
	}
	if op := got["op"]; op.Source != AniSkip || op.Start != 80 || op.ID != "a1" {
		t.Errorf("opening = %+v, want AniSkip's", op)
	}
	if ed := got["ed"]; ed.Source != IntroDB || ed.Start != 1300 || ed.End != 1390 {
		t.Errorf("ending = %+v, want TheIntroDB's", ed)
	}
	if recap := got["recap"]; recap.Source != AnimeSkip || recap.End != 60 {
		t.Errorf("recap = %+v, want Anime-Skip's", recap)
	}
}

func TestAServiceThatFailsLeavesTheOthers(t *testing.T) {
	s, _ := fake(t, "", `{"intro":[{"start_ms":null,"end_ms":90000}]}`, `{"errors":[{"message":"boom"}]}`)
	spans, errs := s.Lookup(context.Background(), http.DefaultClient, Ref{MalID: 1, AniListID: 1, Episode: 1}, all())
	if len(spans) != 1 || spans[0].Source != IntroDB || spans[0].Start != 0 || spans[0].End != 90 {
		t.Fatalf("spans = %+v", spans)
	}
	if len(errs) != 1 {
		t.Errorf("errors = %v, want Anime-Skip's", errs)
	}
}

func TestTimesLongerThanTheEpisodeAreDropped(t *testing.T) {
	s, _ := fake(t, "", `{"credits":[{"start_ms":1500000,"end_ms":1590000}]}`, "")
	spans, _ := s.Lookup(context.Background(), http.DefaultClient, Ref{AniListID: 1, Episode: 1, Length: 1420}, Options{IntroDB: true})
	if len(spans) != 0 {
		t.Errorf("spans = %+v", spans)
	}
}

func TestCreditsWithNoEndRunToTheEndWhenTheLengthIsKnown(t *testing.T) {
	spans := introDBSpans([]byte(`{"credits":[{"start_ms":1300000,"end_ms":null}]}`), 1420)
	if len(spans) != 1 || spans[0].End != 1420 {
		t.Errorf("spans = %+v", spans)
	}
	if spans := introDBSpans([]byte(`{"credits":[{"start_ms":1300000,"end_ms":null}]}`), 0); len(spans) != 0 {
		t.Errorf("without a length: %+v", spans)
	}
}

func TestOffServicesAreNotAsked(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	s := New(t.TempDir(), nil)
	s.introDBBase, s.animeSkipBase = srv.URL, srv.URL
	s.mapping = newTMDBMapping(filepath.Join(t.TempDir(), "m"), srv.URL, nil)
	s.Lookup(context.Background(), http.DefaultClient, Ref{AniListID: 1, Episode: 1}, Options{})
	if asked.Load() != 0 {
		t.Errorf("%d requests with every optional service off", asked.Load())
	}
}

func TestTheTMDBTableIsKeptOnDisk(t *testing.T) {
	s, srv := fake(t, "", `{}`, "")
	if _, ok := s.mapping.Show(context.Background(), 1, time.Second); !ok {
		t.Fatal("show 1 not mapped")
	}
	if _, ok := s.mapping.Show(context.Background(), 2, time.Second); ok {
		t.Error("a film was mapped as a show")
	}
	srv.Close()
	again := newTMDBMapping(s.mapping.path, "http://127.0.0.1:1/", nil)
	if show, ok := again.Show(context.Background(), 1, time.Second); !ok || show.ID != 30991 {
		t.Errorf("stored table gave %+v %v", show, ok)
	}
}

func TestARefusedClientIDIsFetchedAgain(t *testing.T) {
	s, _ := fake(t, "", "", `{"data":{"findEpisodesByShowId":[{"number":"1","timestamps":[{"at":30,"type":{"name":"Intro"}},{"at":120,"type":{"name":"Canon"}}]}]}}`)
	if err := os.WriteFile(s.ids.path, []byte("old-rotated-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spans, errs := s.Lookup(context.Background(), http.DefaultClient, Ref{AniListID: 1, Episode: 1}, Options{AnimeSkipClientID: AutoClientID})
	if len(errs) != 0 || len(spans) != 1 || spans[0].Start != 30 || spans[0].End != 120 {
		t.Fatalf("spans %+v errs %v", spans, errs)
	}
}

func TestSubmitAndVote(t *testing.T) {
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"message":"ok","skipId":"new-id"}`)
	}))
	defer srv.Close()
	s := New(t.TempDir(), nil)
	s.aniSkipBase = srv.URL + "/v2/skip-times"
	sub := Submission{MalID: 5, Episode: 2, Type: "op", Start: 61.2, End: 150.7, Length: 1420, Submitter: "0d6c3f0a-3b6e-4f3c-9a51-1c2b3d4e5f60"}
	id, err := s.Submit(http.DefaultClient, sub)
	if err != nil || id != "new-id" || path != "/v2/skip-times/5/2" || got["providerName"] != "otakase" || got["skipType"] != "op" {
		t.Fatalf("submit: id %q err %v path %s body %v", id, err, path, got)
	}
	if err := s.Vote(http.DefaultClient, "abc", false); err != nil || path != "/v2/skip-times/vote/abc" || got["voteType"] != "downvote" {
		t.Fatalf("vote: %v %s %v", err, path, got)
	}
	if err := s.Vote(http.DefaultClient, "../x", true); err == nil {
		t.Error("a path in the id was sent")
	}
}

func TestSubmissionsAreChecked(t *testing.T) {
	ok := Submission{MalID: 5, Episode: 2, Type: "op", Start: 60, End: 150, Length: 1420, Submitter: "0d6c3f0a-3b6e-4f3c-9a51-1c2b3d4e5f60"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Submission){
		"no mal":    func(s *Submission) { s.MalID = 0 },
		"short":     func(s *Submission) { s.End = s.Start + 2 },
		"backwards": func(s *Submission) { s.End = s.Start - 1 },
		"past end":  func(s *Submission) { s.End = 1500 },
		"bad type":  func(s *Submission) { s.Type = "preview" },
		"no length": func(s *Submission) { s.Length = 0 },
		"submitter": func(s *Submission) { s.Submitter = "me" },
		"very long": func(s *Submission) { s.Start, s.End = 0, 700 },
	} {
		s := ok
		change(&s)
		if s.Validate() == nil {
			t.Errorf("%s passed", name)
		}
	}
}
