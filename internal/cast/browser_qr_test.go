package cast

import "testing"

func TestQRLinesDrawsASquareCode(t *testing.T) {
	lines := QRLines("http://192.168.1.20:8012/abcde")
	if len(lines) == 0 {
		t.Fatal("no code drawn")
	}
	width := len([]rune(lines[0]))
	// At least version 1's 21 modules plus two of quiet zone each side, and
	// two rows of modules to a line.
	if width < 25 || len(lines) != (width+1)/2 {
		t.Fatalf("code is %d wide and %d lines, not a square", width, len(lines))
	}
	for i, line := range lines {
		if len([]rune(line)) != width {
			t.Fatalf("line %d is %d wide, want %d", i, len([]rune(line)), width)
		}
		if !IsQRLine(line) {
			t.Fatalf("line %d is not recognised as part of the code: %q", i, line)
		}
	}
}

func TestIsQRLineLeavesTextAlone(t *testing.T) {
	for _, line := range []string{"", "   ", "Open this on your TV", "█ text"} {
		if IsQRLine(line) {
			t.Errorf("IsQRLine(%q) = true", line)
		}
	}
}
