package anipub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/thexykril/otakase/internal/providerhost"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Variables so tests can point them at local servers.
var (
	baseURL         = "https://anipub.xyz"
	megaplayBaseURL = "https://megaplay.buzz"
)

// get returns the body of a GET sent with the given Referer and, when set,
// Accept header.
func get(rawURL, referer, accept string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
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
		return nil, providerhost.HTTPStatusError("anipub request", resp.StatusCode, body)
	}
	return body, nil
}

func getJSON(rawURL, referer string, out any) error {
	body, err := get(rawURL, referer, "application/json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse anipub response: %w", err)
	}
	return nil
}
