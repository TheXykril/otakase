# Cast Idle/Rating Card Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a generated cover-art card on the cast device (instead of the device's own idle screen) during the mid-season next-episode countdown and the season-end rating prompt.

**Architecture:** Generalize `cast.Session.Play` to load arbitrary content types (adds `PlayImage`), add a pure image-composition helper that draws cover art + text into a PNG, and push that PNG to the device at the two points the terminal already prompts the viewer: `castAwaitNextEpisode` (session still connected) and `HandleLastEpisodeCompletion`'s cast-window branch (session already torn down, so this path reconnects briefly). Every push is best-effort: any failure is logged and the existing terminal-only prompt behavior continues unchanged.

**Tech Stack:** Go 1.26, `golang.org/x/image` (new dependency: `font`, `font/basicfont`, `draw`, `math/fixed`) for text rendering and image scaling, stdlib `image`/`image/png`/`image/jpeg`.

**Spec:** No separate spec file — this is a bounded change (existing `internal/cast/` and `internal/cast_playback.go` flows, no new subsystem), so brainstorming's bounded path went straight from an in-chat design to this plan. The approved design is summarized above and in the two paragraphs below; there is no other document to consult.

Approved design recap: (1) `cast.Session` gets a content-type-generic load path; (2) a new pure `internal/cast_idle_card.go` composes a 1280x720 PNG — cover art stretched to fill, a dark band across the bottom, up to two lines of white bitmap-font text — falling back to a plain dark background if the cover art can't be fetched; (3) `CastEpisode` pushes an "Episode N watched — Episode N+1 up next" card before the mid-season countdown; (4) `HandleLastEpisodeCompletion`'s non-interactive cast-window branch reconnects to `config.CastDevice`, pushes a "Rate this anime — check your terminal" card, runs the existing score picker unchanged, then disconnects.

## Global Constraints

- Go 1.26; build with `CGO_ENABLED=0 go build -mod=vendor` (dependencies are vendored).
- `go mod tidy` requires `GOFLAGS=-mod=mod`; after tidying, run `go mod vendor` to update `vendor/`.
- `./Build/ci-local` (or at minimum `./Build/ci-local --quick`) must pass before any task is considered done.
- No `Co-authored-by:` or `Claude-Session:` trailers in commit messages (this project's CLAUDE.md forbids them; it overrides the session-level attribution reminder).
- No `release:` in any commit message — releases only happen when the user explicitly asks.
- Do not rename anything under the storage directory (`curd_history.txt`, `curd_version`, `curd_id`) — unrelated to this feature, but touched files live nearby.
- Casting behavior that needs a real device is verified through `docs/casting-verification.md`, not through unit tests — this plan adds checklist items there rather than inventing unit tests for code paths that need real hardware.

## Review Focus

- **Cover-art fetch fails (404, timeout, non-image content-type, or `anime.CoverImage == ""`).** The card must still render — title and message on a plain dark background — never block the countdown or the rating prompt. Covered in Task 2.
- **`config.CastDevice` is empty, or the named device is off/renamed by the time the season ends.** `pushCastRatingCard` must no-op silently (no error surfaced, no hang) rather than delaying `HandleLastEpisodeCompletion`. Covered in Task 5.
- **Two discovered devices share the same name.** `findCastDeviceByName` returns the first match, matching `chooseCastDevice`'s existing exact-match behavior — not a new inconsistency. Covered in Task 3.
- **Idle card write fails** (scratch directory unwritable, disk full) during the mid-season push. Must log and continue straight to the countdown, not abort the cast. Covered in Task 4 (via manual checklist, since `CastEpisode` itself needs a device to exercise).
- **The last episode of a season** (`castSeasonFinished` true) must not get a pointless mid-season "up next" push right before the device disconnects — only `pushCastRatingCard`'s season-end card applies there. Covered in Task 4.

---

## File Structure

- `internal/cast/session.go` — modify: generalize `Play`/`PlayWithin` onto a shared content-type-aware loader; add `PlayImage`.
- `internal/cast_idle_card.go` — create: pure image composition (`composeIdleCard`) + cover-art fetch (`fetchCoverImage`) + the glue that combines them (`buildIdleCardPNG`). No device or cast-session code here.
- `internal/cast_idle_card_test.go` — create: tests for the above, using `httptest` for the fetch and a synthetic `image.Image` for composition.
- `internal/cast_playback.go` — modify: extract `findCastDeviceByName`, use it from `chooseCastDevice`; wire the mid-season idle-card push into `CastEpisode`.
- `internal/cast_playback_test.go` — modify: add a test for `findCastDeviceByName`.
- `internal/cast_rating_card.go` — create: `pushCastRatingCard`, the season-end reconnect-and-push helper.
- `internal/cast_rating_card_test.go` — create: tests for the guard clauses that don't need a device (nil anime, empty `CastDevice`).
- `internal/otakase.go` — modify: call `pushCastRatingCard` in `HandleLastEpisodeCompletion`'s `castWindowNonInteractive()` branch.
- `docs/casting-verification.md` — modify: add a new numbered section covering the idle/rating card on real hardware.
- `go.mod`, `go.sum`, `vendor/` — modify: add `golang.org/x/image`.

---

### Task 1: Vendor `golang.org/x/image` and generalize `cast.Session`'s load path

**Files:**
- Modify: `internal/cast/session.go`
- Modify: `go.mod`, `go.sum`, `vendor/modules.txt`, `vendor/golang.org/x/image/**` (via tooling, not hand-edited)

**Interfaces:**
- Consumes: nothing new.
- Produces: `func (s *Session) PlayImage(url string) error` — loads `url` on the device as `image/png`, using the same launch-retry behavior as `Play`. `Play`/`PlayWithin` keep their existing signatures and behavior.

- [ ] **Step 1: Add the dependency**

```bash
cd /home/xykril/Work/Otakase
GOFLAGS=-mod=mod go get golang.org/x/image@latest
GOFLAGS=-mod=mod go mod tidy
go mod vendor
```

- [ ] **Step 2: Confirm the vendor tree is consistent**

Run: `go build -mod=vendor ./...`
Expected: builds cleanly (the new dependency isn't used by any code yet, so this just proves vendoring worked).

- [ ] **Step 3: Generalize the loader in `internal/cast/session.go`**

Replace the existing `Play`/`PlayWithin` block (currently hardcoding `"application/x-mpegURL"`) with:

```go
// Play loads a URL on the device and starts it.
//
// The content type is stated rather than guessed: the stream is served from a
// directory with no meaningful extension handling, and the Default Media
// Receiver picks its player from this.
//
// Play returns as soon as the device has been told to start, rather than
// waiting for the episode to finish: the detach flag passed to Load is what
// makes that happen, and the caller's polling loop -- watched threshold,
// opening/ending skips, deferred Stop -- depends on Play not blocking.
func (s *Session) Play(url string) error {
	return s.PlayWithin(url, DefaultLaunchTimeout)
}

// PlayWithin loads a URL as an HLS stream, retrying the launch until the
// deadline passes.
//
// Only the timeout is retried. A device that refuses the stream refuses it the
// same way every time, so retrying that would turn a clear failure into a long
// one.
func (s *Session) PlayWithin(url string, within time.Duration) error {
	return s.loadWithin(url, "application/x-mpegURL", within)
}

// PlayImage loads a still image on the device -- the idle/rating card shown
// between episodes and at season end. It uses the same launch-retry behavior
// as PlayWithin: a device waking from standby needs the same grace whether
// what it is asked to show is a stream or a picture.
func (s *Session) PlayImage(url string) error {
	return s.loadWithin(url, "image/png", DefaultLaunchTimeout)
}

// loadWithin is the shared retry loop under Play and PlayImage, parameterized
// on content type so a still image does not have to pretend to be HLS.
func (s *Session) loadWithin(url, contentType string, within time.Duration) error {
	return playWithin(func(u string) error {
		return s.app.Load(u, 0, contentType, false, true, false)
	}, url, within)
}
```

Remove the old `PlayWithin` body (the one directly calling `s.app.Load` with the hardcoded content type) — it's replaced by `loadWithin`.

- [ ] **Step 4: Run the existing cast package tests**

Run: `go test ./internal/cast/... -run . -v -count=1`
Expected: all pass, including `TestPlayRetriesASlowLaunch`, `TestPlayDoesNotRetryARefusal`, `TestPlayGivesUpAtTheDeadline` (these test `playWithin` directly and are unaffected by the refactor).

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum vendor/ internal/cast/session.go
git commit -m "feat(cast): generalize Session.Play to support loading images"
```

---

### Task 2: Idle card image composition and cover-art fetch

**Files:**
- Create: `internal/cast_idle_card.go`
- Create: `internal/cast_idle_card_test.go`

**Interfaces:**
- Consumes: `sharedHTTPClient` (`internal/http_client.go`), `httpStatusOK` (`internal/http_client.go`), `Log` (package-level logger already used throughout `internal/`).
- Produces:
  - `func composeIdleCard(cover image.Image, lines []string) []byte` — pure; `cover` may be `nil`.
  - `func fetchCoverImage(url string) (image.Image, error)` — network; returns an error for empty `url`, a failed request, a non-2xx status, or an undecodable body.
  - `func buildIdleCardPNG(coverURL string, lines []string) []byte` — combines the two; never errors, always returns a valid PNG.

- [ ] **Step 1: Write the failing tests**

```go
package internal

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComposeIdleCardProducesAValidPNGWithNoCover(t *testing.T) {
	data := composeIdleCard(nil, []string{"Attack on Titan", "Episode 5 watched"})

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("composeIdleCard did not produce a decodable PNG: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != idleCardWidth || bounds.Dy() != idleCardHeight {
		t.Fatalf("got %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), idleCardWidth, idleCardHeight)
	}
}

func TestComposeIdleCardStretchesTheCoverToFillTheFrame(t *testing.T) {
	cover := image.NewRGBA(image.Rect(0, 0, 40, 30))
	// A distinctive color away from the card's plain background and dark
	// band, so a pixel sampled from the top-left corner (outside the band)
	// proves the cover was actually drawn rather than skipped.
	fill := color.RGBA{R: 200, G: 10, B: 10, A: 255}
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			cover.Set(x, y, fill)
		}
	}

	data := composeIdleCard(cover, []string{"Title"})
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("composeIdleCard did not produce a decodable PNG: %v", err)
	}

	r, g, b, _ := img.At(10, 10).RGBA()
	if r>>8 < 150 || g>>8 > 60 || b>>8 > 60 {
		t.Fatalf("top-left pixel is %d,%d,%d -- cover was not drawn", r>>8, g>>8, b>>8)
	}
}

func TestFetchCoverImageRejectsAnEmptyURL(t *testing.T) {
	if _, err := fetchCoverImage(""); err == nil {
		t.Fatal("expected an error for an empty URL")
	}
}

func TestFetchCoverImageDecodesAServedImage(t *testing.T) {
	cover := image.NewRGBA(image.Rect(0, 0, 10, 10))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, cover)
	}))
	defer server.Close()

	img, err := fetchCoverImage(server.URL)
	if err != nil {
		t.Fatalf("fetchCoverImage returned an error: %v", err)
	}
	if img.Bounds().Dx() != 10 || img.Bounds().Dy() != 10 {
		t.Fatalf("got %dx%d, want 10x10", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestFetchCoverImageReportsA404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	if _, err := fetchCoverImage(server.URL); err == nil {
		t.Fatal("expected an error for a 404")
	}
}

func TestBuildIdleCardPNGNeverErrorsWhenTheCoverCannotBeFetched(t *testing.T) {
	data := buildIdleCardPNG("http://127.0.0.1:1/no-such-host", []string{"Title", "Message"})
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("buildIdleCardPNG did not produce a decodable PNG on a fetch failure: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to confirm they fail**

Run: `go test ./internal/ -run 'TestComposeIdleCard|TestFetchCoverImage|TestBuildIdleCardPNG' -v -count=1`
Expected: FAIL — `composeIdleCard`, `fetchCoverImage`, `buildIdleCardPNG`, `idleCardWidth`, `idleCardHeight` undefined.

- [ ] **Step 3: Implement `internal/cast_idle_card.go`**

```go
package internal

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// idleCardWidth and idleCardHeight are the frame size pushed to the device.
// 1280x720 is a plain 16:9 the Default Media Receiver displays at full
// screen without letterboxing on any device this project has been run
// against.
const (
	idleCardWidth      = 1280
	idleCardHeight     = 720
	idleCardBandHeight = 160
)

// composeIdleCard draws cover art, stretched to fill the frame, under a dark
// band carrying up to a few lines of text.
//
// cover may be nil -- a fetch failure must still produce a screen, not no
// screen at all -- in which case the frame is a plain dark background.
//
// The cover is stretched rather than cropped to preserve aspect ratio: a
// centered crop needs source-and-destination aspect math this feature does
// not need to get exactly right, and a slightly stretched cover for ten
// seconds between episodes is not worth the extra code.
func composeIdleCard(cover image.Image, lines []string) []byte {
	canvas := image.NewRGBA(image.Rect(0, 0, idleCardWidth, idleCardHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{R: 20, G: 20, B: 20, A: 255}), image.Point{}, draw.Src)
	if cover != nil {
		xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), cover, cover.Bounds(), xdraw.Over, nil)
	}

	band := image.Rect(0, idleCardHeight-idleCardBandHeight, idleCardWidth, idleCardHeight)
	draw.Draw(canvas, band, image.NewUniform(color.RGBA{A: 190}), image.Point{}, draw.Over)

	face := basicfont.Face7x13
	lineHeight := face.Metrics().Height.Ceil() + 6
	y := idleCardHeight - idleCardBandHeight + 30
	for _, line := range lines {
		drawer := &font.Drawer{
			Dst:  canvas,
			Src:  image.White,
			Face: face,
			Dot:  fixed.Point26_6{X: fixed.I(40), Y: fixed.I(y)},
		}
		drawer.DrawString(line)
		y += lineHeight
	}

	var buf bytes.Buffer
	// png.Encode only errors if the writer fails; bytes.Buffer's Write never
	// does, so there is nothing a caller could do with this error.
	_ = png.Encode(&buf, canvas)
	return buf.Bytes()
}

// fetchCoverImage downloads and decodes a cover art URL.
//
// Any failure here is meant to be logged and treated as non-fatal by the
// caller -- buildIdleCardPNG is what callers actually use, and it never
// fails even when this does.
func fetchCoverImage(url string) (image.Image, error) {
	if url == "" {
		return nil, fmt.Errorf("cast: no cover image URL")
	}
	resp, err := sharedHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("cast: could not fetch cover image: %w", err)
	}
	defer resp.Body.Close()
	if !httpStatusOK(resp.StatusCode) {
		return nil, fmt.Errorf("cast: cover image request failed with status %d", resp.StatusCode)
	}
	// 10MB is far more than any AniList/MAL cover art needs; it exists to
	// bound a misbehaving or malicious response, not real covers.
	img, _, err := image.Decode(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("cast: could not decode cover image: %w", err)
	}
	return img, nil
}

// buildIdleCardPNG is what callers use: cover art plus lines of text,
// encoded as a PNG. It never returns an error -- a cover that could not be
// fetched still produces a card, just without the picture, because the
// alternative is no screen at all between episodes.
func buildIdleCardPNG(coverURL string, lines []string) []byte {
	cover, err := fetchCoverImage(coverURL)
	if err != nil {
		Log(fmt.Sprintf("cast: idle card cover image unavailable: %v", err))
	}
	return composeIdleCard(cover, lines)
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `go test ./internal/ -run 'TestComposeIdleCard|TestFetchCoverImage|TestBuildIdleCardPNG' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cast_idle_card.go internal/cast_idle_card_test.go
git commit -m "feat(cast): compose an idle card image from cover art and text"
```

---

### Task 3: Extract `findCastDeviceByName` and use it from `chooseCastDevice`

**Files:**
- Modify: `internal/cast_playback.go`
- Modify: `internal/cast_playback_test.go`

**Interfaces:**
- Consumes: `cast.Device` (`internal/cast/device.go`).
- Produces: `func findCastDeviceByName(devices []cast.Device, name string) (cast.Device, bool)`, used by `chooseCastDevice` (Task 3) and `pushCastRatingCard` (Task 5).

- [ ] **Step 1: Write the failing test**

Add to `internal/cast_playback_test.go`:

```go
func TestFindCastDeviceByNameMatchesExactly(t *testing.T) {
	devices := []cast.Device{
		{Name: "Living Room TV"},
		{Name: "Bedroom TV"},
	}

	got, ok := findCastDeviceByName(devices, "Bedroom TV")
	if !ok || got.Name != "Bedroom TV" {
		t.Fatalf("got %+v, %v; want Bedroom TV, true", got, ok)
	}
}

func TestFindCastDeviceByNameReportsNoMatch(t *testing.T) {
	devices := []cast.Device{{Name: "Living Room TV"}}

	if _, ok := findCastDeviceByName(devices, "Kitchen TV"); ok {
		t.Fatal("expected no match")
	}
}

func TestFindCastDeviceByNameTakesTheFirstOfDuplicates(t *testing.T) {
	first := cast.Device{Name: "TV", UUID: "first"}
	devices := []cast.Device{first, {Name: "TV", UUID: "second"}}

	got, ok := findCastDeviceByName(devices, "TV")
	if !ok || got.UUID != "first" {
		t.Fatalf("got %+v, %v; want the first duplicate", got, ok)
	}
}
```

- [ ] **Step 2: Run the tests to confirm they fail**

Run: `go test ./internal/ -run TestFindCastDeviceByName -v -count=1`
Expected: FAIL — `findCastDeviceByName` undefined.

- [ ] **Step 3: Add the helper and use it in `chooseCastDevice`**

Add above `chooseCastDevice` in `internal/cast_playback.go`:

```go
// findCastDeviceByName looks for an exact name match among discovered
// devices, taking the first if more than one answers with the same name.
func findCastDeviceByName(devices []cast.Device, name string) (cast.Device, bool) {
	for _, device := range devices {
		if device.Name == name {
			return device, true
		}
	}
	return cast.Device{}, false
}
```

Inside `chooseCastDevice`, replace the manual loop:

```go
	configuredMissing := false
	if configured := config.CastDevice; configured != "" {
		for _, device := range devices {
			if device.Name == configured {
				return device, nil
			}
		}
		Out(fmt.Sprintf("%q was not found; pick another device.", configured))
		configuredMissing = true
	}
```

with:

```go
	configuredMissing := false
	if configured := config.CastDevice; configured != "" {
		if device, ok := findCastDeviceByName(devices, configured); ok {
			return device, nil
		}
		Out(fmt.Sprintf("%q was not found; pick another device.", configured))
		configuredMissing = true
	}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `go test ./internal/ -run 'TestFindCastDeviceByName|TestChooseCastDevice' -v -count=1`
Expected: PASS. (If no `TestChooseCastDevice*` tests exist yet, running just `TestFindCastDeviceByName` is enough — the point of this step is confirming the refactor didn't break device selection, so also run the full package: `go test ./internal/... -count=1`.)

- [ ] **Step 5: Commit**

```bash
git add internal/cast_playback.go internal/cast_playback_test.go
git commit -m "refactor(cast): extract findCastDeviceByName from chooseCastDevice"
```

---

### Task 4: Push the idle card mid-season, before the next-episode countdown

**Files:**
- Modify: `internal/cast_playback.go`
- Modify: `docs/casting-verification.md`

**Interfaces:**
- Consumes: `buildIdleCardPNG` (Task 2), `s.PlayImage` (Task 1), `castSeasonFinished`, `castNextEpisodeNumber` (`internal/cast_next_episode.go`, unchanged).
- Produces: nothing new for later tasks — this is glue inside `CastEpisode`.

- [ ] **Step 1: Add the push in `CastEpisode`**

In `internal/cast_playback.go`, in `CastEpisode`, immediately before the existing:

```go
	if err := watchCastWithControls(config, anime, seeking, srv, remuxes, device, commands, durationEstimated); err != nil {
		return err
	}
```
(this call stays exactly as-is), find the block right after it:

```go
	if !castAwaitNextEpisode(config, anime, panel, commands) {
		return ErrCastStopped
	}
	return nil
```

Replace it with:

```go
	// Shown before the countdown starts, not during it: the device player
	// would otherwise sit on the Default Media Receiver's own idle screen for
	// the whole countdown, which looks like the cast died rather than like an
	// episode that just finished. Skipped when the season is over -- the
	// device is about to be disconnected by this function's teardown, and
	// pushCastRatingCard (season end) shows its own card instead.
	if !castSeasonFinished(anime) {
		next := castNextEpisodeNumber(config, anime)
		lines := []string{
			GetAnimeName(*anime),
			fmt.Sprintf("Episode %d watched — Episode %d up next", anime.Ep.Number, next),
		}
		card := buildIdleCardPNG(anime.CoverImage, lines)
		idlePath := filepath.Join(streamDir, "idle.png")
		if err := os.WriteFile(idlePath, card, 0o644); err != nil {
			Log(fmt.Sprintf("cast: could not write the idle card: %v", err))
		} else if err := s.PlayImage(srv.URL("idle.png")); err != nil {
			Log(fmt.Sprintf("cast: could not show the idle card: %v", err))
		}
	}

	if !castAwaitNextEpisode(config, anime, panel, commands) {
		return ErrCastStopped
	}
	return nil
```

- [ ] **Step 2: Build and run the full package test suite**

Run: `go build -mod=vendor ./... && go test ./internal/... -count=1`
Expected: builds and all existing tests still pass. `CastEpisode` itself has no unit test (it needs a real device), so this step is a regression check on everything around it, not new coverage.

- [ ] **Step 3: Add a manual verification item**

In `docs/casting-verification.md`, under `## 6. Continuing to the next episode`, add:

```markdown
- [ ] **Between episodes, the TV shows the idle card** — cover art, the
      anime's title, and "Episode N watched — Episode N+1 up next" — instead
      of the Default Media Receiver's own idle screen, for the whole
      countdown.
- [ ] **At the end of a season, the idle card does not flash on screen right
      before the device disconnects.** The countdown is skipped entirely at
      season end (see `castSeasonFinished`), and so is this card.
```

- [ ] **Step 4: Commit**

```bash
git add internal/cast_playback.go docs/casting-verification.md
git commit -m "feat(cast): show an idle card on the device between episodes"
```

---

### Task 5: Season-end rating card via a brief reconnect

**Files:**
- Create: `internal/cast_rating_card.go`
- Create: `internal/cast_rating_card_test.go`
- Modify: `internal/otakase.go`
- Modify: `docs/casting-verification.md`

**Interfaces:**
- Consumes: `findCastDeviceByName` (Task 3), `buildIdleCardPNG` (Task 2), `cast.Discover`, `cast.Connect`, `cast.NewServer` (`internal/cast/`), `GetAnimeName` (`internal/otakase.go`, unchanged).
- Produces: `func pushCastRatingCard(config *Config, anime *Anime) func()` — the returned func tears down whatever was set up (or is a no-op if nothing was).

- [ ] **Step 1: Write the failing tests for the guard clauses**

```go
package internal

import "testing"

func TestPushCastRatingCardIsANoOpWithoutAConfiguredDevice(t *testing.T) {
	config := &Config{CastDevice: ""}
	anime := &Anime{}

	cleanup := pushCastRatingCard(config, anime)
	// Must not panic, hang, or touch the network: nothing to reach without a
	// configured device name.
	cleanup()
}

func TestPushCastRatingCardIsANoOpWithNilInputs(t *testing.T) {
	cleanup := pushCastRatingCard(nil, nil)
	cleanup()

	config := &Config{CastDevice: "Living Room TV"}
	cleanup = pushCastRatingCard(config, nil)
	cleanup()
}
```

- [ ] **Step 2: Run the tests to confirm they fail**

Run: `go test ./internal/ -run TestPushCastRatingCard -v -count=1`
Expected: FAIL — `pushCastRatingCard` undefined.

- [ ] **Step 3: Implement `internal/cast_rating_card.go`**

```go
package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/thexykril/otakase/internal/cast"
)

// pushCastRatingCard shows a rating card on the cast device for the season-end
// score prompt, reconnecting to it first.
//
// By the time HandleLastEpisodeCompletion runs, CastEpisode has already torn
// its session down -- the device is disconnected and its stream server is
// closed. Nothing here can reuse that connection, so this opens a small one
// of its own: discover, match the configured device, serve one PNG, load it,
// and hand back a cleanup that disconnects again.
//
// Every failure is logged and answered with a no-op cleanup. A viewer without
// a working reconnect still gets the terminal-only prompt exactly as before
// this feature existed -- the card is decoration, not a dependency of rating
// actually working.
func pushCastRatingCard(config *Config, anime *Anime) func() {
	noop := func() {}
	if config == nil || anime == nil || strings.TrimSpace(config.CastDevice) == "" {
		return noop
	}

	devices, err := cast.Discover(context.Background(), cast.DefaultDiscoveryTimeout)
	if err != nil {
		Log(fmt.Sprintf("cast: could not discover devices for the rating card: %v", err))
		return noop
	}
	device, ok := findCastDeviceByName(devices, config.CastDevice)
	if !ok {
		Log(fmt.Sprintf("cast: %q was not found for the rating card", config.CastDevice))
		return noop
	}

	scratchRoot := filepath.Join(os.ExpandEnv(config.StoragePath), "cast-scratch")
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		Log(fmt.Sprintf("cast: could not create a scratch directory for the rating card: %v", err))
		return noop
	}
	dir, err := os.MkdirTemp(scratchRoot, "otakase-rating-")
	if err != nil {
		Log(fmt.Sprintf("cast: could not create a scratch directory for the rating card: %v", err))
		return noop
	}
	cleanupDir := func() { _ = os.RemoveAll(dir) }

	srv, err := cast.NewServer(dir)
	if err != nil {
		Log(fmt.Sprintf("cast: could not serve the rating card: %v", err))
		cleanupDir()
		return noop
	}

	session, err := cast.Connect(device)
	if err != nil {
		Log(fmt.Sprintf("cast: could not reconnect to %s for the rating card: %v", device.Name, err))
		_ = srv.Close()
		cleanupDir()
		return noop
	}

	lines := []string{GetAnimeName(*anime), "Rate this anime — check your terminal"}
	card := buildIdleCardPNG(anime.CoverImage, lines)
	if err := os.WriteFile(filepath.Join(dir, "rating.png"), card, 0o644); err != nil {
		Log(fmt.Sprintf("cast: could not write the rating card: %v", err))
		_ = session.Stop()
		_ = srv.Close()
		cleanupDir()
		return noop
	}
	if err := session.PlayImage(srv.URL("rating.png")); err != nil {
		Log(fmt.Sprintf("cast: could not show the rating card: %v", err))
		// Fall through to the same cleanup as a successful push: the
		// connection and server were still opened and still need closing.
	}

	return func() {
		_ = session.Stop()
		_ = srv.Close()
		cleanupDir()
	}
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `go test ./internal/ -run TestPushCastRatingCard -v -count=1`
Expected: PASS

- [ ] **Step 5: Wire it into `HandleLastEpisodeCompletion`**

In `internal/otakase.go`, inside the `if castWindowNonInteractive() {` branch, change:

```go
		if castWindowNonInteractive() {
			// Asked in the panel rather than answered for the viewer, because the
			// alternative is a countdown whose only outcomes are "write a rating
			// nobody chose" and "write nothing at all". Arrows and one key work
			// from a sofa; typing a number does not. See
			// docs/cast-window-prompts.md.
			if score, given := castAwaitScore(userConfig, anime, castCountdownDuration); given {
```

to:

```go
		if castWindowNonInteractive() {
			// Asked in the panel rather than answered for the viewer, because the
			// alternative is a countdown whose only outcomes are "write a rating
			// nobody chose" and "write nothing at all". Arrows and one key work
			// from a sofa; typing a number does not. See
			// docs/cast-window-prompts.md.
			//
			// The cast session is already torn down by this point (CastEpisode
			// returned before HandleLastEpisodeCompletion runs), so this
			// reconnects just long enough to show a card -- decoration, not a
			// dependency: any failure here falls back to the terminal-only
			// prompt this already was.
			releaseRatingCard := pushCastRatingCard(userConfig, anime)
			if score, given := castAwaitScore(userConfig, anime, castCountdownDuration); given {
```

Then, at the end of that same `if castWindowNonInteractive() {` block — right after its closing `}` from the inner `if score, given := ...` / `else` pair, but still inside the outer `if castWindowNonInteractive() {` block — add the release call. Concretely, the block currently ends:

```go
			} else {
				castPanelSay(userConfig, anime, "No answer -- rating skipped.")
				summary = append(summary, "rating skipped (cast window)")
			}
		} else {
```

Change it to:

```go
			} else {
				castPanelSay(userConfig, anime, "No answer -- rating skipped.")
				summary = append(summary, "rating skipped (cast window)")
			}
			releaseRatingCard()
		} else {
```

- [ ] **Step 6: Build and run the full package test suite**

Run: `go build -mod=vendor ./... && go test ./internal/... -count=1`
Expected: builds and all tests pass.

- [ ] **Step 7: Add a manual verification item**

In `docs/casting-verification.md`, under `## 7. The spawned window answers for itself`, add:

```markdown
- [ ] **At season end, the TV reconnects and shows a rating card** — cover
      art, the anime's title, "Rate this anime — check your terminal" —
      while the terminal panel runs the actual score picker. Confirm the
      device returns to its own idle screen once a score is given or the
      window times out (not left showing the card forever).
- [ ] **Power the device off (or block it at the firewall) before the season
      ends.** The score picker in the terminal must still work exactly as
      before this feature existed -- no delay, no error shown to the
      viewer, just a log line about the reconnect failing.
```

- [ ] **Step 8: Commit**

```bash
git add internal/cast_rating_card.go internal/cast_rating_card_test.go internal/otakase.go docs/casting-verification.md
git commit -m "feat(cast): show a rating card on the device at season end"
```

---

### Task 6: Full gate

**Files:** none (verification only).

- [ ] **Step 1: Run the canonical gate**

Run: `./Build/ci-local`
Expected: `go mod tidy` reports no changes, `go vet` is clean, `go test -short -race` passes, both Linux builds succeed.

- [ ] **Step 2: Manual hardware pass**

Work through the two new checklist items added to `docs/casting-verification.md` (Task 4 Step 3, Task 5 Step 7) against a real Chromecast/Default Media Receiver device, per that document's existing setup instructions.

- [ ] **Step 3: Commit if `go mod tidy`/`go vet` touched anything**

Only if Step 1 produced file changes (it shouldn't, since Tasks 1–5 already ran tidy/vendor):

```bash
git add -A
git commit -m "chore: sync go.mod/vendor after ci-local"
```
