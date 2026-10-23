package main

import "testing"

func TestWatchValidLanguage(t *testing.T) {
	cases := []struct {
		value string
		subtitles bool
		valid bool
	}{
		{"auto", false, true},
		{"auto", true, true},
		{"off", true, true},
		{"off", false, false},
		{"en", false, true},
		{"eng", false, true},
		{"pt-br", true, true},
		{"", false, false},
		{"<script>", true, false},
		{"en/../../etc", false, false},
	}
	for _, tc := range cases {
		if got := watchValidLanguage(tc.value, tc.subtitles); got != tc.valid {
			t.Errorf("watchValidLanguage(%q, %v) = %v, want %v", tc.value, tc.subtitles, got, tc.valid)
		}
	}
}
