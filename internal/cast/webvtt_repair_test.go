package cast

import (
	"bytes"
	"strings"
	"testing"
)

func TestRepairWebVTTKeepsLaterCues(t *testing.T) {
	raw := []byte("WEBVTT\n\n02:58.650 --> 03:01.660\nFirst\n\n\nOrphan\n \t\nAnother orphan\n\nid\n03:02.020 --> 03:05.100 align:start\nLater\n\n23:47.000 --> 23:49.990\nFinal\n")
	got := RepairWebVTT(raw)
	if strings.Contains(string(got), "orphan") || strings.Contains(string(got), "Orphan") {
		t.Fatal("orphan blocks retained")
	}
	if strings.Count(string(got), "-->") != 3 || !strings.Contains(string(got), "id\n03:02.020 --> 03:05.100 align:start") {
		t.Fatal("cue data lost")
	}
	if !bytes.Equal(RepairWebVTT(got), got) {
		t.Fatal("repair is not idempotent")
	}
}
func TestRepairWebVTTPreservesValidAndOtherFormats(t *testing.T) {
	for _, raw := range []string{
		"\ufeffWEBVTT\r\n\r\nNOTE comment\r\n\r\nSTYLE\r\n::cue { color: white; }\r\n\r\nREGION\r\nid:main\r\n\r\nid\r\n00:00:01.000 --> 00:00:02.000\r\nText\r\n",
		"[Script Info]\nASS data\n", "1\n00:00:01,000 --> 00:00:02,000\nText\n",
	} {
		if !bytes.Equal(RepairWebVTT([]byte(raw)), []byte(raw)) {
			t.Fatal("valid data changed")
		}
	}
}
