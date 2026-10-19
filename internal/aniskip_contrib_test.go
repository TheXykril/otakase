package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Everything submitted is public, and every player reading AniSkip inherits it.
// A wrong entry is worse for them than a missing one, so the checks happen
// before anything leaves the machine.
func TestASubmissionIsCheckedBeforeItIsSent(t *testing.T) {
	good := SkipSubmission{MalID: 52991, Episode: 1, SkipType: "op", StartTime: 3, EndTime: 93, EpisodeLength: 1560}
	if err := good.Validate(); err != nil {
		t.Fatalf("a plain opening was rejected: %v", err)
	}

	cases := []struct {
		name       string
		submission SkipSubmission
	}{
		{"no MAL id", SkipSubmission{Episode: 1, SkipType: "op", StartTime: 3, EndTime: 93, EpisodeLength: 1560}},
		{"no episode", SkipSubmission{MalID: 52991, SkipType: "op", StartTime: 3, EndTime: 93, EpisodeLength: 1560}},
		{"a type AniSkip does not take", SkipSubmission{MalID: 52991, Episode: 1, SkipType: "intro", StartTime: 3, EndTime: 93, EpisodeLength: 1560}},
		{"unknown episode length", SkipSubmission{MalID: 52991, Episode: 1, SkipType: "op", StartTime: 3, EndTime: 93}},
		{"end before start", SkipSubmission{MalID: 52991, Episode: 1, SkipType: "op", StartTime: 93, EndTime: 3, EpisodeLength: 1560}},
		{"past the end of the episode", SkipSubmission{MalID: 52991, Episode: 1, SkipType: "op", StartTime: 1500, EndTime: 1700, EpisodeLength: 1560}},
		{"a mis-press, not an opening", SkipSubmission{MalID: 52991, Episode: 1, SkipType: "op", StartTime: 10, EndTime: 12, EpisodeLength: 1560}},
		{"a mark someone forgot to close", SkipSubmission{MalID: 52991, Episode: 1, SkipType: "op", StartTime: 10, EndTime: 1000, EpisodeLength: 1560}},
	}
	for _, test := range cases {
		if err := test.submission.Validate(); err == nil {
			t.Errorf("%s was accepted", test.name)
		}
	}
}

func TestASubmissionCarriesWhatAniSkipAsksFor(t *testing.T) {
	var got aniSkipSubmitRequest
	var path string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		fmt.Fprint(w, `{"statusCode":201,"message":"Created","skipId":"a-new-skip-id"}`)
	}))
	t.Cleanup(server.Close)

	restore := aniSkipWriteBaseForTest(t, server.URL)
	defer restore()

	storage := t.TempDir()
	skipID, err := SubmitSkipTime(storage, SkipSubmission{
		MalID: 52991, Episode: 4, SkipType: "op",
		StartTime: 3.5, EndTime: 93.5, EpisodeLength: 1560,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipID != "a-new-skip-id" {
		t.Errorf("the new entry's id came back as %q", skipID)
	}
	if path != "/52991/4" {
		t.Errorf("posted to %q", path)
	}
	if got.SkipType != "op" || got.StartTime != 3.5 || got.EndTime != 93.5 || got.EpisodeLength != 1560 {
		t.Errorf("the times did not survive the trip: %+v", got)
	}
	// The provider name records which program filed it. Naming the streaming
	// source would blame it for times this program's viewer marked.
	if got.ProviderName != "otakase" {
		t.Errorf("filed under provider %q", got.ProviderName)
	}
	if len(got.SubmitterID) != 36 {
		t.Errorf("submitter id %q is not a uuid", got.SubmitterID)
	}
}

// AniSkip explains a rejection in the body. Reporting the status alone turns
// "these times overlap an entry that already exists" into an opaque 400.
func TestARejectionSaysWhatAniSkipSaid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"statusCode":400,"message":"Skip time already exists"}`)
	}))
	t.Cleanup(server.Close)
	restore := aniSkipWriteBaseForTest(t, server.URL)
	defer restore()

	_, err := SubmitSkipTime(t.TempDir(), SkipSubmission{
		MalID: 52991, Episode: 1, SkipType: "op", StartTime: 3, EndTime: 93, EpisodeLength: 1560,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the reason was lost: %v", err)
	}
}

// The submitter id is an identity across runs, not per run: AniSkip uses it to
// group one person's entries.
func TestTheSubmitterIDIsKeptBetweenRuns(t *testing.T) {
	storage := t.TempDir()
	resetSubmitterID()

	first := AniSkipSubmitterID(storage)
	if len(first) != 36 {
		t.Fatalf("not a uuid: %q", first)
	}

	resetSubmitterID()
	if second := AniSkipSubmitterID(storage); second != first {
		t.Errorf("a restart invented a new identity: %q then %q", first, second)
	}

	stored, err := os.ReadFile(filepath.Join(storage, aniSkipSubmitterFile))
	if err != nil {
		t.Fatalf("nothing was saved: %v", err)
	}
	if strings.TrimSpace(string(stored)) != first {
		t.Errorf("saved %q", strings.TrimSpace(string(stored)))
	}

	// A different storage directory is a different person.
	resetSubmitterID()
	if other := AniSkipSubmitterID(t.TempDir()); other == first {
		t.Error("two storage directories shared one identity")
	}
}

func TestAVoteNamesTheEntryAndTheDirection(t *testing.T) {
	var path, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		fmt.Fprint(w, `{"statusCode":200,"message":"ok","skipId":""}`)
	}))
	t.Cleanup(server.Close)
	restore := aniSkipWriteBaseForTest(t, server.URL)
	defer restore()

	if err := VoteSkipTime("c2cacbe5-4247-4ed7-bb64-28e780daf975", "downvote"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/vote/c2cacbe5-4247-4ed7-bb64-28e780daf975" {
		t.Errorf("voted at %q", path)
	}
	if !strings.Contains(body, "downvote") {
		t.Errorf("the vote said %q", body)
	}

	if err := VoteSkipTime("", "downvote"); err == nil {
		t.Error("voted on nothing")
	}
	if err := VoteSkipTime("some-id", "sideways"); err == nil {
		t.Error("cast a vote that is not a vote")
	}
}

// aniSkipWriteBaseForTest points the write calls at a local server.
func aniSkipWriteBaseForTest(t *testing.T, base string) func() {
	t.Helper()
	previous := aniSkipWriteBase
	aniSkipWriteBase = base
	return func() { aniSkipWriteBase = previous }
}

// resetSubmitterID forgets the identity held in memory, which is how a restart
// looks from the inside.
func resetSubmitterID() {
	aniSkipSubmitterMu.Lock()
	aniSkipSubmitterID = ""
	aniSkipSubmitterMu.Unlock()
}
