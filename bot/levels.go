package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Levels: members earn 15-25 XP for a message, at most once a minute, in
// the chat channels (not in bot command or staff channels, and not for
// messages the spam filter removed). Level n needs 5n²+50n+100 more XP to
// reach n+1. Nothing is posted on a level up; /rank and /levels show it.
// Roles named "Level 5", "Level 10", "Level 20" and "Level 30", when the
// server has them, follow the highest level reached (staff can pick other
// levels and roles on the dashboard instead). XP is kept in the
// store but written at most every 30 seconds.

const (
	xpCooldown = time.Minute
	xpMin      = 15
	xpMax      = 25
	xpSaveGap  = 30 * time.Second
)

// levelRoleSteps are the levels that have a role, when the server has it.
var levelRoleSteps = []int{5, 10, 20, 30}

type xpTracker struct {
	mu    sync.Mutex
	last  map[string]time.Time // member → last message that earned XP
	dirty bool                 // XP changed since the last save
}

func levelCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{Name: "rank", Description: "Your level and XP, or someone else's", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Whose rank (default: yours)"},
		}},
		{Name: "levels", Description: "The 10 most active members by level"},
	}
}

// xpToNext is the XP level n needs to reach level n+1.
func xpToNext(n int) int { return 5*n*n + 50*n + 100 }

// levelFor splits total XP into a level, the XP into that level and the
// XP the next level needs.
func levelFor(total int) (level, into, need int) {
	into = total
	for into >= xpToNext(level) {
		into -= xpToNext(level)
		level++
	}
	return level, into, xpToNext(level)
}

// earn reports whether userID may earn XP now, and marks it.
func (x *xpTracker) earn(userID string, now time.Time) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.last == nil {
		x.last = map[string]time.Time{}
	}
	if now.Sub(x.last[userID]) < xpCooldown {
		return false
	}
	x.last[userID] = now
	if len(x.last) > 5000 {
		for id, t := range x.last {
			if now.Sub(t) >= xpCooldown {
				delete(x.last, id)
			}
		}
	}
	return true
}

// gainXP gives XP for a chat message that got past the filters.
func (b *bot) gainXP(m *discordgo.MessageCreate) {
	if m.Member == nil || strings.TrimSpace(m.Content) == "" && len(m.Attachments) == 0 {
		return
	}
	if p := b.place(m.ChannelID); p.Staff || p.Bots {
		return
	}
	if !b.xp.earn(m.Author.ID, time.Now()) {
		return
	}
	gain := xpMin + rand.IntN(xpMax-xpMin+1)
	var total int
	// Changed in memory only; xpSaveLoop writes it out.
	b.store.view(func(d *storeData) {
		if d.XP == nil {
			d.XP = map[string]int{}
		}
		d.XP[m.Author.ID] += gain
		total = d.XP[m.Author.ID]
		earnHanko(d, m.Author.ID, 1)
	})
	b.xp.mu.Lock()
	b.xp.dirty = true
	b.xp.mu.Unlock()
	level, _, _ := levelFor(total)
	b.syncLevelRoles(m.Author.ID, m.Member.Roles, level)
}

// syncLevelRoles gives the member the role for the highest level step they
// reached and takes the lower ones away. Steps without a role are skipped.
func (b *bot) syncLevelRoles(userID string, has []string, level int) {
	roles := b.levelRoles()
	if len(roles) == 0 {
		return
	}
	want, best := "", -1
	for s, id := range roles {
		if id != "" && level >= s && s > best {
			want, best = id, s
		}
	}
	for _, id := range roles {
		if id != want && contains(has, id) {
			if err := b.s.GuildMemberRoleRemove(b.cfg.GuildID, userID, id); err != nil {
				log.Printf("level role remove: %v", err)
			}
		}
	}
	if want != "" && !contains(has, want) {
		if err := b.s.GuildMemberRoleAdd(b.cfg.GuildID, userID, want); err != nil {
			log.Printf("level role add: %v", err)
		}
	}
}

// levelRoles is level → role id: the roles staff picked on the dashboard,
// else the "Level N" roles found by name.
func (b *bot) levelRoles() map[int]string {
	picked := map[int]string{}
	b.store.view(func(d *storeData) {
		for l, id := range d.LevelRoles {
			picked[l] = id
		}
	})
	if len(picked) > 0 {
		return picked
	}
	var roles []*discordgo.Role
	if b.s != nil && b.s.State != nil {
		if g, err := b.s.State.Guild(b.cfg.GuildID); err == nil {
			roles = g.Roles
		}
	}
	out := map[int]string{}
	for _, r := range roles {
		for _, s := range levelRoleSteps {
			if strings.EqualFold(strings.TrimSpace(r.Name), fmt.Sprintf("Level %d", s)) {
				out[s] = r.ID
			}
		}
	}
	return out
}

// xpSaveLoop writes XP to disk at most every 30 seconds, to spare the SD card.
func (b *bot) xpSaveLoop() {
	for {
		time.Sleep(xpSaveGap)
		b.saveXP()
	}
}

// saveXP writes the store when XP changed.
func (b *bot) saveXP() {
	b.xp.mu.Lock()
	dirty := b.xp.dirty
	b.xp.dirty = false
	b.xp.mu.Unlock()
	if dirty {
		b.store.update(func(*storeData) {})
	}
}

// xpRanked is every member with XP, most first.
func xpRanked(xp map[string]int) []guessRank {
	var list []guessRank
	for id, n := range xp {
		if n > 0 {
			list = append(list, guessRank{id, n})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Points != list[j].Points {
			return list[i].Points > list[j].Points
		}
		return list[i].User < list[j].User
	})
	return list
}

func (b *bot) levels(i *discordgo.InteractionCreate) {
	var list []guessRank
	b.store.view(func(d *storeData) { list = xpRanked(d.XP) })
	if len(list) == 0 {
		b.respond(i, true, errorEmbed("Nobody has any XP yet. Chat a bit!"))
		return
	}
	var lines []string
	for k, r := range list[:min(10, len(list))] {
		level, _, _ := levelFor(r.Points)
		lines = append(lines, fmt.Sprintf("**%d.** <@%s> · level %d · %s XP", k+1, r.User, level, thousands(r.Points)))
	}
	b.respond(i, onlyAsker(i), &discordgo.MessageEmbed{Title: "Levels", Description: strings.Join(lines, "\n"), Color: shu,
		Footer: &discordgo.MessageEmbedFooter{Text: "XP for chatting, once a minute · /rank for yours"}})
}

func (b *bot) rank(i *discordgo.InteractionCreate) {
	who := i.Member.User
	if o, ok := optionMap(i)["member"]; ok {
		who = optUser(i, o)
	}
	if who.Bot {
		b.respond(i, true, errorEmbed("Bots don't collect XP."))
		return
	}
	var total, pos int
	b.store.view(func(d *storeData) {
		total = d.XP[who.ID]
		for k, r := range xpRanked(d.XP) {
			if r.User == who.ID {
				pos = k + 1
			}
		}
	})
	if total == 0 {
		if who.ID == i.Member.User.ID {
			b.respond(i, true, errorEmbed("You have no XP yet. Chat a bit and check again."))
		} else {
			b.respond(i, true, errorEmbed("<@%s> has no XP yet.", who.ID))
		}
		return
	}
	level, into, need := levelFor(total)
	name := who.GlobalName
	if name == "" {
		name = who.Username
	}
	if r := i.ApplicationCommandData().Resolved; r != nil && r.Members[who.ID] != nil && r.Members[who.ID].Nick != "" {
		name = r.Members[who.ID].Nick
	} else if who.ID == i.Member.User.ID && i.Member.Nick != "" {
		name = i.Member.Nick
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: answerFlags(i)}}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	avatar, _ := fetchImage(who.AvatarURL("256"))
	png, err := renderRankCard(rankCard{Name: name, Avatar: avatar, Level: level, Rank: pos, Into: into, Need: need, Total: total})
	if err == nil {
		color := shu
		box := []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: []discordgo.MessageComponent{
			discordgo.MediaGallery{Items: []discordgo.MediaGalleryItem{{Media: discordgo.UnfurledMediaItem{URL: "attachment://rank.png"}}}},
		}}}
		err = b.editComponentsV2(i.Interaction, &discordgo.WebhookEdit{Components: &box,
			Files: []*discordgo.File{{Name: "rank.png", ContentType: "image/png", Reader: bytes.NewReader(png)}}})
		if err == nil {
			return
		}
	}
	log.Printf("rank card: %v", err)
	e := &discordgo.MessageEmbed{Color: shu, Title: name,
		Description: fmt.Sprintf("**Level %d** · rank #%d\n%s %s / %s XP", level, pos, xpBar(into, need, 14), thousands(into), thousands(need)),
		Footer:      &discordgo.MessageEmbedFooter{Text: thousands(total) + " XP in total"}}
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e}}); err != nil {
		log.Printf("edit: %v", err)
	}
}

// xpBar is a text progress bar n blocks long.
func xpBar(into, need, n int) string {
	done := 0
	if need > 0 {
		done = min(n, into*n/need)
	}
	return strings.Repeat("▰", done) + strings.Repeat("▱", n-done)
}

// ── The /rank picture ──

type rankCard struct {
	Name   string
	Avatar image.Image // nil draws the ring only
	Level  int
	Rank   int
	Into   int
	Need   int
	Total  int
}

// renderRankCard draws /rank in the style of the /profile card: the avatar
// in a Shu ring, the name, rank and level, and the XP bar.
func renderRankCard(c rankCard) ([]byte, error) {
	f, err := loadFaces()
	if err != nil {
		return nil, err
	}
	const w, h = cardWidth, 176.0
	d := &canvas{f: f, img: image.NewRGBA(image.Rect(0, 0, w*cardScale, h*cardScale))}
	d.fill(0, 0, w, h, cardBg)
	// Shu glow from the bottom left, as on the profile card.
	b := d.img.Bounds()
	for py := range b.Dy() {
		for px := range b.Dx() {
			gx, gy := float64(px)/float64(b.Dx())/1.2, (1-float64(py)/float64(b.Dy()))/0.9
			if g := 1 - math.Hypot(gx, gy)/0.6; g > 0 {
				d.img.SetRGBA(px, py, blend(cardShu, d.img.RGBAAt(px, py), 0.28*g))
			}
		}
	}

	ax, ay, as := 24.0, 33.0, 110.0
	d.disc(ax+as/2, ay+as/2, as/2, cardAccent)
	d.disc(ax+as/2, ay+as/2, as/2-3, cardBg)
	if c.Avatar != nil {
		in := as - 12
		r := d.rect(ax+6, ay+6, in, in)
		src := coverCrop(c.Avatar, r.Dx(), r.Dy())
		draw.DrawMask(d.img, r, src, image.Point{}, circleMask{cx: float64(r.Min.X) + float64(r.Dx())/2, cy: float64(r.Min.Y) + float64(r.Dy())/2, r: float64(r.Dx()) / 2}, r.Min, draw.Over)
	}

	// Rank and level, top right.
	x := float64(w - cardPad)
	stat := func(label, value string, valueColor color.Color) {
		vw := d.width(f.big, value)
		lw := d.spacedWidth(f.label, label, 0.08)
		x -= vw
		d.text(f.big, value, x, 66, valueColor)
		x -= lw + 8
		d.spaced(f.label, label, x, 66, cardDim, 0.08)
		x -= 22
	}
	stat("LEVEL", fmt.Sprint(c.Level), cardAccent)
	stat("RANK", fmt.Sprintf("#%d", c.Rank), cardText)

	left := 158.0
	d.text(f.name, fit(f.name, c.Name, (x-left)*cardScale), left, 72, cardText)

	// XP bar.
	barY, barW, barH := 98.0, float64(w-cardPad)-left, 14.0
	d.roundRect(left, barY, barW, barH, barH/2, cardTile)
	if c.Need > 0 && c.Into > 0 {
		share := math.Min(float64(c.Into)/float64(c.Need), 1)
		d.roundRect(left, barY, math.Max(barW*share, barH), barH, barH/2, cardAccent)
	}
	progress := thousands(c.Into) + " / " + thousands(c.Need) + " XP"
	d.text(f.legend, progress, float64(w-cardPad)-d.width(f.legend, progress), barY+barH+22, cardSoft)
	d.text(f.legend, thousands(c.Total)+" XP in total", left, barY+barH+22, cardMuted)
	d.text(f.legend, "otakase", left, barY+barH+46, cardFaint)
	return encodeRounded(d.img)
}
