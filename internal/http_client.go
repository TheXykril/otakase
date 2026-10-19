package internal

import (
	"net/http"
	"net/http/cookiejar"
	"time"
)

// sharedHTTPClient is used for every request the app makes itself. Providers
// are searched in parallel, so it keeps several idle connections per host.
var sharedHTTPClient = newHTTPClient()

func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       time.Minute,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	// cookiejar.New only fails on a bad PublicSuffixList, and nil is fine.
	cookies, _ := cookiejar.New(nil)
	return &http.Client{
		Jar:       cookies,
		Transport: transport,
		// Some anime hosts take a while on a cold connection.
		Timeout: 25 * time.Second,
	}
}
