package anidb

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:            "anidb",
		Aliases:         []string{"anidb.app", "ani-db"},
		Referrer:        "https://anidb.app/",
		DefaultDisabled: true,
		// anidb.app has been serving a site-wide maintenance page, so this provider
		// could not be verified against live traffic. It stays out of the default
		// stack until the host is reachable; add "anidb" to Provider to try it.
		DisableReason: "anidb.app is serving a maintenance page; add \"anidb\" to Provider to try it anyway",
	}, func() providers.Provider {
		return &Provider{}
	})
}
