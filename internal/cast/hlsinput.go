package cast

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// HLSInputArgs are the demuxer options every HLS input needs.
//
// Providers routinely disguise HLS segments as images (…/seg-1-f1-v1-a1.jpg)
// to slip past filters. -allowed_extensions ALL lets the demuxer open them,
// and ffmpeg 7.1 added a second check, -extension_picky, that rejects them
// anyway unless turned off. Older ffmpeg -- what Ubuntu 24.04 and Debian 12
// ship -- does not know that option and refuses to start at all when given it,
// so it is only passed to an ffmpeg that has it.
func HLSInputArgs() []string {
	if hlsExtensionPicky() {
		return []string{"-allowed_extensions", "ALL", "-extension_picky", "0"}
	}
	return []string{"-allowed_extensions", "ALL"}
}

// hlsExtensionPicky reports whether the installed ffmpeg's HLS demuxer has
// -extension_picky. It is asked once. When ffmpeg cannot be asked, the option
// is assumed: a current ffmpeg needs it, and one that is missing fails anyway.
var hlsExtensionPicky = sync.OnceValue(probeHLSExtensionPicky)

func probeHLSExtensionPicky() bool {
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "-hide_banner", "-h", "demuxer=hls").Output()
	if err != nil || !strings.Contains(string(out), "allowed_extensions") {
		return true
	}
	return strings.Contains(string(out), "extension_picky")
}
