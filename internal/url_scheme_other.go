//go:build !windows

package internal

// RegisterURLScheme does nothing here: Linux registers otakase:// through the
// desktop entry, and macOS cannot without an app bundle.
func RegisterURLScheme() {}
