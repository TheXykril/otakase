package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestPublicUpstreamIP(t *testing.T) {
	for addr, want := range map[string]bool{
		"1.1.1.1":         true,
		"2606:4700::1111": true,
		"127.0.0.1":       false,
		"::1":             false,
		"10.0.0.5":        false,
		"172.16.3.4":      false,
		"192.168.1.10":    false,
		"169.254.169.254": false,
		"100.100.1.1":     false,
		"0.0.0.0":         false,
		"fd00::1":         false,
		"fe80::1":         false,
		"::ffff:10.0.0.1": false,
		"224.0.0.1":       false,
	} {
		if got := publicUpstreamIP(netip.MustParseAddr(addr)); got != want {
			t.Errorf("publicUpstreamIP(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestWatchLocalAddr(t *testing.T) {
	for link, want := range map[string]string{
		"http://127.0.0.1:8123/playlist.m3u8": "127.0.0.1:8123",
		"http://[::1]:99/a":                   "[::1]:99",
		"https://127.0.0.1:8123/a":            "",
		"http://127.0.0.1/a":                  "",
		"http://192.168.1.2:80/a":             "",
		"https://cdn.example.com/a.m3u8":      "",
	} {
		if got := watchLocalAddr(link); got != want {
			t.Errorf("watchLocalAddr(%q) = %q, want %q", link, got, want)
		}
	}
}

// A remote episode's playlist must not reach a loopback service, while an
// episode that starts on the local torrent helper keeps working.
func TestWatchUpstreamRefusesPrivateAddresses(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer local.Close()
	client := newWatchUpstream()

	remote := &watchMedia{link: streamLink{URL: "https://cdn.example.com/master.m3u8"}}
	req, err := remote.request(context.Background(), local.URL+"/api/states")
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
		t.Fatal("remote episode fetched a loopback address")
	} else if !errors.Is(err, errPrivateUpstream) {
		t.Fatalf("err = %v, want errPrivateUpstream", err)
	}

	helper := &watchMedia{link: streamLink{URL: local.URL + "/playlist.m3u8"}}
	req, err = helper.request(context.Background(), local.URL+"/seg-1.ts")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("local helper episode refused its own address: %v", err)
	}
	resp.Body.Close()

	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer other.Close()
	req, err = helper.request(context.Background(), other.URL+"/x")
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
		t.Fatal("local helper episode reached a different loopback port")
	}
}
