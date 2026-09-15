package internal

import (
	"fmt"
	"strings"
)

// SkipRef is everything known about an episode that a skip service might key
// on. Each service identifies shows differently -- AniSkip by MyAnimeList id,
// Anime-Skip by its own id found from an AniList id, a provider by whatever it
// calls the show -- so the reference carries all of them rather than each
// source having to reach back for what it needs.
type SkipRef struct {
	MalID      int
	AniListID  int
	Episode    int
	Mode       string
	Provider   string
	ProviderID string
}

// SkipSource is one place intro and outro timings can come from.
//
// Returning found=false is the ordinary case, not a failure: most services know
// nothing about most episodes. An error means the service was asked and could
// not answer, which is worth logging but is equally not fatal.
type SkipSource interface {
	Name() string
	Lookup(ref SkipRef) (times SkipTimes, found bool, err error)
}

// SkipResolution records what was found and where each half came from, so a log
// can say "opening from anikoto, ending from aniskip" rather than leaving the
// user to guess why only half an episode skipped.
type SkipResolution struct {
	Times     SkipTimes
	OpSource  string
	EdSource  string
	Attempted []string
	Errors    []error
}

// Found reports whether anything usable was resolved at all.
func (r SkipResolution) Found() bool {
	return r.Times.Op.End > r.Times.Op.Start || r.Times.Ed.End > r.Times.Ed.Start
}

// Describe summarises the outcome for a log line.
func (r SkipResolution) Describe() string {
	if !r.Found() {
		return fmt.Sprintf("no skip times from %s", strings.Join(r.Attempted, ", "))
	}
	parts := []string{}
	if r.OpSource != "" {
		parts = append(parts, fmt.Sprintf("opening %d-%ds from %s", r.Times.Op.Start, r.Times.Op.End, r.OpSource))
	}
	if r.EdSource != "" {
		parts = append(parts, fmt.Sprintf("ending %d-%ds from %s", r.Times.Ed.Start, r.Times.Ed.End, r.EdSource))
	}
	return strings.Join(parts, ", ")
}

// ResolveSkipTimes asks each source in turn and takes the opening from the
// first that has one and the ending from the first that has one.
//
// They need not be the same source. AniSkip frequently knows an opening and not
// an ending, and a provider that ships timings with the stream often knows
// both; taking each half independently means a half-answer from one service is
// completed by the next rather than discarded. Asking stops once both halves
// are filled, so the cheapest source being first actually saves the requests.
func ResolveSkipTimes(ref SkipRef, sources ...SkipSource) SkipResolution {
	resolution := SkipResolution{}

	for _, source := range sources {
		if source == nil {
			continue
		}
		if resolution.OpSource != "" && resolution.EdSource != "" {
			break
		}
		resolution.Attempted = append(resolution.Attempted, source.Name())

		times, found, err := source.Lookup(ref)
		if err != nil {
			resolution.Errors = append(resolution.Errors, fmt.Errorf("%s: %w", source.Name(), err))
			continue
		}
		if !found {
			continue
		}
		if resolution.OpSource == "" && usableSpan(times.Op) {
			resolution.Times.Op = times.Op
			resolution.OpSource = source.Name()
		}
		if resolution.EdSource == "" && usableSpan(times.Ed) {
			resolution.Times.Ed = times.Ed
			resolution.EdSource = source.Name()
		}
	}
	return resolution
}

// usableSpan rejects a span that would send the player somewhere wrong. A zero
// span means "not known"; a reversed or negative one means the service answered
// with something broken, and skipping to it is worse than not skipping.
func usableSpan(span Skip) bool {
	return span.End > span.Start && span.End > 0 && span.Start >= 0
}
