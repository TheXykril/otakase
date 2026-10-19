package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// On the first day of each anime season (January, April, July, October),
// at the airing hour, the bot posts the season's most anticipated new shows
// in the anime channel: a picture like the airing board's, the top ten and
// a menu of the top 25. Picking a show opens its card with Plan to watch and
// DM me when it airs. Staff can post it with /season-preview. Each season is
// posted once; delete the post and /season-preview posts it again.

// seasonPost is the season preview message the bot posted last.
type seasonPost struct {
	Season  string `json:"season"` // "FALL 2026"
	Channel string `json:"channel"`
	Message string `json:"message"`
}

var seasonNames = []string{"WINTER", "SPRING", "SUMMER", "FALL"}

// seasonOf is the anime season a date falls in.
func seasonOf(t time.Time) (string, int) {
	return seasonNames[(int(t.Month())-1)/3], t.Year()
}

// nextSeason is the season after season, year.
func nextSeason(season string, year int) (string, int) {
	for k, s := range seasonNames {
		if s == season {
			if k == len(seasonNames)-1 {
				return seasonNames[0], year + 1
			}
			return seasonNames[k+1], year
		}
	}
	return season, year
}

// seasonTitle is "Fall 2026".
func seasonTitle(season string, year int) string {
	return strings.ToUpper(season[:1]) + strings.ToLower(season[1:]) + " " + strconv.Itoa(year)
}

// seasonPreviewDue reports which season to post at now: on the season's
// first day from the airing hour, or the two days after when the bot was
// offline then. posted is the season posted last ("FALL 2026").
func seasonPreviewDue(now time.Time, hour int, posted string) (string, int, bool) {
	now = now.UTC()
	season, year := seasonOf(now)
	if (now.Month()-1)%3 != 0 || now.Day() > 3 || (now.Day() == 1 && now.Hour() < hour) {
		return "", 0, false
	}
	if posted == fmt.Sprintf("%s %d", season, year) {
		return "", 0, false
	}
	return season, year, true
}

const seasonQuery = `query ($season: MediaSeason, $year: Int) {
  Page(perPage: 50) {
    media(season: $season, seasonYear: $year, type: ANIME, format_in: [TV, ONA], isAdult: false, sort: POPULARITY_DESC) {
      ` + mediaBriefFields + ` countryOfOrigin episodes
      startDate { year month day }
      relations { edges { relationType } }
    }
  }
}`

// seasonShow is one show in the preview.
type seasonShow struct {
	Media    mediaBrief
	Start    fuzzyDate
	Episodes int
	Sequel   bool
}

func fetchSeason(season string, year int) ([]seasonShow, error) {
	var r struct {
		Data struct {
			Page struct {
				Media []struct {
					mediaBrief
					CountryOfOrigin string    `json:"countryOfOrigin"`
					Episodes        int       `json:"episodes"`
					StartDate       fuzzyDate `json:"startDate"`
					Relations       struct {
						Edges []struct {
							RelationType string `json:"relationType"`
						} `json:"edges"`
					} `json:"relations"`
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := anilistSocialPost("", seasonQuery, map[string]any{"season": season, "year": year}, &r); err != nil {
		return nil, err
	}
	var out []seasonShow
	for _, m := range r.Data.Page.Media {
		// Japanese shows only, as on the airing board.
		if m.IsAdult || m.CountryOfOrigin != "JP" {
			continue
		}
		s := seasonShow{Media: m.mediaBrief, Start: m.StartDate, Episodes: m.Episodes}
		for _, e := range m.Relations.Edges {
			if e.RelationType == "PREQUEL" {
				s.Sequel = true
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// seasonFacts is "Sequel · TV · starts 3 Oct".
func seasonFacts(s seasonShow) string {
	var facts []string
	if s.Sequel {
		facts = append(facts, "Sequel")
	} else {
		facts = append(facts, "New")
	}
	if f := formatNames[s.Media.Format]; f != "" {
		facts = append(facts, f)
	}
	if t := s.Start.time(); !t.IsZero() && s.Start.Day > 0 {
		facts = append(facts, "starts "+t.Format("2 Jan"))
	}
	return strings.Join(facts, " · ")
}

// seasonPicture draws the spotlight on the most popular show and six more.
func seasonPicture(title string, shows []seasonShow) ([]byte, error) {
	top := shows[:min(len(shows), boardCovers+1)]
	tiles := make([]tileShow, len(top))
	covers := make([]string, len(top))
	for k, s := range top {
		chip := "NEW"
		if s.Sequel {
			chip = "SEQUEL"
		}
		tiles[k] = tileShow{Title: s.Media.name(), Sub: seasonFacts(s), Chip: chip}
		covers[k] = s.Media.cover()
	}
	banner := ""
	if len(top) > 0 {
		banner = top[0].Media.BannerImage
	}
	tileImages(tiles, covers, banner)
	return renderCoverBoard(coverBoard{Label: "SEASON PREVIEW", Title: title, Count: strconv.Itoa(len(shows)), Unit: " shows",
		SpotLabel: "MOST POPULAR", Empty: "Nothing announced yet", Shows: tiles})
}

// seasonMessage is the preview card: the picture, the top ten and a menu
// of the top 25.
func seasonMessage(title string, shows []seasonShow, picture string) []discordgo.MessageComponent {
	color := shu
	var parts []discordgo.MessageComponent
	if picture != "" {
		parts = append(parts, discordgo.MediaGallery{Items: []discordgo.MediaGalleryItem{{Media: discordgo.UnfurledMediaItem{URL: picture}}}})
	}
	if len(shows) == 0 {
		parts = append(parts, discordgo.TextDisplay{Content: "Nothing announced for " + title + " yet."})
		return []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: parts}}
	}
	lines := []string{"## " + title + " · most anticipated"}
	for k, s := range shows[:min(len(shows), 10)] {
		lines = append(lines, fmt.Sprintf("**%d.** [%s](%s)\n-# %s", k+1, mdEscape(clip(s.Media.name(), 70)), s.Media.url(), seasonFacts(s)))
	}
	lines = append(lines, "-# By how many AniList users have them on a list. Pick a show for Plan to watch or a DM when it airs.")
	var opts []discordgo.SelectMenuOption
	for _, s := range shows[:min(len(shows), 25)] {
		opts = append(opts, discordgo.SelectMenuOption{Label: clip(s.Media.name(), 100), Value: strconv.Itoa(s.Media.ID), Description: clip(seasonFacts(s), 100)})
	}
	large, off := discordgo.SeparatorSpacingSizeLarge, false
	parts = append(parts,
		discordgo.Separator{Divider: &off, Spacing: &large},
		discordgo.TextDisplay{Content: clip(strings.Join(lines, "\n"), 3500)},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.SelectMenu{
			MenuType: discordgo.StringSelectMenu, CustomID: "season-pick", Placeholder: "Pick a show for Plan to watch or a DM when it airs…", Options: opts}}},
	)
	return []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: parts}}
}

// errSeasonPosted means the preview for that season is up already.
var errSeasonPosted = errors.New("already posted")

// postSeasonPreview posts the preview for a season and returns a link to
// it. When that season's post still exists it returns its link and
// errSeasonPosted.
func (b *bot) postSeasonPreview(season string, year int) (string, error) {
	if b.cfg.AiringChannel == "" {
		return "", fmt.Errorf("no anime channel")
	}
	key := fmt.Sprintf("%s %d", season, year)
	var old *seasonPost
	b.store.view(func(d *storeData) {
		if d.SeasonPreview != nil {
			p := *d.SeasonPreview
			old = &p
		}
	})
	if old != nil && old.Season == key && old.Message != "" {
		if _, err := b.s.ChannelMessage(old.Channel, old.Message); err == nil {
			return messageLink(b.cfg.GuildID, old.Channel, old.Message), errSeasonPosted
		}
	}
	shows, err := fetchSeason(season, year)
	if err != nil {
		return "", fmt.Errorf("fetching the season: %w", err)
	}
	title := seasonTitle(season, year)
	msg := &discordgo.MessageSend{Flags: discordgo.MessageFlagsIsComponentsV2}
	picture := ""
	if png, err := seasonPicture(title, shows); err != nil {
		log.Printf("season picture: %v", err) // post the list alone
	} else {
		msg.Files = []*discordgo.File{{Name: "season.png", ContentType: "image/png", Reader: bytes.NewReader(png)}}
		picture = "attachment://season.png"
	}
	msg.Components = seasonMessage(title, shows, picture)
	m, err := b.s.ChannelMessageSendComplex(b.cfg.AiringChannel, msg)
	if err != nil {
		return "", fmt.Errorf("posting: %w", err)
	}
	b.store.update(func(d *storeData) {
		d.SeasonPreview = &seasonPost{Season: key, Channel: m.ChannelID, Message: m.ID}
	})
	return messageLink(b.cfg.GuildID, m.ChannelID, m.ID), nil
}

// seasonPreviewCommand is /season-preview.
func (b *bot) seasonPreviewCommand(i *discordgo.InteractionCreate) {
	season, year := seasonOf(time.Now().UTC())
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "season" && o.StringValue() == "next" {
			season, year = nextSeason(season, year)
		}
	}
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		link, err := b.postSeasonPreview(season, year)
		switch {
		case errors.Is(err, errSeasonPosted):
			return errorEmbed("The %s preview is up already: %s\nDelete it first to post a fresh one.", seasonTitle(season, year), link), nil
		case err != nil:
			log.Printf("season preview: %v", err)
			return errorEmbed("Couldn't post the season preview: %s", err), nil
		}
		return &discordgo.MessageEmbed{Description: fmt.Sprintf("Posted the %s preview: %s", seasonTitle(season, year), link), Color: shu}, nil
	})
}

// seasonLoop posts the preview when a season starts.
func (b *bot) seasonLoop() {
	if !b.cfg.SeasonPreview || b.cfg.AiringChannel == "" {
		return
	}
	for {
		posted := ""
		b.store.view(func(d *storeData) {
			if d.SeasonPreview != nil {
				posted = d.SeasonPreview.Season
			}
		})
		if season, year, due := seasonPreviewDue(time.Now(), b.cfg.AiringHour, posted); due {
			if _, err := b.postSeasonPreview(season, year); err != nil && !errors.Is(err, errSeasonPosted) {
				b.jobFailed("Season preview", err)
			} else {
				b.jobOK("Season preview")
			}
		}
		time.Sleep(15 * time.Minute)
	}
}
