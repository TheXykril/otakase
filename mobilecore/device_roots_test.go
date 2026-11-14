package mobilecore

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/x509roots/fallback/bundle"
)

func TestDeviceRootsReadFolderAndTrustSite(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	get := func(pool *x509.CertPool) error {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = nil
		trustRoots(transport, pool)
		t.Cleanup(transport.CloseIdleConnections)
		resp, err := (&http.Client{Transport: transport}).Get(server.URL)
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	dir := t.TempDir()
	if err := get(rootPool([]string{dir}, false)); err == nil {
		t.Fatal("a site whose root is in no folder must not verify")
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(dir, "4a123456.0"), cert, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := get(rootPool([]string{dir, filepath.Join(dir, "missing")}, false)); err != nil {
		t.Fatalf("root from the updatable folder: %v", err)
	}
}

// Let's Encrypt's YE chains end at ISRG Root X2, which older Android stores
// and Go's legacy Android folder lack.
func TestDeviceRootsCarryISRGRootX2(t *testing.T) {
	var x2 *x509.Certificate
	for root := range bundle.Roots() {
		cert, err := x509.ParseCertificate(root.Certificate)
		if err == nil && cert.Subject.CommonName == "ISRG Root X2" {
			x2 = cert
		}
	}
	if x2 == nil {
		t.Fatal("bundled roots have no ISRG Root X2")
	}
	opts := x509.VerifyOptions{Roots: rootPool(nil, true), CurrentTime: x2.NotBefore.AddDate(1, 0, 0)}
	if _, err := x2.Verify(opts); err != nil {
		t.Fatalf("ISRG Root X2 not trusted: %v", err)
	}
	opts.Roots = x509.NewCertPool()
	if _, err := x2.Verify(opts); err == nil {
		t.Fatal("an empty pool must not trust it")
	}
}

func TestTrustRootsKeepsExplicitRoots(t *testing.T) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	explicit := x509.NewCertPool()
	trustRoots(transport, explicit)
	trustRoots(transport, rootPool(nil, false))
	if transport.TLSClientConfig.RootCAs != explicit {
		t.Fatal("roots set first must stay")
	}
}
