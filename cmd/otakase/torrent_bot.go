package main

import (
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/thexykril/otakase/internal/torrentstream"
)

var torrentBotHash = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

// serveTorrentForBot owns the torrent client for as long as the bot keeps its
// stdin open. The caller reads the URL from the first stdout line and proxies
// its HTTP ranges. Closing stdin stops the torrent and deletes its cache.
func serveTorrentForBot(hash string) error {
	if !torrentBotHash.MatchString(hash) {
		return fmt.Errorf("invalid torrent infohash")
	}
	defer torrentstream.Shutdown()
	link, err := torrentstream.Stream(hash)
	if err != nil {
		return err
	}
	fmt.Println(link)
	_, _ = io.Copy(io.Discard, os.Stdin)
	return nil
}
