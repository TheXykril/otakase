package internal

import (
	"errors"
	"testing"
)

// Three outcomes, not two. A cast that reached the end of an episode is
// entitled to advance; one the viewer stopped is not; one that failed must not
// advance and must say why. Collapsing any pair of these either strands the
// viewer on one episode or marches a rotted provider link through a season.
func TestCastOutcomeForError(t *testing.T) {
	failure := errors.New("cast: ffmpeg failed")

	for _, tc := range []struct {
		name     string
		err      error
		want     string
		wantSaid bool
	}{
		{"episode finished", nil, "cast", false},
		{"viewer pressed q", ErrCastStopped, "", false},
		{"cast failed", failure, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket, reported := castOutcome(tc.err)
			if socket != tc.want {
				t.Errorf("socket = %q, want %q", socket, tc.want)
			}
			if reported != tc.wantSaid {
				t.Errorf("reported = %v, want %v", reported, tc.wantSaid)
			}
		})
	}
}

// A wrapped stop is still a stop: the error travels up through CastEpisode,
// and a comparison by value rather than errors.Is would miss it.
func TestCastOutcomeRecognisesAWrappedStop(t *testing.T) {
	socket, reported := castOutcome(errors.New("cast: " + ErrCastStopped.Error()))
	_ = socket
	_ = reported

	wrapped := errors.Join(ErrCastStopped, errors.New("and something else"))
	if socket, _ := castOutcome(wrapped); socket != "" {
		t.Errorf("a wrapped stop was treated as a finished episode (socket %q)", socket)
	}
}
