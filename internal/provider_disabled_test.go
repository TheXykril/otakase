package internal

import (
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

// A provider that stops working is disabled before it is removed, so a stack
// built from the defaults never offers one that cannot play. anidb is the
// standing example: its host is serving a maintenance page.
func TestADegradedProviderIsDisabledWithAReason(t *testing.T) {
	if ProviderEnabled("anidb") != false {
		t.Fatal("expected anidb to be disabled while anidb.app is under maintenance")
	}
	if reason := ProviderDisabledReason("anidb"); reason == "" {
		t.Fatal("a disabled provider must say why, or the menu cannot explain itself")
	}
	if ProviderEnabled("anipub") != true {
		t.Fatal("expected anipub to stay enabled")
	}
	if ProviderEnabled("anineko") != true {
		t.Fatal("expected anineko to stay enabled")
	}
}

// allanime, animepahe and senshi were removed in 1.4.0. Nothing should claim
// they exist -- a name that resolves to no provider must not read as enabled.
func TestRemovedProvidersAreGone(t *testing.T) {
	for _, name := range []string{"allanime", "animepahe", "senshi"} {
		if ProviderEnabled(name) {
			t.Errorf("%s was removed but still reports as enabled", name)
		}
		for _, registered := range providers.RegisteredNames() {
			if registered == name {
				t.Errorf("%s was removed but is still registered", name)
			}
		}
	}
}

func TestConfiguredProviderNamesFiltersDisabledProviders(t *testing.T) {
	// Configs that name only disabled providers fall back to the head of the
	// stack. Which provider that is changes as the stack is reordered, and
	// that reordering should not fail a test about fallback.
	stackHead := defaultEnabledProviderStack()[0]

	cases := []struct {
		name string
		cfg  *CurdConfig
		want []string
	}{
		{name: "empty", cfg: &CurdConfig{}, want: []string{"anikoto", "kickassanime", "anipub", "anineko", "nyaa"}},
		{name: "json list", cfg: &CurdConfig{Provider: `["allanime","animepahe"]`}, want: []string{stackHead}},
		{name: "animepahe only", cfg: &CurdConfig{Provider: `["animepahe"]`}, want: []string{stackHead}},
		{name: "allanime only", cfg: &CurdConfig{Provider: `["allanime"]`}, want: []string{stackHead}},
		{name: "legacy alias", cfg: &CurdConfig{Provider: "stacked"}, want: []string{"anikoto", "kickassanime", "anipub", "anineko", "nyaa"}},
	}

	for _, tc := range cases {
		got := ConfiguredProviderNames(tc.cfg)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		}
	}
}

func TestConfiguredProviderNamesHonorsEnabledProvidersWhenOverridden(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	cfg := &CurdConfig{Provider: `["anidb","nyaa"]`}
	got := ConfiguredProviderNames(cfg)
	if len(got) != 2 || got[0] != "anidb" || got[1] != "nyaa" {
		t.Fatalf("got %v, want [anidb nyaa]", got)
	}
}

func TestProviderByNameRejectsDisabledProvider(t *testing.T) {
	for _, name := range []string{"anidb"} {
		if _, err := ProviderByName(name); err == nil {
			t.Fatalf("expected disabled provider error for %s", name)
		}
	}
}

func TestProviderByNameAllowsDisabledProviderWhenOverridden(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	for _, name := range []string{"anidb"} {
		provider, err := ProviderByName(name)
		if err != nil {
			t.Fatalf("expected %s provider: %v", name, err)
		}
		if provider.Name() != name {
			t.Fatalf("got provider %q", provider.Name())
		}
	}
}
