package mobilecore

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const maxSubtitleBytes = 16 << 20

type subtitleDownloadRequest struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type subtitleHTTPError int

func (e subtitleHTTPError) Error() string {
	return fmt.Sprintf("subtitle server returned HTTP %d", e)
}

var errSubtitleTooLarge = errors.New("subtitle file is too large")
var errInsecureSubtitle = errors.New("subtitle URL must use HTTPS")

func subtitleTransport(c *Core) http.RoundTripper {
	if c != nil && c.transport != nil {
		return c.transport
	}
	return http.DefaultTransport
}

func readSubtitle(ctx context.Context, client *http.Client, link string, headers map[string]string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, subtitleHTTPError(resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubtitleBytes+1))
	if len(body) > maxSubtitleBytes {
		return nil, errSubtitleTooLarge
	}
	return body, err
}

// Android uses the same transport as the provider resolver to fetch sidecars.
// Only local playback relays may use plain HTTP.
func allowedSubtitleURL(u *url.URL) bool {
	if u.Hostname() == "" || u.User != nil {
		return false
	}
	host := u.Hostname()
	return u.Scheme == "https" ||
		u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}

func (c *Core) downloadSubtitle(ctx context.Context, req *subtitleDownloadRequest) string {
	if req == nil {
		return failure("subtitle_url", "The subtitle link is invalid.", false)
	}
	u, err := url.Parse(req.URL)
	if err != nil || !allowedSubtitleURL(u) {
		return failure("subtitle_url", "The subtitle link must use HTTPS.", false)
	}
	client := &http.Client{
		Transport: subtitleTransport(c),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !allowedSubtitleURL(req.URL) || (via[0].URL.Scheme == "https" && req.URL.Scheme != "https") {
				return errInsecureSubtitle
			}
			if len(via) >= 10 {
				return errors.New("too many subtitle redirects")
			}
			return nil
		},
	}
	body, err := readSubtitle(ctx, client, req.URL, req.Headers)
	if err == nil {
		return encode(response{Version: 1, Data: map[string][]byte{"body": body}})
	}
	var status subtitleHTTPError
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return failure("subtitle_timeout", "The subtitle download timed out.", true)
	case errors.Is(err, context.Canceled):
		return contextFailure(err)
	case errors.Is(err, errSubtitleTooLarge):
		return failure("subtitle_size", "The subtitle file is too large.", false)
	case errors.Is(err, errInsecureSubtitle):
		return failure("subtitle_url", "The subtitle server redirected to an insecure link.", false)
	case errors.As(err, &status):
		return failure("subtitle_http", fmt.Sprintf("Subtitle download failed (HTTP %d).", status), false)
	case errors.As(err, &dns):
		return failure("subtitle_dns", "The subtitle server address could not be found (DNS).", true)
	case errors.As(err, &certificate):
		return failure("subtitle_tls", "The subtitle server certificate could not be verified.", false)
	default:
		return failure("subtitle_network", "The subtitle server connection failed.", true)
	}
}
