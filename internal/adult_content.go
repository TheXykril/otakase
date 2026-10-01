package internal

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
