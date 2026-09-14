package kickassanime

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "kickassanime",
		Aliases:  []string{"kaa", "kaa.lt", "kickass"},
		Referrer: referer,
	}, func() providers.Provider {
		return &Provider{}
	})
}
