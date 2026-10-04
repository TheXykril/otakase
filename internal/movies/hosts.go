package movies

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Stream is a playable video: a direct file URL and what its host needs to
// serve it.
type Stream struct {
	URL      string
	Referrer string
	// Server names where it came from, for the log and the player title.
	Server string
}

// Source is one of a movie's servers, resolved only when it is tried: each
// resolve costs requests to a video host, and the first that plays is enough.
type Source struct {
	Server  string
	Resolve func() (Stream, error)
}

// The page's player turns server n's id into a link: servers 1 to 3 go
// through p2.php on the site, which is a page holding one iframe to a video
// host; server 0 is a file on imgsto.re, offered by the site only to viewers
// who do not block its ads.
var p2Params = map[int]string{1: "n", 2: "d", 3: "str"}

// knownEmbeds are where p2.php is known to send each parameter, used when the
// p2 page itself cannot be read. Doodstream changes domain often, so the page
// is always asked first.
var knownEmbeds = map[string]string{
	"d":   "https://dood.pm/e/",
	"str": "https://streamtape.com/e/",
}

// serverOrder is the order servers are tried in: Streamtape first, which
// hands over a plain file with the fewest steps, then Doodstream, then the
// server whose host is not known, then the ad-gated file.
var serverOrder = []int{3, 2, 1, 0}

// Sources lists a movie's servers in the order to try them.
func (s *Site) Sources(page Page) []Source {
	sources := []Source{}
	for _, n := range serverOrder {
		if n >= len(page.Servers) || page.Servers[n] == "" {
			continue
		}
		id := page.Servers[n]
		if n == 0 {
			sources = append(sources, Source{
				Server: "imgsto.re",
				Resolve: func() (Stream, error) {
					base, err := s.Base()
					if err != nil {
						return Stream{}, err
					}
					return Stream{URL: "https://imgsto.re/files/" + id, Referrer: base + "/", Server: "imgsto.re"}, nil
				},
			})
			continue
		}
		param := p2Params[n]
		pagePath := page.Path
		sources = append(sources, Source{
			Server: fmt.Sprintf("server %d", n),
			Resolve: func() (Stream, error) {
				embed, err := s.embedURL(param, id, pagePath)
				if err != nil {
					return Stream{}, err
				}
				return ResolveEmbed(embed)
			},
		})
	}
	return sources
}

var iframeSrcPattern = regexp.MustCompile(`<iframe[^>]+src="(https?://[^"]+)"`)

// embedURL is the video host's embed page that p2.php wraps.
func (s *Site) embedURL(param, id, pagePath string) (string, error) {
	base, err := s.Base()
	if err != nil {
		return "", err
	}
	p2 := base + "/p2.php?" + param + "=" + url.QueryEscape(id)
	body, fetchErr := fetchPage(s.Client, p2, base+pagePath)
	if fetchErr == nil {
		if match := iframeSrcPattern.FindStringSubmatch(body); match != nil {
			return match[1], nil
		}
		fetchErr = fmt.Errorf("no player in %s", p2)
	}
	if prefix, ok := knownEmbeds[param]; ok {
		logf("%v; using %s", fetchErr, prefix+id)
		return prefix + id, nil
	}
	return "", fetchErr
}

// ResolveEmbed turns a video host's embed page into its file.
func ResolveEmbed(embed string) (Stream, error) {
	parsed, err := url.Parse(embed)
	if err != nil {
		return Stream{}, err
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case strings.Contains(host, "streamtape") || strings.Contains(host, "strtape") || strings.Contains(host, "stape"):
		return resolveStreamtape(embed)
	case strings.Contains(host, "dood") || strings.Contains(host, "d000d") || strings.Contains(host, "ds2play") || strings.Contains(host, "dooood"):
		return resolveDood(embed)
	}
	return Stream{}, fmt.Errorf("no extractor for %s", host)
}

// hostClient follows redirects: video hosts move their embed pages between
// domains and redirect the old ones.
var hostClient = &http.Client{Timeout: 25 * time.Second}

func fetchPage(client *http.Client, rawURL, referrer string) (string, error) {
	body, _, err := fetchFinal(client, rawURL, referrer)
	return body, err
}

// fetchFinal fetches a page and reports the URL it ended at.
func fetchFinal(client *http.Client, rawURL, referrer string) (string, *url.URL, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if referrer != "" {
		req.Header.Set("Referer", referrer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("%s answered %d", rawURL, resp.StatusCode)
	}
	return string(body), resp.Request.URL, nil
}

// Streamtape writes its file link into a hidden element from two pieces: a
// literal start, and a string with a few characters cut off its front by
// substring calls. Several such lines are on the page, most of them decoys;
// the one for robotlink is the one its player reads.
var streamtapeLinkPattern = regexp.MustCompile(`getElementById\(\s*'(\w+)'\s*\)\.innerHTML\s*=\s*["']([^"']+)["']\s*\+\s*\(?\s*["']([^"']+)["']\s*\)?((?:\.substring\(\d+\))*)`)

var substringPattern = regexp.MustCompile(`\.substring\((\d+)\)`)

func resolveStreamtape(embed string) (Stream, error) {
	body, err := fetchPage(hostClient, embed, "")
	if err != nil {
		return Stream{}, err
	}
	link, err := streamtapeLink(body)
	if err != nil {
		return Stream{}, err
	}
	return Stream{URL: link, Referrer: embed, Server: "Streamtape"}, nil
}

// streamtapeLink assembles the file link from an embed page.
func streamtapeLink(body string) (string, error) {
	var chosen []string
	for _, match := range streamtapeLinkPattern.FindAllStringSubmatch(body, -1) {
		if !strings.Contains(match[2], "get_video") {
			continue
		}
		chosen = match
		if match[1] == "robotlink" {
			break
		}
	}
	if chosen == nil {
		return "", fmt.Errorf("streamtape: no video link on the page (removed, or the page changed)")
	}
	tail := chosen[3]
	for _, cut := range substringPattern.FindAllStringSubmatch(chosen[4], -1) {
		n, _ := strconv.Atoi(cut[1])
		if n > len(tail) {
			n = len(tail)
		}
		tail = tail[n:]
	}
	link := chosen[2] + tail
	if strings.HasPrefix(link, "//") {
		link = "https:" + link
	}
	if !strings.Contains(link, "stream=") {
		link += "&stream=1"
	}
	return link, nil
}

// Doodstream's embed page asks /pass_md5/... for the start of the file link
// and finishes it in script: ten random characters, then the token (the last
// part of that path) and the time. The file is only served with the embed
// page's host as referrer.
var doodPassPattern = regexp.MustCompile(`/pass_md5/[\w./~-]+`)

func resolveDood(embed string) (Stream, error) {
	body, final, err := fetchFinal(hostClient, embed, "")
	if err != nil {
		return Stream{}, err
	}
	pass := doodPassPattern.FindString(body)
	if pass == "" {
		return Stream{}, fmt.Errorf("doodstream: no pass_md5 on the page (removed, or the page changed)")
	}
	origin := final.Scheme + "://" + final.Host
	start, err := fetchPage(hostClient, origin+pass, final.String())
	if err != nil {
		return Stream{}, fmt.Errorf("doodstream: %w", err)
	}
	start = strings.TrimSpace(start)
	if !strings.HasPrefix(start, "http") {
		return Stream{}, fmt.Errorf("doodstream: pass_md5 gave no link")
	}
	return Stream{URL: doodLink(start, pass, time.Now()), Referrer: origin + "/", Server: "Doodstream"}, nil
}

// doodLink finishes a Doodstream file link the way its player does.
func doodLink(start, pass string, now time.Time) string {
	token := pass[strings.LastIndex(pass, "/")+1:]
	return start + randomString(10) + "?token=" + token + "&expiry=" + strconv.FormatInt(now.UnixMilli(), 10)
}

func randomString(n int) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, n)
	for i := range out {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			out[i] = letters[i%len(letters)]
			continue
		}
		out[i] = letters[index.Int64()]
	}
	return string(out)
}
