package internal

import (
	"regexp"
	"strconv"
	"strings"
)

// preferredStreamHosts are hosts whose links play most reliably, best first.
// A link on one of them beats any link elsewhere, whatever the resolution.
var preferredStreamHosts = []string{
	"video.wixstatic.com",
	"sharepoint.com",
	"wixmp.com",
	"dropbox.com",
	"wetransfer.com",
	"gogoanime.com",
}

var resolutionInURL = regexp.MustCompile(`(\d{3,4})p`)

// BestLink picks the link to play from those a provider resolved: a preferred
// host first, then the highest resolution named in the URL. Ties keep the
// provider's order.
func BestLink(links []string) string {
	best, bestRank := "", -1
	for _, link := range links {
		if rank := streamLinkRank(link); rank > bestRank {
			best, bestRank = link, rank
		}
	}
	return best
}

func streamLinkRank(link string) int {
	rank := 0
	for i, host := range preferredStreamHosts {
		if strings.Contains(link, host) {
			rank = (len(preferredStreamHosts) - i) * 10000
			break
		}
	}
	for _, m := range resolutionInURL.FindAllStringSubmatch(link, -1) {
		if height, err := strconv.Atoi(m[1]); err == nil && height <= 2160 && height > rank%10000 {
			rank = rank - rank%10000 + height
		}
	}
	return rank
}
