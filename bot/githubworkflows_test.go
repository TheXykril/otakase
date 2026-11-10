package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowInputs(t *testing.T) {
	build, _ := workflowByKey("build")
	wiki, _ := workflowByKey("wiki")

	in, err := workflowInputs(build, "", 0, "", "")
	if err != nil || in["publish"] != "nothing" || len(in) != 1 {
		t.Fatalf("test build = %v %v", in, err)
	}
	if in, err = workflowInputs(build, "release", 0, "", ""); err != nil || in["publish"] != "release" || len(in) != 1 {
		t.Fatalf("release build = %v %v", in, err)
	}
	if in, err = workflowInputs(build, "nothing", 12, "", ""); err != nil || in["pr_number"] != "12" || in["publish"] != "nothing" {
		t.Fatalf("pr build = %v %v", in, err)
	}
	if in, err = workflowInputs(build, "dev", 0, " v26.8.0 ", ""); err != nil || in["version"] != "26.8.0" || in["publish"] != "dev" {
		t.Fatalf("dev build = %v %v", in, err)
	}
	if in, err = workflowInputs(build, "dev", 0, "", ""); err != nil || in["version"] != "" {
		t.Fatalf("dev build, next drop = %v %v", in, err)
	}
	if in, err = workflowInputs(build, "release", 0, "", "full"); err != nil || in["site"] != "full" || in["publish"] != "release" {
		t.Fatalf("release with full site = %v %v", in, err)
	}
	if in, err = workflowInputs(wiki, "", 0, "", ""); err != nil || len(in) != 0 {
		t.Fatalf("wiki = %v %v", in, err)
	}
	for name, bad := range map[string]func() error{
		"site without release": func() error { _, err := workflowInputs(build, "dev", 0, "", "full"); return err },
		"site on wiki":         func() error { _, err := workflowInputs(wiki, "", 0, "", "quick"); return err },
		"unknown site":         func() error { _, err := workflowInputs(build, "release", 0, "", "huge"); return err },
		"unknown publish":      func() error { _, err := workflowInputs(build, "beta", 0, "", ""); return err },
		"release with pr":      func() error { _, err := workflowInputs(build, "release", 3, "", ""); return err },
		"dev with pr":          func() error { _, err := workflowInputs(build, "dev", 3, "", ""); return err },
		"release on wiki":      func() error { _, err := workflowInputs(wiki, "release", 0, "", ""); return err },
		"version on release":   func() error { _, err := workflowInputs(build, "release", 0, "26.8.0", ""); return err },
		"version on wiki":      func() error { _, err := workflowInputs(wiki, "", 0, "1.0.0", ""); return err },
	} {
		if bad() == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// Every workflow /github offers exists and is started by hand, and every
// input it sends is one the workflow declares.
func TestGitHubWorkflowsMatchRepository(t *testing.T) {
	for _, w := range ghWorkflows {
		raw, err := os.ReadFile(filepath.Join("..", ".github", "workflows", w.file))
		if err != nil {
			t.Fatalf("%s: %v", w.key, err)
		}
		text := string(raw)
		if !strings.Contains(text, "workflow_dispatch:") {
			t.Errorf("%s can't be started by hand", w.file)
		}
	}
	ci, _ := os.ReadFile(filepath.Join("..", ".github", "workflows", "ci-cd.yml"))
	for _, input := range []string{"publish:", "pr_number:", "version:", "site:"} {
		if !strings.Contains(string(ci), input) {
			t.Errorf("ci-cd.yml lost its %s input", input)
		}
	}
}

func TestDispatchWorkflow(t *testing.T) {
	var path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&body)
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	old := workflowsURL
	workflowsURL = srv.URL
	defer func() { workflowsURL = old }()

	if status, err := dispatchWorkflow("tok", "ci-cd.yml", map[string]string{"publish": "release"}); err != nil || status != http.StatusNoContent {
		t.Fatalf("dispatch = %d %v", status, err)
	}
	if path != "/ci-cd.yml/dispatches" || body["ref"] != "main" || body["inputs"].(map[string]any)["publish"] != "release" {
		t.Errorf("sent %s %v", path, body)
	}
	if status, err := dispatchWorkflow("bad", "wiki-sync.yml", nil); err == nil || status != http.StatusUnauthorized {
		t.Errorf("bad token: %d %v", status, err)
	}
}
