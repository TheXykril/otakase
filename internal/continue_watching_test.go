package internal

import (
	"testing"
)

func TestContinueWatchingLabel(t *testing.T) {
	cases := []struct {
		episode, seconds int
		want             string
	}{
		{13, 754, "Frieren · ep 13 at 12:34"},
		{13, 20, "Frieren · ep 13"},
		{2, 3725, "Frieren · ep 2 at 1:02:05"},
		{0, 0, "Frieren"},
	}
	for _, tc := range cases {
		if got := continueWatchingLabel("Frieren", tc.episode, tc.seconds); got != tc.want {
			t.Fatalf("label(%d, %d) = %q, want %q", tc.episode, tc.seconds, got, tc.want)
		}
	}
}

func TestResumeRowAnilistID(t *testing.T) {
	if id, ok := resumeRowAnilistID("RESUME:154587"); !ok || id != 154587 {
		t.Fatalf("got %d, %v", id, ok)
	}
	for _, key := range []string{"CURRENT", "RESUME:", "RESUME:x", "RESUME:-4", "154587"} {
		if _, ok := resumeRowAnilistID(key); ok {
			t.Fatalf("%q read as a resume row", key)
		}
	}
}
