package providers

import (
	"slices"
	"strings"
	"testing"
)

type fakeProvider struct{ name string }

func (f fakeProvider) Name() string { return f.name }
func (f fakeProvider) SearchAnime(string, string) ([]SelectionOption, error) {
	return nil, nil
}
func (f fakeProvider) EpisodesList(string, string) ([]string, error) { return nil, nil }
func (f fakeProvider) GetEpisodeURL(PlaybackConfig, string, int) ([]string, error) {
	return nil, nil
}

func init() {
	Register(Meta{
		Name:     ` "Test-Host" `,
		Aliases:  []string{"Registry Test", "rt_alias"},
		Referrer: "https://test.example/",
	}, func() Provider { return fakeProvider{name: "testhost"} })
}

func TestRegisterNormalizesTheName(t *testing.T) {
	if !slices.Contains(RegisteredNames(), "testhost") {
		t.Fatalf("registered as %v", RegisteredNames())
	}
	if !slices.IsSorted(RegisteredNames()) {
		t.Fatal("RegisteredNames is not sorted")
	}
}

func TestNormalizeNameAcceptsEverySpelling(t *testing.T) {
	for _, spelling := range []string{
		"testhost", "TestHost", "test-host", "Test Host", " [\"test_host\"] ",
		"registry test", "Registry   Test", "registry-test", "registrytest",
		"rt_alias", "RT-Alias", "rtalias",
	} {
		if got := NormalizeName(spelling); got != "testhost" {
			t.Errorf("NormalizeName(%q) = %q, want testhost", spelling, got)
		}
	}
	for _, unknown := range []string{"", "  ", "[]", "nothost"} {
		if got := NormalizeName(unknown); got != "" {
			t.Errorf("NormalizeName(%q) = %q, want empty", unknown, got)
		}
	}
}

func TestMetaForAndReferrer(t *testing.T) {
	meta, ok := MetaFor("Registry Test")
	if !ok || meta.Name != "testhost" {
		t.Fatalf("MetaFor = (%+v, %v)", meta, ok)
	}
	if got := Referrer("test-host"); got != "https://test.example/" {
		t.Fatalf("Referrer = %q", got)
	}
	if _, ok := MetaFor("nothost"); ok {
		t.Fatal("MetaFor found an unregistered name")
	}
	if got := Referrer("nothost"); got != "" {
		t.Fatalf("Referrer of an unknown provider = %q", got)
	}
}

func TestNewAndSetFactoryForTest(t *testing.T) {
	provider, err := New("rt alias")
	if err != nil || provider.Name() != "testhost" {
		t.Fatalf("New = (%v, %v)", provider, err)
	}
	if _, err := New("nothost"); err == nil || !strings.Contains(err.Error(), `"nothost"`) {
		t.Fatalf("New of an unknown provider: %v", err)
	}

	restore := SetFactoryForTest("Test Host", func() Provider { return fakeProvider{name: "swapped"} })
	if provider, _ := New("testhost"); provider.Name() != "swapped" {
		t.Fatalf("factory not swapped, got %q", provider.Name())
	}
	if meta, _ := MetaFor("testhost"); meta.Referrer != "https://test.example/" {
		t.Fatal("swapping the factory lost the registration")
	}
	restore()
	if provider, _ := New("testhost"); provider.Name() != "testhost" {
		t.Fatalf("factory not restored, got %q", provider.Name())
	}
}

func TestRegisterRejectsAnEmptyName(t *testing.T) {
	for _, name := range []string{"", ` "" `, "--"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%q) did not panic", name)
				}
			}()
			Register(Meta{Name: name}, func() Provider { return fakeProvider{} })
		}()
	}
}

func TestTranslationTypes(t *testing.T) {
	for in, want := range map[string]string{"dub": "dub", " DUB ": "dub", "sub": "sub", "": "sub", "raw": "sub"} {
		if got := NormalizeTranslationType(in); got != want {
			t.Errorf("NormalizeTranslationType(%q) = %q", in, got)
		}
	}
	if AlternateTranslationType("dub") != "sub" || AlternateTranslationType("anything") != "dub" {
		t.Error("AlternateTranslationType does not swap")
	}
}
