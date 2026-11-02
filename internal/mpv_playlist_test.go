package internal

import (
	"strings"
	"testing"
)

func TestHistoryFileLocation(t *testing.T) {
	if p := localHistoryPath(" "); p != "" {
		t.Fatalf("blank storage path gave %q", p)
	}
	if p := localHistoryPath("/tmp/otakase-test-store"); !strings.HasSuffix(p, historyFileName) {
		t.Fatalf("history path %q does not end in %s", p, historyFileName)
	}
}
