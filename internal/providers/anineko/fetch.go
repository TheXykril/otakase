package anineko

import (
	"io"
	"net/http"

	"github.com/thexykril/otakase/internal/providerhost"
)

const (
	siteURL   = "https://anineko.to"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// baseURL is a variable so tests can point it at a local server.
var baseURL = siteURL

// get returns the body of a GET. The embeds and their CDNs check the
// Referer, so every request names the page it came from.
func get(rawURL, referer string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := providerhost.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if !providerhost.HTTPStatusOK(resp.StatusCode) {
		return nil, providerhost.HTTPStatusError("anineko request", resp.StatusCode, body)
	}
	return body, nil
}
