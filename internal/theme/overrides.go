package theme

import (
	"fmt"
	"sort"
	"strings"
)

// Override names, as written in the config. They are the palette's own roles
// rather than its Go field names, because a config is read by people.
var overrideTargets = map[string]func(*Palette) *string{
	"background":        func(p *Palette) *string { return &p.Background },
	"background-dark":   func(p *Palette) *string { return &p.DarkBackground },
	"background-light":  func(p *Palette) *string { return &p.LighterBackground },
	"foreground":        func(p *Palette) *string { return &p.Foreground },
	"foreground-dark":   func(p *Palette) *string { return &p.DarkForeground },
	"foreground-bright": func(p *Palette) *string { return &p.BrightForeground },
	"accent":            func(p *Palette) *string { return &p.Accent },
	"selection":         func(p *Palette) *string { return &p.Selection },
	"muted":             func(p *Palette) *string { return &p.Muted },
	"red":               func(p *Palette) *string { return &p.Red },
	"green":             func(p *Palette) *string { return &p.Green },
	"yellow":            func(p *Palette) *string { return &p.Yellow },
	"blue":              func(p *Palette) *string { return &p.Blue },
	"magenta":           func(p *Palette) *string { return &p.Magenta },
	"cyan":              func(p *Palette) *string { return &p.Cyan },
}

// OverrideNames lists what may be overridden, in a stable order, for error
// messages and documentation.
func OverrideNames() []string {
	names := make([]string, 0, len(overrideTargets))
	for name := range overrideTargets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ApplyOverrides layers hand-picked colours onto a palette.
//
// Overrides sit on top of whatever was resolved rather than replacing it, which
// is the point: following the desktop theme and disliking one colour in it is a
// far more common wish than wanting to specify all fifteen. Anything not named
// keeps the value it had.
//
// The spec is a comma-separated list of name:#hex pairs. Every problem is
// reported and skipped rather than being fatal, because a typo in one colour
// should cost that colour and not the program.
func ApplyOverrides(palette Palette, spec string) (Palette, []error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return palette, nil
	}

	problems := []error{}
	applied := 0

	for _, pair := range strings.Split(spec, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		name, value, found := strings.Cut(pair, ":")
		if !found {
			problems = append(problems, fmt.Errorf("%q is not name:#rrggbb", pair))
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)

		target, known := overrideTargets[name]
		if !known {
			problems = append(problems, fmt.Errorf("no colour called %q; try one of %s",
				name, strings.Join(OverrideNames(), ", ")))
			continue
		}
		if !isHexColor(value) {
			problems = append(problems, fmt.Errorf("%s: %q is not a #rgb or #rrggbb colour", name, value))
			continue
		}
		*target(&palette) = normalizeHex(value)
		applied++
	}

	if applied > 0 {
		// The name no longer describes what is on screen once a colour in it has
		// been replaced, and the source line in the log would otherwise claim
		// the theme is untouched.
		palette.Name = palette.Name + " (customised)"
	}
	return palette, problems
}
