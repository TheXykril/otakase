package internal

import (
	"slices"

	"github.com/thexykril/otakase/internal/providers"
)

// disabledProviderReasonForTest, when set, decides alone which providers are
// disabled, so tests do not depend on which hosts are down this month.
var disabledProviderReasonForTest func(name string) string

// providerDisabledReason says why a provider may not be used, or "" when it
// may. The config's DisabledProviders comes first, then the provider's own
// registration: switched off by default, or 18+ only with AdultContent off.
func providerDisabledReason(provider string) string {
	canonical := normalizeProviderName(provider)
	switch {
	case canonical == "":
		return "" // not a provider; ProviderEnabled deals with that
	case disabledProviderReasonForTest != nil:
		return disabledProviderReasonForTest(canonical)
	}

	meta, registered := providers.MetaFor(canonical)
	if config := GetGlobalConfig(); config != nil && slices.Contains(parseDisabledProviderNames(config.DisabledProviders), canonical) {
		if meta.DisableReason != "" {
			return meta.DisableReason
		}
		return "disabled in config"
	}
	off := registered && (meta.DefaultDisabled || (meta.Adult && !adultContentOn()))
	if off {
		return meta.DisableReason
	}
	return ""
}

// adultContentOn reports whether the config allows 18+ titles. Without a
// config they stay hidden.
func adultContentOn() bool {
	config := GetGlobalConfig()
	return config != nil && config.AdultContent
}

// parseDisabledProviderNames reads DisabledProviders, which takes the same
// list formats as Provider.
func parseDisabledProviderNames(value string) []string {
	return uniqueProviderNames(parseProviderConfigParts(value))
}

// ProviderEnabled says whether a provider can be used in this run.
//
// A name nothing registers is not a provider that happens to be switched on
// -- it is one this build does not have. Reading it as enabled is how a
// config naming a provider that has since been removed would survive
// migration untouched, leaving the user pointed at nothing.
func ProviderEnabled(name string) bool {
	return normalizeProviderName(name) != "" && providerDisabledReason(name) == ""
}

// ProviderDisabledReason is why a provider cannot be used; "" means it can.
func ProviderDisabledReason(provider string) string {
	return providerDisabledReason(provider)
}

// filterEnabledProviders keeps the enabled providers among names, once each,
// in their canonical spelling.
func filterEnabledProviders(candidates []string) []string {
	return slices.DeleteFunc(uniqueProviderNames(candidates), func(name string) bool {
		return !ProviderEnabled(name)
	})
}

// preferredProviderOrder ranks providers by how reliably they currently resolve
// streams.
//
// anikoto leads: it carries sub and dub for nearly everything, answers with a
// plain HLS manifest, English captions and per-episode intro and outro ranges,
// and its servers fall back to one another.
//
// megavid follows: it is addressed by MAL id, so it never plays the wrong show,
// and it keeps its own copy of what it carries rather than relaying the same
// CDN anikoto uses, so it still answers when that one is down.
//
// kickassanime follows: it answers as cheaply, carries whole seasons rather than
// only what is recent, and like anikoto indexes dubs separately, so a dub
// request either gets a dub or a straight answer that there is none.
//
// animex follows: like megavid it is addressed by id (AniList's), and it
// reaches two hosts through one API, one of which no other provider uses.
//
// reanime follows animex: resolving its player takes extra work, so prefer
// the other web providers before trying it, then fall back to torrents.
//
// nyaa sits behind the streaming hosts because it costs more to start -- peers
// have to be found before the first frame -- but it reliably carries an episode
// in the week it airs even when every streaming host is still missing it.
//
// sukebei comes last: it carries only adult titles, which none of the others
// do, so it is the one left to try for those, and is only in the stack at all
// when AdultContent is on.
var preferredProviderOrder = []string{"anikoto", "megavid", "kickassanime", "anizone", "animex", "reanime", "nyaa", "sukebei"}

// defaultEnabledProviderStack is the "stacked" setting spelled out: the
// enabled providers in preferredProviderOrder, then any others registered, in
// name order.
func defaultEnabledProviderStack() []string {
	known := providers.RegisteredNames() // sorted by name
	stack := make([]string, 0, len(known))
	preferred := func(name string) bool { return slices.Contains(preferredProviderOrder, name) }
	for _, name := range preferredProviderOrder {
		if slices.Contains(known, name) {
			stack = append(stack, name)
		}
	}
	for _, name := range known {
		if !preferred(name) {
			stack = append(stack, name)
		}
	}
	return filterEnabledProviders(stack)
}

// providerSelectionOptions is the Provider setting's menu: the default stack,
// then each enabled provider on its own.
func providerSelectionOptions() []SelectionOption {
	stack := defaultEnabledProviderStack()
	menu := make([]SelectionOption, 0, len(stack)+1)
	menu = append(menu, SelectionOption{Key: stackedProviderConfigValue, Label: providerConfigDisplayLabel(stackedProviderConfigValue)})
	for _, name := range stack {
		single := formatProviderConfigValue([]string{name})
		menu = append(menu, SelectionOption{Key: single, Label: name})
	}
	return menu
}

// firstEnabledProviderName is the provider to fall back to when nothing
// configured is usable.
func firstEnabledProviderName() string {
	if stack := defaultEnabledProviderStack(); len(stack) > 0 {
		return stack[0]
	}
	// Every preferred provider is disabled, so fall back to the first registered
	// one rather than a hardcoded name that may no longer resolve.
	for _, name := range preferredProviderOrder {
		if _, ok := providers.MetaFor(name); ok {
			return name
		}
	}
	if registered := providers.RegisteredNames(); len(registered) > 0 {
		return registered[0]
	}
	return ""
}

// ensureEnabledProviderNames returns the enabled names, or a single provider
// to use when none is. That one is disabled only when every provider is,
// since there is nothing better left to hand back.
func ensureEnabledProviderNames(names []string) []string {
	if usable := filterEnabledProviders(names); len(usable) > 0 {
		return usable
	}
	// One stand-in, an enabled provider if there is any.
	if stack := defaultEnabledProviderStack(); len(stack) > 0 {
		return stack[:1]
	}
	if known := providers.RegisteredNames(); len(known) > 0 {
		return known[:1]
	}
	fallback := firstEnabledProviderName()
	return []string{fallback}
}
