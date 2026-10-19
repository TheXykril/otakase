package cast

import "testing"

// Review Focus 3. Holding the volume key must settle at the end of the range
// rather than erroring or wrapping. Clamping is tested through the pure helper
// because SetVolume itself needs a device.
func TestClampVolume(t *testing.T) {
	for _, tc := range []struct {
		in, want float64
	}{
		{-0.5, 0}, {0, 0}, {0.42, 0.42}, {1, 1}, {1.5, 1},
	} {
		if got := clampVolume(tc.in); got != tc.want {
			t.Errorf("clampVolume(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
