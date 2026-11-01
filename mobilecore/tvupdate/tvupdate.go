// Package tvupdate installs signed app updates on Samsung (Tizen) TVs.
//
// A Tizen app cannot install packages, but it can choose which Dart code
// and Go core to run (see app/tizen/Brain.cs). A release publishes
// otakase-samsung-tv.json, signed with an ed25519 key, listing one bundle
// per TizenOS group. Run downloads the bundle for this TV, checks its
// SHA-256 and unpacks it to <data>/brain/<code>/, then points
// <data>/brain/active at it. The shell switches on the next start.
package tvupdate

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ManifestURL always names the newest release's manifest.
const ManifestURL = "https://github.com/TheXykril/otakase/releases/latest/download/otakase-samsung-tv.json"

// PublicKey checks otakase-samsung-tv.json.sig. The private half is the
// TV_UPDATE_KEY_PEM release secret.
var PublicKey = mustKey("cd18e67b30220447ce738a5cd8542cbfb58fb1e61c0b98adc0b5717c68c0c85c")

const (
	maxManifest = 64 << 10
	maxBundle   = 200 << 20
	maxFiles    = 5000
)

// Request comes from the app as JSON.
type Request struct {
	DataDir string `json:"data_dir"`
	Group   string `json:"group"`
	Shell   string `json:"shell"`
	Current int    `json:"current"`
	// Running is the brain directory name in use now, kept on disk.
	Running string `json:"running"`
	// ManifestURL overrides the release manifest (tests only).
	ManifestURL string `json:"manifest_url,omitempty"`
}

// Result statuses: "current" (nothing newer), "installed" (ready on next
// start), "reinstall" (the newer release needs a new package).
type Result struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Code    int    `json:"code,omitempty"`
}

type manifest struct {
	Version string            `json:"version"`
	Code    int               `json:"code"`
	Bundles map[string]bundle `json:"bundles"`
}

type bundle struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Shell  string `json:"shell"`
}

func mustKey(h string) ed25519.PublicKey {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("tvupdate: bad public key")
	}
	return b
}

// Run checks for and installs an update. It never touches the running
// brain, so a failed or partial download leaves the app as it was.
func Run(ctx context.Context, client *http.Client, req Request, key ed25519.PublicKey) (Result, error) {
	if req.DataDir == "" || req.Group == "" || req.Shell == "" {
		return Result{}, errors.New("data_dir, group and shell are required")
	}
	source := req.ManifestURL
	if source == "" {
		source = ManifestURL
	}
	body, err := get(ctx, client, source, maxManifest)
	if err != nil {
		return Result{}, err
	}
	sigText, err := get(ctx, client, source+".sig", 1024)
	if err != nil {
		return Result{}, err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || !ed25519.Verify(key, body, sig) {
		return Result{}, errors.New("update manifest signature is invalid")
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return Result{}, fmt.Errorf("update manifest: %w", err)
	}
	root := filepath.Join(req.DataDir, "brain")
	name := strconv.Itoa(m.Code)
	b, ok := m.Bundles[req.Group]
	if m.Code <= req.Current || !ok || failed(root, name) {
		return Result{Status: "current"}, nil
	}
	if b.Shell != req.Shell {
		return Result{Status: "reinstall", Version: m.Version, Code: m.Code}, nil
	}
	if b.Size <= 0 || b.Size > maxBundle {
		return Result{}, errors.New("update bundle size is invalid")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Result{}, err
	}
	dir := filepath.Join(root, name)
	if !complete(dir) {
		if err := install(ctx, client, b, root, dir); err != nil {
			return Result{}, err
		}
	}
	if err := writeFile(filepath.Join(root, "active"), name); err != nil {
		return Result{}, err
	}
	prune(root, name, req.Running)
	return Result{Status: "installed", Version: m.Version, Code: m.Code}, nil
}

func get(ctx context.Context, client *http.Client, source string, limit int64) ([]byte, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update download: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("update download is too large")
	}
	return b, nil
}

func install(ctx context.Context, client *http.Client, b bundle, root, dir string) error {
	if u, err := url.Parse(b.URL); err != nil || u.Scheme != "https" {
		return errors.New("update bundle URL must be https")
	}
	want, err := hex.DecodeString(b.SHA256)
	if err != nil || len(want) != sha256.Size {
		return errors.New("update bundle checksum is invalid")
	}
	tmp, err := os.CreateTemp(root, "download-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, b.URL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update download: HTTP %d", resp.StatusCode)
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, b.Size+1))
	if err != nil {
		return err
	}
	if n != b.Size || subtle.ConstantTimeCompare(hash.Sum(nil), want) != 1 {
		return errors.New("update bundle does not match its checksum")
	}
	part := dir + ".part"
	os.RemoveAll(part)
	if err := unzip(tmp.Name(), part); err != nil {
		os.RemoveAll(part)
		return err
	}
	shell, err := os.ReadFile(filepath.Join(part, "shell-id"))
	if err != nil || strings.TrimSpace(string(shell)) != b.Shell {
		os.RemoveAll(part)
		return errors.New("update bundle is for another shell")
	}
	if err := writeFile(filepath.Join(part, "complete"), "1"); err != nil {
		os.RemoveAll(part)
		return err
	}
	os.RemoveAll(dir)
	return os.Rename(part, dir)
}

func unzip(source, dest string) error {
	z, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer z.Close()
	if len(z.File) > maxFiles {
		return errors.New("update bundle has too many files")
	}
	var total uint64
	for _, f := range z.File {
		name := filepath.Clean(filepath.FromSlash(f.Name))
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("update bundle has an unsafe path %q", f.Name)
		}
		target := filepath.Join(dest, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			return fmt.Errorf("update bundle has a special file %q", f.Name)
		}
		total += f.UncompressedSize64
		if total > maxBundle*3 {
			return errors.New("update bundle unpacks too large")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := extract(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extract(f *zip.File, target string) error {
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(in, int64(f.UncompressedSize64)+1)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func complete(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "complete"))
	return err == nil
}

// failed reports a release whose last start crashed; the shell wrote its
// name to brain/failed. It is not offered again.
func failed(root, name string) bool {
	b, err := os.ReadFile(filepath.Join(root, "failed"))
	return err == nil && strings.TrimSpace(string(b)) == name
}

func prune(root, keep, running string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || n == keep || (running != "" && n == running) {
			continue
		}
		os.RemoveAll(filepath.Join(root, n))
	}
}

func writeFile(path, value string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(value), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
