package internal

import (
	"fmt"
	"os"
	"sync"
)

// PosterSource supplies rendered posters to the menu's detail pane.
//
// Drawing happens on every keystroke, so it has to be cheap and it must never
// block: encoding a PNG into an escape sequence is slow enough to feel, and
// downloading one would stall the interface entirely. So a rendered poster is
// kept once made, a cover that is not yet on disk is fetched in the background
// and simply absent until it arrives, and the pane renders without it in the
// meantime.
type PosterSource struct {
	protocol TerminalImageProtocol
	cols     int
	rows     int

	mu          sync.Mutex
	rendered    map[string]string
	fetching    map[string]bool
	loggedFirst bool
}

// NewPosterSource makes a source sized to a pane. Width and height are in
// pixels, which is what the image protocols speak, not terminal cells.
// NewPosterSource makes a source drawing into a block of cols by rows terminal
// cells. Cells, not pixels: a picture at its natural size occupies no cells as
// far as the layout is concerned, and every redraw then miscounts.
func NewPosterSource(protocol TerminalImageProtocol, cols, rows int) *PosterSource {
	return &PosterSource{
		protocol: protocol,
		cols:     cols,
		rows:     rows,
		rendered: map[string]string{},
		fetching: map[string]bool{},
	}
}

// Poster returns the escape sequence for an entry's cover, or an empty string
// when there is nothing to draw yet. It never blocks and never fails: a pane
// without a picture is a fine outcome, an interface that pauses is not.
func (s *PosterSource) Poster(option SelectionOption) string {
	// Only Kitty can be told to occupy a known block of cells. The others draw
	// at their natural size, which is the failure this path exists to avoid, so
	// they get no poster rather than a broken one.
	if s == nil || s.protocol != TerminalImageKitty || option.Thumbnail == "" {
		return ""
	}
	path, err := coverCachePath(option.Thumbnail)
	if err != nil {
		return ""
	}

	s.mu.Lock()
	if cached, ok := s.rendered[path]; ok {
		s.mu.Unlock()
		return cached
	}
	alreadyFetching := s.fetching[path]
	s.mu.Unlock()

	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		out, err := RenderTerminalImageCells(path, s.cols, s.rows)
		if err != nil {
			Log(fmt.Sprintf("Poster: %s failed to render: %v", path, err))
			// Remember the failure as an empty render so a corrupt file is not
			// decoded again on every keypress.
			out = ""
		} else if !s.loggedFirst {
			s.loggedFirst = true
			Log(fmt.Sprintf("Poster: drew the first cover, %d bytes of %s escape sequence from a %d byte file",
				len(out), s.protocol, info.Size()))
		}
		if out != "" {
			// Delete first, on the same line, so a frame can never erase the
			// poster and then skip redrawing it.
			out = DeleteAllImages + out
		}
		s.mu.Lock()
		s.rendered[path] = out
		s.mu.Unlock()
		return out
	}

	if !alreadyFetching {
		s.mu.Lock()
		s.fetching[path] = true
		s.mu.Unlock()
		go func() {
			if _, err := downloadToCache(option.Thumbnail); err != nil {
				Log(fmt.Sprintf("Poster: could not fetch %s: %v", option.Thumbnail, err))
			}
			s.mu.Lock()
			delete(s.fetching, path)
			s.mu.Unlock()
		}()
	}
	return ""
}

// Rows is how many terminal rows a poster occupies, so the pane can reserve
// them. lipgloss counts the escape sequence as a single line, being invisible
// to it, while the terminal paints it over this many.
func (s *PosterSource) Rows() int {
	if s == nil {
		return 0
	}
	return s.rows
}
