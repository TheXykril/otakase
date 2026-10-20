package cast

import (
	"context"
	"net/url"
	"os/exec"
	"path"
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

// InputArgs are the demuxer options for streamURL: HLSInputArgs for a
// playlist, nothing for a whole file. They are HLS demuxer options, and any
// other demuxer -- Matroska from a torrent, say -- refuses to start when given
// one ("Option extension_picky not found").
func InputArgs(streamURL string) []string {
	if IsWholeFile(streamURL) {
		return nil
	}
	return HLSInputArgs()
}

// IsWholeFile reports whether a stream is a single file rather than an HLS
// playlist. The torrent provider serves the release itself from a local port,
// and a direct link names its container. A playlist is always named as one:
// anineko proxies its HLS through a local port too.
func IsWholeFile(streamURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(streamURL))
	if err != nil {
		return false
	}
	switch strings.ToLower(path.Ext(parsed.Path)) {
	case ".m3u8", ".m3u":
		return false
	case ".mkv", ".mp4", ".webm", ".avi", ".m4v":
		return true
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
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
