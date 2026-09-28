package internal

import "testing"

func TestPushCastRatingCardIsANoOpWithoutAConfiguredDevice(t *testing.T) {
	config := &Config{CastDevice: ""}
	anime := &Anime{}

	cleanup := pushCastRatingCard(config, anime)
	// Must not panic, hang, or touch the network: nothing to reach without a
	// configured device name.
	cleanup()
}

func TestPushCastRatingCardIsANoOpWithNilInputs(t *testing.T) {
	cleanup := pushCastRatingCard(nil, nil)
	cleanup()

	config := &Config{CastDevice: "Living Room TV"}
	cleanup = pushCastRatingCard(config, nil)
	cleanup()
}
