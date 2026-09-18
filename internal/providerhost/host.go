package providerhost

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// PromptOption is a minimal menu item for provider-driven prompts.
type PromptOption struct {
	Key   string
	Label string
}

// Host hooks are wired from the main application package during init.
var (
	HTTPClient                func() *http.Client
	Log                       func(string)
	Out                       func(string)
	PromptSelect              func(options []PromptOption) (PromptOption, error)
	CurrentSubStyle           func() string
	PersistSubStylePreference func(style string) error
	StoragePath               func() string
	AnimeNameLanguage         func() string
	SetCookiesForAnimepahe    func(u *url.URL, cookies []*http.Cookie)
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

func HTTPStatusOK(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
}

func HTTPStatusError(context string, statusCode int, body []byte) error {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 300 {
		snippet = snippet[:300] + "..."
	}
	if snippet != "" {
		return fmt.Errorf("%s failed with status %d: %s", context, statusCode, snippet)
	}
	return fmt.Errorf("%s failed with status %d", context, statusCode)
}
