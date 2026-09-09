package allanime

import (
	"strings"
	"testing"
)

// AllAnime's episode endpoint refuses unsigned requests with HTTP 200, a null
// episode and a GraphQL error. Without detecting that, the failure surfaced as
// "no encoded Allanime provider sources found", which says nothing about the
// actual cause.
//
// The refusal is not one fixed string: the same query answered AA_CRYPTO_MISSING
// and, later the same day, NEED_CAPTCHA. Both are captured verbatim below.
func TestAllanimeGateResponsesAreDetected(t *testing.T) {
	refusals := map[string]string{
		"missing signature": `{"errors":[{"message":"AA_CRYPTO_MISSING","locations":[{"line":1,"column":102}],"path":["episode"],"extensions":{"code":"AA_CRYPTO_MISSING"}}],"data":{"episode":null}}`,
		"captcha demanded":  `{"errors":[{"message":"NEED_CAPTCHA","locations":[{"line":1,"column":102}],"path":["episode"],"extensions":{"code":"INTERNAL_SERVER_ERROR"}}],"data":{"episode":null}}`,
	}
	for name, body := range refusals {
		if !isAllanimeGateResponse([]byte(body)) {
			t.Errorf("%s: expected the refusal to be recognised", name)
		}
	}
}

// A real answer must not be mistaken for a refusal, or a working provider would
// report itself broken.
func TestAllanimeGateDetectionIgnoresRealAnswers(t *testing.T) {
	answers := []string{
		`{"data":{"episode":{"episodeString":"10","sourceUrls":[{"sourceUrl":"--elhbmltZQ","sourceName":"Yt-mp4","priority":9}]}}}`,
		`{"data":{"episode":{"episodeString":"10","sourceUrls":[]}}}`,
		`{"errors":[{"message":"PersistedQueryNotFound","extensions":{"code":"PERSISTED_QUERY_NOT_FOUND"}}]}`,
	}
	for _, body := range answers {
		if isAllanimeGateResponse([]byte(body)) {
			t.Errorf("expected %.60s... not to read as a refusal", body)
		}
	}
}

// The message has to name both possible demands, since which one comes back
// varies, and say plainly that the provider cannot be used.
func TestAllanimeGatedErrorExplainsItself(t *testing.T) {
	message := errAllanimeGated.Error()
	for _, want := range []string{"signed request", "CAPTCHA", "unusable"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected %q in %q", want, message)
		}
	}
}
