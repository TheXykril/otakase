package theme

import (
	"strings"
	"testing"
)

// The point of layering: keep the desktop's theme and change one colour in it.
func TestOverridesLayerOnWhatWasResolved(t *testing.T) {
	base := Builtin()
	base.Name = "solitude"

	got, problems := ApplyOverrides(base, "accent:#ff6188")
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if got.Accent != "#ff6188" {
		t.Errorf("the accent was not replaced: %q", got.Accent)
	}
	if got.Background != base.Background || got.Green != base.Green {
		t.Error("a colour that was not named should keep its value")
	}
	// The theme name no longer describes what is on screen.
	if !strings.Contains(got.Name, "solitude") || !strings.Contains(got.Name, "customised") {
		t.Errorf("the name should say it was changed, got %q", got.Name)
	}
}

// Several at once, in any spacing a hand-edited config might have.
func TestOverridesAcceptSeveralAndToleratePadding(t *testing.T) {
	got, problems := ApplyOverrides(Builtin(), "  accent : #ff6188 , GREEN:#a9dc76,,muted:#777 ")
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if got.Accent != "#ff6188" {
		t.Errorf("accent: %q", got.Accent)
	}
	if got.Green != "#a9dc76" {
		t.Errorf("green, written in capitals, was not applied: %q", got.Green)
	}
	// Short form expands, so everything downstream sees one shape.
	if got.Muted != "#777777" {
		t.Errorf("#777 should expand to #777777, got %q", got.Muted)
	}
}

// A typo should cost that colour, not the program.
func TestOneBadOverrideDoesNotLoseTheOthers(t *testing.T) {
	got, problems := ApplyOverrides(Builtin(), "accent:#ff6188,nonsense:#000000,green:notacolour,blue:#0000ff")

	if got.Accent != "#ff6188" || got.Blue != "#0000ff" {
		t.Errorf("valid overrides were lost alongside the invalid ones: %+v", got)
	}
	if len(problems) != 2 {
		t.Fatalf("expected two problems, got %d: %v", len(problems), problems)
	}
	// The messages have to be usable: name the offender, and say what is allowed.
	joined := problems[0].Error() + " " + problems[1].Error()
	if !strings.Contains(joined, "nonsense") || !strings.Contains(joined, "notacolour") {
		t.Errorf("the problems do not name what was wrong: %v", problems)
	}
	if !strings.Contains(problems[0].Error(), "accent") {
		t.Errorf("an unknown name should list the names that work: %v", problems[0])
	}
}

// Nothing configured must leave the palette exactly as it was, including its
// name -- which is what the log reports.
func TestNoOverridesChangesNothing(t *testing.T) {
	base := Builtin()
	base.Name = "solitude"
	for _, spec := range []string{"", "   ", ",,"} {
		got, problems := ApplyOverrides(base, spec)
		if len(problems) != 0 {
			t.Errorf("%q produced problems: %v", spec, problems)
		}
		if got != base {
			t.Errorf("%q changed the palette", spec)
		}
	}
}

// A pair with no colon is the likeliest way to mistype this.
func TestOverrideWithoutAColourIsReported(t *testing.T) {
	_, problems := ApplyOverrides(Builtin(), "accent")
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "name:#rrggbb") {
		t.Errorf("expected a message showing the shape, got %v", problems)
	}
}

// Every name offered in an error message has to actually work, or the advice
// sends people in circles.
func TestEveryAdvertisedNameIsApplicable(t *testing.T) {
	for _, name := range OverrideNames() {
		got, problems := ApplyOverrides(Builtin(), name+":#123456")
		if len(problems) != 0 {
			t.Errorf("%s is offered but rejected: %v", name, problems)
			continue
		}
		if got == Builtin() {
			t.Errorf("%s is offered but changed nothing", name)
		}
	}
}
