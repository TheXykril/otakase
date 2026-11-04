package mobilecore

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSubtitleTransportUsesUpdatedRootsAndStillVerifiesHostname(t *testing.T) {
	const subtitle = "WEBVTT\n\n00:00.000 --> 00:01.000\nExample\n"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(subtitle))
	}))
	t.Cleanup(server.Close)

	base := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(base.CloseIdleConnections)
	dir := t.TempDir()
	c := NewCore()
	t.Cleanup(c.Close)
	c.transport = withSubtitleRoots(base, dir)
	if c.transport != base {
		t.Fatal("empty certificate directory should leave the transport intact")
	}
	if _, err := subtitleResponse(t, c, server.URL); err == nil || err.Code != "subtitle_tls_chain" {
		t.Fatalf("untrusted certificate error = %+v", err)
	}

	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(dir, "4a123456.0"), cert, 0600); err != nil {
		t.Fatal(err)
	}
	updated := withSubtitleRoots(base, dir)
	t.Cleanup(updated.CloseIdleConnections)
	if updated == base || updated.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("updated roots should use a separate transport with verification enabled")
	}
	c.transport = updated
	if body, err := subtitleResponse(t, c, server.URL); err != nil || string(body) != subtitle {
		t.Fatalf("with updated roots: body = %q, error = %+v", body, err)
	}

	wrongHost := updated.Clone()
	t.Cleanup(wrongHost.CloseIdleConnections)
	wrongHost.TLSClientConfig.ServerName = "wrong.example"
	c.transport = wrongHost
	if _, err := subtitleResponse(t, c, server.URL); err == nil || err.Code != "subtitle_tls_hostname" {
		t.Fatalf("wrong hostname error = %+v", err)
	}

	explicit := base.Clone()
	explicit.TLSClientConfig = &tls.Config{RootCAs: x509.NewCertPool()}
	if withSubtitleRoots(explicit, dir) != explicit {
		t.Fatal("explicitly configured trust roots should remain intact")
	}
}
