package aniskip

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func testClient(t *testing.T, v2, v1 http.HandlerFunc) *Client {
	t.Helper()
	s2 := httptest.NewServer(v2)
	s1 := httptest.NewServer(v1)
	t.Cleanup(s2.Close)
	t.Cleanup(s1.Close)
	c := New(nil)
	c.V2, c.V1 = s2.URL, s1.URL
	return c
}

func TestLookupReadsV2WithLengths(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "episodeLength=0") {
			t.Errorf("v2 query %q does not ask for every length", r.URL.RawQuery)
		}
		w.Write([]byte(`{"found":true,"results":[{"interval":{"startTime":165.2,"endTime":255.4},"skipType":"op","skipId":"a","episodeLength":1421.5}]}`))
	}, func(w http.ResponseWriter, r *http.Request) {
		t.Error("v1 asked although v2 answered")
	})

	got, err := c.Lookup(1, 2)
	want := []Entry{{Kind: "op", ID: "a", Start: 165.2, End: 255.4, Length: 1421.5}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Lookup = %+v, %v; want %+v", got, err, want)
	}
}

func TestLookupNothingOnFile(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}, func(w http.ResponseWriter, r *http.Request) {
		t.Error("v1 asked although v2 said nothing is on file")
	})
	if got, err := c.Lookup(1, 2); len(got) != 0 || err != nil {
		t.Fatalf("Lookup = %+v, %v; want nothing", got, err)
	}
}

func TestLookupFallsBackToV1(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"found":true,"results":[{"interval":{"start_time":3,"end_time":93},"skip_type":"op","skip_id":"v1"}]}`))
	})
	got, err := c.Lookup(1, 2)
	if err != nil || len(got) != 1 || got[0].ID != "v1" || got[0].End != 93 {
		t.Fatalf("Lookup = %+v, %v", got, err)
	}
}

func TestLookupBothFail(t *testing.T) {
	fail := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }
	if _, err := testClient(t, fail, fail).Lookup(1, 2); err == nil {
		t.Fatal("want an error when both versions fail")
	}
}

func TestFitDropsAnotherCut(t *testing.T) {
	entries := []Entry{
		{Kind: "op", ID: "short", Start: 0, End: 60, Length: 1255},
		{Kind: "op", ID: "this", Start: 165, End: 255, Length: 1421},
	}
	got := Fit(entries, 1420)
	if len(got) != 1 || got[0].ID != "this" {
		t.Fatalf("Fit = %+v, want only the entry for this cut", got)
	}
}

func TestFitClosestFirst(t *testing.T) {
	entries := []Entry{
		{Kind: "op", ID: "far", End: 180, Length: 1435},
		{Kind: "op", ID: "near", End: 178, Length: 1421},
	}
	if got := Fit(entries, 1420); got[0].ID != "near" {
		t.Fatalf("first = %q, want near", got[0].ID)
	}
}

func TestFitDropsEntryPastTheEnd(t *testing.T) {
	if got := Fit([]Entry{{Kind: "ed", Start: 1400, End: 1490}}, 1420); len(got) != 0 {
		t.Fatalf("Fit = %+v, want the entry past the end dropped", got)
	}
}

func TestFitUnknownLength(t *testing.T) {
	entries := []Entry{{Kind: "op", End: 60, Length: 1255}}
	if got := Fit(entries, 0); !reflect.DeepEqual(got, entries) {
		t.Fatalf("Fit = %+v, want unchanged", got)
	}
}
