package anipub

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestMediaPlaylistURL(t *testing.T) {
	master, _ := url.Parse("https://megap.kotocdn.site/abc/master.m3u8")
	cases := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "already a media playlist", body: "#EXTM3U\n#EXTINF:5,\nhttps://seg/1.ts\n", want: master.String()},
		{
			name: "highest relative variant",
			body: "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=500000\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2000000\nhigh.m3u8\n",
			want: "https://megap.kotocdn.site/abc/high.m3u8",
		},
		{
			name: "highest absolute variant",
			body: "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2000000\nhttps://cdn.example/high.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=500000\nlow.m3u8\n",
			want: "https://cdn.example/high.m3u8",
		},
		{
			name: "tie keeps the first, unreadable bandwidth counts as zero",
			body: "#EXTM3U\r\n#EXT-X-STREAM-INF:bandwidth=x\r\nzero.m3u8\r\n#EXT-X-STREAM-INF:RESOLUTION=1x1\r\n\r\n#comment\r\nalso-zero.m3u8\r\n",
			want: "https://megap.kotocdn.site/abc/zero.m3u8",
		},
		{name: "variant tag with no uri", body: "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=500000\n", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mediaPlaylistURL(master, tc.body)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBandwidth(t *testing.T) {
	cases := map[string]int{
		"#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=1280x720":  1280000,
		"#EXT-X-STREAM-INF:RESOLUTION=1280x720,bandwidth= 640000 ": 640000,
		"#EXT-X-STREAM-INF:RESOLUTION=1280x720":                    0,
		"#EXT-X-STREAM-INF:BANDWIDTH=lots":                         0,
	}
	for tag, want := range cases {
		if got := bandwidth(tag); got != want {
			t.Errorf("bandwidth(%q) = %d, want %d", tag, got, want)
		}
	}
}

func TestPlaylistURIsAndAdSegments(t *testing.T) {
	body := "#EXTM3U\n#EXTINF:5,\n  https://megap.kotocdn.site/seg/1.ts \n\n#EXTINF:5,\nhttps://P16-AD-SG.IBYTEIMG.COM/obj/ad-site-i18n/decoy\n"
	uris := playlistURIs(body)
	if len(uris) != 2 || uris[0] != "https://megap.kotocdn.site/seg/1.ts" {
		t.Fatalf("uris = %q", uris)
	}
	if isAdSegment(uris[0]) || !isAdSegment(uris[1]) {
		t.Fatalf("ad detection wrong for %q", uris)
	}
}

func TestNotVideo(t *testing.T) {
	cases := []struct {
		label  string
		head   []byte
		reject bool
	}{
		{"png signature", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), true},
		{"jpeg with exif", []byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x10}, true},
		{"gif87", []byte("GIF87a\x01\x00"), true},
		{"webp", []byte("RIFF\x24\x00\x00\x00WEBPVP8 "), true},
		{"riff but not webp", []byte("RIFFxxxxWAVE"), false},
		{"error page", []byte("<HTML><HEAD>403"), true},
		{"doctype", []byte("<!doctype html>"), true},
		{"whitespace then html", []byte("  \n<html>"), true},
		{"mpeg-ts sync bytes", []byte{0x47, 0x40, 0x00, 0x10, 0x00}, false},
		{"fmp4 box", []byte("\x00\x00\x00\x20ftypisom"), false},
		{"too short to tell", []byte{0x89, 'P'}, false},
	}
	for _, tc := range cases {
		if got := notVideo(tc.head); got != tc.reject {
			t.Errorf("%s: notVideo = %v, want %v", tc.label, got, tc.reject)
		}
	}
}

func TestIsMegaplayCDNHost(t *testing.T) {
	for host, want := range map[string]bool{
		"kotocdn.site":        true,
		"megap.kotocdn.site":  true,
		" MEGAP.KotoCDN.site": true,
		"notkotocdn.site":     false,
		"kotocdn.site.evil":   false,
		"cdn.example":         false,
	} {
		if got := isMegaplayCDNHost(host); got != want {
			t.Errorf("%q: %v, want %v", host, got, want)
		}
	}
}

func TestValidateResolvedStream(t *testing.T) {
	const master = "http://megap.kotocdn.site/abc/master.m3u8"
	const variantMaster = "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nindex.m3u8\n"
	const ad = "https://p16-ad-sg.ibyteimg.com/obj/ad-site-i18n/"
	tsHead := string([]byte{0x47, 0x47, 0x00, 0x1B})

	cases := []struct {
		name    string
		setup   func(web *fakeWeb)
		stream  string
		wantErr string // "" for no error
	}{
		{
			name:   "other hosts are not checked",
			stream: "http://cdn.example/master.m3u8",
		},
		{
			name: "clean stream passes",
			setup: func(web *fakeWeb) {
				web.text("megap.kotocdn.site/abc/master.m3u8", variantMaster)
				web.text("megap.kotocdn.site/abc/index.m3u8", "#EXTM3U\n#EXTINF:5,\nhttp://megap.kotocdn.site/abc/seg/1.ts\n#EXTINF:5,\nhttp://megap.kotocdn.site/abc/seg/2.ts\n")
				web.text("megap.kotocdn.site/abc/seg/1.ts", tsHead)
			},
		},
		{
			name: "all segments are ads",
			setup: func(web *fakeWeb) {
				web.text("megap.kotocdn.site/abc/master.m3u8", variantMaster)
				web.text("megap.kotocdn.site/abc/index.m3u8", "#EXTM3U\n#EXTINF:5,\n"+ad+"x\n#EXTINF:5,\n"+ad+"y\n")
			},
			wantErr: "all 2 segments are ad/decoy content",
		},
		{
			name: "half the segments are ads",
			setup: func(web *fakeWeb) {
				web.text("megap.kotocdn.site/abc/master.m3u8", "#EXTM3U\n#EXTINF:5,\nhttp://megap.kotocdn.site/abc/seg/1.ts\n#EXTINF:5,\n"+ad+"a\n")
			},
			wantErr: "1/2 segments are ad/decoy content",
		},
		{
			name: "a clean-looking segment redirects to an ad",
			setup: func(web *fakeWeb) {
				web.text("megap.kotocdn.site/abc/master.m3u8", variantMaster)
				web.text("megap.kotocdn.site/abc/index.m3u8", "#EXTM3U\n#EXTINF:5,\nhttp://megap.kotocdn.site/abc/seg/1.ts\n")
				web.routes["megap.kotocdn.site/abc/seg/1.ts"] = func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "http://p16-ad-sg.ibyteimg.com/obj/ad-site-i18n/decoy", http.StatusFound)
				}
				web.text("p16-ad-sg.ibyteimg.com/obj/ad-site-i18n/decoy", "\x89PNG\r\n\x1a\n")
			},
			wantErr: "not video content",
		},
		{
			name: "a failed probe is not held against the stream",
			setup: func(web *fakeWeb) {
				web.text("megap.kotocdn.site/abc/master.m3u8", variantMaster)
				web.text("megap.kotocdn.site/abc/index.m3u8", "#EXTM3U\n#EXTINF:5,\nhttp://megap.kotocdn.site/abc/seg/1.ts\n")
				web.status("megap.kotocdn.site/abc/seg/1.ts", http.StatusInternalServerError, "")
			},
		},
		{
			name: "master refused",
			setup: func(web *fakeWeb) {
				web.status("megap.kotocdn.site/abc/master.m3u8", http.StatusForbidden, "denied")
			},
			wantErr: "megaplay hls manifest failed with status 403",
		},
		{
			name: "master is not HLS",
			setup: func(web *fakeWeb) {
				web.text("megap.kotocdn.site/abc/master.m3u8", "<html>challenge</html>")
			},
			wantErr: "is not an HLS playlist",
		},
		{
			name: "media playlist missing",
			setup: func(web *fakeWeb) {
				web.text("megap.kotocdn.site/abc/master.m3u8", variantMaster)
			},
			wantErr: "media playlist fetch failed",
		},
		{
			name: "media playlist is empty",
			setup: func(web *fakeWeb) {
				web.text("megap.kotocdn.site/abc/master.m3u8", variantMaster)
				web.text("megap.kotocdn.site/abc/index.m3u8", "#EXTM3U\n")
			},
			wantErr: "has no media segments",
		},
		{
			name:    "not a URL",
			stream:  "megap.kotocdn.site/abc",
			wantErr: "invalid anipub stream url",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			web := newFakeWeb(t)
			if tc.setup != nil {
				tc.setup(web)
			}
			stream := tc.stream
			if stream == "" {
				stream = master
			}
			err := validateResolvedStream(stream)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateSendsPlayerHeaders(t *testing.T) {
	web := newFakeWeb(t)
	web.text("megap.kotocdn.site/abc/master.m3u8", "#EXTM3U\n#EXTINF:5,\nhttp://megap.kotocdn.site/abc/seg/1.ts\n")
	web.text("megap.kotocdn.site/abc/seg/1.ts", "\x47\x47\x00\x1b")
	if err := validateResolvedStream("http://megap.kotocdn.site/abc/master.m3u8"); err != nil {
		t.Fatal(err)
	}
	if got := web.header("megap.kotocdn.site/abc/master.m3u8", "Referer"); got != "http://megaplay.test/" {
		t.Errorf("playlist Referer = %q", got)
	}
	if got := web.header("megap.kotocdn.site/abc/seg/1.ts", "Range"); got != "bytes=0-15" {
		t.Errorf("probe Range = %q", got)
	}
}
