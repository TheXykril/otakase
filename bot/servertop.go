package main

import (
	"bytes"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /server-top: from every linked AniList of members still in the server,
// the shows most of them are watching now and the ones they finished this
// week, ten each, with a picture in the airing board's style. The answer
// is kept for an hour.

const serverTopSize = 10

// topShow is a show and how many members have it.
type topShow struct {
	Media   mediaBrief
	Members int
}

// rankShows counts, per show, the members with an entry that keep says
// yes to, most members first (then most popular). Adult shows are left
// out.
func rankShows(lists map[string][]listEntry, keep func(listEntry) bool, n int) []topShow {
	counts := map[int]*topShow{}
	for _, entries := range lists {
		for _, e := range entries {
			if e.Media.IsAdult || !keep(e) {
				continue
			}
			t := counts[e.MediaID]
			if t == nil {
				t = &topShow{Media: e.Media}
				counts[e.MediaID] = t
			}
			t.Members++
		}
	}
	out := make([]topShow, 0, len(counts))
	for _, t := range counts {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Members != out[j].Members {
			return out[i].Members > out[j].Members
		}
		if out[i].Media.Popularity != out[j].Media.Popularity {
			return out[i].Media.Popularity > out[j].Media.Popularity
		}
		return out[i].Media.ID < out[j].Media.ID
	})
	return out[:min(len(out), n)]
}

func isWatching(e listEntry) bool { return e.Status == "CURRENT" }

// finishedSince reports whether an entry was completed after since: by its
// completion date when it has one, else when it was last updated.
func finishedSince(since time.Time) func(listEntry) bool {
	day := time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, time.UTC)
	return func(e listEntry) bool {
		if e.Status != "COMPLETED" || e.UpdatedAt < since.Unix() {
			return false
		}
		if c := e.CompletedAt; c.Year > 0 && c.Month > 0 && c.Day > 0 {
			return !c.time().Before(day)
		}
		return true
	}
}

type serverTopResult struct {
	at       time.Time
	members  int
	watching []topShow
	finished []topShow
	png      []byte
}

var serverTopCache struct {
	sync.Mutex // held while building, so a second call waits for the first
	r          *serverTopResult
}

func (b *bot) serverTopResult() (*serverTopResult, error) {
	serverTopCache.Lock()
	defer serverTopCache.Unlock()
	if r := serverTopCache.r; r != nil && time.Since(r.at) < time.Hour {
		return r, nil
	}
	lists, err := b.memberLists("")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	r := &serverTopResult{at: now, members: len(lists),
		watching: rankShows(lists, isWatching, serverTopSize),
		finished: rankShows(lists, finishedSince(now.Add(-7*24*time.Hour)), serverTopSize)}
	if png, err := serverTopPicture(r); err != nil {
		log.Printf("server top picture: %v", err)
	} else {
		r.png = png
	}
	serverTopCache.r = r
	return r, nil
}

func membersText(n int) string { return plural(n, "member") }

func serverTopPicture(r *serverTopResult) ([]byte, error) {
	top := r.watching[:min(len(r.watching), boardCovers+1)]
	tiles := make([]tileShow, len(top))
	covers := make([]string, len(top))
	for k, t := range top {
		tiles[k] = tileShow{Title: t.Media.name(), Sub: membersText(t.Members) + " watching", Chip: strconv.Itoa(t.Members)}
		covers[k] = t.Media.cover()
	}
	banner := ""
	if len(top) > 0 {
		banner = top[0].Media.BannerImage
	}
	tileImages(tiles, covers, banner)
	return renderCoverBoard(coverBoard{Label: "SERVER TOP", Title: "Watching now", Count: strconv.Itoa(r.members), Unit: strings.TrimPrefix(membersText(r.members), strconv.Itoa(r.members)),
		SpotLabel: "MOST WATCHED", Empty: "Nobody here is watching anything", Shows: tiles})
}

// topLines lists shows as "1. Title · 4 members".
func topLines(list []topShow, empty string) string {
	if len(list) == 0 {
		return empty
	}
	var lines []string
	for k, t := range list {
		lines = append(lines, fmt.Sprintf("**%d.** [%s](%s) · %s", k+1, mdEscape(clip(t.Media.name(), 60)), t.Media.url(), membersText(t.Members)))
	}
	return strings.Join(lines, "\n")
}

func serverTopFooter(r *serverTopResult) string {
	return fmt.Sprintf("From the linked AniList of %s · updated <t:%d:R> · `/link-anilist` to count yours", membersText(r.members), r.at.Unix())
}

// serverTopCard is the answer as a Shu card (components v2).
func serverTopCard(r *serverTopResult, picture string) []discordgo.MessageComponent {
	color := shu
	var parts []discordgo.MessageComponent
	if picture != "" {
		parts = append(parts, discordgo.MediaGallery{Items: []discordgo.MediaGalleryItem{{Media: discordgo.UnfurledMediaItem{URL: picture}}}})
	}
	divider := true
	small := discordgo.SeparatorSpacingSizeSmall
	parts = append(parts,
		discordgo.TextDisplay{Content: clip("### Watching now\n"+topLines(r.watching, "Nobody here is watching anything right now."), 1900)},
		discordgo.Separator{Divider: &divider, Spacing: &small},
		discordgo.TextDisplay{Content: clip("### Finished this week\n"+topLines(r.finished, "Nobody finished a show this week."), 1900)},
		discordgo.TextDisplay{Content: "-# " + serverTopFooter(r)},
	)
	return []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: parts}}
}

// serverTopEmbed is the answer when the card can't be sent.
func serverTopEmbed(r *serverTopResult) *discordgo.MessageEmbed {
	e := &discordgo.MessageEmbed{Title: "Server top", Color: shu, Fields: []*discordgo.MessageEmbedField{
		{Name: "Watching now", Value: clip(topLines(r.watching, "Nobody right now."), 1024)},
		{Name: "Finished this week", Value: clip(topLines(r.finished, "Nothing this week."), 1024)},
	}, Footer: &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("From the linked AniList of %s", membersText(r.members))}}
	if len(r.watching) > 0 && r.watching[0].Media.cover() != "" {
		e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: r.watching[0].Media.cover()}
	}
	return e
}

// serverTop is /server-top.
func (b *bot) serverTop(i *discordgo.InteractionCreate) {
	if len(b.linkedMembers()) == 0 {
		b.respond(i, true, errorEmbed("Nobody has linked AniList yet. Link yours with `/link-anilist`."))
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: answerFlags(i)}}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	r, err := b.serverTopResult()
	if err != nil {
		log.Printf("server top: %v", err)
		b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{errorEmbed("Couldn't read AniList right now. Try again in a minute.")}})
		return
	}
	if r.members == 0 {
		b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{errorEmbed("Every linked AniList here is private, so there's nothing to count.")}})
		return
	}
	edit := &discordgo.WebhookEdit{}
	picture := ""
	if r.png != nil {
		edit.Files = []*discordgo.File{{Name: "server-top.png", ContentType: "image/png", Reader: bytes.NewReader(r.png)}}
		picture = "attachment://server-top.png"
	}
	comps := serverTopCard(r, picture)
	edit.Components = &comps
	err = b.editComponentsV2(i.Interaction, edit)
	if err == nil {
		return
	}
	log.Printf("server top card: %v", err)
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{serverTopEmbed(r)}}); err != nil {
		log.Printf("edit: %v", err)
	}
}
