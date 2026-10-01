package internal

import "github.com/thexykril/otakase/internal/providers"

// AdultContent=false keeps 18+ titles out of sight: the list menus, their
// counts, Surprise Me, the continue-watching rows and AniList search. It only
// hides them. The entries stay on the tracker and keep syncing, because a
// list that silently lost entries would read to dual sync as entries to copy
// or delete.

// hideAdultEntries drops 18+ titles unless the config shows them.
func hideAdultEntries(entries []Entry, config *Config) []Entry {
	if config == nil || config.AdultContent {
		return entries
	}
	visible := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if !entry.Media.IsAdult {
			visible = append(visible, entry)
		}
	}
	return visible
}

// visibleEntriesByCategory is getEntriesByCategory as the menus show it.
func visibleEntriesByCategory(list AnimeList, category string, config *Config) []Entry {
	return hideAdultEntries(getEntriesByCategory(list, category), config)
}

// searchVariables are the AniList search variables. isAdult is sent only to
// hide 18+ titles; left out, AniList returns both.
func searchVariables(query string) map[string]interface{} {
	variables := map[string]interface{}{"search": query}
	if config := GetGlobalConfig(); config != nil && !config.AdultContent {
		variables["isAdult"] = false
	}
	return variables
}

// providerNamesForShow is the provider stack to look for one show on. An
// adult show is carried only by adult providers, so with AdultContent on it
// goes to those alone: the general hosts cannot have it, and asking them
// first only costs time and can map the show onto a same-named series that is
// not it. Every other show skips the adult providers for the same reason.
func providerNamesForShow(config *Config, anime *Anime) []string {
	configured := configuredProviderNames(config)
	adultShow := routesToAdultProviders(config, anime)
	var names []string
	for _, name := range configured {
		if isAdultProvider(name) == adultShow {
			names = append(names, name)
		}
	}
	if adultShow && len(names) == 0 {
		// A stack the viewer wrote without the adult providers: add them
		// rather than search hosts that cannot carry the show.
		for _, name := range providers.RegisteredNames() {
			if isAdultProvider(name) && ProviderEnabled(name) {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return configured
	}
	return names
}

func isAdultProvider(name string) bool {
	meta, ok := providers.MetaFor(name)
	return ok && meta.Adult
}

// providerSuitsShow reports whether a provider is the right kind for this
// show: an adult provider for an adult show while AdultContent is on, a
// general one otherwise. Whether it is in the configured stack is a separate
// question, answered where it always was.
func providerSuitsShow(config *Config, anime *Anime, name string) bool {
	return isAdultProvider(name) == routesToAdultProviders(config, anime)
}

func routesToAdultProviders(config *Config, anime *Anime) bool {
	return anime != nil && anime.IsAdult && config != nil && config.AdultContent
}
