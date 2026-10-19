// Package anidb implements the anidb.app streaming provider.
//
// ani-cli moved to this host for its v5 provider after AllAnime's episode API
// started rejecting unsigned requests. The endpoint shapes below follow that
// implementation.
//
// anidb.app was serving a site-wide maintenance page when this provider was
// written, so the request/response handling here is guarded rather than verified
// against live traffic: isMaintenancePage turns that page into an explicit,
// readable error instead of a confusing parse failure.
package anidb

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/thexykril/otakase/internal/providerhost"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

var baseURL = "https://anidb.app"

// errMaintenance reports that anidb.app is serving its maintenance page.
var errMaintenance = fmt.Errorf("anidb.app is under maintenance")

// isMaintenancePage detects the site-wide maintenance placeholder, which is
// returned with HTTP 200 for every path including the JSON API routes.
func isMaintenancePage(body string) bool {
	if len(body) > 4096 {
		body = body[:4096]
	}
	lower := strings.ToLower(body)
	return strings.Contains(lower, "<title>under maintenance</title>") ||
		strings.Contains(lower, ">under maintenance<")
}

// isCloudflareChallenge detects an interstitial bot check, which no amount of
// retrying will clear without a real browser.
func isCloudflareChallenge(body string) bool {
	if len(body) > 4096 {
		body = body[:4096]
	}
	return strings.Contains(body, "Just a moment")
}

func fetchString(rawURL, referer string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/json;q=0.9,*/*;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := providerhost.HTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	body := string(raw)

	if isMaintenancePage(body) {
		return "", errMaintenance
	}
	if isCloudflareChallenge(body) {
		return "", fmt.Errorf("anidb.app returned a Cloudflare challenge")
	}
	if !providerhost.HTTPStatusOK(resp.StatusCode) {
		return "", providerhost.HTTPStatusError("anidb request", resp.StatusCode, raw)
	}
	return body, nil
}
