package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The Touch Grass badge: about one in touchGrassDropOdds of the rare
// touch-grass goodbyes (so roughly one exit in a thousand) also drops a
// hidden badge for the Otakase Discord, once per install. A linked app hands
// it to the bot on the spot; otherwise it waits, and goes over at the next
// launch once the app is linked.
//
//	POST /v1/badge/touch-grass → {"new":true,"finders":3}
//
// The small touchGrassFileName file in the storage directory remembers the
// drop: "pending" until the bot has it, then "claimed". Without the file the
// badge has not dropped yet.

const (
	touchGrassDropOdds  = 10
	touchGrassFileName  = "touch_grass"
	touchGrassPending   = "pending"
	touchGrassClaimed   = "claimed"
	touchGrassExitWait  = 3 * time.Second
	touchGrassBadgeLine = "Rare drop: the Touch Grass badge for the Otakase Discord."
)

func touchGrassFile(config *Config) string {
	storage := ""
	if config != nil {
		storage = storageDir(config.StoragePath)
	}
	if strings.TrimSpace(storage) == "" {
		storage = GetStoragePath()
	}
	return filepath.Join(storage, touchGrassFileName)
}

// touchGrassState is "", touchGrassPending or touchGrassClaimed.
func touchGrassState(config *Config) string {
	raw, err := os.ReadFile(touchGrassFile(config))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func setTouchGrassState(config *Config, state string) {
	path := touchGrassFile(config)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		Log(fmt.Sprintf("Touch Grass badge: %v", err))
		return
	}
	if err := os.WriteFile(path, []byte(state+"\n"), 0o644); err != nil {
		Log(fmt.Sprintf("Touch Grass badge: %v", err))
	}
}

type touchGrassAnswer struct {
	New     bool `json:"new"`
	Finders int  `json:"finders"`
}

// claimTouchGrass gives the badge to the linked Discord member and marks it
// claimed.
func claimTouchGrass(config *Config, wait time.Duration) (touchGrassAnswer, error) {
	var answer touchGrassAnswer
	if !discordLinked(config) {
		return answer, errors.New("not linked to Discord")
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if _, err := discordPostJSON(ctx, config, "/v1/badge/touch-grass", loadDiscordLink(config).Token, struct{}{}, &answer); err != nil {
		return answer, err
	}
	setTouchGrassState(config, touchGrassClaimed)
	return answer, nil
}

// dropTouchGrassBadge follows a rare goodbye: one roll in touchGrassDropOdds
// drops the badge, if it never has before. roll is rand.IntN.
func dropTouchGrassBadge(config *Config, roll func(int) int) {
	if !discordEnabled(config) || touchGrassState(config) != "" || roll(touchGrassDropOdds) != 0 {
		return
	}
	setTouchGrassState(config, touchGrassPending)
	Out(touchGrassBadgeLine)
	if !discordLinked(config) {
		Out("Link Discord (Community menu, or otakase -discord-link) to claim it. It'll wait for you.")
		return
	}
	answer, err := claimTouchGrass(config, touchGrassExitWait)
	switch {
	case err != nil:
		Log(fmt.Sprintf("Touch Grass badge: %v", err))
		Out("Couldn't reach Discord just now. It goes over next time otakase starts.")
	case !answer.New:
		Out("Your Discord already has it.")
	case answer.Finders <= 1:
		Out("It's on your Discord profile now (/badges). You're the first to find it.")
	default:
		Out(fmt.Sprintf("It's on your Discord profile now (/badges). Only %d people have found it.", answer.Finders))
	}
}

// claimPendingTouchGrass hands over a badge that dropped while the app was
// offline or not linked yet. Quiet: it runs in the background.
func claimPendingTouchGrass(config *Config) {
	if touchGrassState(config) != touchGrassPending || !discordLinked(config) {
		return
	}
	if _, err := claimTouchGrass(config, discordRequestTimeout); err != nil {
		Log(fmt.Sprintf("Touch Grass badge: claim failed, will retry next launch: %v", err))
		return
	}
	Log("Touch Grass badge: claimed on Discord")
}
