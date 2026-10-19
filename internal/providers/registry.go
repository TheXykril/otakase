package providers

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// Meta is what a provider registers about itself besides its factory.
type Meta struct {
	// Name is the canonical name, the one written to the config and the
	// watch history. Aliases are other spellings that resolve to it.
	Name    string
	Aliases []string
	// Referrer is sent with stream requests for hosts that check it.
	Referrer string
	// DefaultDisabled keeps a provider out of the stack until the config
	// asks for it.
	DefaultDisabled bool // off unless the config names it
	// DisableReason says why a provider is off, for the menu.
	DisableReason string
	// Adult marks a provider that serves 18+ titles only. It is used only
	// when the AdultContent setting is on.
	Adult bool
}

// registration is one provider as the registry keeps it.
type registration struct {
	meta    Meta            // as registered, Name canonical
	factory func() Provider // makes a fresh instance
}

var (
	mu sync.RWMutex // guards both maps below
	// registered holds each provider under its canonical name.
	registered = map[string]registration{}
	// lookup maps every accepted spelling of a name to its canonical form.
	lookup = map[string]string{}
)

// trimName drops the quotes and brackets a hand-edited config list leaves
// around a name.
func trimName(name string) string {
	return strings.Trim(strings.TrimSpace(name), "\"'[]")
}

// spacedKey is a name as typed, lower-cased with its spacing collapsed.
func spacedKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// compactKey also drops separators, so "Kick-Ass Anime" finds kickassanime.
func compactKey(name string) string {
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(spacedKey(name))
}

// Register adds a provider. Providers call it from init, so a bad
// registration panics at startup rather than failing a lookup later.
func Register(meta Meta, factory func() Provider) {
	meta.Name = strings.NewReplacer("-", "", " ", "").Replace(strings.ToLower(trimName(meta.Name)))
	switch {
	case meta.Name == "":
		panic("providers.Register: a provider needs a Name")
	case factory == nil:
		panic("providers.Register: no factory for " + meta.Name)
	}

	mu.Lock()
	defer mu.Unlock()
	registered[meta.Name] = registration{meta: meta, factory: factory}
	for _, alias := range append([]string{meta.Name}, meta.Aliases...) {
		addAlias(alias, meta.Name)
	}
}

// addAlias makes spelling, and its compact form, resolve to canonical. The
// caller holds the write lock.
func addAlias(spelling, canonical string) {
	alias := trimName(spelling)
	if alias == "" {
		return
	}
	lookup[spacedKey(alias)] = canonical
	if key := compactKey(alias); key != "" {
		lookup[key] = canonical
	}
}

// NormalizeName returns the canonical name for a name or alias from the user
// or the config, or "" when nothing registered goes by it.
func NormalizeName(spelling string) string {
	mu.RLock()
	defer mu.RUnlock()
	return canonicalName(spelling)
}

// canonicalName is NormalizeName for callers already holding the lock.
func canonicalName(spelling string) string {
	name := trimName(spelling)
	if name == "" {
		return ""
	}
	if hit, ok := lookup[spacedKey(name)]; ok {
		return hit
	}
	return lookup[compactKey(name)]
}

// MetaFor returns what the provider registered under name.
func MetaFor(name string) (Meta, bool) {
	mu.RLock()
	defer mu.RUnlock()
	reg, ok := registered[canonicalName(name)]
	return reg.meta, ok
}

// RegisteredNames returns every canonical name, sorted.
func RegisteredNames() []string {
	mu.RLock()
	defer mu.RUnlock()
	return slices.Sorted(maps.Keys(registered))
}

// New builds a fresh instance of the named provider.
func New(requested string) (Provider, error) {
	mu.RLock()
	reg, ok := registered[canonicalName(requested)]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", requested)
	}
	return reg.factory(), nil
}

// Referrer is the referrer the named provider's streams need, if any.
func Referrer(provider string) string {
	if meta, ok := MetaFor(provider); ok {
		return meta.Referrer
	}
	return ""
}

// SetFactoryForTest swaps a registered provider's factory and returns the
// function that puts the original back.
func SetFactoryForTest(provider string, factory func() Provider) (restore func()) {
	mu.Lock()
	defer mu.Unlock()

	name := canonicalName(provider)
	original, ok := registered[name]
	if !ok {
		panic(fmt.Sprintf("providers.SetFactoryForTest: no provider called %q", provider))
	}
	registered[name] = registration{meta: original.meta, factory: factory}
	restore = func() {
		mu.Lock()
		defer mu.Unlock()
		registered[name] = original
	}
	return restore
}
