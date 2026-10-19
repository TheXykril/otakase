package internal

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

var appVersion = "dev"

// SetVersion records the version of the running build.
func SetVersion(version string) {
	if version = strings.TrimSpace(version); version != "" {
		appVersion = version
	}
}

// Version is the version of the running build, "dev" when it has none.
func Version() string {
	if v := appVersion; v != "" {
		return v
	}
	return "dev"
}

// DisplayVersion is the version as menus show it: "v26.1.0" for a release,
// the raw string for anything else.
func DisplayVersion() string {
	version := Version()
	if versionNumbers(version) == nil {
		return version
	}
	return "v" + strings.TrimLeft(version, "vV")
}

// versionNumbers reads "26.1.0", "v2.0.3" or "26.1.0-dev.4" as three
// numbers; anything after a - or + is ignored and missing parts are zero.
// Releases are numbered YY.DROP.HOTFIX, so the year outranks the old 2.x
// numbers with no special case. It returns nil for "dev" and anything else
// that is not a version.
func versionNumbers(version string) []int {
	version = strings.TrimLeft(strings.TrimSpace(version), "vV")
	if cut := strings.IndexAny(version, "-+"); cut >= 0 {
		version = version[:cut]
	}
	if version == "" {
		return nil
	}
	fields := strings.Split(version, ".")
	if len(fields) > 3 {
		fields = fields[:3]
	}
	parts := make([]int, 3)
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return nil
		}
		parts[i] = n
	}
	return parts
}

// versionOrder returns -1, 0 or 1 as a is older than, the same as or newer
// than b. An empty version is older than everything; "dev" and other
// non-versions are newer than any release, so a development build is never
// offered an older release or migrated backwards.
func versionOrder(a, b string) int {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	na, nb := versionNumbers(a), versionNumbers(b)
	switch {
	case na != nil && nb != nil:
		return slices.Compare(na, nb)
	case na == nil && nb == nil:
		return strings.Compare(a, b)
	}
	return cmp.Compare(versionRank(a, na), versionRank(b, nb))
}

// versionRank orders a release against something that isn't one: an empty
// string first, the release, then "dev" and the like.
func versionRank(raw string, nums []int) int {
	switch {
	case nums != nil:
		return 1
	case raw == "":
		return 0
	}
	return 2
}

func versionLess(a, b string) bool { return versionOrder(a, b) < 0 }

func versionAtMost(a, b string) bool { return versionOrder(a, b) <= 0 }
