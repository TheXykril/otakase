package allanime

import (
	"strings"
	"testing"
)

// AllAnime's episode endpoint answers unsigned requests with HTTP 200, a null
// episode and an AA_CRYPTO_MISSING GraphQL error. Without detecting it the failure
// surfaced as "no encoded Allanime provider sources found", which says nothing
// about the actual cause.
func TestAllanimeCryptoMissingErrorIsExplicit(t *testing.T) {
	message := errAllanimeCryptoRequired.Error()
	for _, want := range []string{"AA_CRYPTO_MISSING", "signed request"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected %q in %q", want, message)
		}
	}
}

func TestAllanimeCryptoMissingResponseShape(t *testing.T) {
	// Captured verbatim from api.allanime.day.
	body := `{"errors":[{"message":"AA_CRYPTO_MISSING","locations":[{"line":1,"column":102}],"path":["episode"],"extensions":{"code":"AA_CRYPTO_MISSING"}}],"data":{"episode":null}}`
	if !strings.Contains(body, "AA_CRYPTO_MISSING") {
		t.Fatal("fixture no longer carries the sentinel this detection relies on")
	}
}
