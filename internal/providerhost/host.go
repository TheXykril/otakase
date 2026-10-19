// Package providerhost is what providers may use of the app. The app fills
// the hooks in from its init, so providers never import it.
package providerhost

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// PromptOption is one entry of a menu a provider asks the user to pick from.
type PromptOption struct {
	Key, Label string // what is returned, and what is shown
}

// Hooks into the app, set by its init before any provider runs.
var (
	HTTPClient   func() *http.Client
	Log          func(string)
	Out          func(string)
	PromptSelect func(options []PromptOption) (PromptOption, error)
	// CurrentSubStyle and PersistSubStylePreference read and save the
	// SubStyle setting, for hosts that ask how subtitles should be delivered.
	CurrentSubStyle           func() string
	PersistSubStylePreference func(value string) error
)

// ErrRateLimited reports that a provider is throttling us. It is distinct from
// "this host does not have that show": a rate limit means try again shortly,
// and reporting it as an empty result set tells the user their anime does not
// exist when it does.
var ErrRateLimited = errors.New("rate limited by the provider")

// IsRateLimitedBody reports whether a response body is a throttling message.
// Some providers answer HTTP 200 with an error object rather than sending 429,
// so the status code alone is not enough.
func IsRateLimitedBody(body []byte) bool {
	if len(body) > 512 {
		body = body[:512]
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "ratelimit") ||
		strings.Contains(lower, "slow down")
}

// HTTPStatusOK reports a 2xx status.
func HTTPStatusOK(code int) bool {
	return code/100 == 2
}

// HTTPStatusError describes a failed request, with the start of the body when
// there is one: hosts often say in it why they refused.
func HTTPStatusError(what string, code int, body []byte) error {
	const maxBody = 300
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return fmt.Errorf("%s failed with status %d", what, code)
	}
	if len(detail) > maxBody {
		detail = detail[:maxBody] + "..."
	}
	return fmt.Errorf("%s failed with status %d: %s", what, code, detail)
}
