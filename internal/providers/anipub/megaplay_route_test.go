package anipub

import "testing"

// anipub serves two episode link shapes. Only the legacy /video/ one was handled,
// so newer entries failed with `unsupported video link ".../play/62876/1/sub"`
// even though the show searched and listed fine.
func TestParseVideoLinkSupportsBothAnipubShapes(t *testing.T) {
	cases := []struct {
		name string
		link string
		want megaplayRoute
	}{
		{
			name: "legacy video link",
			link: "https://www.anipub.xyz/video/2142/sub",
			want: megaplayRoute{path: "stream/s-2/2142", mode: "sub"},
		},
		{
			name: "legacy dub link",
			link: "https://www.anipub.xyz/video/2144/dub",
			want: megaplayRoute{path: "stream/s-2/2144", mode: "dub"},
		},
		{
			name: "play link keyed by mal id and episode",
			link: "https://anipub.xyz/play/62876/1/sub",
			want: megaplayRoute{path: "stream/mal/62876/1", mode: "sub"},
		},
		{
			name: "play link for a later episode",
			link: "https://anipub.xyz/play/62876/5/sub",
			want: megaplayRoute{path: "stream/mal/62876/5", mode: "sub"},
		},
	}

	for _, tc := range cases {
		got, err := parseVideoLink(tc.link)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestParseVideoLinkRejectsUnknownShapes(t *testing.T) {
	for _, link := range []string{
		"",
		"https://anipub.xyz/",
		"https://anipub.xyz/watch/62876",
		"https://anipub.xyz/play/62876/sub",
	} {
		if _, err := parseVideoLink(link); err == nil {
			t.Fatalf("expected %q to be rejected", link)
		}
	}
}
