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
		//
		// Re-checked 2026-09-08; three independent blockers, any one of which is
		// enough on its own:
		//
		//   1. The persisted-query hash below is no longer registered. Both it and
		//      the hash other clients use answer PersistedQueryNotFound. Sending the
		//      full query text by GET is refused by Cloudflare (403) -- only the
		//      persisted form gets through -- so the hash cannot simply be dropped.
		//   2. Sending the full query by POST does reach the API (200), and that is
		//      what surfaces the real wall: AA_CRYPTO_MISSING. The episode field
		//      wants a request signature Curd cannot produce.
		//   3. The signature could be learned by watching their own frontend, but
		//      allanime.to no longer resolves, allanime.day is a 301 loop, and the
		//      live frontend (allmanga.to) sits behind Cloudflare Turnstile, which
		//      does not clear for an automated browser. So the scheme cannot be
		//      observed either.
		//
		// Reviving this provider means solving the signing scheme, not refreshing a
		// hash or a domain.
		DisableReason: "AllAnime requires a signed request for episode sources (AA_CRYPTO_MISSING); search works but playback does not",
	}, func() providers.Provider {
		return &Provider{}
	})
}
