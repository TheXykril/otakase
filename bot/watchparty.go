package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /watchparty creates a server event in a voice channel for watching a show
// together, with the show's AniList banner as the event image.

const watchpartyQuery = `query ($q: String) {
  Media(search: $q, type: ANIME, isAdult: false) {
    siteUrl bannerImage coverImage { extraLarge }
    title { romaji english }
  }
}`

type partyShow struct {
	Title string
	URL   string
	Image string
}

func findPartyShow(q string) (partyShow, error) {
	body, _ := json.Marshal(map[string]any{"query": watchpartyQuery, "variables": map[string]string{"q": q}})
	req, _ := http.NewRequest("POST", "https://graphql.anilist.co", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	var r struct {
		Data struct {
			Media *struct {
				SiteURL     string `json:"siteUrl"`
				BannerImage string `json:"bannerImage"`
				CoverImage  struct {
					ExtraLarge string `json:"extraLarge"`
				} `json:"coverImage"`
				Title struct{ Romaji, English string }
			} `json:"Media"`
		} `json:"data"`
	}
	if err := getJSON(req, &r); err != nil {
		return partyShow{}, err
	}
	m := r.Data.Media
	if m == nil {
		return partyShow{Title: q}, nil
	}
	p := partyShow{Title: m.Title.English, URL: m.SiteURL, Image: m.BannerImage}
	if p.Title == "" {
		p.Title = m.Title.Romaji
	}
	if p.Image == "" {
		p.Image = m.CoverImage.ExtraLarge
	}
	return p, nil
}

// imageDataURI downloads an image for the event cover. Failures just leave
// the event without one.
func imageDataURI(url string) string {
	if url == "" {
		return ""
	}
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	return "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// Who can talk and screen share in a party's voice channel.
const (
	partyTalk     = "talk"  // everyone talks, only the host screen shares
	partyShare    = "share" // everyone talks and screen shares
	partyHostOnly = "host"  // only the host talks and screen shares
)

const partyVoice = int64(discordgo.PermissionVoiceSpeak | discordgo.PermissionVoiceStreamVideo)

// partyOverwrites lets the host talk and screen share and sets what
// everyone else may do, whatever the server's defaults are.
func partyOverwrites(guildID, host, mode string) []*discordgo.PermissionOverwrite {
	everyone := &discordgo.PermissionOverwrite{ID: guildID, Type: discordgo.PermissionOverwriteTypeRole}
	switch mode {
	case partyShare:
		everyone.Allow = partyVoice
	case partyHostOnly:
		everyone.Deny = partyVoice
	default:
		everyone.Allow, everyone.Deny = discordgo.PermissionVoiceSpeak, discordgo.PermissionVoiceStreamVideo
	}
	return []*discordgo.PermissionOverwrite{
		everyone,
		{ID: host, Type: discordgo.PermissionOverwriteTypeMember, Allow: partyVoice},
	}
}

func (b *bot) watchparty(i *discordgo.InteractionCreate) {
	d := i.ApplicationCommandData()
	title, minutes, limit, mode := "", int64(15), 0, partyTalk
	for _, o := range d.Options {
		switch o.Name {
		case "anime":
			title = o.StringValue()
		case "starts_in":
			minutes = o.IntValue()
		case "limit":
			limit = int(o.IntValue())
		case "voice":
			mode = o.StringValue()
		}
	}
	host := i.Member.User
	if ev := b.hostedParty(host.ID); ev != nil {
		b.respond(i, true, errorEmbed("You already have a watch party planned: %s\nCancel it first with the **Cancel** button on its post.", eventLink(ev)))
		return
	}
	b.deferThen(i, false, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		show, err := findPartyShow(title)
		if err != nil {
			show = partyShow{Title: title}
		}
		start := time.Now().Add(time.Duration(minutes) * time.Minute)
		if minutes < 1 {
			start = time.Now().Add(time.Minute) // events must start in the future
		}
		cat, err := b.partyCategory()
		if err != nil {
			log.Printf("watchparty category: %v", err) // the room goes with the other rooms
		}
		name, perms := clip("🎬 "+show.Title, 90), partyOverwrites(b.cfg.GuildID, host.ID, mode)
		c, err := b.createRoom(i.Member, name, false, limit, cat, perms...)
		if err != nil && cat != "" {
			// The category may have just gone with the last party; use the rooms one.
			c, err = b.createRoom(i.Member, name, false, limit, "", perms...)
		}
		if err != nil {
			log.Printf("watchparty channel: %v", err)
			return errorEmbed("Couldn't make a voice channel. The bot needs **Manage Channels**."), nil
		}
		channel := c.ID
		desc := fmt.Sprintf("Watch party hosted by <@%s>. Join the voice channel and watch along with otakase.", host.ID)
		if show.URL != "" {
			desc += "\n" + show.URL
		}
		ev, err := b.s.GuildScheduledEventCreate(b.cfg.GuildID, &discordgo.GuildScheduledEventParams{
			ChannelID:          channel,
			Name:               clip("Watch party: "+show.Title, 100),
			Description:        desc,
			ScheduledStartTime: &start,
			PrivacyLevel:       discordgo.GuildScheduledEventPrivacyLevelGuildOnly,
			EntityType:         discordgo.GuildScheduledEventEntityTypeVoice,
			Image:              imageDataURI(show.Image),
		})
		if err != nil {
			log.Printf("watchparty: %v", err)
			b.sweepRoom(channel, true)
			return errorEmbed("Couldn't create the event. The bot needs the **Create Events** permission."), nil
		}
		// Kept until half an hour after the start, then removed once empty.
		b.store.update(func(d *storeData) {
			r := d.Rooms[channel]
			r.Event, r.KeepUntil = ev.ID, start.Add(30*time.Minute)
			d.Rooms[channel] = r
		})
		link := eventLink(ev)
		e := &discordgo.MessageEmbed{
			Title:       "Watch party: " + show.Title,
			URL:         link,
			Description: fmt.Sprintf("<@%s> is hosting in <#%s>, starting <t:%d:R>. Press **Interested** on the event to get a reminder.", host.ID, channel, start.Unix()),
			Color:       shu,
		}
		if show.Image != "" {
			e.Image = &discordgo.MessageEmbedImage{URL: show.Image}
		}
		return e, []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Open event", Style: discordgo.LinkButton, URL: link},
			discordgo.Button{Label: "Cancel", Style: discordgo.DangerButton, CustomID: "party-cancel:" + ev.ID},
		}}}
	})
}

// Watch party channels go in their own category, made with the first party
// and deleted once the last party's channel is gone.
const partyCategoryName = "WatchParty"

var partyCategoryMu sync.Mutex

func isPartyCategory(c *discordgo.Channel) bool {
	if c.Type != discordgo.ChannelTypeGuildCategory {
		return false
	}
	n := strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(channelBase(c.Name)))
	return n == "watchparty" || n == "watchparties"
}

// partyCategory returns the watch party category, making it if needed.
func (b *bot) partyCategory() (string, error) {
	partyCategoryMu.Lock()
	defer partyCategoryMu.Unlock()
	chans, err := b.s.GuildChannels(b.cfg.GuildID)
	if err != nil {
		return "", err
	}
	for _, c := range chans {
		if isPartyCategory(c) {
			return c.ID, nil
		}
	}
	c, err := b.s.GuildChannelCreateComplex(b.cfg.GuildID, discordgo.GuildChannelCreateData{
		Name: partyCategoryName, Type: discordgo.ChannelTypeGuildCategory,
	}, discordgo.WithAuditLogReason("Watch party"))
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

// dropPartyCategory deletes the watch party category once nothing is in it.
func (b *bot) dropPartyCategory(id string) {
	partyCategoryMu.Lock()
	defer partyCategoryMu.Unlock()
	chans, err := b.s.GuildChannels(b.cfg.GuildID)
	if err != nil {
		return
	}
	var cat *discordgo.Channel
	for _, c := range chans {
		if c.ParentID == id {
			return
		}
		if c.ID == id {
			cat = c
		}
	}
	if cat == nil || !isPartyCategory(cat) {
		return
	}
	if _, err := b.s.ChannelDelete(id, discordgo.WithAuditLogReason("No watch parties left")); err != nil {
		log.Printf("delete party category: %v", err)
	}
}

func eventLink(ev *discordgo.GuildScheduledEvent) string {
	return fmt.Sprintf("https://discord.com/events/%s/%s", ev.GuildID, ev.ID)
}

var partyHostRe = regexp.MustCompile(`hosted by <@(\d+)>`)

// partyHost returns the member who started a watch party event.
func partyHost(ev *discordgo.GuildScheduledEvent) string {
	if m := partyHostRe.FindStringSubmatch(ev.Description); m != nil {
		return m[1]
	}
	return ""
}

// hostedParty returns the upcoming or running watch party a member started,
// so each member has at most one at a time.
func (b *bot) hostedParty(userID string) *discordgo.GuildScheduledEvent {
	events, err := b.s.GuildScheduledEvents(b.cfg.GuildID, false)
	if err != nil {
		return nil
	}
	for _, ev := range events {
		if ev.CreatorID == b.s.State.User.ID && partyHost(ev) == userID &&
			(ev.Status == discordgo.GuildScheduledEventStatusScheduled || ev.Status == discordgo.GuildScheduledEventStatusActive) {
			return ev
		}
	}
	return nil
}

// cancelParty handles the Cancel button: the host or anyone who can manage
// events deletes the event, and the post says it was cancelled.
func (b *bot) cancelParty(i *discordgo.InteractionCreate, eventID string) {
	ev, err := b.s.GuildScheduledEvent(b.cfg.GuildID, eventID, false)
	if err != nil {
		b.respond(i, true, errorEmbed("This watch party is already over or cancelled."))
		return
	}
	user := i.Member.User.ID
	if user != partyHost(ev) && i.Member.Permissions&discordgo.PermissionManageEvents == 0 {
		b.respond(i, true, errorEmbed("Only the host or a moderator can cancel this watch party."))
		return
	}
	if err := b.s.GuildScheduledEventDelete(b.cfg.GuildID, eventID); err != nil {
		log.Printf("cancel party: %v", err)
		b.respond(i, true, errorEmbed("Couldn't cancel the event."))
		return
	}
	b.dropPartyRoom(eventID)
	e := &discordgo.MessageEmbed{Title: "Cancelled: " + strings.TrimPrefix(ev.Name, "Watch party: "), Color: 0x8A8276,
		Description: fmt.Sprintf("Watch party cancelled by <@%s>.", user)}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: []discordgo.MessageComponent{}},
	}); err != nil {
		log.Printf("cancel party update: %v", err)
	}
}

// dropPartyRoom stops keeping a cancelled party's voice channel, which then
// goes once nobody is in it.
func (b *bot) dropPartyRoom(eventID string) {
	var id string
	b.store.update(func(d *storeData) {
		for ch, r := range d.Rooms {
			if r.Event == eventID {
				r.KeepUntil, r.Used = time.Time{}, true
				d.Rooms[ch] = r
				id = ch
			}
		}
	})
	if id != "" {
		b.sweepRoom(id, false)
	}
}
