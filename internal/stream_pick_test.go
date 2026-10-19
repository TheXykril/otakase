package internal

import "testing"

func TestBestLink(t *testing.T) {
	cases := []struct {
		links []string
		want  string
	}{
		{nil, ""},
		{[]string{"https://a/x-480p.m3u8", "https://b/x-1080p.m3u8", "https://c/x-720p.m3u8"}, "https://b/x-1080p.m3u8"},
		{[]string{"https://cdn.example/1080p.mp4", "https://video.wixstatic.com/360p.mp4"}, "https://video.wixstatic.com/360p.mp4"},
		{[]string{"https://a/one", "https://b/two"}, "https://a/one"},
	}
	for _, c := range cases {
		if got := BestLink(c.links); got != c.want {
			t.Errorf("BestLink(%v) = %q, want %q", c.links, got, c.want)
		}
	}
}

func TestFillerHelpers(t *testing.T) {
	filler := []int{4, 5, 9}
	if !IsFiller(filler, 5) || IsFiller(filler, 6) {
		t.Error("IsFiller wrong")
	}
	if got := NextCanonEpisode(filler, 3); got != 6 {
		t.Errorf("NextCanonEpisode(3) = %d, want 6", got)
	}
	if got := NextCanonEpisode(nil, 3); got != 4 {
		t.Errorf("NextCanonEpisode with no filler = %d, want 4", got)
	}
}
