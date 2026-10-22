package cast

import (
	"strings"

	"rsc.io/qr"
)

// QRLines draws text as a QR code in a terminal, two rows of modules to a line
// of half blocks. Light modules are drawn and dark ones left blank, which is
// the right way round on the dark background a terminal usually has; phone
// scanners read the inverted code a light terminal shows as well. nil when the
// text does not fit a code.
func QRLines(text string) []string {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return nil
	}
	const quiet = 2
	size := code.Size + 2*quiet
	light := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		if x < 0 || y < 0 || x >= code.Size || y >= code.Size {
			return true
		}
		return !code.Black(x, y)
	}
	lines := make([]string, 0, (size+1)/2)
	for y := 0; y < size; y += 2 {
		var line strings.Builder
		for x := 0; x < size; x++ {
			upper, lower := light(x, y), y+1 < size && light(x, y+1)
			switch {
			case upper && lower:
				line.WriteString("█")
			case upper:
				line.WriteString("▀")
			case lower:
				line.WriteString("▄")
			default:
				line.WriteString(" ")
			}
		}
		lines = append(lines, line.String())
	}
	return lines
}

// IsQRLine reports whether a line is one of QRLines' rows, which a frame has to
// draw at full contrast rather than dimmed like the text around it.
func IsQRLine(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	for _, r := range line {
		switch r {
		case '█', '▀', '▄', ' ':
		default:
			return false
		}
	}
	return true
}
