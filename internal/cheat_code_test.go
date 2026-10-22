package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKonamiTracker(t *testing.T) {
	var k konamiTracker
	keys := append([]string{"up", "down", "x"}, konamiKeys...)
	for i, key := range keys {
		if done := k.press(key); done != (i == len(keys)-1) {
			t.Fatalf("key %d (%s): done=%v", i, key, done)
		}
	}
	// Capital B and A count too; one wrong key breaks it.
	for _, key := range []string{"up", "up", "down", "down", "left", "right", "left", "right", "B", "A"} {
		if k.press(key) && key != "A" {
			t.Fatal("finished early")
		}
	}
	for _, key := range []string{"up", "up", "down", "down", "left", "left", "left", "right", "b", "a"} {
		if k.press(key) {
			t.Fatal("wrong sequence finished")
		}
	}
}

func TestMainListKonamiFinishes(t *testing.T) {
	m := &selectionModel{allOptions: []SelectionOption{{Key: "1", Label: "Frieren"}}, isHomeMenu: true}
	m.applyFilter()
	var cmd any
	for _, key := range konamiKeys {
		cmd = m.handleKey(key)
	}
	if cmd == nil {
		t.Fatal("menu did not end")
	}
	if picked, _ := m.highlighted(); picked.Key != cheatCodeKey {
		t.Fatalf("picked %+v", picked)
	}

	other := &selectionModel{allOptions: []SelectionOption{{Key: "1", Label: "Frieren"}}}
	other.applyFilter()
	for _, key := range konamiKeys {
		other.handleKey(key)
	}
	if picked, _ := other.highlighted(); picked.Key == cheatCodeKey {
		t.Fatal("only the main list takes the code")
	}
}

func TestRofiKonamiText(t *testing.T) {
	picked, err := parseRofiSelection(nil, "UUDD LRLR BA\n", []SelectionOption{{Key: "1", Label: "Frieren"}}, true)
	if err != nil || picked.Key != cheatCodeKey {
		t.Fatalf("list: %+v %v", picked, err)
	}
	rows := []SelectionOption{{Key: "1", Label: "Frieren"}}
	if picked, _ := parseMainGridSelection("-1 uuddlrlrba\n", rows); picked.Key != cheatCodeKey {
		t.Fatalf("grid code: %+v", picked)
	}
	if picked, _ := parseMainGridSelection("-1 typo\n", rows); picked.Key != "" {
		t.Fatalf("grid typo: %+v", picked)
	}
	if picked, _ := parseMainGridSelection("0 fri\n", rows); picked.Key != "1" {
		t.Fatalf("grid pick: %+v", picked)
	}
	if !isMainMenuKey(cheatCodeKey) {
		t.Fatal("not dispatched as a main menu key")
	}
}

func TestCheatOpenCommand(t *testing.T) {
	for in, want := range map[string]int{"open": 1, "Open 3": 3, "open x": 0, "opener": 0, "teapot": 0} {
		if n, ok := cheatOpenCommand(in); n != want && ok {
			t.Errorf("%q: %d %v", in, n, ok)
		} else if !ok && want != 0 {
			t.Errorf("%q not read", in)
		}
	}
}

func TestRunCheatCodeSolves(t *testing.T) {
	step := 1
	var answers []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		view := map[string]any{"status": "puzzle", "kind": "Letter grid", "step": step, "steps": 2, "text": "find it", "where": "app"}
		if r.URL.Path == "/v1/cheat/answer" {
			var in map[string]string
			json.NewDecoder(r.Body).Decode(&in)
			answers = append(answers, in["answer"])
			if in["answer"] == "teapot" {
				step++
				view["correct"] = true
				view["step"] = step
			} else {
				view["message"] = "Nothing happens."
			}
			if step > 2 {
				view = map[string]any{"correct": true, "status": "found", "code": "ABCD-EFGH-JKLM"}
			}
		}
		json.NewEncoder(w).Encode(view)
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	if err := saveDiscordLink(config, discordLink{Token: "tok"}); err != nil {
		t.Fatal(err)
	}

	typed := []string{"kettle", "teapot", "teapot"}
	var titles, notes, bodies []string
	old := cheatPage
	t.Cleanup(func() { cheatPage = old })
	cheatPage = func(_ *Config, title, body, note, _ string) (string, bool) {
		titles, notes, bodies = append(titles, title), append(notes, note), append(bodies, body)
		if len(typed) == 0 {
			return "", true
		}
		answer := typed[0]
		typed = typed[1:]
		return answer, false
	}
	RunCheatCode(config)

	if strings.Join(answers, ",") != "kettle,teapot,teapot" {
		t.Errorf("answers sent %v", answers)
	}
	if !strings.HasPrefix(titles[0], "CHEAT ACTIVATED") || strings.HasPrefix(titles[1], "CHEAT ACTIVATED") {
		t.Errorf("titles %q", titles)
	}
	if notes[1] != "Nothing happens." || notes[2] != "Correct." {
		t.Errorf("notes %q", notes)
	}
	if last := bodies[len(bodies)-1]; !strings.Contains(last, "/redeem code:ABCD-EFGH-JKLM") {
		t.Errorf("last page %q", last)
	}
}

func TestRunCheatCodeNeedsLink(t *testing.T) {
	config := newDiscordTestConfig(t, "http://127.0.0.1:1")
	var body string
	old := cheatPage
	t.Cleanup(func() { cheatPage = old })
	cheatPage = func(_ *Config, _, b, _, _ string) (string, bool) { body = b; return "", true }
	RunCheatCode(config)
	if !strings.Contains(body, "Link Otakase to Discord") {
		t.Errorf("unlinked page %q", body)
	}
}
