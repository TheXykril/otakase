package internal

import (
	"strings"
)

// normalizeReleaseVersion drops the v a release tag starts with.
func normalizeReleaseVersion(tag string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(tag), "v"), "V")
}
