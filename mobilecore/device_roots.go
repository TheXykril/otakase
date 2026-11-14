package mobilecore

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"golang.org/x/crypto/x509roots/fallback/bundle"
)

// Go reads only Android's legacy certificate folder, which stops getting new
// roots once the device ships. Android 14 and later keep the current ones in
// Conscrypt's updatable folder, and older devices have no newer roots at all.
// Let's Encrypt's current chains (YE1/YE2 under Root YE) need ISRG Root X2,
// which neither the legacy folder nor an older device has, so vidsrc, 2Embed
// and OpenSubtitles all failed with an untrusted certificate.
var androidRootDirs = []string{
	androidConscryptRoots,
	"/data/misc/user/0/cacerts-added",
}

// deviceRoots is the device's own roots, the updatable ones, and the Mozilla
// roots shipped with the app, so a site with a root newer than the device
// still verifies.
var deviceRoots = sync.OnceValue(func() *x509.CertPool {
	return rootPool(androidRootDirs, true)
})

func rootPool(dirs []string, bundled bool) *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(dir, entry.Name())); err == nil {
				pool.AppendCertsFromPEM(data)
			}
		}
	}
	if bundled {
		for root := range bundle.Roots() {
			cert, err := x509.ParseCertificate(root.Certificate)
			if err != nil {
				continue
			}
			if root.Constraint != nil {
				pool.AddCertWithConstraint(cert, root.Constraint)
			} else {
				pool.AddCert(cert)
			}
		}
	}
	return pool
}

// useDeviceRoots points the default transport, which the sources' clients
// use, at deviceRoots on Android. Other systems keep their own verifier.
func useDeviceRoots() {
	if runtime.GOOS != "android" {
		return
	}
	trustRoots(http.DefaultTransport, deviceRoots())
}

func trustRoots(rt http.RoundTripper, pool *x509.CertPool) {
	t, ok := rt.(*http.Transport)
	if !ok {
		return
	}
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{}
	}
	if t.TLSClientConfig.RootCAs == nil {
		t.TLSClientConfig.RootCAs = pool
	}
}
