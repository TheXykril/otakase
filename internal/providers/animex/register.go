package animex

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "animex",
		Referrer: siteReferer,
	}, func() providers.Provider {
		return &Provider{}
	})
}
