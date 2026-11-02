package internal

import (
	"testing"

	"github.com/thexykril/otakase/internal/aniskip"
)

// Each result says which it is. Taking the first as the opening and the last as
// the ending sends the player to the credits a minute into an episode that has
// only an ending on file.
func TestAnEndingOnItsOwnIsNotReadAsAnOpening(t *testing.T) {
	times, ids := skipTimesFromEntries([]aniskip.Entry{
		{Kind: "ed", ID: "ed-id", Start: 1417, End: 1507},
	})

	if usableSpan(times.Op) {
		t.Errorf("an ending was filed as the opening: %+v", times.Op)
	}
	if times.Ed.Start != 1417 || times.Ed.End != 1507 || ids.Ed != "ed-id" {
		t.Errorf("the ending came out as %+v / %+v", times.Ed, ids)
	}
}

func TestBothHalvesKeepTheirOwnID(t *testing.T) {
	times, ids := skipTimesFromEntries([]aniskip.Entry{
		{Kind: "op", ID: "op-id", Start: 3, End: 93},
		{Kind: "ed", ID: "ed-id", Start: 1417, End: 1507},
	})

	if times.Op.End != 93 || times.Ed.End != 1507 {
		t.Errorf("times came out as %+v", times)
	}
	if ids.Op != "op-id" || ids.Ed != "ed-id" {
		t.Errorf("ids came out as %+v", ids)
	}
}
