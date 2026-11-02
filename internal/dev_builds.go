package internal

import (
	"strings"
)

// splitPrerelease splits "26.1.0-dev.12+abc" into "26.1.0" and "dev.12".
func splitPrerelease(version string) (base, pre string) {
	version = normalizeReleaseVersion(version)
	if i := strings.Index(version, "+"); i >= 0 {
		version = version[:i]
	}
	if i := strings.Index(version, "-"); i >= 0 {
		return version[:i], version[i+1:]
	}
	return version, ""
}
