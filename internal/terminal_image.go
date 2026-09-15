package internal

import (
	"bytes"
	"fmt"
	"image"
	"image/color/palette"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"

	"github.com/BourgeoisBear/rasterm"
	"golang.org/x/image/draw"
)

// TerminalImageProtocol is the way a terminal accepts pictures, if it accepts
// them at all. Kitty's protocol is spoken by Kitty and Ghostty; iTerm2's by
// iTerm2 and WezTerm; Sixel by a long tail of others.
type TerminalImageProtocol int

const (
	TerminalImageNone TerminalImageProtocol = iota
	TerminalImageKitty
	TerminalImageIterm
	TerminalImageSixel
)

func (p TerminalImageProtocol) String() string {
	switch p {
	case TerminalImageKitty:
		return "kitty"
	case TerminalImageIterm:
		return "iterm"
	case TerminalImageSixel:
		return "sixel"
	default:
		return "none"
	}
}

// DetectTerminalImageProtocol reports how this terminal can draw a picture.
//
// tmux and screen are excluded deliberately rather than attempted: they
// multiplex the escape stream, and an image protocol sent through them is at
// best drawn in the wrong pane and at worst leaves the terminal in a state the
// user has to reset by hand. Text is the honest answer there.
func DetectTerminalImageProtocol() TerminalImageProtocol {
	if insideMultiplexer() {
		return TerminalImageNone
	}
	if rasterm.IsKittyCapable() {
		return TerminalImageKitty
	}
	if rasterm.IsItermCapable() {
		return TerminalImageIterm
	}
	if ok, err := rasterm.IsSixelCapable(); err == nil && ok {
		return TerminalImageSixel
	}
	return TerminalImageNone
}

// DeleteAllImages removes every image the terminal is currently showing.
//
// Kitty images are not erased by drawing over them or by clearing the screen,
// so without this every poster ever drawn stays on screen. It is emitted on the
// same line as the image that replaces it, which matters: Bubble Tea redraws
// only the lines that changed, and splitting the delete from the draw would let
// a frame erase the poster and then skip putting it back.
const DeleteAllImages = "\x1b_Ga=d\x1b\\"

// RenderTerminalImageCells draws an image into an exact block of terminal
// cells.
//
// Sizing in cells rather than pixels is what makes an image usable inside a TUI.
// A picture placed at its natural size occupies no cells at all as far as the
// program is concerned, so the layout cannot reserve room for it and every
// redraw miscounts the lines it wrote. With c= and r= the terminal scales the
// image into a known block, and the caller can leave exactly that many rows
// empty beneath it.
//
// Only the Kitty protocol is offered here. Sixel has no cell sizing at all, and
// iTerm2's is a different mechanism; drawing either at its natural size is the
// failure this function exists to avoid.
func RenderTerminalImageCells(path string, cols, rows int) (string, error) {
	if cols <= 0 || rows <= 0 {
		return "", fmt.Errorf("image must have a positive size in cells, got %dx%d", cols, rows)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	source, _, err := image.Decode(file)
	if err != nil {
		return "", fmt.Errorf("decoding %s: %w", path, err)
	}
	// Scale down before sending. The terminal would scale it anyway, and a
	// cover is several hundred kilobytes that would otherwise be base64'd into
	// the frame on every redraw.
	scaled := scaleToFit(source, cols*12, rows*24)

	var out bytes.Buffer
	if err := rasterm.KittyWriteImage(&out, scaled, rasterm.KittyImgOpts{
		DstCols: uint32(cols),
		DstRows: uint32(rows),
	}); err != nil {
		return "", fmt.Errorf("encoding for kitty: %w", err)
	}
	return out.String(), nil
}

func RenderTerminalImage(path string, protocol TerminalImageProtocol, maxWidth, maxHeight int) (string, error) {
	if protocol == TerminalImageNone {
		return "", nil
	}
	if maxWidth <= 0 || maxHeight <= 0 {
		return "", fmt.Errorf("image box must have a positive size, got %dx%d", maxWidth, maxHeight)
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	source, _, err := image.Decode(file)
	if err != nil {
		return "", fmt.Errorf("decoding %s: %w", path, err)
	}

	scaled := scaleToFit(source, maxWidth, maxHeight)

	var out bytes.Buffer
	switch protocol {
	case TerminalImageKitty:
		err = rasterm.KittyWriteImage(&out, scaled, rasterm.KittyImgOpts{})
	case TerminalImageIterm:
		err = rasterm.ItermWriteImage(&out, scaled)
	case TerminalImageSixel:
		err = rasterm.SixelWriteImage(&out, toPaletted(scaled))
	}
	if err != nil {
		return "", fmt.Errorf("encoding for %s: %w", protocol, err)
	}
	return out.String(), nil
}

// scaleToFit shrinks an image to sit inside the box without distorting it. An
// image already smaller than the box is left alone: scaling a poster up only
// makes it blurry.
func scaleToFit(src image.Image, maxWidth, maxHeight int) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return src
	}
	if width <= maxWidth && height <= maxHeight {
		return src
	}

	scale := float64(maxWidth) / float64(width)
	if vertical := float64(maxHeight) / float64(height); vertical < scale {
		scale = vertical
	}
	targetWidth := max(1, int(float64(width)*scale))
	targetHeight := max(1, int(float64(height)*scale))

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	return dst
}

// toPaletted converts for Sixel, which is an indexed-colour format and cannot
// take an RGBA image directly.
func toPaletted(src image.Image) *image.Paletted {
	if already, ok := src.(*image.Paletted); ok {
		return already
	}
	bounds := src.Bounds()
	dst := image.NewPaletted(bounds, palette.WebSafe)
	draw.FloydSteinberg.Draw(dst, bounds, src, bounds.Min)
	return dst
}

// EncodePNG is used by the tests to make a file to render, and by callers that
// need a cached poster on disk in a known format.
func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// insideMultiplexer reports whether a terminal multiplexer sits between this
// program and the real terminal.
//
// rasterm.IsTmuxScreen only tests whether TERM begins with "screen", which
// misses modern tmux entirely: it sets TERM to tmux-256color, and a passed
// through TERM may name the outer terminal instead. The environment variables
// each multiplexer sets for its children are the reliable signal, so both are
// checked.
func insideMultiplexer() bool {
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		return true
	}
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	return strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux")
}
