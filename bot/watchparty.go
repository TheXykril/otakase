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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /watchparty creates a server event in a voice channel for watching a show
// together, with the show's AniList banner as the event image.

const watchpartyQuery = `query ($q: String, $id: Int) {
  Media(id: $id, search: $q, type: ANIME, isAdult: false) {
    siteUrl bannerImage coverImage { extraLarge }
    title { romaji english }
  }
}`

type partyShow struct {
	Title string
	URL   string
	Image string
}

// pickedShowRe matches the value of a title picked from the suggestions.
var pickedShowRe = regexp.MustCompile(`^anilist:(\d+)$`)

func findPartyShow(q string) (partyShow, error) {
	vars := map[string]any{"q": q}
	if m := pickedShowRe.FindStringSubmatch(q); m != nil {
		id, _ := strconv.Atoi(m[1])
		vars = map[string]any{"id": id}
	}
	body, _ := json.Marshal(map[string]any{"query": watchpartyQuery, "variables": vars})
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
		if pickedShowRe.MatchString(q) {
			return partyShow{}, fmt.Errorf("no show %s", q)
		}
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

const titleSuggestQuery = `query ($q: String) {
  Page(perPage: 10) {
    media(search: $q, type: ANIME, isAdult: false, sort: SEARCH_MATCH) {
      id format seasonYear title { romaji english }
    }
  }
}`

// suggestClient is quick to give up: Discord drops suggestions after three
// seconds.
var suggestClient = &http.Client{Timeout: 2500 * time.Millisecond}

// suggestTitles answers typing in /watchparty's anime option with the
// matching AniList titles, so the host picks the exact show.
func (b *bot) suggestTitles(i *discordgo.InteractionCreate) {
	var q string
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "anime" && o.Focused {
			q = strings.TrimSpace(o.StringValue())
		}
	}
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	if len(q) >= 2 && !pickedShowRe.MatchString(q) {
		choices = titleChoices(q)
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	}); err != nil {
		log.Printf("title suggestions: %v", err)
	}
}

type titleMatch struct {
	ID         int    `json:"id"`
	Format     string `json:"format"`
	SeasonYear int    `json:"seasonYear"`
	Title      struct{ Romaji, English string }
}

func titleChoices(q string) []*discordgo.ApplicationCommandOptionChoice {
	body, _ := json.Marshal(map[string]any{"query": titleSuggestQuery, "variables": map[string]string{"q": q}})
	req, _ := http.NewRequest("POST", "https://graphql.anilist.co", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "otakase-bot (https://github.com/TheXykril/otakase)")
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	resp, err := suggestClient.Do(req)
	if err != nil {
		return choices
	}
	defer resp.Body.Close()
	var r struct {
		Data struct {
			Page struct{ Media []titleMatch } `json:"Page"`
		} `json:"data"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&r) != nil {
		return choices
	}
	for _, m := range r.Data.Page.Media {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: titleLabel(m), Value: fmt.Sprintf("anilist:%d", m.ID)})
	}
	return choices
}

// titleLabel is how a suggestion reads: the title, with the romaji one,
// format and year to tell apart shows of the same name.
func titleLabel(m titleMatch) string {
	name := m.Title.English
	if name == "" {
		name = m.Title.Romaji
	} else if m.Title.Romaji != "" && !strings.EqualFold(m.Title.Romaji, name) {
		name += " (" + m.Title.Romaji + ")"
	}
	var info []string
	if m.Format != "" {
		info = append(info, strings.ReplaceAll(m.Format, "_", " "))
	}
	if m.SeasonYear > 0 {
		info = append(info, strconv.Itoa(m.SeasonYear))
	}
	if len(info) > 0 {
		return clip(name, 100-len(strings.Join(info, ", "))-3) + " · " + strings.Join(info, ", ")
	}
	return clip(name, 100)
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
			if pickedShowRe.MatchString(title) {
				return errorEmbed("Couldn't look up that show on AniList. Try again."), nil
			}
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
		// Kept until the start; after that the party ends once everyone has
		// left (or partyNoShowWait after the start if nobody came).
		b.store.update(func(d *storeData) {
			r := d.Rooms[channel]
			r.Event, r.KeepUntil = ev.ID, start
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

// partyNoShowWait is how long after the start a party nobody joined is kept.
const partyNoShowWait = 15 * time.Minute

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
	if err := b.s.GuildChannelsReorder(b.cfg.GuildID, categoriesFirst(chans, c.ID)); err != nil {
		log.Printf("move watch party category: %v", err) // stays at the bottom
	}
	return c.ID, nil
}

// categoriesFirst orders the server's categories with first at the top and
// the others after it in their current order.
func categoriesFirst(chans []*discordgo.Channel, first string) []*discordgo.Channel {
	var cats []*discordgo.Channel
	for _, c := range chans {
		if c.Type == discordgo.ChannelTypeGuildCategory && c.ID != first {
			cats = append(cats, c)
		}
	}
	sort.SliceStable(cats, func(i, j int) bool { return cats[i].Position < cats[j].Position })
	order := []*discordgo.Channel{{ID: first, Position: 0}}
	for n, c := range cats {
		order = append(order, &discordgo.Channel{ID: c.ID, Position: n + 1})
	}
	return order
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
// events deletes the event and its channel, and the post says it was
// cancelled. It acknowledges first, since the deletes can take longer than
// Discord's three seconds.
func (b *bot) cancelParty(i *discordgo.InteractionCreate, eventID string) {
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate}); err != nil {
		log.Printf("cancel party defer: %v", err)
		return
	}
	fail := func(format string, a ...any) {
		if _, err := b.s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Embeds: []*discordgo.MessageEmbed{errorEmbed(format, a...)}, Flags: discordgo.MessageFlagsEphemeral}); err != nil {
			log.Printf("cancel party reply: %v", err)
		}
	}
	ev, err := b.s.GuildScheduledEvent(b.cfg.GuildID, eventID, false)
	if err != nil {
		fail("This watch party is already over or cancelled.")
		return
	}
	user := i.Member.User.ID
	if user != partyHost(ev) && i.Member.Permissions&discordgo.PermissionManageEvents == 0 {
		fail("Only the host or a moderator can cancel this watch party.")
		return
	}
	if err := b.s.GuildScheduledEventDelete(b.cfg.GuildID, eventID); err != nil {
		log.Printf("cancel party: %v", err)
		fail("Couldn't cancel the event.")
		return
	}
	e := &discordgo.MessageEmbed{Title: "Cancelled: " + strings.TrimPrefix(ev.Name, "Watch party: "), Color: 0x8A8276,
		Description: fmt.Sprintf("Watch party cancelled by <@%s>.", user)}
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{e}, Components: &[]discordgo.MessageComponent{}}); err != nil {
		log.Printf("cancel party update: %v", err)
	}
	b.dropPartyRoom(eventID)
}

// dropPartyRoom deletes a cancelled party's voice channel straight away.
func (b *bot) dropPartyRoom(eventID string) {
	var id string
	var r room
	b.store.view(func(d *storeData) {
		for ch, x := range d.Rooms {
			if x.Event == eventID {
				id, r = ch, x
			}
		}
	})
	if id != "" {
		r.Event = "" // already deleted
		b.deleteRoom(id, r, "Watch party cancelled")
	}
}

// endParty finishes a party's event once its channel is gone: a running one
// is marked completed, one that never started is deleted.
func (b *bot) endParty(eventID string) {
	ev, err := b.s.GuildScheduledEvent(b.cfg.GuildID, eventID, false)
	if err != nil {
		return // already over or deleted
	}
	switch ev.Status {
	case discordgo.GuildScheduledEventStatusActive:
		_, err = b.s.GuildScheduledEventEdit(b.cfg.GuildID, eventID, &discordgo.GuildScheduledEventParams{Status: discordgo.GuildScheduledEventStatusCompleted})
	case discordgo.GuildScheduledEventStatusScheduled:
		err = b.s.GuildScheduledEventDelete(b.cfg.GuildID, eventID)
	}
	if err != nil {
		log.Printf("end party: %v", err)
	}
}
