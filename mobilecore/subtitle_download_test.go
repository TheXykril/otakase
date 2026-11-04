package mobilecore

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func subtitleResponse(t *testing.T, c *Core, link string) ([]byte, *apiError) {
	t.Helper()
	input, err := json.Marshal(request{
		Version: 1, Operation: "subtitle_download",
		Subtitle: &subtitleDownloadRequest{URL: link, Headers: map[string]string{
			"Referer":    "https://provider.example/watch/",
			"User-Agent": "Provider player",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Data  map[string][]byte `json:"data"`
		Error *apiError         `json:"error"`
	}
	if err := json.Unmarshal([]byte(c.Result(c.Start(string(input)))), &result); err != nil {
		t.Fatal(err)
	}
	return result.Data["body"], result.Error
}

func TestSubtitleDownloadUsesProviderTransportAndPreservesBytes(t *testing.T) {
	c := NewCore()
	t.Cleanup(c.Close)
	want := []byte("\xef\xbb\xbfWEBVTT\r\n\r\n00:00.000 --> 00:02.000\r\n日本語\r\n")
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "subtitles.example" ||
			r.Header.Get("Referer") != "https://provider.example/watch/" ||
			r.Header.Get("User-Agent") != "Provider player" {
			t.Errorf("unexpected subtitle request: %s %v", r.URL, r.Header)
		}
		return reply(r, http.StatusOK, want), nil
	})
	body, err := subtitleResponse(t, c, "https://subtitles.example/track")
	if err != nil || !bytes.Equal(body, want) {
		t.Fatalf("body = %q, error = %+v", body, err)
	}
}

func TestSubtitleDownloadClassifiesFailures(t *testing.T) {
	for _, tc := range []struct {
		code string
		err  error
	}{
		{"subtitle_dns", &net.DNSError{Err: "lookup failed", Name: "subtitles.example"}},
		{"subtitle_tls", &tls.CertificateVerificationError{Err: errors.New("untrusted")}},
		{"subtitle_tls_chain", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}},
		{"subtitle_tls_hostname", &tls.CertificateVerificationError{Err: x509.HostnameError{}}},
		{"subtitle_tls_date", &tls.CertificateVerificationError{Err: x509.CertificateInvalidError{Reason: x509.Expired}}},
		{"subtitle_timeout", context.DeadlineExceeded},
		{"subtitle_network", errors.New("connection reset")},
		{"subtitle_http", nil},
	} {
		t.Run(tc.code, func(t *testing.T) {
			c := NewCore()
			t.Cleanup(c.Close)
			c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return reply(r, http.StatusForbidden, nil), nil
			})
			_, got := subtitleResponse(t, c, "https://subtitles.example/track")
			if got == nil || got.Code != tc.code {
				t.Fatalf("error = %+v, want %s", got, tc.code)
			}
		})
	}
}

func TestSubtitleDownloadRejectsRemoteCleartextAndRedirects(t *testing.T) {
	c := NewCore()
	t.Cleanup(c.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://subtitles.example/track", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	for _, link := range []string{"http://subtitles.example/track", server.URL} {
		_, got := subtitleResponse(t, c, link)
		if got == nil || got.Code != "subtitle_url" {
			t.Fatalf("error = %+v, want subtitle_url", got)
		}
	}
}
