// Package anizone resolves streams from anizone.to.
//
// The site is a Laravel/Livewire application, so every page is server
// rendered: the search results and the player's stream URL both arrive in the
// first response, embedded as JSON inside HTML attributes. Nothing here needs
// to execute JavaScript, which is why this provider is a fetch and two regular
// expressions rather than a browser.
package anizone

import (
	"fmt"
	"io"
	"net/http"

	"github.com/thexykril/otakase/internal/providerhost"
)

const (
	baseURL = "https://anizone.to"
	referer = baseURL + "/"
	// A desktop browser string. The site sits behind Cloudflare, which serves
	// the page to an ordinary client but not to something that announces
	// itself as a script.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

// fetch returns the body of a GET, or the reason it could not.
func fetch(rawURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("anizone: bad url %q: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := providerhost.HTTPClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("anizone: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("anizone: could not read the response: %w", err)
	}
	if !providerhost.HTTPStatusOK(resp.StatusCode) {
		return "", providerhost.HTTPStatusError("anizone request", resp.StatusCode, body)
	}
	return string(body), nil
}
