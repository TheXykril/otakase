package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Members link their AniList account with /link-anilist; /profile then shows
// what they watch and their stats. With an AniList app client id and secret
// configured the link is proven: the member approves the bot on anilist.co and
// pastes the code it shows, the bot trades it for a token and reads who they
// are. The token is kept so Plan to watch can add shows to their list
// (/unlink-anilist deletes it). Without them, members type their username, which needs no
// setup but isn't checked.
// Linked members get the "AniList linked" role when the server has one.

// anilistAPI and anilistTokenURL are variables so tests can point them at a
// fake server.
var (
	anilistAPI      = "https://graphql.anilist.co"
	anilistTokenURL = "https://anilist.co/api/v2/oauth/token"
)

// anilistPinURL is the redirect URL the AniList app must have: AniList then
// shows the code on its own page for the member to copy.
const anilistPinURL = "https://anilist.co/api/v2/oauth/pin"

type anilistLink struct {
	ID       int       `json:"id"`
	Name     string    `json:"name"`
	Verified bool      `json:"verified"`
	Linked   time.Time `json:"linked"`
	Token    string    `json:"token,omitempty"` // AniList access token from a proven link, for Plan to watch
}

func anilistCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{Name: "link-anilist", Description: "Link your AniList account to show it with /profile"},
		{Name: "unlink-anilist", Description: "Remove your linked AniList account"},
		{Name: "profile", Description: "Someone's AniList: watching now, episodes watched, top genres", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Whose profile (default: yours)"},
		}},
	}
}

// anilistAuthURL is where members approve the bot. AniList only offers the
// authorization code grant (response_type=token fails with
// unsupported_grant_type), so the code shown is traded for a token after.
func anilistAuthURL(clientID string) string {
	return "https://anilist.co/api/v2/oauth/authorize?" + url.Values{
		"client_id": {clientID}, "redirect_uri": {anilistPinURL}, "response_type": {"code"}}.Encode()
}

// anilistExchange trades the code a member pasted for an access token.
func anilistExchange(clientID, secret, code string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"grant_type": "authorization_code", "client_id": clientID, "client_secret": secret,
		"redirect_uri": anilistPinURL, "code": code,
	})
	req, _ := http.NewRequest("POST", anilistTokenURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	var r struct {
		AccessToken string `json:"access_token"`
	}
	if err := getJSON(req, &r); err != nil {
		return "", err
	}
	if r.AccessToken == "" {
		return "", fmt.Errorf("no access token")
	}
	return r.AccessToken, nil
}

func (b *bot) linkAniList(i *discordgo.InteractionCreate) {
	if b.cfg.AniListClientID == "" || b.cfg.AniListSecret == "" {
		b.anilistForm(i, "anilist-user-form", "Link AniList", discordgo.TextInput{
			CustomID: "name", Label: "AniList username", Style: discordgo.TextInputShort, Required: true,
			MinLength: 2, MaxLength: 20, Placeholder: "As in anilist.co/user/…"})
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{
		Title: "Link your AniList account",
		Description: "1. Open AniList and press **Approve**\n2. Copy the code it shows\n3. Press **Paste code** here\n\n" +
			"The bot reads who you are and keeps AniList's access so **Plan to watch** can add shows to your list. `/unlink-anilist` removes it.",
		Color: shu,
	}, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Open AniList", URL: anilistAuthURL(b.cfg.AniListClientID), Style: discordgo.LinkButton},
		discordgo.Button{Label: "Paste code", CustomID: "anilist-paste", Style: discordgo.PrimaryButton},
	}})
}

func (b *bot) anilistForm(i *discordgo.InteractionCreate, id, title string, input discordgo.TextInput) {
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{CustomID: id, Title: title, Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{input}},
		}},
	}); err != nil {
		log.Printf("anilist form: %v", err)
	}
}

func (b *bot) anilistPasteForm(i *discordgo.InteractionCreate) {
	b.anilistForm(i, "anilist-token-form", "Paste your AniList code", discordgo.TextInput{
		CustomID: "code", Label: "Code from AniList", Style: discordgo.TextInputParagraph, Required: true,
		MinLength: 20, MaxLength: 4000})
}

// finishAniListLink handles both forms: the username one and the pasted code.
func (b *bot) finishAniListLink(i *discordgo.InteractionCreate, form string) {
	v := modalValues(i)
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		var (
			u     anilistUser
			err   error
			token string
		)
		verified := form == "anilist-token-form"
		if verified {
			token, err = anilistExchange(b.cfg.AniListClientID, b.cfg.AniListSecret, strings.Join(strings.Fields(v["code"]), ""))
			if err == nil {
				u, err = anilistViewer(token)
			}
		} else {
			u, err = anilistUserByName(v["name"])
		}
		if err != nil {
			log.Printf("anilist link: %v", err)
			if verified {
				return errorEmbed("AniList didn't accept that code. Run `/link-anilist` and try again."), nil
			}
			return errorEmbed("No AniList user called **%s**.", v["name"]), nil
		}
		userID := i.Member.User.ID
		b.store.update(func(d *storeData) {
			d.AniList[userID] = anilistLink{ID: u.ID, Name: u.Name, Verified: verified, Linked: time.Now(), Token: token}
		})
		if b.cfg.AniListRole != "" {
			if err := b.s.GuildMemberRoleAdd(b.cfg.GuildID, userID, b.cfg.AniListRole); err != nil {
				log.Printf("anilist role add: %v", err)
			}
		}
		return &discordgo.MessageEmbed{Description: fmt.Sprintf("Linked to AniList **%s**. Try `/profile`.", u.Name), Color: shu}, nil
	})
}

func (b *bot) unlinkAniList(i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID
	var had bool
	b.store.update(func(d *storeData) {
		_, had = d.AniList[userID]
		delete(d.AniList, userID)
	})
	if !had {
		b.respond(i, true, errorEmbed("You have no AniList account linked."))
		return
	}
	if b.cfg.AniListRole != "" {
		if err := b.s.GuildMemberRoleRemove(b.cfg.GuildID, userID, b.cfg.AniListRole); err != nil {
			log.Printf("anilist role remove: %v", err)
		}
	}
	b.respond(i, true, &discordgo.MessageEmbed{Description: "AniList unlinked.", Color: shu})
}

func (b *bot) profile(i *discordgo.InteractionCreate) {
	who := i.Member.User
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "member" {
			who = o.UserValue(b.s)
		}
	}
	var link anilistLink
	var ok bool
	b.store.view(func(d *storeData) { link, ok = d.AniList[who.ID] })
	if !ok {
		if who.ID == i.Member.User.ID {
			b.respond(i, true, errorEmbed("Link your AniList first with `/link-anilist`."))
		} else {
			b.respond(i, true, errorEmbed("<@%s> hasn't linked AniList.", who.ID))
		}
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	edit, card := b.profileMessage(who.ID, link)
	if card != nil {
		err := b.editComponentsV2(i.Interaction, card)
		if err == nil {
			return
		}
		log.Printf("profile card message: %v", err) // fall back to the picture in an embed
	}
	if _, err := b.s.InteractionResponseEdit(i.Interaction, edit); err != nil {
		log.Printf("edit: %v", err)
	}
}

// editComponentsV2 edits the deferred interaction reply into a components v2
// message (discordgo's WebhookEdit has no flags).
func (b *bot) editComponentsV2(it *discordgo.Interaction, edit *discordgo.WebhookEdit) error {
	data := struct {
		*discordgo.WebhookEdit
		Flags discordgo.MessageFlags `json:"flags"`
	}{edit, discordgo.MessageFlagsIsComponentsV2}
	uri := discordgo.EndpointWebhookMessage(it.AppID, it.Token, "@original")
	contentType, body, err := discordgo.MultipartBodyWithJSON(data, edit.Files)
	if err != nil {
		return err
	}
	for _, f := range edit.Files {
		if sk, ok := f.Reader.(io.Seeker); ok {
			sk.Seek(0, io.SeekStart) // the embed fallback reads them again
		}
	}
	_, err = b.s.RequestRaw("PATCH", uri, contentType, body, uri, 0)
	return err
}

// profileMessage is the /profile answer. card is the picture card in a Shu
// container (components v2): the picture, what they last watched and the
// Profile / Anime list buttons. edit is the fallback: the picture in an
// embed, or the plain embed when drawing fails (card is nil then).
func (b *bot) profileMessage(discordID string, link anilistLink) (edit, card *discordgo.WebhookEdit) {
	u, err := anilistProfile(link.ID)
	if err != nil {
		log.Printf("anilist profile: %v", err)
		return &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{errorEmbed("Couldn't load **%s** from AniList right now.", link.Name)}}, nil
	}
	watching, err := anilistWatching(link.ID)
	hidden := err != nil
	if hidden {
		log.Printf("anilist watching: %v", err) // private list: show the stats anyway
	}
	buttons := linkButtons("Profile", u.SiteURL, "Anime list", strings.TrimSuffix(u.SiteURL, "/")+"/animelist")
	c := profileCardFor(u, watching, hidden, link.Verified)
	c.Otakase = b.otakaseWatched(discordID)
	png, err := renderProfileCard(c)
	if err != nil {
		log.Printf("profile card: %v", err)
		e := profileEmbed(discordID, u, watching, link.Verified)
		if hidden {
			e.Fields[0].Value = "Hidden on AniList"
		}
		if c.Otakase != "" {
			e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Watched in Otakase", Value: c.Otakase, Inline: true})
		}
		return &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e}, Components: &buttons}, nil
	}
	file := &discordgo.File{Name: "profile.png", ContentType: "image/png", Reader: bytes.NewReader(png)}
	last, lastErr := anilistLastWatched(link.ID)
	if lastErr != nil {
		log.Printf("anilist activity: %v", lastErr)
	}
	e := &discordgo.MessageEmbed{Color: shu, Image: &discordgo.MessageEmbedImage{URL: "attachment://profile.png"}}
	if last.Title != "" {
		e.Footer = &discordgo.MessageEmbedFooter{Text: last.line(false)}
		e.Timestamp = time.Unix(last.At, 0).UTC().Format(time.RFC3339)
	}
	watchLine := b.profileWatchLine(discordID, appNow()) // badges, the year wrap
	e.Description = watchLine
	edit = &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e}, Components: &buttons, Files: []*discordgo.File{file}}

	color := shu
	parts := []discordgo.MessageComponent{
		discordgo.MediaGallery{Items: []discordgo.MediaGalleryItem{{Media: discordgo.UnfurledMediaItem{URL: "attachment://profile.png"}}}},
	}
	if last.Title != "" {
		parts = append(parts, discordgo.TextDisplay{Content: fmt.Sprintf("-# %s · <t:%d:R>", last.line(true), last.At)})
	}
	if watchLine != "" {
		parts = append(parts, discordgo.TextDisplay{Content: watchLine})
	}
	parts = append(parts, buttons[0])
	box := []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: parts}}
	card = &discordgo.WebhookEdit{Components: &box, Files: []*discordgo.File{file}}
	return edit, card
}

// lastWatched is their latest AniList list activity that watched or
// finished something.
type lastWatched struct {
	Title    string
	Progress string // "21" or "6 - 7"; empty when they finished it
	At       int64
}

// line is "Last watched Title ep 21" or "Finished Title", the title bold
// in markdown.
func (l lastWatched) line(md bool) string {
	t := l.Title
	if md {
		t = "**" + mdEscape(t) + "**"
	}
	if l.Progress == "" {
		return "Finished " + t
	}
	ep := "ep " + l.Progress
	if strings.Contains(l.Progress, "-") {
		ep = "eps " + strings.ReplaceAll(l.Progress, " - ", "–")
	}
	return "Last watched " + t + " " + ep
}

// anilistLastWatched finds their latest watched or completed anime, adult
// titles skipped. Empty when the activity is private or there is none.
func anilistLastWatched(id int) (lastWatched, error) {
	var r struct {
		Data struct {
			Page struct {
				Activities []struct {
					Status    string  `json:"status"`
					Progress  *string `json:"progress"`
					CreatedAt int64   `json:"createdAt"`
					Media     *struct {
						IsAdult bool `json:"isAdult"`
						Title   struct{ Romaji, English string }
					} `json:"media"`
				} `json:"activities"`
			} `json:"Page"`
		} `json:"data"`
	}
	err := anilistPost("", `query ($id: Int) {
  Page(perPage: 15) {
    activities(userId: $id, type: ANIME_LIST, sort: ID_DESC) {
      ... on ListActivity { status progress createdAt media { isAdult title { romaji english } } }
    }
  }
}`, map[string]any{"id": id}, &r)
	for _, a := range r.Data.Page.Activities {
		watched := strings.HasSuffix(a.Status, "episode") && a.Progress != nil
		if a.Media == nil || a.Media.IsAdult || !(watched || a.Status == "completed" || a.Status == "rewatched") {
			continue
		}
		l := lastWatched{Title: a.Media.Title.English, At: a.CreatedAt}
		if l.Title == "" {
			l.Title = a.Media.Title.Romaji
		}
		if r := []rune(l.Title); len(r) > 60 {
			l.Title = strings.TrimSpace(string(r[:59])) + "…"
		}
		if watched {
			l.Progress = *a.Progress
		}
		return l, nil
	}
	return lastWatched{}, err
}

func mdEscape(s string) string {
	return strings.NewReplacer("*", "\\*", "_", "\\_", "~", "\\~", "`", "\\`", "|", "\\|").Replace(s)
}

// profileCardFor picks what the picture card shows and downloads its images.
// Adult titles and genres are left out, as everywhere else in the bot.
func profileCardFor(u anilistUser, watching []watchingEntry, hidden, verified bool) profileCard {
	a := u.Statistics.Anime
	c := profileCard{
		Name: u.Name, Score: a.MeanScore, Anime: a.Count, Episodes: a.EpisodesWatched,
		Days: float64(a.MinutesWatched) / 1440, Hidden: hidden,
		Note: "/link-anilist to get yours",
	}
	if !verified {
		c.Note = "username not verified"
	}
	if u.CreatedAt > 0 {
		c.Since = time.Unix(u.CreatedAt, 0).UTC().Year()
	}
	for _, s := range a.Statuses {
		if s.Status == "COMPLETED" {
			c.Completed = s.Count
		}
	}
	for _, g := range a.Genres {
		if g.Genre != "Hentai" && len(c.Genres) < 5 {
			c.Genres = append(c.Genres, cardGenre{g.Genre, g.Count})
		}
	}
	urls := []string{u.Avatar.Large, u.BannerImage}
	for _, w := range watching {
		if w.Media.IsAdult || len(c.Shows) == 5 {
			continue
		}
		s := cardShow{Title: w.Media.Title.English, Progress: w.Progress}
		if s.Title == "" {
			s.Title = w.Media.Title.Romaji
		}
		if w.Media.Episodes != nil {
			s.Episodes = *w.Media.Episodes
		}
		c.Shows = append(c.Shows, s)
		urls = append(urls, w.Media.CoverImage.Large)
	}
	imgs := fetchImages(urls)
	c.Avatar, c.Banner = imgs[0], imgs[1]
	for k := range c.Shows {
		c.Shows[k].Cover = imgs[k+2]
		if c.Hero == nil {
			c.Hero = imgs[k+2]
		}
	}
	return c
}

type anilistUser struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	SiteURL     string `json:"siteUrl"`
	BannerImage string `json:"bannerImage"`
	CreatedAt   int64  `json:"createdAt"`
	Avatar      struct {
		Large string `json:"large"`
	} `json:"avatar"`
	Statistics struct {
		Anime struct {
			Count           int     `json:"count"`
			EpisodesWatched int     `json:"episodesWatched"`
			MinutesWatched  int     `json:"minutesWatched"`
			MeanScore       float64 `json:"meanScore"`
			Statuses        []struct {
				Status string `json:"status"`
				Count  int    `json:"count"`
			} `json:"statuses"`
			Genres []struct {
				Genre string `json:"genre"`
				Count int    `json:"count"`
			} `json:"genres"`
		} `json:"anime"`
	} `json:"statistics"`
}

type watchingEntry struct {
	Progress int `json:"progress"`
	Media    struct {
		SiteURL    string `json:"siteUrl"`
		Episodes   *int   `json:"episodes"`
		IsAdult    bool   `json:"isAdult"`
		CoverImage struct {
			Large string `json:"large"`
		} `json:"coverImage"`
		Title struct{ Romaji, English string }
	} `json:"media"`
}

const anilistUserFields = `id name siteUrl bannerImage createdAt avatar { large }
    statistics { anime { count episodesWatched minutesWatched meanScore statuses { status count } genres(limit: 6, sort: COUNT_DESC) { genre count } } }`

func anilistPost(token, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, _ := http.NewRequest("POST", anilistAPI, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return getJSON(req, out)
}

func anilistViewer(token string) (anilistUser, error) {
	var r struct {
		Data struct{ Viewer *anilistUser } `json:"data"`
	}
	if err := anilistPost(token, `{ Viewer { id name } }`, nil, &r); err != nil {
		return anilistUser{}, err
	}
	if r.Data.Viewer == nil {
		return anilistUser{}, fmt.Errorf("no viewer")
	}
	return *r.Data.Viewer, nil
}

func anilistUserByName(name string) (anilistUser, error) {
	return anilistUserQuery(`query ($name: String) { User(name: $name) { id name } }`, map[string]any{"name": name})
}

func anilistProfile(id int) (anilistUser, error) {
	return anilistUserQuery(`query ($id: Int) { User(id: $id) { `+anilistUserFields+` } }`, map[string]any{"id": id})
}

func anilistUserQuery(query string, vars map[string]any) (anilistUser, error) {
	var r struct {
		Data struct{ User *anilistUser } `json:"data"`
	}
	if err := anilistPost("", query, vars, &r); err != nil {
		return anilistUser{}, err
	}
	if r.Data.User == nil {
		return anilistUser{}, fmt.Errorf("no user")
	}
	return *r.Data.User, nil
}

func anilistWatching(id int) ([]watchingEntry, error) {
	var r struct {
		Data struct {
			Page struct {
				MediaList []watchingEntry `json:"mediaList"`
			} `json:"Page"`
		} `json:"data"`
	}
	err := anilistPost("", `query ($id: Int) {
  Page(perPage: 8) {
    mediaList(userId: $id, type: ANIME, status_in: [CURRENT, REPEATING], sort: UPDATED_TIME_DESC) {
      progress media { siteUrl episodes isAdult coverImage { large } title { romaji english } }
    }
  }
}`, map[string]any{"id": id}, &r)
	return r.Data.Page.MediaList, err
}

// profileEmbed is the /profile answer when the picture card can't be drawn.
// Adult titles and genres are left out, as everywhere else in the bot.
func profileEmbed(discordID string, u anilistUser, watching []watchingEntry, verified bool) *discordgo.MessageEmbed {
	var lines []string
	for _, w := range watching {
		if w.Media.IsAdult || len(lines) == 5 {
			continue
		}
		title := w.Media.Title.English
		if title == "" {
			title = w.Media.Title.Romaji
		}
		ep := fmt.Sprintf("ep %d", w.Progress)
		if w.Media.Episodes != nil {
			ep += fmt.Sprintf("/%d", *w.Media.Episodes)
		}
		if r := []rune(title); len(r) > 60 {
			title = string(r[:59]) + "…"
		}
		lines = append(lines, fmt.Sprintf("[%s](%s) · %s", strings.ReplaceAll(title, "]", ")"), w.Media.SiteURL, ep))
	}
	if len(lines) == 0 {
		lines = []string{"Nothing right now"}
	}
	a := u.Statistics.Anime
	var genres []string
	for _, g := range a.Genres {
		if g.Genre != "Hentai" && len(genres) < 3 {
			genres = append(genres, g.Genre)
		}
	}
	fields := []*discordgo.MessageEmbedField{
		{Name: "Watching now", Value: strings.Join(lines, "\n")},
		{Name: "Episodes watched", Value: fmt.Sprint(a.EpisodesWatched), Inline: true},
		{Name: "Anime", Value: fmt.Sprint(a.Count), Inline: true},
		{Name: "Time watched", Value: fmt.Sprintf("%.1f days", float64(a.MinutesWatched)/1440), Inline: true},
	}
	if len(genres) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Top genres", Value: strings.Join(genres, ", "), Inline: true})
	}
	if a.MeanScore > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Mean score", Value: fmt.Sprintf("%.1f", a.MeanScore), Inline: true})
	}
	foot := "AniList · link yours with /link-anilist"
	if !verified {
		foot = "AniList (username not verified) · link yours with /link-anilist"
	}
	e := &discordgo.MessageEmbed{
		Title: u.Name, URL: u.SiteURL, Description: "<@" + discordID + ">", Color: shu, Fields: fields,
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: u.Avatar.Large},
		Footer:    &discordgo.MessageEmbedFooter{Text: foot},
	}
	if u.BannerImage != "" {
		e.Image = &discordgo.MessageEmbedImage{URL: u.BannerImage}
	}
	return e
}
