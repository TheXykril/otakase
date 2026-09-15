package anikoto

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "anikoto",
		Aliases:  []string{"koto"},
		Referrer: referer,
	}, func() providers.Provider {
		return &Provider{}
	})
}
