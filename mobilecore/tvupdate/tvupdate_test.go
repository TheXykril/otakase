package tvupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type fixture struct {
	server   *httptest.Server
	key      ed25519.PublicKey
	files    map[string][]byte
	requests map[string]int
}

func newFixture(t *testing.T, shell string, code int, bundleFiles map[string]string, tamper func(m *manifest)) *fixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{key: pub, files: map[string][]byte{}, requests: map[string]int{}}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests[r.URL.Path]++
		b, ok := f.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(f.server.Close)
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	for name, body := range bundleFiles {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	sum := sha256.Sum256(zipped.Bytes())
	f.files["/brain.zip"] = zipped.Bytes()
	m := manifest{Version: "26.8.0", Code: code, Bundles: map[string]bundle{
		"6.5": {URL: f.server.URL + "/brain.zip", SHA256: hex.EncodeToString(sum[:]), Size: int64(zipped.Len()), Shell: shell},
	}}
	if tamper != nil {
		tamper(&m)
	}
	body, _ := json.Marshal(m)
	f.files["/m.json"] = body
	f.files["/m.json.sig"] = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body)))
	return f
}

func (f *fixture) run(t *testing.T, req Request) (Result, error) {
	t.Helper()
	req.ManifestURL = f.server.URL + "/m.json"
	return Run(context.Background(), f.server.Client(), req, f.key)
}

func goodBundle(shell string) map[string]string {
	return map[string]string{
		"shell-id":                         shell + "\n",
		"libapp.so":                        "aot",
		"libotakasecore.so":                "core",
		"flutter_assets/AssetManifest.bin": "assets",
	}
}

func TestInstallsNewerBundleAndKeepsRunningBrain(t *testing.T) {
	f := newFixture(t, "shell-a", 26008000, goodBundle("shell-a"), nil)
	data := t.TempDir()
	os.MkdirAll(filepath.Join(data, "brain", "26007000"), 0o700)
	os.MkdirAll(filepath.Join(data, "brain", "26006000"), 0o700)
	res, err := f.run(t, Request{DataDir: data, Group: "6.5", Shell: "shell-a", Current: 26007000, Running: "26007000"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "installed" || res.Code != 26008000 {
		t.Fatalf("result = %+v", res)
	}
	active, _ := os.ReadFile(filepath.Join(data, "brain", "active"))
	if string(active) != "26008000" {
		t.Fatalf("active = %q", active)
	}
	if b, _ := os.ReadFile(filepath.Join(data, "brain", "26008000", "flutter_assets", "AssetManifest.bin")); string(b) != "assets" {
		t.Fatalf("asset = %q", b)
	}
	if _, err := os.Stat(filepath.Join(data, "brain", "26007000")); err != nil {
		t.Fatal("running brain was removed")
	}
	if _, err := os.Stat(filepath.Join(data, "brain", "26006000")); !os.IsNotExist(err) {
		t.Fatal("old brain was kept")
	}
	// A second check reuses the unpacked bundle.
	if _, err := f.run(t, Request{DataDir: data, Group: "6.5", Shell: "shell-a", Current: 26007000}); err != nil {
		t.Fatal(err)
	}
	if f.requests["/brain.zip"] != 1 {
		t.Fatalf("bundle downloaded %d times", f.requests["/brain.zip"])
	}
}

func TestCurrentReinstallAndFailedReleases(t *testing.T) {
	f := newFixture(t, "shell-a", 26008000, goodBundle("shell-a"), nil)
	data := t.TempDir()
	for _, tc := range []struct {
		req  Request
		want string
	}{
		{Request{DataDir: data, Group: "6.5", Shell: "shell-a", Current: 26008000}, "current"},
		{Request{DataDir: data, Group: "10.0", Shell: "shell-a", Current: 1}, "current"},
		{Request{DataDir: data, Group: "6.5", Shell: "shell-b", Current: 1}, "reinstall"},
	} {
		res, err := f.run(t, tc.req)
		if err != nil || res.Status != tc.want {
			t.Fatalf("%+v: %+v, %v", tc.req, res, err)
		}
	}
	os.MkdirAll(filepath.Join(data, "brain"), 0o700)
	os.WriteFile(filepath.Join(data, "brain", "failed"), []byte("26008000"), 0o600)
	res, err := f.run(t, Request{DataDir: data, Group: "6.5", Shell: "shell-a", Current: 1})
	if err != nil || res.Status != "current" {
		t.Fatalf("failed release offered again: %+v, %v", res, err)
	}
	if f.requests["/brain.zip"] != 0 {
		t.Fatal("bundle downloaded without need")
	}
}

func TestRejectsBadSignatureChecksumAndPaths(t *testing.T) {
	data := t.TempDir()
	req := Request{DataDir: data, Group: "6.5", Shell: "shell-a", Current: 1}

	f := newFixture(t, "shell-a", 26008000, goodBundle("shell-a"), nil)
	f.files["/m.json"] = append([]byte(" "), f.files["/m.json"]...)
	if _, err := f.run(t, req); err == nil {
		t.Fatal("accepted a changed manifest")
	}

	f = newFixture(t, "shell-a", 26008000, goodBundle("shell-a"), func(m *manifest) {
		b := m.Bundles["6.5"]
		b.SHA256 = hex.EncodeToString(make([]byte, 32))
		m.Bundles["6.5"] = b
	})
	if _, err := f.run(t, req); err == nil {
		t.Fatal("accepted a bundle with the wrong checksum")
	}

	evil := goodBundle("shell-a")
	evil["../escape"] = "x"
	f = newFixture(t, "shell-a", 26008000, evil, nil)
	if _, err := f.run(t, req); err == nil {
		t.Fatal("accepted a path outside the bundle")
	}
	if _, err := os.Stat(filepath.Join(data, "escape")); !os.IsNotExist(err) {
		t.Fatal("wrote outside the bundle")
	}

	f = newFixture(t, "shell-a", 26008000, goodBundle("shell-b"), nil)
	if _, err := f.run(t, req); err == nil {
		t.Fatal("accepted a bundle built for another shell")
	}
	if _, err := os.Stat(filepath.Join(data, "brain", "active")); !os.IsNotExist(err) {
		t.Fatal("a rejected bundle became active")
	}
}
