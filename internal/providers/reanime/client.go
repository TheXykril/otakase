package reanime

import "net/http"

// NewWithHTTPClient creates an isolated provider using the supplied client.
// A host uses its transport to attach cancellation to every provider request.
// The default registry factory and desktop behaviour are unchanged.
func NewWithHTTPClient(httpClient *http.Client) *Provider {
	c := newClient()
	if httpClient != nil {
		c.http = httpClient
	}
	return &Provider{client: c}
}
