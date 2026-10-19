package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
)

// As a Home Assistant add-on, the bot puts an "Otakase Bot" card in the
// dashboard card picker: it writes the card's script to /config/www (the
// add-on maps Home Assistant's config folder at /homeassistant) and adds it
// as a dashboard resource through Home Assistant's websocket API. The card
// shows the add-on's dashboard page in an iframe. Dashboards kept in YAML
// mode need the resource added by hand (see the wiki).

//go:embed assets/ha-card.js
var haCardJS string

const (
	haConfigDir   = "/homeassistant"
	haCardFile    = "otakase-bot-card.js"
	haCardURLPath = "/local/" + haCardFile
	haWebsocket   = "ws://supervisor/core/websocket"
)

// haSlug is the add-on's slug from its hostname: the supervisor names the
// container after the slug with dashes, like a0d7b954-otakase-bot for
// a0d7b954_otakase_bot. The repository part never has a dash.
func haSlug(hostname string) string {
	repo, name, ok := strings.Cut(hostname, "-")
	if !ok || repo == "" || name == "" {
		return ""
	}
	return repo + "_" + strings.ReplaceAll(name, "-", "_")
}

// haCardScript is the card's script for this add-on and version, with the
// dashboard pages to pick from.
func haCardScript(slug, version string) []byte {
	type option struct {
		Value string `json:"value"`
		Label string `json:"label"`
	}
	var pages []option
	for _, s := range dashSections {
		pages = append(pages, option{s.Slug, s.Name})
	}
	list, _ := json.Marshal(pages)
	return []byte(strings.NewReplacer("__SLUG__", slug, "__VERSION__", version, "__PAGES__", string(list)).Replace(haCardJS))
}

func (b *bot) haCardLoop() {
	token := os.Getenv("SUPERVISOR_TOKEN")
	host, _ := os.Hostname()
	slug := haSlug(host)
	if token == "" || slug == "" {
		return
	}
	if fi, err := os.Stat(haConfigDir); err != nil || !fi.IsDir() {
		log.Printf("dashboard card: no Home Assistant config folder at %s", haConfigDir)
		return
	}
	newWWW, err := writeHACard(haConfigDir, slug, botVersion())
	if err != nil {
		b.jobFailed("Dashboard card", err)
		return
	}
	url := haCardURLPath + "?v=" + botVersion()
	for {
		err := haCall(token, func(call haCaller) error { return syncCardResource(call, url) })
		var refused haRefused
		switch {
		case err == nil:
			b.jobOK("Dashboard card")
			if newWWW {
				b.staffNote(&discordgo.MessageEmbed{Title: "Dashboard card added", Color: shu,
					Description: "Restart Home Assistant once so the **Otakase Bot** card can load, then find it when adding a card to a dashboard."})
			}
			return
		case errors.As(err, &refused):
			// Usually dashboards in YAML mode; retrying won't help.
			log.Printf("dashboard card: Home Assistant didn't add the resource (%v). Add %s as a JavaScript module resource by hand.", err, url)
			return
		}
		b.jobFailed("Dashboard card", err)
		time.Sleep(5 * time.Minute)
	}
}

// writeHACard writes the card's script to the config folder's www folder,
// and reports whether it had to make that folder (Home Assistant only
// serves /local from a www folder that was there when it started).
func writeHACard(configDir, slug, version string) (newWWW bool, err error) {
	www := filepath.Join(configDir, "www")
	if _, err := os.Stat(www); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(www, 0o755); err != nil {
			return false, err
		}
		newWWW = true
	}
	return newWWW, os.WriteFile(filepath.Join(www, haCardFile), haCardScript(slug, version), 0o644)
}

// haCaller sends one websocket command and returns its result.
type haCaller func(msg map[string]any) (json.RawMessage, error)

// haRefused is Home Assistant answering a command with an error.
type haRefused struct{ Code, Message string }

func (e haRefused) Error() string { return e.Code + ": " + e.Message }

// syncCardResource makes sure the dashboard resources hold the card's
// script at url, updating an older version instead of adding a second one.
func syncCardResource(call haCaller, url string) error {
	raw, err := call(map[string]any{"type": "lovelace/resources"})
	if err != nil {
		return err
	}
	var list []struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	for _, r := range list {
		path, _, _ := strings.Cut(r.URL, "?")
		if path != haCardURLPath {
			continue
		}
		if r.URL == url {
			return nil
		}
		_, err := call(map[string]any{"type": "lovelace/resources/update", "resource_id": r.ID, "res_type": "module", "url": url})
		return err
	}
	_, err = call(map[string]any{"type": "lovelace/resources/create", "res_type": "module", "url": url})
	return err
}

// haCall opens Home Assistant's websocket API through the supervisor,
// signs in with the add-on's token and runs fn.
func haCall(token string, fn func(haCaller) error) error {
	d := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	c, _, err := d.Dial(haWebsocket, http.Header{"Authorization": {"Bearer " + token}})
	if err != nil {
		return fmt.Errorf("connecting to Home Assistant: %w", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(time.Minute))
	var hello struct{ Type string }
	if err := c.ReadJSON(&hello); err != nil {
		return err
	}
	if err := c.WriteJSON(map[string]string{"type": "auth", "access_token": token}); err != nil {
		return err
	}
	if err := c.ReadJSON(&hello); err != nil {
		return err
	}
	if hello.Type != "auth_ok" {
		return fmt.Errorf("Home Assistant sign-in: %s (the add-on needs homeassistant_api)", hello.Type)
	}
	id := 0
	return fn(func(msg map[string]any) (json.RawMessage, error) {
		id++
		msg["id"] = id
		if err := c.WriteJSON(msg); err != nil {
			return nil, err
		}
		for {
			var res struct {
				ID      int             `json:"id"`
				Type    string          `json:"type"`
				Success bool            `json:"success"`
				Result  json.RawMessage `json:"result"`
				Error   *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := c.ReadJSON(&res); err != nil {
				return nil, err
			}
			if res.ID != id || res.Type != "result" {
				continue
			}
			if !res.Success {
				if res.Error != nil {
					return nil, haRefused{res.Error.Code, res.Error.Message}
				}
				return nil, haRefused{"unknown_error", "failed"}
			}
			return res.Result, nil
		}
	})
}
