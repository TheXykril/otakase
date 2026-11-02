package internal

import (
	"testing"
)

// A dev build of 26.1.0 must still get 26.1.0's new settings and menu rows.
func TestMigrationsTreatDevBuildAsItsVersion(t *testing.T) {
	if versionOrder("26.1.0-dev.4", "26.1.0") != 0 {
		t.Fatal("migrations should count 26.1.0-dev.4 as 26.1.0")
	}
}
