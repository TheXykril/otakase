package mobilecore

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const androidConscryptRoots = "/apex/com.android.conscrypt/cacerts"

var androidSubtitleTransport = sync.OnceValue(func() http.RoundTripper {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		return withSubtitleRoots(base, androidConscryptRoots)
	}
	return http.DefaultTransport
})

func subtitleDownloadTransport(c *Core) http.RoundTripper {
	base := subtitleTransport(c)
	if runtime.GOOS == "android" && base == http.DefaultTransport {
		return androidSubtitleTransport()
	}
	return base
}

// Go 1.26 looks in Android's legacy certificate directory. On Android 14
// and later Conscrypt can use its updatable store instead.
func withSubtitleRoots(base *http.Transport, directory string) *http.Transport {
	if base.TLSClientConfig != nil && base.TLSClientConfig.RootCAs != nil {
		return base
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return base
	}
	roots := x509.NewCertPool()
	loaded := false
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err == nil && roots.AppendCertsFromPEM(data) {
			loaded = true
		}
	}
	if !loaded {
		return base
	}

	transport := base.Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.RootCAs = roots
	return transport
}
