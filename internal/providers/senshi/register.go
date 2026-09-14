package senshi

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:            "senshi",
		Aliases:         []string{"senshi.live", "senshi project"},
		Referrer:        "https://senshi.live/",
		DefaultDisabled: true,
		// senshi.live lapsed and is now a parked domain listed for sale: it serves a
		// domain-broker page over HTTP and aborts the TLS handshake outright, so every
		// request fails with an EOF after burning the connect timeout. Keeping it in
		// the default stack made each search wait on a host that can never answer.
		DisableReason: "senshi.live is no longer an anime host (the domain lapsed and is parked for sale)",
	}, func() providers.Provider {
		return &Provider{}
	})
}
