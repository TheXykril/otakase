package icons

import (
	"encoding/binary"
	"testing"
)

// cmapCodepoints reads the format 12 (full Unicode) subtable of a TrueType
// font's cmap: the private-use glyphs are all above U+FFFF, so it is the only
// subtable that can hold them.
func cmapCodepoints(t *testing.T, font []byte) map[rune]bool {
	t.Helper()
	if len(font) < 12 {
		t.Fatal("font too short")
	}
	numTables := int(binary.BigEndian.Uint16(font[4:6]))
	var cmap []byte
	for i := 0; i < numTables; i++ {
		record := font[12+16*i : 12+16*(i+1)]
		if string(record[:4]) == "cmap" {
			offset := binary.BigEndian.Uint32(record[8:12])
			length := binary.BigEndian.Uint32(record[12:16])
			cmap = font[offset : offset+length]
		}
	}
	if cmap == nil {
		t.Fatal("font has no cmap table")
	}
	found := map[rune]bool{}
	subtables := int(binary.BigEndian.Uint16(cmap[2:4]))
	for i := 0; i < subtables; i++ {
		record := cmap[4+8*i : 4+8*(i+1)]
		sub := cmap[binary.BigEndian.Uint32(record[4:8]):]
		if binary.BigEndian.Uint16(sub[0:2]) != 12 {
			continue
		}
		groups := int(binary.BigEndian.Uint32(sub[12:16]))
		for g := 0; g < groups; g++ {
			group := sub[16+12*g : 16+12*(g+1)]
			start := binary.BigEndian.Uint32(group[0:4])
			end := binary.BigEndian.Uint32(group[4:8])
			for cp := start; cp <= end; cp++ {
				found[rune(cp)] = true
			}
		}
	}
	return found
}

// An icon added to All without regenerating the font would draw as an empty
// box on every desktop that relies on the bundled copy.
func TestBundledFontCoversEveryIcon(t *testing.T) {
	covered := cmapCodepoints(t, fontData)
	for _, icon := range All {
		if !covered[rune(icon)] {
			t.Errorf("bundled font lacks U+%X; run Build/icon-font.py", rune(icon))
		}
	}
}

func TestLabelFollowsTheSwitch(t *testing.T) {
	Resolve(false)
	if got := Label(Download, "Update now"); got != "Update now" {
		t.Fatalf("icons off: got %q", got)
	}
	Resolve(true)
	t.Cleanup(func() { Resolve(false) })
	if got := Label(Download, "Update now"); got != string(rune(Download))+"  Update now" {
		t.Fatalf("icons on: got %q", got)
	}
}
