package allanime

import "github.com/wraient/curd/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:            "allanime",
		Aliases:         []string{"all-anime", "all anime"},
		Referrer:        "https://allanime.day/",
		DefaultDisabled: true,
		// Catalogue search still answers, but the episode endpoint now rejects
		// unsigned requests with AA_CRYPTO_MISSING, so every selection made from an
		// AllAnime result fails at playback. Listing it would only offer shows that
		// cannot be played.
		DisableReason: "AllAnime requires a signed request for episode sources (AA_CRYPTO_MISSING); search works but playback does not",
	}, func() providers.Provider {
		return &Provider{}
	})
}
