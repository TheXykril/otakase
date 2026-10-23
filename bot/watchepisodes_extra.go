package main

import (
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strconv"
 "strings"
)

// episodeJSON fetches bounded public metadata without following redirects.
func episodeJSON(ctx context.Context, endpoint string, out any) bool {
 req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
 if err != nil { return false }
 client := *activityClient
 client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
   if strings.HasPrefix(endpoint, "https://api.tvmaze.com/lookup/") && next.URL.Scheme == "https" && next.URL.Hostname() == "api.tvmaze.com" { return nil }
   return http.ErrUseLastResponse
 }
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
 case "media.kitsu.io", "static.tvmaze.com", "s4.anilist.co", "img1.ak.crunchyroll.com", "static.crunchyroll.com":
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
 TVDBID []int `json:"tvdb_id"`
 IMDBID []string `json:"imdb_id"`
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
 // Only use TVmaze when its entire first-season episode count matches this
 // AniList entry. Franchise-wide IDs otherwise risk mixing split cours.
 if len(mapping.TVDBID) == 1 && mapping.TVDBID[0] > 0 && show.Episodes > 0 {
   var tvshow struct { ID int `json:"id"`; Premiered string `json:"premiered"` }
   if episodeJSON(ctx, "https://api.tvmaze.com/lookup/shows?thetvdb="+strconv.Itoa(mapping.TVDBID[0]), &tvshow) && tvshow.ID > 0 && (show.Year == 0 || strings.HasPrefix(tvshow.Premiered, strconv.Itoa(show.Year)+"-")) {
     var episodes []struct {
       Season int `json:"season"`
       Number int `json:"number"`
       Name string `json:"name"`
       Image *struct { Medium string `json:"medium"` } `json:"image"`
     }
     if episodeJSON(ctx, fmt.Sprintf("https://api.tvmaze.com/shows/%d/episodes", tvshow.ID), &episodes) {
       season := make([]int,0)
       for _, ep := range episodes { if ep.Season == 1 && ep.Number > 0 { season = append(season, ep.Number) } }
       if len(season) == show.Episodes {
         for _, ep := range episodes {
           if ep.Season != 1 || ep.Image == nil { continue }
           fillEpisode(result, ep.Number, max, ep.Name, ep.Image.Medium)
         }
       }
     }
   }
 }

}
