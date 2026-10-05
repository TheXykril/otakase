package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// store keeps what has to survive a restart (warnings, voice rooms) in a
// small JSON file in the data directory.
type store struct {
	mu   sync.Mutex
	path string
	data storeData
}

type storeData struct {
	Warnings map[string][]warning `json:"warnings"`
	Rooms    map[string]room      `json:"rooms"` // voice channel id → room
}

type warning struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	Reason string    `json:"reason"`
}

type room struct {
	Owner   string    `json:"owner"`
	Created time.Time `json:"created"`
}

// dataDir is OTAKASE_DATA_DIR, else /data (the Home Assistant add-on's
// persistent folder) when it exists, else the working directory.
func dataDir() string {
	if d := os.Getenv("OTAKASE_DATA_DIR"); d != "" {
		return d
	}
	if fi, err := os.Stat("/data"); err == nil && fi.IsDir() {
		return "/data"
	}
	return "."
}

func openStore(path string) *store {
	st := &store{path: path}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &st.data); err != nil {
			log.Printf("%s: %v", path, err)
		}
	}
	if st.data.Warnings == nil {
		st.data.Warnings = map[string][]warning{}
	}
	if st.data.Rooms == nil {
		st.data.Rooms = map[string]room{}
	}
	return st
}

// update runs fn with the lock held and saves the result.
func (st *store) update(fn func(d *storeData)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.data)
	b, _ := json.MarshalIndent(st.data, "", "  ")
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("saving %s: %v", st.path, err)
		return
	}
	if err := os.Rename(tmp, st.path); err != nil {
		log.Printf("saving %s: %v", st.path, err)
	}
}

func (st *store) view(fn func(d *storeData)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.data)
}

func storePath() string { return filepath.Join(dataDir(), "otakase-bot.json") }
