package mobilecore

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Downloaded episodes and films are cast like streams: a server on this
// device's loopback address hands the file to the cast relay, which serves
// it to the TV as it does any web stream. Only files the app asked to cast
// are served, each under a random address.

var localFiles struct {
	sync.Mutex
	addr  string
	paths map[string]string
	order []string
}

// localFilePath reads a file address the app sends for a download: an
// absolute path or a file:// URL. ok is false for web addresses.
func localFilePath(u string) (string, bool) {
	if strings.HasPrefix(u, "file://") {
		parsed, err := url.Parse(u)
		if err != nil {
			return "", true
		}
		return parsed.Path, true
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return "", false
	}
	return u, filepath.IsAbs(u) || len(u) > 2 && u[1] == ':' // C:\ on Windows
}

// serveLocalFile gives a loopback web address for the file at path.
func serveLocalFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", castRefusal("Only files can be cast.")
	}
	localFiles.Lock()
	defer localFiles.Unlock()
	if localFiles.addr == "" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
		localFiles.addr = listener.Addr().String()
		localFiles.paths = map[string]string{}
		go http.Serve(listener, http.HandlerFunc(serveLocal))
	}
	token := make([]byte, 16)
	rand.Read(token)
	key := hex.EncodeToString(token)
	localFiles.paths[key] = path
	localFiles.order = append(localFiles.order, key)
	if len(localFiles.order) > 16 {
		delete(localFiles.paths, localFiles.order[0])
		localFiles.order = localFiles.order[1:]
	}
	return "http://" + localFiles.addr + "/" + key + "/" + url.PathEscape(filepath.Base(path)), nil
}

func serveLocal(w http.ResponseWriter, r *http.Request) {
	key, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	localFiles.Lock()
	path, ok := localFiles.paths[key]
	localFiles.Unlock()
	if !ok || r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// localCastAddresses swaps a download's file and subtitle paths for
// loopback addresses the relay can fetch.
func localCastAddresses(in castRequest) (castRequest, error) {
	if path, ok := localFilePath(in.URL); ok {
		served, err := serveLocalFile(path)
		if err != nil {
			return in, castRefusal("The downloaded file could not be opened.")
		}
		in.URL, in.Headers = served, nil
		in.HLS = in.HLS && strings.HasSuffix(strings.ToLower(path), ".m3u8")
	}
	if path, ok := localFilePath(in.Subtitle); ok && in.Subtitle != "" {
		served, err := serveLocalFile(path)
		if err != nil {
			in.Subtitle = ""
		} else {
			in.Subtitle = served
		}
	}
	return in, nil
}
