package main

import (
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

// episodeJSON fetches bounded public metadata without following redirects.
func episodeJSON(ctx context.Context, endpoint string, out any) bool {
 req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
 if err != nil { return false }
 client := *activityClient
 client.Timeout = 2500 * time.Millisecond
 client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
 resp, err := client.Do(req)
 if err != nil { return false }
 defer resp.Body.Close()
 if resp.StatusCode != http.StatusOK { return false }
 return json.NewDecoder(io.LimitReader(resp.Body, 3<<20)).Decode(out) == nil
}

func episodeImage(src string) string {
 u, err := url.Parse(src)
 if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" { return "" }
 switch u.Hostname() {
 case "media.kitsu.io", "s4.anilist.co", "img1.ak.crunchyroll.com", "static.crunchyroll.com":
   return "api/watch/cover?u=" + url.QueryEscape(src)
 }
 return ""
}

func fillEpisode(result map[int]watchEpisodePreview, number, max int, title, picture string) {
 if number < 1 || number > max { return }
 current := result[number]
 if current.Title == "" { current.Title = strings.TrimSpace(title) }
 if current.Thumbnail == "" { current.Thumbnail = episodeImage(picture) }
 result[number] = current
}

// AniMap is an ID mapping service, not a source of episode stills.
type episodeMapping struct {
 KitsuID []int `json:"kitsu_id"`
}

func extraEpisodePreviews(ctx context.Context, show watchShow, result map[int]watchEpisodePreview) {
 if show.MalID < 1 { return }
 var mappings []episodeMapping
 if !episodeJSON(ctx, fmt.Sprintf("https://animap.id/api/map/mal/%d", show.MalID), &mappings) || len(mappings) != 1 { return }
 mapping := mappings[0]
 max := show.lastEpisode()
 if len(mapping.KitsuID) == 1 && mapping.KitsuID[0] > 0 {
   var episodes struct { Episodes []struct {
     Number int `json:"number"`
     RelativeNumber int `json:"relative_number"`
     SeasonNumber int `json:"season_number"`
     Title string `json:"title_en_us"`
     CanonicalTitle string `json:"canonical_title"`
     Thumbnail string `json:"thumbnail"`
   } `json:"episodes"` }
   endpoint := fmt.Sprintf("https://animap.id/api/kitsu/%d/episodes", mapping.KitsuID[0])
   if episodeJSON(ctx, endpoint, &episodes) {
     for _, ep := range episodes.Episodes {
       // Kitsu absolute numbers are safer than relative season numbering.
       title := ep.Title
       if title == "" { title = ep.CanonicalTitle }
       fillEpisode(result, ep.Number, max, title, ep.Thumbnail)
     }
   }
 }

}
