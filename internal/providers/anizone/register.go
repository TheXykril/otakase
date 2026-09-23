package anizone

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "anizone",
		Aliases:  []string{"az"},
		Referrer: referer,
	}, func() providers.Provider {
		return &Provider{}
	})
}
