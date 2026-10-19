package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// /recommend: five shows for the caller from three places, leaving out
// anything already on their AniList:
//   - AniList's recommendations for the shows they scored highest,
//   - shows other members here scored 8/10 or more,
//   - well rated shows in their favourite genres.
// Each comes with why ("Because you liked X", "3 people here rated it 9+").
// A menu opens a show's card with Plan to watch.

const recommendCount = 5

// recCandidate is a show that could be recommended and why.
type recCandidate struct {
	Media     mediaBrief
	Because   []string // your shows AniList recommends it for, best first
	Rating    int      // AniList recommendation ratings, summed
	Fans      int      // members here who scored it 80+
	Nines     int      // of them, those who scored it 90+
	Genres    []string // your favourite genres it has
	FromGenre bool     // found by the genre search
	score     float64
}

// recPool collects candidates by show id.
type recPool map[int]*recCandidate

func (p recPool) get(m mediaBrief) *recCandidate {
	c := p[m.ID]
	if c == nil {
		c = &recCandidate{Media: m}
		p[m.ID] = c
	}
	return c
}

// favouriteGenres are the caller's top genres, weighted by how they scored
// what they watched.
func favouriteGenres(entries []listEntry, n int) []string {
	weight := map[string]float64{}
	for _, e := range entries {
		if e.Status == "PLANNING" || e.Status == "DROPPED" {
			continue
		}
		w := 0.6
		if e.Score > 0 {
			w = e.Score / 100
		}
		for _, g := range e.Media.Genres {
			if g != "Hentai" && g != "Ecchi" {
				weight[g] += w
			}
		}
	}
	genres := make([]string, 0, len(weight))
	for g := range weight {
		genres = append(genres, g)
	}
	sort.Slice(genres, func(i, j int) bool {
		if weight[genres[i]] != weight[genres[j]] {
			return weight[genres[i]] > weight[genres[j]]
		}
		return genres[i] < genres[j]
	})
	return genres[:min(len(genres), n)]
}

// topRated are the caller's best scored shows they watched, or the latest
// finished when they score nothing.
func topRated(entries []listEntry, n int) []listEntry {
	var list []listEntry
	for _, e := range entries {
		if (e.Status == "COMPLETED" || e.Status == "CURRENT" || e.Status == "REPEATING") && !e.Media.IsAdult {
			list = append(list, e)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Score != list[j].Score {
			return list[i].Score > list[j].Score
		}
		return list[i].UpdatedAt > list[j].UpdatedAt
	})
	return list[:min(len(list), n)]
}

// addFans counts members who scored a show 80+ and 90+.
func (p recPool) addFans(lists map[string][]listEntry) {
	for _, entries := range lists {
		for _, e := range entries {
			if e.Score < 80 || e.Media.IsAdult {
				continue
			}
			c := p.get(e.Media)
			c.Fans++
			if e.Score >= 90 {
				c.Nines++
			}
		}
	}
}

// rankRecommendations scores the candidates, drops what is on the
// caller's list and returns the best n.
func rankRecommendations(p recPool, onList map[int]bool, fav []string, n int) []*recCandidate {
	var out []*recCandidate
	for id, c := range p {
		if onList[id] || c.Media.IsAdult || c.Media.name() == "" {
			continue
		}
		c.Genres = nil
		for _, g := range fav {
			for _, mg := range c.Media.Genres {
				if g == mg {
					c.Genres = append(c.Genres, g)
				}
			}
		}
		c.score = 3*float64(len(c.Because)) + math.Log1p(float64(max(c.Rating, 0))) + 2.5*float64(c.Fans) + 0.5*float64(c.Nines) + 0.75*float64(len(c.Genres))
		if c.FromGenre && len(c.Because) == 0 && c.Fans == 0 && len(c.Genres) < 2 {
			continue // a genre search hit that barely matches
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if out[i].Media.Popularity != out[j].Media.Popularity {
			return out[i].Media.Popularity > out[j].Media.Popularity
		}
		return out[i].Media.ID < out[j].Media.ID
	})
	return out[:min(len(out), n)]
}

// reason is the line saying why a show is recommended.
func (c *recCandidate) reason() string {
	rated := func(n int) string {
		mark := "8+"
		if c.Nines == c.Fans {
			mark = "9+"
		}
		if n == 1 {
			return "Someone here rated it " + mark
		}
		return fmt.Sprintf("%d people here rated it %s", n, mark)
	}
	switch {
	case c.Fans >= 2:
		return rated(c.Fans)
	case len(c.Because) > 0:
		return "Because you liked " + c.Because[0]
	case c.Fans == 1:
		return rated(1)
	case len(c.Genres) > 0:
		return "More " + joinAnd(c.Genres) + ", your favourite genres"
	}
	return "Popular on AniList"
}

// joinAnd is "a, b and c".
func joinAnd(xs []string) string {
	if len(xs) < 2 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

const recsQuery = `query ($ids: [Int]) {
  Page(perPage: 10) {
    media(id_in: $ids) {
      id title { romaji english }
      recommendations(sort: RATING_DESC, perPage: 8) { nodes { rating mediaRecommendation { ` + mediaBriefFields + ` } } }
    }
  }
}`

// addAniListRecs adds AniList's recommendations for the caller's top shows.
func (p recPool) addAniListRecs(top []listEntry) error {
	if len(top) == 0 {
		return nil
	}
	ids := make([]int, len(top))
	order := map[int]int{}
	for k, e := range top {
		ids[k] = e.MediaID
		order[e.MediaID] = k
	}
	var r struct {
		Data struct {
			Page struct {
				Media []struct {
					ID              int `json:"id"`
					Title           struct{ Romaji, English string }
					Recommendations struct {
						Nodes []struct {
							Rating              int         `json:"rating"`
							MediaRecommendation *mediaBrief `json:"mediaRecommendation"`
						} `json:"nodes"`
					} `json:"recommendations"`
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := anilistSocialPost("", recsQuery, map[string]any{"ids": ids}, &r); err != nil {
		return err
	}
	// Your best show first, so "Because you liked" names it.
	media := r.Data.Page.Media
	sort.SliceStable(media, func(i, j int) bool { return order[media[i].ID] < order[media[j].ID] })
	for _, m := range media {
		title := m.Title.English
		if title == "" {
			title = m.Title.Romaji
		}
		for _, n := range m.Recommendations.Nodes {
			if n.MediaRecommendation == nil || n.Rating <= 0 {
				continue
			}
			c := p.get(*n.MediaRecommendation)
			c.Because = append(c.Because, title)
			c.Rating += n.Rating
		}
	}
	return nil
}

const genreQuery = `query ($genres: [String]) {
  Page(perPage: 40) {
    media(genre_in: $genres, type: ANIME, isAdult: false, sort: SCORE_DESC, popularity_greater: 20000) { ` + mediaBriefFields + ` }
  }
}`

// addGenreHits adds well rated shows in the caller's favourite genres.
func (p recPool) addGenreHits(fav []string) error {
	if len(fav) == 0 {
		return nil
	}
	var r struct {
		Data struct {
			Page struct {
				Media []mediaBrief `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := anilistSocialPost("", genreQuery, map[string]any{"genres": fav[:min(len(fav), 2)]}, &r); err != nil {
		return err
	}
	for _, m := range r.Data.Page.Media {
		p.get(m).FromGenre = true
	}
	return nil
}

// recommend is /recommend.
func (b *bot) recommend(i *discordgo.InteractionCreate) {
	user := i.Member.User.ID
	var link anilistLink
	var linked bool
	b.store.view(func(d *storeData) { link, linked = d.AniList[user] })
	if !linked {
		b.respond(i, true, errorEmbed("Link your AniList first with `/link-anilist`, then `/recommend` picks shows from your list."))
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	reply := func(e *discordgo.MessageEmbed) {
		if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e}}); err != nil {
			log.Printf("edit: %v", err)
		}
	}
	mine, err := anilistLists.get(link.ID)
	switch {
	case errors.Is(err, errPrivateList):
		reply(errorEmbed("Your AniList list is private, so there's nothing to go on. Make it public in AniList's settings."))
		return
	case err != nil:
		log.Printf("recommend: %v", err)
		reply(errorEmbed("Couldn't read AniList right now. Try again in a minute."))
		return
	}
	onList := map[int]bool{}
	for _, e := range mine {
		onList[e.MediaID] = true
	}
	fav := favouriteGenres(mine, 3)
	pool := recPool{}
	if err := pool.addAniListRecs(topRated(mine, 5)); err != nil {
		log.Printf("recommend: anilist recommendations: %v", err)
	}
	if lists, err := b.memberLists(user); err != nil {
		log.Printf("recommend: member lists: %v", err)
	} else {
		pool.addFans(lists)
	}
	if err := pool.addGenreHits(fav); err != nil {
		log.Printf("recommend: genres: %v", err)
	}
	recs := rankRecommendations(pool, onList, fav, recommendCount)
	if len(recs) == 0 {
		reply(errorEmbed("Nothing new to recommend right now. Add a few scores on AniList and try again."))
		return
	}
	edit := &discordgo.WebhookEdit{}
	picture := ""
	if png, err := recommendPicture(link.Name, recs); err != nil {
		log.Printf("recommend picture: %v", err)
	} else {
		edit.Files = []*discordgo.File{{Name: "recommend.png", ContentType: "image/png", Reader: bytes.NewReader(png)}}
		picture = "attachment://recommend.png"
	}
	comps := recommendCard(recs, picture)
	edit.Components = &comps
	err = b.editComponentsV2(i.Interaction, edit)
	if err == nil {
		return
	}
	log.Printf("recommend card: %v", err)
	e, rows := recommendEmbed(recs)
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e}, Components: &rows}); err != nil {
		log.Printf("edit: %v", err)
	}
}

func recommendPicture(name string, recs []*recCandidate) ([]byte, error) {
	tiles := make([]tileShow, len(recs))
	covers := make([]string, len(recs))
	for k, c := range recs {
		tiles[k] = tileShow{Title: c.Media.name(), Sub: c.reason(), Chip: strconv.Itoa(k + 1)}
		covers[k] = c.Media.cover()
	}
	tileImages(tiles, covers, recs[0].Media.BannerImage)
	return renderCoverBoard(coverBoard{Label: "FOR YOU", Title: name, Count: strconv.Itoa(len(recs)), Unit: " picks",
		SpotLabel: "TOP PICK", Shows: tiles})
}

// recLines lists the picks with their reasons.
func recLines(recs []*recCandidate) string {
	var lines []string
	for k, c := range recs {
		lines = append(lines, fmt.Sprintf("**%d.** [%s](%s)\n-# %s", k+1, mdEscape(clip(c.Media.name(), 70)), c.Media.url(), mdEscape(clip(c.reason(), 120))))
	}
	return strings.Join(lines, "\n")
}

func recMenu(recs []*recCandidate) discordgo.ActionsRow {
	var opts []discordgo.SelectMenuOption
	for _, c := range recs {
		opts = append(opts, discordgo.SelectMenuOption{Label: clip(c.Media.name(), 100), Value: strconv.Itoa(c.Media.ID), Description: clip(c.reason(), 100)})
	}
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.SelectMenu{
		MenuType: discordgo.StringSelectMenu, CustomID: "rec-pick", Placeholder: "Pick a show for Plan to watch…", Options: opts}}}
}

func recommendCard(recs []*recCandidate, picture string) []discordgo.MessageComponent {
	color := shu
	var parts []discordgo.MessageComponent
	if picture != "" {
		parts = append(parts, discordgo.MediaGallery{Items: []discordgo.MediaGalleryItem{{Media: discordgo.UnfurledMediaItem{URL: picture}}}})
	}
	parts = append(parts,
		discordgo.TextDisplay{Content: clip("### Picked for you\n"+recLines(recs), 3500)},
		recMenu(recs),
		discordgo.TextDisplay{Content: "-# From your AniList, AniList's recommendations and what members here rated highly"},
	)
	return []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: parts}}
}

// recommendEmbed is the answer when the card can't be sent.
func recommendEmbed(recs []*recCandidate) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	e := &discordgo.MessageEmbed{Title: "Picked for you", Description: clip(recLines(recs), 4000), Color: shu}
	if c := recs[0].Media.cover(); c != "" {
		e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: c}
	}
	return e, []discordgo.MessageComponent{recMenu(recs)}
}
