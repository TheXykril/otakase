package internal

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// The Otakase Discord's planned watch parties in the Community menu: the
// list comes from the bot when the row is picked (GET /v1/parties), and a
// party can be answered (POST /v1/party/{id}/rsvp {"going":bool}), joined
// when it is synced, or opened in Discord. Going members' apps also get a
// "notice" in the inbox shortly before the start, with a desktop
// notification.

// discordParty is a planned party as the bot lists it.
type discordParty struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Start     time.Time `json:"start"`
	AnilistID int       `json:"anilist_id"`
	Sync      bool      `json:"sync"`
	Host      string    `json:"host"`
	Going     int       `json:"going"`
	RSVP      bool      `json:"rsvp"`
	Live      bool      `json:"live"`
	JoinURL   string    `json:"join_url"`
	EventURL  string    `json:"event_url"`
}

func init() {
	addCommunityRow(communityRow{
		options: func(config *Config, linked bool) []SelectionOption {
			if !linked {
				return nil
			}
			return []SelectionOption{{Key: "parties", Label: "Watch parties · planned in the Discord", Icon: icons.Calendar}}
		},
		handle: func(config *Config, key string) (bool, bool) {
			if key != "parties" {
				return false, false
			}
			EnterAppScreen()
			return true, showDiscordParties(config)
		},
	})
}

func fetchDiscordParties(config *Config) ([]discordParty, error) {
	var answer struct {
		Parties []discordParty `json:"parties"`
	}
	token := loadDiscordLink(config).Token
	if _, err := discordDo(context.Background(), config, http.MethodGet, "/v1/parties", token, "", nil, &answer); err != nil {
		return nil, err
	}
	return answer.Parties, nil
}

func rsvpDiscordParty(config *Config, id string, going bool) (bool, int, error) {
	var answer struct {
		Going bool `json:"going"`
		Count int  `json:"count"`
	}
	token := loadDiscordLink(config).Token
	_, err := discordPostJSON(context.Background(), config, "/v1/party/"+url.PathEscape(id)+"/rsvp", token, map[string]bool{"going": going}, &answer)
	return answer.Going, answer.Count, err
}

// partyStartLabel is when a party starts, said for the menu.
func partyStartLabel(start, now time.Time) string {
	start, now = start.Local(), now.Local()
	if !start.After(now) {
		return "started " + start.Format("15:04")
	}
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()) }
	switch int(day(start).Sub(day(now)).Hours() / 24) {
	case 0:
		return "today " + start.Format("15:04")
	case 1:
		return "tomorrow " + start.Format("15:04")
	}
	return start.Format("Mon 2 Jan 15:04")
}

// partyLabel is a party's row in the list.
func partyLabel(p discordParty, now time.Time) string {
	parts := []string{p.Title}
	if p.Live {
		parts = append(parts, "on now")
	} else {
		parts = append(parts, partyStartLabel(p.Start, now))
	}
	parts = append(parts, fmt.Sprintf("%d going", p.Going))
	if p.Sync {
		parts = append(parts, "synced")
	}
	if p.RSVP {
		parts = append(parts, "you're in")
	}
	return strings.Join(parts, " · ")
}

// showDiscordParties lists the parties. It reports true when the member
// hosts a synced one and its show was queued to play (SetOpenLink).
func showDiscordParties(config *Config) bool {
	for {
		endBusy := BeginBusy(config, "Asking the Discord for watch parties")
		parties, err := fetchDiscordParties(config)
		endBusy()
		if err != nil {
			Log(fmt.Sprintf("Discord: watch parties: %v", err))
			Out("Couldn't reach the Discord bot. Try again in a moment.")
			return false
		}
		if len(parties) == 0 {
			Out("No watch parties are planned. Start one in the Discord with /watchparty.")
			return false
		}
		now := time.Now()
		options := make([]SelectionOption, 0, len(parties))
		for _, p := range parties {
			icon := icons.Calendar
			if p.Live && p.Sync {
				icon = icons.Play
			}
			options = append(options, SelectionOption{Key: "party:" + p.ID, Label: partyLabel(p, now), Icon: icon})
		}
		selected, err := promptSelectOrdered(options)
		if err != nil {
			Log(fmt.Sprintf("Discord: watch party menu: %v", err))
			return false
		}
		selected = NormalizeSelectionKey(selected)
		switch {
		case SelectionMeansQuit(selected):
			Exit(nil)
		case SelectionMeansBack(selected):
			return false
		}
		id, _ := strings.CutPrefix(selected.Key, "party:")
		for _, p := range parties {
			if p.ID == id {
				EnterAppScreen()
				if done, play := discordPartyActions(config, p); done {
					return play
				}
			}
		}
		EnterAppScreen()
	}
}

// discordPartyActions is one party's menu. done is true when the member
// joined it (and play when their show was queued to play).
func discordPartyActions(config *Config, p discordParty) (done, play bool) {
	var options []SelectionOption
	if p.RSVP {
		options = append(options, SelectionOption{Key: "out", Label: "I'm not going after all", Icon: icons.No})
	} else {
		options = append(options, SelectionOption{Key: "in", Label: "I'm in · get a reminder before it starts", Icon: icons.Yes})
	}
	if p.Sync {
		label := "Join now · your player follows the host's"
		if !p.Live {
			label = "Join early · wait in the party for the host"
		}
		options = append(options, SelectionOption{Key: "join", Label: label, Icon: icons.Play})
	}
	if p.EventURL != "" {
		options = append(options, SelectionOption{Key: "event", Label: "Open the event in Discord", Icon: icons.OpenLink})
	}
	selected, err := promptSelectOrdered(options)
	if err != nil {
		Log(fmt.Sprintf("Discord: watch party: %v", err))
		return false, false
	}
	selected = NormalizeSelectionKey(selected)
	if SelectionMeansQuit(selected) {
		Exit(nil)
	}
	switch selected.Key {
	case "in", "out":
		going, count, err := rsvpDiscordParty(config, p.ID, selected.Key == "in")
		switch {
		case err != nil:
			Log(fmt.Sprintf("Discord: watch party rsvp: %v", err))
			Out("Couldn't answer: " + err.Error())
		case going:
			Out(fmt.Sprintf("You're in (%d going). You'll get a reminder here and in Discord before it starts.", count))
		default:
			Out("Took you off the going list.")
		}
	case "join":
		hosting, err := StartWatchParty(config, HistoryFile(config.StoragePath), p.ID)
		if err != nil {
			Out("Couldn't join the watch party: " + err.Error())
			Log("watch party: " + err.Error())
			return false, false
		}
		if hosting && p.AnilistID > 0 {
			SetOpenLink(OpenLink{AnilistID: p.AnilistID})
			return true, true
		}
		return true, false
	case "event":
		if err := discordOpenURL(p.EventURL); err != nil {
			Out("Could not open a browser: " + p.EventURL)
		}
	}
	return false, false
}
