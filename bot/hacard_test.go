package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHASlug(t *testing.T) {
	for host, want := range map[string]string{
		"a0d7b954-otakase-bot": "a0d7b954_otakase_bot",
		"local-otakase-bot":    "local_otakase_bot",
		"otakasebot":           "",
		"-x":                   "",
	} {
		if got := haSlug(host); got != want {
			t.Errorf("%s: got %q", host, got)
		}
	}
}

func TestHACardScript(t *testing.T) {
	js := string(haCardScript("a0d7b954_otakase_bot", "1.17.0"))
	for _, want := range []string{`const SLUG = "a0d7b954_otakase_bot"`, `"1.17.0"`, `{"value":"games","label":"Games"}`, `"otakase-bot-status-card"`, `"otakase-bot-controls-card"`, `"otakase-bot-today-card"`, `"otakase-bot-leaderboard-card"`, `"otakase-bot-support-card"`, `customElements.define(type, cls)`} {
		if !strings.Contains(js, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(js, "__") {
		t.Error("placeholder left")
	}

	dir := t.TempDir()
	fresh, err := writeHACard(dir, "s", "1")
	if err != nil || !fresh {
		t.Fatalf("first write: %v %v", fresh, err)
	}
	if fresh, _ := writeHACard(dir, "s", "2"); fresh {
		t.Error("www folder counted as new twice")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "www", haCardFile)); !strings.Contains(string(b), `VERSION = "2"`) {
		t.Error("script not rewritten")
	}
}

func TestSyncCardResource(t *testing.T) {
	run := func(existing string) []string {
		var sent []string
		call := func(msg map[string]any) (json.RawMessage, error) {
			sent = append(sent, msg["type"].(string))
			if msg["type"] == "lovelace/resources" {
				return json.RawMessage(existing), nil
			}
			return json.RawMessage(`{}`), nil
		}
		if err := syncCardResource(call, "/local/otakase-bot-card.js?v=2"); err != nil {
			t.Fatal(err)
		}
		return sent
	}
	if got := strings.Join(run(`[{"id":"a","url":"/local/other.js"}]`), ","); got != "lovelace/resources,lovelace/resources/create" {
		t.Errorf("new: %s", got)
	}
	if got := strings.Join(run(`[{"id":"a","url":"/local/otakase-bot-card.js?v=1"}]`), ","); got != "lovelace/resources,lovelace/resources/update" {
		t.Errorf("older: %s", got)
	}
	if got := strings.Join(run(`[{"id":"a","url":"/local/otakase-bot-card.js?v=2"}]`), ","); got != "lovelace/resources" {
		t.Errorf("current: %s", got)
	}
}
