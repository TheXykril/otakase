package internal

import (
	"fmt"

	"github.com/pkg/browser"
)

// animeSkipSignupURL is where an account is created. animeSkipClientsURL is
// where a signed-in account creates a client id for an application.
const (
	animeSkipSignupURL  = "https://anime-skip.com/sign-up"
	animeSkipClientsURL = "https://anime-skip.com/account/api-clients"
)

// SetupAnimeSkipClientID walks the user through getting a client id of their
// own, rather than either typing one in blind or relying on `auto`.
//
// It can only guide, not automate. Anime-Skip's own docs say accounts "must be
// created at anime-skip.com/sign-up" and cannot be created through their API,
// and creating a client id is a page in account settings with no API at all --
// unlike AniList, there is no redirect this program can catch the end of. So
// this opens the right pages and asks for what comes out of them, the same
// shape as the manual-entry fallback already used for AniList and MyAnimeList.
func SetupAnimeSkipClientID(config *Config) error {
	if config == nil {
		return fmt.Errorf("missing config")
	}

	Out("Anime-Skip needs a client id created from an account of your own.")
	Out("Sign up first if you haven't already: " + animeSkipSignupURL)
	Out("Then create a client id at: " + animeSkipClientsURL)
	if err := browser.OpenURL(animeSkipClientsURL); err != nil {
		Out("Could not open the browser automatically: " + err.Error())
	}

	clientID, cancelled, err := promptCancelable(config, "Anime-Skip",
		"Paste the client id you created",
		"Account -> API Clients on anime-skip.com · esc to cancel")
	if err != nil {
		return err
	}
	if cancelled || clientID == "" {
		return fmt.Errorf("no client id provided")
	}

	config.AnimeSkipClientID = clientID
	if err := persistAnimeSkipClientID(clientID); err != nil {
		return err
	}
	Out("Anime-Skip client id saved.")
	return nil
}

// persistAnimeSkipClientID writes the id into the config file, alongside
// whatever else it already has, the same way persistTrackingConfig does for
// tracker settings.
func persistAnimeSkipClientID(clientID string) error {
	if GlobalConfigPath == "" {
		return nil
	}

	configMap, err := LoadConfigFromFile(GlobalConfigPath)
	if err != nil {
		return err
	}
	configMap["AnimeSkipClientID"] = clientID
	return SaveConfigToFile(GlobalConfigPath, configMap)
}
