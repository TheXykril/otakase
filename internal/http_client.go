package internal

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

var sharedHTTPClient *http.Client

func SetCookiesForAnimepahe(u *url.URL, cookies []*http.Cookie) {
	if sharedHTTPClient != nil && sharedHTTPClient.Jar != nil {
		sharedHTTPClient.Jar.SetCookies(u, cookies)
	}
}

func httpStatusOK(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
}

func httpStatusError(context string, statusCode int, body []byte) error {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 300 {
		snippet = snippet[:300] + "..."
	}
	if snippet != "" {
		return fmt.Errorf("%s failed with status %d: %s", context, statusCode, snippet)
	}
	return fmt.Errorf("%s failed with status %d", context, statusCode)
}

func init() {
	jar, _ := cookiejar.New(nil)
	sharedHTTPClient = &http.Client{
		Transport: &http.Transport{
			// Providers are now searched concurrently, so the pool has to hold a
			// live connection per host instead of serialising them.
			MaxIdleConns:          50,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		// Anime hosts are frequently slow on a cold connection; 15s was tight
		// enough that a single stall aborted an otherwise healthy search.
		Timeout: 25 * time.Second,
		Jar:     jar,
	}
}
