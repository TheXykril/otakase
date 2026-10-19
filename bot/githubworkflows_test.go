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
	dev, _ := workflowByKey("dev")
	site, _ := workflowByKey("site")

	in, err := workflowInputs(build, true, 0, "")
	if err != nil || in["release"] != "true" || len(in) != 1 {
		t.Fatalf("release build = %v %v", in, err)
	}
	if in, err = workflowInputs(build, false, 12, ""); err != nil || in["pr_number"] != "12" || in["release"] != "false" {
		t.Fatalf("pr build = %v %v", in, err)
	}
	if in, err = workflowInputs(dev, false, 0, " v26.8.0 "); err != nil || in["version"] != "26.8.0" {
		t.Fatalf("dev build = %v %v", in, err)
	}
	for name, bad := range map[string]func() error{
		"release with pr":     func() error { _, err := workflowInputs(build, true, 3, ""); return err },
		"release on site":     func() error { _, err := workflowInputs(site, true, 0, ""); return err },
		"dev without version": func() error { _, err := workflowInputs(dev, false, 0, ""); return err },
		"version on site":     func() error { _, err := workflowInputs(site, false, 0, "1.0.0"); return err },
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
		if w.takesVersion && !strings.Contains(text, "version:") {
			t.Errorf("%s has no version input", w.file)
		}
	}
	ci, _ := os.ReadFile(filepath.Join("..", ".github", "workflows", "ci-cd.yml"))
	for _, input := range []string{"release:", "pr_number:"} {
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

	if status, err := dispatchWorkflow("tok", "ci-cd.yml", map[string]string{"release": "true"}); err != nil || status != http.StatusNoContent {
		t.Fatalf("dispatch = %d %v", status, err)
	}
	if path != "/ci-cd.yml/dispatches" || body["ref"] != "main" || body["inputs"].(map[string]any)["release"] != "true" {
		t.Errorf("sent %s %v", path, body)
	}
	if status, err := dispatchWorkflow("bad", "site.yml", nil); err == nil || status != http.StatusUnauthorized {
		t.Errorf("bad token: %d %v", status, err)
	}
}
