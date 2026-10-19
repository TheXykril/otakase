package internal

import (
	"reflect"
	"testing"
)

func TestParseVersionParts(t *testing.T) {
	for in, want := range map[string][]int{
		"26.1.0":       {26, 1, 0},
		"v2.0.3":       {2, 0, 3},
		"26.1.0-dev.4": {26, 1, 0},
		"26.2":         {26, 2, 0},
		"1.2.3.4":      {1, 2, 3},
		"dev":          nil,
		"":             nil,
		"x.1.0":        nil,
	} {
		if got := versionNumbers(in); !reflect.DeepEqual(got, want) {
			t.Errorf("versionNumbers(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"26.1.0", "2.2.2", 1},
		{"2.2.2", "26.1.0", -1},
		{"v26.1.0", "26.1.0", 0},
		{"26.1.1", "26.1.0", 1},
		{"", "1.0.0", -1},
		{"1.0.0", "", 1},
		{"dev", "26.1.0", 1},
		{"26.1.0", "dev", -1},
		{"dev", "dev", 0},
		{"", "dev", -1},
		{"dev", "", 1},
		{"dev", "nightly", -1}, // two non-releases: string order
		{" 26.1.0 ", "26.1.0", 0},
	}
	for _, c := range cases {
		if got := versionOrder(c.a, c.b); got != c.want {
			t.Errorf("versionOrder(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
