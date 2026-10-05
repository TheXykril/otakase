// Command app-icon renders the app icon into every file a platform wants.
//
// Run it from the repository root after changing site/img/icon.svg or
// site/img/favicon.svg:
//
//	cd Build/app-icon && go run .
//
// It is its own module so its dependencies stay out of otakase's vendor tree.
// The two SVGs are the source; everything below is written from them:
//
//   - Build/app-icon/png/otakase-<size>.png, the sizes Linux and the icon pack use
//   - Build/app-icon/otakase.ico, the installer's icon
//   - Build/app-icon/otakase.icns, kept ready for a macOS app bundle
//   - cmd/otakase/rsrc_windows_*.syso, which gives otakase.exe its icon
//   - internal/appicon/otakase.png, otakase-small.png and otakase.svg, embedded
//     in the binary
//   - internal/mpvskin/assets/logo/otakase.ass, the player's loading logo
//
// Only the shapes the icon uses are understood: rect with rx, and paths made
// of M, L, H, V, Q and Z with absolute coordinates.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/tc-hib/winres"
	"golang.org/x/image/vector"
)

// Below this size the full icon's lettering is mush, so the 任-only favicon is
// drawn instead.
const smallBelow = 64

var pngSizes = []int{16, 24, 32, 48, 64, 128, 256, 512, 1024}

func main() {
	root, err := filepath.Abs("../..")
	if err != nil {
		log.Fatal(err)
	}
	full := mustParse(filepath.Join(root, "site/img/icon.svg"))
	small := mustParse(filepath.Join(root, "site/img/favicon.svg"))
	pick := func(size int) *icon {
		if size < smallBelow {
			return small
		}
		return full
	}

	renders := map[int][]byte{}
	images := map[int]image.Image{}
	for _, size := range pngSizes {
		img := pick(size).render(size)
		images[size] = img
		renders[size] = encodePNG(img)
		write(filepath.Join(root, "Build/app-icon/png", fmt.Sprintf("otakase-%d.png", size)), renders[size])
	}

	write(filepath.Join(root, "Build/app-icon/otakase.ico"), buildICO(images, renders, []int{16, 24, 32, 48, 64, 128, 256}))
	write(filepath.Join(root, "Build/app-icon/otakase.icns"), buildICNS(renders))
	write(filepath.Join(root, "internal/appicon/otakase.png"), renders[256])
	write(filepath.Join(root, "internal/appicon/otakase-small.png"), encodePNG(small.render(64)))
	copyFile(filepath.Join(root, "site/img/icon.svg"), filepath.Join(root, "internal/appicon/otakase.svg"))
	write(filepath.Join(root, "internal/mpvskin/assets/logo/otakase.ass"), []byte(full.ass()))

	var exeIcons []image.Image
	for _, size := range []int{16, 24, 32, 48, 64, 128, 256} {
		exeIcons = append(exeIcons, images[size])
	}
	writeSyso(filepath.Join(root, "cmd/otakase"), exeIcons)
}

type shape struct {
	fill color.NRGBA
	// role names the palette colour the player logo takes for this shape.
	role string
	cmds []cmd
	// rect, when set, is a rounded tile instead of a path.
	rect *[5]float64 // x, y, w, h, rx
}

type cmd struct {
	op  byte
	pts []float64
}

type icon struct{ shapes []shape }

var (
	rectRe = regexp.MustCompile(`<rect[^>]*>`)
	pathRe = regexp.MustCompile(`<path[^>]*>`)
	attrRe = regexp.MustCompile(`([a-z]+)="([^"]*)"`)
	numRe  = regexp.MustCompile(`-?(?:\d+\.?\d*|\.\d+)(?:e-?\d+)?`)
)

// roles maps the icon's colours to what they are, so the player can draw the
// logo in the active theme instead of the icon's own red.
var roles = map[string]string{
	"#0e0d0c": "tile",
	"#582419": "shadow",
	"#d2492f": "mark",
	"#ece4d6": "kana",
	"#8a8276": "name",
}

func mustParse(path string) *icon {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	src := string(data)
	ic := &icon{}
	for _, tag := range rectRe.FindAllString(src, -1) {
		a := attrs(tag)
		r := [5]float64{num(a["x"]), num(a["y"]), num(a["width"]), num(a["height"]), num(a["rx"])}
		ic.shapes = append(ic.shapes, shape{fill: hexColor(a["fill"]), role: roles[a["fill"]], rect: &r})
	}
	for _, tag := range pathRe.FindAllString(src, -1) {
		a := attrs(tag)
		cmds, err := parsePath(a["d"])
		if err != nil {
			log.Fatalf("%s: %v", path, err)
		}
		ic.shapes = append(ic.shapes, shape{fill: hexColor(a["fill"]), role: roles[a["fill"]], cmds: cmds})
	}
	return ic
}

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
		out[m[1]] = m[2]
	}
	return out
}

func num(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func hexColor(s string) color.NRGBA {
	var r, g, b uint8
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		log.Fatalf("colour %q: %v", s, err)
	}
	return color.NRGBA{r, g, b, 255}
}

// parsePath reads an absolute M/L/H/V/Q/Z path into M, L, Q and Z commands,
// with H and V turned into L. A command letter may be followed by several
// coordinate groups, which repeat it (an M's extra groups are L).
func parsePath(d string) ([]cmd, error) {
	var out []cmd
	var x, y float64
	i := 0
	for i < len(d) {
		c := d[i]
		if c == ' ' || c == ',' {
			i++
			continue
		}
		if !strings.ContainsRune("MLHVQZ", rune(c)) {
			return nil, fmt.Errorf("unsupported path command %q", c)
		}
		i++
		j := i
		for j < len(d) && !strings.ContainsRune("MLHVQZmlhvqzCcSsTtAa", rune(d[j])) {
			j++
		}
		nums := numRe.FindAllString(d[i:j], -1)
		i = j
		vals := make([]float64, len(nums))
		for k, n := range nums {
			vals[k], _ = strconv.ParseFloat(n, 64)
		}
		per := map[byte]int{'M': 2, 'L': 2, 'H': 1, 'V': 1, 'Q': 4, 'Z': 0}[c]
		if per == 0 {
			out = append(out, cmd{op: 'Z'})
			continue
		}
		if len(vals)%per != 0 || len(vals) == 0 {
			return nil, fmt.Errorf("command %c has %d numbers", c, len(vals))
		}
		for k := 0; k < len(vals); k += per {
			op := c
			v := vals[k : k+per]
			switch c {
			case 'M':
				if k > 0 {
					op = 'L'
				}
				x, y = v[0], v[1]
				out = append(out, cmd{op: op, pts: []float64{x, y}})
			case 'L':
				x, y = v[0], v[1]
				out = append(out, cmd{op: 'L', pts: []float64{x, y}})
			case 'H':
				x = v[0]
				out = append(out, cmd{op: 'L', pts: []float64{x, y}})
			case 'V':
				y = v[0]
				out = append(out, cmd{op: 'L', pts: []float64{x, y}})
			case 'Q':
				out = append(out, cmd{op: 'Q', pts: []float64{v[0], v[1], v[2], v[3]}})
				x, y = v[2], v[3]
			}
		}
	}
	return out, nil
}

// render draws the icon at size×size pixels on a transparent background.
func (ic *icon) render(size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float32(size) / 100
	for _, sh := range ic.shapes {
		r := vector.NewRasterizer(size, size)
		if sh.rect != nil {
			roundedRect(r, sh.rect, s)
		} else {
			open := false
			for _, c := range sh.cmds {
				p := c.pts
				switch c.op {
				case 'M':
					if open {
						r.ClosePath()
					}
					open = true
					r.MoveTo(float32(p[0])*s, float32(p[1])*s)
				case 'L':
					r.LineTo(float32(p[0])*s, float32(p[1])*s)
				case 'Q':
					r.QuadTo(float32(p[0])*s, float32(p[1])*s, float32(p[2])*s, float32(p[3])*s)
				case 'Z':
					r.ClosePath()
					open = false
				}
			}
			if open {
				r.ClosePath()
			}
		}
		r.Draw(dst, dst.Bounds(), image.NewUniform(sh.fill), image.Point{})
	}
	return dst
}

func roundedRect(r *vector.Rasterizer, rc *[5]float64, s float32) {
	x, y, w, h, rx := float32(rc[0])*s, float32(rc[1])*s, float32(rc[2])*s, float32(rc[3])*s, float32(rc[4])*s
	k := rx * 0.5523 // bezier quarter circle
	r.MoveTo(x+rx, y)
	r.LineTo(x+w-rx, y)
	r.CubeTo(x+w-rx+k, y, x+w, y+rx-k, x+w, y+rx)
	r.LineTo(x+w, y+h-rx)
	r.CubeTo(x+w, y+h-rx+k, x+w-rx+k, y+h, x+w-rx, y+h)
	r.LineTo(x+rx, y+h)
	r.CubeTo(x+rx-k, y+h, x, y+h-rx+k, x, y+h-rx)
	r.LineTo(x, y+rx)
	r.CubeTo(x, y+rx-k, x+rx-k, y, x+rx, y)
	r.ClosePath()
}

// ass writes each shape but the tile as an ASS vector drawing in the icon's
// 0-100 units, one "role drawing" line each. ASS has only cubic curves, so
// each quadratic is raised to the cubic that traces the same curve.
func (ic *icon) ass() string {
	var b strings.Builder
	b.WriteString("; otakase logo for the player, written by Build/app-icon. One shape per line:\n")
	b.WriteString("; the palette role it is drawn in, then an ASS drawing in 0-100 units.\n")
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
	for _, sh := range ic.shapes {
		if sh.rect != nil || sh.role == "" || sh.role == "tile" {
			continue
		}
		b.WriteString(sh.role)
		var x, y float64
		for _, c := range sh.cmds {
			p := c.pts
			switch c.op {
			case 'M':
				fmt.Fprintf(&b, " m %s %s", f(p[0]), f(p[1]))
				x, y = p[0], p[1]
			case 'L':
				fmt.Fprintf(&b, " l %s %s", f(p[0]), f(p[1]))
				x, y = p[0], p[1]
			case 'Q':
				c1x, c1y := x+2.0/3*(p[0]-x), y+2.0/3*(p[1]-y)
				c2x, c2y := p[2]+2.0/3*(p[0]-p[2]), p[3]+2.0/3*(p[1]-p[3])
				fmt.Fprintf(&b, " b %s %s %s %s %s %s", f(c1x), f(c1y), f(c2x), f(c2y), f(p[2]), f(p[3]))
				x, y = p[2], p[3]
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

// buildICO packs the renders into an .ico. 256 px is stored as PNG, which
// Windows reads from Vista on; the smaller sizes as classic 32-bit bitmaps,
// which every reader of .ico files (Inno Setup's compiler among them) takes.
func buildICO(images map[int]image.Image, renders map[int][]byte, sizes []int) []byte {
	var buf bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&buf, le, [3]uint16{0, 1, uint16(len(sizes))})
	var blobs [][]byte
	for _, size := range sizes {
		if size >= 256 {
			blobs = append(blobs, renders[size])
		} else {
			blobs = append(blobs, dib(images[size]))
		}
	}
	offset := 6 + 16*len(sizes)
	for i, size := range sizes {
		dim := uint8(size)
		if size >= 256 {
			dim = 0
		}
		buf.Write([]byte{dim, dim, 0, 0})
		_ = binary.Write(&buf, le, [2]uint16{1, 32})
		_ = binary.Write(&buf, le, [2]uint32{uint32(len(blobs[i])), uint32(offset)})
		offset += len(blobs[i])
	}
	for _, blob := range blobs {
		buf.Write(blob)
	}
	return buf.Bytes()
}

// dib is an icon image as a BITMAPINFOHEADER, bottom-up BGRA rows and the
// 1-bit AND mask, all transparent bits clear since the alpha carries it.
func dib(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	maskRow := ((w + 31) / 32) * 4
	var buf bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&buf, le, struct {
		Size                 uint32
		Width, Height        int32
		Planes, BitCount     uint16
		Compression, SizeImg uint32
		XPPM, YPPM           int32
		ClrUsed, ClrImp      uint32
	}{40, int32(w), int32(2 * h), 1, 32, 0, uint32(w*h*4 + maskRow*h), 0, 0, 0, 0})
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			buf.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	buf.Write(make([]byte, maskRow*h))
	return buf.Bytes()
}

// buildICNS packs PNG images into an Apple .icns. Every type here takes PNG
// data on macOS 10.7 and later; the @2x ones reuse the doubled render.
func buildICNS(renders map[int][]byte) []byte {
	entries := []struct {
		kind string
		size int
	}{
		{"icp4", 16}, {"icp5", 32}, {"icp6", 64}, {"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024},
		{"ic11", 32}, {"ic12", 64}, {"ic13", 256}, {"ic14", 512},
	}
	var body bytes.Buffer
	for _, e := range entries {
		data := renders[e.size]
		body.WriteString(e.kind)
		_ = binary.Write(&body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

// writeSyso writes the resource objects the Go linker folds into otakase.exe.
// go build picks a .syso up by its _windows_<arch> suffix, so other platforms
// never see them.
func writeSyso(dir string, images []image.Image) {
	ico, err := winres.NewIconFromImages(images)
	if err != nil {
		log.Fatal(err)
	}
	rs := winres.ResourceSet{}
	if err := rs.SetIcon(winres.Name("APPICON"), ico); err != nil {
		log.Fatal(err)
	}
	for _, arch := range []struct {
		name string
		arch winres.Arch
	}{{"amd64", winres.ArchAMD64}, {"arm64", winres.ArchARM64}} {
		var buf bytes.Buffer
		if err := rs.WriteObject(&buf, arch.arch); err != nil {
			log.Fatal(err)
		}
		write(filepath.Join(dir, "rsrc_windows_"+arch.name+".syso"), buf.Bytes())
	}
}

func write(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote", path)
}

func copyFile(from, to string) {
	data, err := os.ReadFile(from)
	if err != nil {
		log.Fatal(err)
	}
	write(to, data)
}
