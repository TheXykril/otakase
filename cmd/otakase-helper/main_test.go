package main

import "testing"

func TestStreamLanguagesArgument(t *testing.T) {
	for _, arg := range []string{"", "kickassanime", "kickassanime:show", "kickassanime::3", "kickassanime:show:x", "nyaa:show:1", "kickassanime:show:0"} {
		if _, err := streamLanguages(arg); err == nil {
			t.Errorf("accepted %q", arg)
		}
	}
}
