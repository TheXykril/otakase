package reanime

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "reanime",
		Aliases:  []string{"flixcloud", "flixcloud.cc", "reanime.to"},
		Referrer: flixURL + "/",
	}, func() providers.Provider {
		return &Provider{}
	})
}
