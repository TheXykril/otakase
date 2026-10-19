package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIssueOutcome(t *testing.T) {
	cases := []struct {
		state, reason, want string
	}{
		{"open", "", ""},
		{"open", "reopened", ""},
		{"closed", "completed", "done"},
		{"closed", "", "done"},
		{"closed", "not_planned", "declined"},
		{"closed", "duplicate", "declined"},
	}
	for _, c := range cases {
		if got := issueOutcome(ghIssue{State: c.state, StateReason: c.reason}); got != c.want {
			t.Errorf("issueOutcome(%s, %s) = %q, want %q", c.state, c.reason, got, c.want)
		}
	}
}

func TestIssueCreateProblem(t *testing.T) {
	if !strings.Contains(issueCreateProblem(401), "expired") ||
		!strings.Contains(issueCreateProblem(403), "Read and write") ||
		!strings.Contains(issueCreateProblem(404), "can't write issues") ||
		!strings.Contains(issueCreateProblem(500), "500") {
		t.Error("issueCreateProblem wording")
	}
}

func TestSuggestionIssueBody(t *testing.T) {
	s := suggestion{Name: "Mika", Title: "Batch download", Details: "Whole seasons at once", Up: []string{"a", "b"}, Down: []string{"c"}}
	body := suggestionIssueBody(s, "https://discord.com/channels/1/2/3")
	for _, want := range []string{"**Mika**", "Whole seasons at once", "Votes: 2 up, 1 down", "(https://discord.com/channels/1/2/3)"} {
		if !strings.Contains(body, want) {
			t.Errorf("body misses %q:\n%s", want, body)
		}
	}
	s.Issue = 42
	if e := s.embed(); len(e.Fields) != 2 || !strings.Contains(e.Fields[1].Value, "/issues/42") {
		t.Errorf("card fields = %+v", e.Fields)
	}
}

func TestCreateIssue(t *testing.T) {
	var calls []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var p map[string]any
		json.NewDecoder(r.Body).Decode(&p)
		calls = append(calls, p)
		if _, labelled := p["labels"]; labelled {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(ghIssue{Number: 7, HTMLURL: "https://example/7"})
	}))
	defer srv.Close()
	old := issuesURL
	issuesURL = srv.URL
	defer func() { issuesURL = old }()

	is, status, err := createIssue("tok", "Idea", "body", []string{"enhancement"})
	if err != nil || status != http.StatusCreated || is.Number != 7 {
		t.Fatalf("createIssue = %+v %d %v", is, status, err)
	}
	if len(calls) != 2 || calls[1]["title"] != "Idea" {
		t.Errorf("calls = %v", calls)
	}
	if _, status, err := createIssue("bad", "Idea", "body", nil); err == nil || status != http.StatusUnauthorized {
		t.Errorf("bad token: %d %v", status, err)
	}
}
