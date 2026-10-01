package nyaa

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "nyaa",
		Aliases:  []string{"nyaa.si", "torrent"},
		Referrer: "https://nyaa.si/",
	}, func() providers.Provider {
		return New()
	})
	providers.Register(providers.Meta{
		Name:          "sukebei",
		Aliases:       []string{"sukebei.nyaa.si"},
		Referrer:      "https://sukebei.nyaa.si/",
		Adult:         true,
		DisableReason: "sukebei only serves adult titles; set AdultContent=true to use it",
	}, func() providers.Provider {
		return NewSukebei()
	})
}
