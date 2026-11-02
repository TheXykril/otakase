package internal

import (
	"slices"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

// withAllProvidersEnabledForTest enables every registered provider for the
// test, whatever the config or the providers' own registrations say.
func withAllProvidersEnabledForTest(t *testing.T) {
	t.Helper()
	previous := disabledProviderReasonForTest
	disabledProviderReasonForTest = func(string) string { return "" }
	t.Cleanup(func() { disabledProviderReasonForTest = previous })
}

// withDisabledProvidersForTest disables exactly the named providers.
func withDisabledProvidersForTest(t *testing.T, names ...string) {
	t.Helper()
	previous := disabledProviderReasonForTest
	disabledProviderReasonForTest = func(name string) string {
		if slices.Contains(names, name) {
			return "off for this test"
		}
		return ""
	}
	t.Cleanup(func() { disabledProviderReasonForTest = previous })
}

// allanime, animepahe and senshi were removed in 2.0.0, and anidb, anineko and
// anipub when their hosts shut down. Nothing should claim they exist -- a name
// that resolves to no provider must not read as enabled.
func TestRemovedProvidersAreGone(t *testing.T) {
	for _, name := range []string{"allanime", "animepahe", "senshi", "anidb", "anineko", "anipub"} {
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

func TestProviderByNameFollowsTheDisabledList(t *testing.T) {
	withGlobalConfig(t, &Config{})
	if _, err := ProviderByName("sukebei"); err == nil {
		t.Fatal("a disabled sukebei was handed out")
	}

	withAllProvidersEnabledForTest(t)
	p, err := ProviderByName("Sukebei")
	if err != nil {
		t.Fatalf("sukebei once enabled: %v", err)
	}
	if p.Name() != "sukebei" {
		t.Fatalf("got provider %q", p.Name())
	}
	if _, err := ProviderByName("nonsense"); err == nil {
		t.Fatal("an unknown name produced a provider")
	}
}

func TestAdultOnlyProvidersNeedAdultContent(t *testing.T) {
	withGlobalConfig(t, &Config{})
	if ProviderEnabled("sukebei") {
		t.Fatal("sukebei is enabled with AdultContent off")
	}
	withGlobalConfig(t, &Config{AdultContent: true})
	if !ProviderEnabled("sukebei") {
		t.Fatalf("sukebei is off with AdultContent on: %s", ProviderDisabledReason("sukebei"))
	}
}

func TestConfigDisabledProvidersAreDisabled(t *testing.T) {
	withGlobalConfig(t, &Config{DisabledProviders: "anizone, Kick-Ass Anime"})

	for _, name := range []string{"anizone", "kickassanime"} {
		if ProviderEnabled(name) {
			t.Errorf("%s is listed in DisabledProviders but reads as enabled", name)
		} else if ProviderDisabledReason(name) == "" {
			t.Errorf("%s has no reason", name)
		}
	}
	if !ProviderEnabled("anikoto") {
		t.Error("anikoto is not listed but reads as disabled")
	}
}

func TestDefaultStackFollowsThePreferredOrderAndSkipsDisabled(t *testing.T) {
	withDisabledProvidersForTest(t, "anikoto", "megavid", "animex", "nyaa")

	got := defaultEnabledProviderStack()
	want := []string{"kickassanime", "anizone", "reanime", "sukebei"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if first := firstEnabledProviderName(); first != "kickassanime" {
		t.Fatalf("first enabled provider = %q, want kickassanime", first)
	}
}

func TestEnsureEnabledProviderNamesFallsBackToOneProvider(t *testing.T) {
	withDisabledProvidersForTest(t, "anikoto", "megavid", "animex", "nyaa")

	if got := ensureEnabledProviderNames([]string{"nyaa", "anizone", "anizone"}); !slices.Equal(got, []string{"anizone"}) {
		t.Errorf("enabled names: got %v", got)
	}
	if got := ensureEnabledProviderNames([]string{"nyaa", "nonsense"}); !slices.Equal(got, []string{"kickassanime"}) {
		t.Errorf("nothing enabled: got %v, want the head of the stack", got)
	}

	disabledProviderReasonForTest = func(string) string { return "everything is off" }
	got := ensureEnabledProviderNames(nil)
	if len(got) != 1 || got[0] != providers.RegisteredNames()[0] {
		t.Errorf("every provider disabled: got %v, want the first registered", got)
	}
}
