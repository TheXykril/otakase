package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// The app talks to the Otakase Discord bot's public API (DiscordBotURL): it
// links to a member with a code they run in Discord, then sends heartbeats,
// reads the server's news and posts problem reports. Everything here is a
// courtesy: a network failure is logged and otherwise ignored, nothing waits
// on the bot for longer than discordRequestTimeout, and an empty DiscordBotURL
// turns all of it off.

const (
	// DefaultDiscordBotURL is the bot's public API.
	DefaultDiscordBotURL = "https://bot.xyverion.com"
	// DiscordInviteURL is the invite to the Otakase Discord, which is also
	// where the support forum lives.
	DiscordInviteURL = "https://otakase.xyverion.com/discord/"

	discordRequestTimeout = 5 * time.Second
	discordLinkFileName   = "discord_link.json"
)

// discordHTTPClient is used for every request to the bot. Its timeout is the
// whole request, so a bot that hangs costs five seconds at most.
var discordHTTPClient = &http.Client{Timeout: discordRequestTimeout}

// discordOpenURL opens a link in the browser; tests replace it.
var discordOpenURL = openInBrowser

// discordPollInterval is how often a pending link is asked about. Tests
// shorten it.
var discordPollInterval = 3 * time.Second

// errDiscordUnauthorized is a token the bot no longer knows: revoked from
// Discord with /unlink-app or /linked-apps. The token has been forgotten by
// the time a caller sees it.
var errDiscordUnauthorized = errors.New("this app is no longer linked to Discord; link it again")

// discordEnabled reports whether the Discord features are on.
func discordEnabled(config *Config) bool {
	return config != nil && strings.TrimSpace(config.DiscordBotURL) != ""
}

func discordBaseURL(config *Config) string {
	return strings.TrimRight(strings.TrimSpace(config.DiscordBotURL), "/")
}

// discordLink is what is kept of the link, beside the other tokens in the
// storage directory and readable only by the user.
type discordLink struct {
	Token       string    `json:"token,omitempty"`
	DiscordName string    `json:"discord_name,omitempty"`
	LinkedAt    time.Time `json:"linked_at,omitzero"`
	// Revoked marks a link the bot turned down, so the menu can say why the
	// app is no longer linked instead of quietly showing "Link".
	Revoked bool `json:"revoked,omitempty"`
}

func discordLinkPath(config *Config) string {
	storage := ""
	if config != nil {
		storage = os.ExpandEnv(config.StoragePath)
	}
	if strings.TrimSpace(storage) == "" {
		storage = GetStoragePath()
	}
	return filepath.Join(storage, discordLinkFileName)
}

func loadDiscordLink(config *Config) discordLink {
	data, err := os.ReadFile(discordLinkPath(config))
	if err != nil {
		return discordLink{}
	}
	var link discordLink
	if err := json.Unmarshal(data, &link); err != nil {
		Log(fmt.Sprintf("Discord: ignoring an unreadable %s: %v", discordLinkFileName, err))
		return discordLink{}
	}
	link.Token = strings.TrimSpace(link.Token)
	return link
}

func saveDiscordLink(config *Config, link discordLink) error {
	path := discordLinkPath(config)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(link, "", "  ")
	if err != nil {
		return err
	}
	// Written aside and renamed into place: heartbeats read the file from
	// other goroutines, and a half-written one would read as not linked.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// forgetDiscordLink drops the token. revoked records that the bot refused
// it rather than the user unlinking.
func forgetDiscordLink(config *Config, revoked bool) {
	if !revoked {
		if err := os.Remove(discordLinkPath(config)); err != nil && !os.IsNotExist(err) {
			Log(fmt.Sprintf("Discord: could not remove the link: %v", err))
		}
		return
	}
	link := loadDiscordLink(config)
	if err := saveDiscordLink(config, discordLink{DiscordName: link.DiscordName, Revoked: true}); err != nil {
		Log(fmt.Sprintf("Discord: could not forget the link: %v", err))
	}
}

// discordLinked reports whether this app holds a token.
func discordLinked(config *Config) bool {
	return discordEnabled(config) && loadDiscordLink(config).Token != ""
}

// discordAPIError is a non-2xx answer from the bot.
type discordAPIError struct {
	Status  int
	Message string
}

func (e *discordAPIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("Discord bot: %s (HTTP %d)", e.Message, e.Status)
	}
	return fmt.Sprintf("Discord bot answered HTTP %d", e.Status)
}

// discordDo sends one request to the bot and decodes a JSON answer into out.
// token, when set, is sent as the bearer token, and a 401 for it forgets the
// link and returns errDiscordUnauthorized. 2xx answers other than 200 (the
// link poll's 202) are returned with their status and no error.
func discordDo(ctx context.Context, config *Config, method, path, token, contentType string, body io.Reader, out any) (int, error) {
	return discordDoWith(discordHTTPClient, ctx, config, method, path, token, contentType, body, out)
}

func discordDoWith(client *http.Client, ctx context.Context, config *Config, method, path, token, contentType string, body io.Reader, out any) (int, error) {
	if !discordEnabled(config) {
		return 0, errors.New("Discord features are off (DiscordBotURL is empty)")
	}
	req, err := http.NewRequestWithContext(ctx, method, discordBaseURL(config)+path, body)
	if err != nil {
		return 0, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", DisplayName+"/"+Version())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusUnauthorized && token != "" {
		forgetDiscordLink(config, true)
		Log("Discord: the bot no longer knows this app's token; forgot it")
		return resp.StatusCode, errDiscordUnauthorized
	}
	if resp.StatusCode/100 != 2 {
		var apiErr struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &apiErr)
		return resp.StatusCode, &discordAPIError{Status: resp.StatusCode, Message: strings.TrimSpace(apiErr.Error)}
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("Discord bot: unreadable answer: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// discordPostJSON is discordDo with a JSON body.
func discordPostJSON(ctx context.Context, config *Config, path, token string, in, out any) (int, error) {
	data, err := json.Marshal(in)
	if err != nil {
		return 0, err
	}
	return discordDo(ctx, config, http.MethodPost, path, token, "application/json", bytes.NewReader(data), out)
}

// discordLinkStart is a pending link: the code the member runs in Discord
// and the secret this app polls with.
type discordLinkStart struct {
	Code      string `json:"code"`
	Poll      string `json:"poll"`
	ExpiresIn int    `json:"expires_in"`
}

func startDiscordLink(config *Config) (discordLinkStart, error) {
	var start discordLinkStart
	if _, err := discordPostJSON(context.Background(), config, "/v1/link/start", "", struct{}{}, &start); err != nil {
		return start, err
	}
	if start.Code == "" || start.Poll == "" {
		return start, errors.New("Discord bot: no link code in the answer")
	}
	if start.ExpiresIn <= 0 {
		start.ExpiresIn = 600
	}
	return start, nil
}

var errDiscordLinkExpired = errors.New("the link code expired; start again for a new one")

// pollDiscordLink asks once whether the code was used. It returns the link
// when it was, a zero link and no error while it is still pending.
func pollDiscordLink(ctx context.Context, config *Config, poll string) (discordLink, error) {
	var answer struct {
		Token       string `json:"token"`
		DiscordName string `json:"discord_name"`
	}
	status, err := discordPostJSON(ctx, config, "/v1/link/poll", "", map[string]string{"poll": poll}, &answer)
	if status == http.StatusGone {
		return discordLink{}, errDiscordLinkExpired
	}
	if err != nil {
		return discordLink{}, err
	}
	if status != http.StatusOK || strings.TrimSpace(answer.Token) == "" {
		return discordLink{}, nil
	}
	return discordLink{Token: strings.TrimSpace(answer.Token), DiscordName: answer.DiscordName, LinkedAt: time.Now().UTC()}, nil
}

// waitForDiscordLink polls until the code is used, expires, or ctx ends, and
// saves the link it gets. A failed poll is retried: the bot restarting or a
// moment offline should not throw the code away.
func waitForDiscordLink(ctx context.Context, config *Config, start discordLinkStart) (discordLink, error) {
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	ticker := time.NewTicker(discordPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return discordLink{}, ctx.Err()
		case <-ticker.C:
		}
		link, err := pollDiscordLink(ctx, config, start.Poll)
		switch {
		case errors.Is(err, errDiscordLinkExpired):
			return discordLink{}, err
		case err != nil:
			Log(fmt.Sprintf("Discord: link poll failed: %v", err))
		case link.Token != "":
			if err := saveDiscordLink(config, link); err != nil {
				return discordLink{}, fmt.Errorf("could not save the Discord link: %w", err)
			}
			Log(fmt.Sprintf("Discord: linked as %s", link.DiscordName))
			// The bot learns the version and OS from a heartbeat, and a dev
			// build gets its role from the first one.
			go func() {
				if err := sendDiscordHeartbeat(config, nil, 0); err != nil {
					Log(fmt.Sprintf("Discord: heartbeat after linking failed: %v", err))
				}
			}()
			return link, nil
		}
		if time.Now().After(deadline) {
			return discordLink{}, errDiscordLinkExpired
		}
	}
}

// unlinkDiscord revokes the token on the bot and forgets it here. The token
// is forgotten even when the bot cannot be reached: the user asked for it to
// go, and /unlink-app in Discord revokes it there.
func unlinkDiscord(config *Config) (reached bool, err error) {
	link := loadDiscordLink(config)
	if link.Token == "" {
		forgetDiscordLink(config, false)
		return true, nil
	}
	_, err = discordPostJSON(context.Background(), config, "/v1/unlink", link.Token, struct{}{}, nil)
	forgetDiscordLink(config, false)
	if err != nil && !errors.Is(err, errDiscordUnauthorized) {
		Log(fmt.Sprintf("Discord: unlink request failed: %v", err))
		return false, nil
	}
	return true, nil
}

func discordLinkInstructions(code string) string {
	return fmt.Sprintf("In the Otakase Discord, run /link-app code:%s", code)
}

// RunDiscordLinkCLI is `otakase -discord-link`: plain terminal output, for a
// command run on purpose.
func RunDiscordLinkCLI(config *Config) error {
	if !discordEnabled(config) {
		return errors.New("Discord features are off: set DiscordBotURL in the config (otakase -e)")
	}
	if link := loadDiscordLink(config); link.Token != "" {
		fmt.Printf("Already linked to Discord as %s. Run otakase -discord-unlink first to link again.\n", discordNameOr(link))
		return nil
	}
	start, err := startDiscordLink(config)
	if err != nil {
		return fmt.Errorf("could not start linking: %w", err)
	}
	fmt.Println(discordLinkInstructions(start.Code))
	fmt.Printf("The code works for %d minutes.\n", (start.ExpiresIn+59)/60)
	// Asked before the wait rather than during it: a read left running would
	// take the answer to whatever is asked next (-report links first, then
	// asks to confirm). A code used while this waits is still picked up by
	// the first poll.
	fmt.Printf("Open the Otakase Discord in your browser? [y/N] ")
	if answer, err := stdinLineReader().ReadString('\n'); err == nil && isAffirmativeAnswer(answer) {
		if err := discordOpenURL(DiscordInviteURL); err != nil {
			fmt.Printf("Could not open a browser: %s\n", DiscordInviteURL)
		}
	}
	fmt.Println("Waiting for the code to be used (Ctrl+C to stop)...")

	link, err := waitForDiscordLink(context.Background(), config, start)
	if err != nil {
		return err
	}
	fmt.Printf("Linked to Discord as %s.\n", discordNameOr(link))
	if !config.DiscordShareWatching {
		fmt.Println("Set DiscordShareWatching=true (otakase -e) to show the server what you are watching.")
	}
	// A beat for the heartbeat started on linking, which is what tells the
	// bot this app's version.
	time.Sleep(200 * time.Millisecond)
	return nil
}

// RunDiscordUnlinkCLI is `otakase -discord-unlink`.
func RunDiscordUnlinkCLI(config *Config) error {
	link := loadDiscordLink(config)
	if link.Token == "" {
		fmt.Println("This app is not linked to Discord.")
		return nil
	}
	if !discordEnabled(config) {
		forgetDiscordLink(config, false)
		fmt.Println("Forgot the Discord link here. Run /unlink-app in the Otakase Discord to revoke it there too.")
		return nil
	}
	reached, _ := unlinkDiscord(config)
	if !reached {
		fmt.Println("Forgot the Discord link here, but the bot could not be reached. Run /unlink-app in the Otakase Discord to revoke it there too.")
		return nil
	}
	fmt.Println("Unlinked from Discord.")
	return nil
}

func discordNameOr(link discordLink) string {
	if strings.TrimSpace(link.DiscordName) != "" {
		return link.DiscordName
	}
	return "your Discord account"
}

// linkDiscordFromMenu links from the Community menu. The code is shown as a
// row (picking it copies it), the poll runs behind the menu, and "check"
// waits a little for it to land. It reports whether the app is now linked.
func linkDiscordFromMenu(config *Config) bool {
	start, err := startDiscordLink(config)
	if err != nil {
		Log(fmt.Sprintf("Discord: link start failed: %v", err))
		Out("Could not reach the Otakase Discord bot. Try again later.")
		pauseAfterNotice(config)
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		link discordLink
		err  error
	}
	done := make(chan result, 1)
	go func() {
		link, err := waitForDiscordLink(ctx, config, start)
		done <- result{link, err}
	}()

	note := ""
	for {
		select {
		case r := <-done:
			return reportMenuLinkResult(r.link, r.err)
		default:
		}
		codeLabel := fmt.Sprintf("Code: %s · run /link-app code:%s in Discord", start.Code, start.Code)
		options := []SelectionOption{
			{Key: "copy", Label: codeLabel, Icon: icons.Tag},
			{Key: "open", Label: "Open the Otakase Discord", Icon: icons.OpenLink},
			{Key: "check", Label: "Done, check now" + note, Icon: icons.Refresh},
		}
		selected, err := promptSelectOrdered(options)
		if err != nil {
			Log(fmt.Sprintf("Discord: link menu: %v", err))
			return false
		}
		selected = NormalizeSelectionKey(selected)
		switch {
		case SelectionMeansQuit(selected):
			Exit(nil)
		case SelectionMeansBack(selected):
			return false
		}
		switch selected.Key {
		case "copy":
			if err := clipboardWriteAll(start.Code); err != nil {
				Log(fmt.Sprintf("Discord: could not copy the code: %v", err))
			}
		case "open":
			if err := discordOpenURL(DiscordInviteURL); err != nil {
				Log(fmt.Sprintf("Discord: opening the invite: %v", err))
				Out("Could not open a browser: " + DiscordInviteURL)
			}
		case "check":
			select {
			case r := <-done:
				return reportMenuLinkResult(r.link, r.err)
			case <-time.After(2 * discordPollInterval):
				note = " · not linked yet"
			}
		}
	}
}

func reportMenuLinkResult(link discordLink, err error) bool {
	if err != nil {
		Log(fmt.Sprintf("Discord: linking failed: %v", err))
		Out("Linking failed: " + err.Error())
		pauseAfterNotice(GetGlobalConfig())
		return false
	}
	Out(fmt.Sprintf("Linked to Discord as %s.", discordNameOr(link)))
	return true
}

// setDiscordShareWatching flips the option and writes it to the config file.
func setDiscordShareWatching(config *Config, on bool) {
	config.DiscordShareWatching = on
	if strings.TrimSpace(ConfigFilePath) == "" {
		return
	}
	configMap, err := LoadConfigFromFile(ConfigFilePath)
	if err != nil {
		Log(fmt.Sprintf("Discord: could not read the config to save DiscordShareWatching: %v", err))
		return
	}
	configMap["DiscordShareWatching"] = fmt.Sprintf("%t", on)
	if err := SaveConfigToFile(ConfigFilePath, configMap); err != nil {
		Log(fmt.Sprintf("Discord: could not save DiscordShareWatching: %v", err))
	}
}
