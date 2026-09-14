package anipub

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "anipub",
		Aliases:  []string{"anipub.xyz"},
		Referrer: "https://megaplay.buzz/",
	}, func() providers.Provider {
		return &Provider{}
	})
}
