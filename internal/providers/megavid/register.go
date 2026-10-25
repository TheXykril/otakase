package megavid

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "megavid",
		Referrer: referer,
	}, func() providers.Provider {
		return &Provider{}
	})
}
