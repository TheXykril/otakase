package nyaa

import "github.com/wraient/curd/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "nyaa",
		Aliases:  []string{"nyaa.si", "torrent"},
		Referrer: "https://nyaa.si/",
	}, func() providers.Provider {
		return &Provider{}
	})
}
