package main

// Hanko (判子), the server's currency, named after the red seal stamps.
// Members earn them with no limit: game points in the Activity (one hanko
// a point), chatting (one a message that earns XP, so the XP cooldown
// applies) and watching in a linked Otakase app (hankoPerWatch for every
// hankoWatchSecs watched). Staff give or take them with /hanko-give. They
// are spent in the notebook giveaway's shop (giveaway.go).

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const (
	hankoWatchSecs = 20 * 60 // a typical episode
	hankoPerWatch  = 5
	hankoMaxGive   = 100_000
)

// hankoWallet is a member's hanko. Earned counts everything ever earned,
// for the board; spending doesn't lower it.
type hankoWallet struct {
	Balance   int `json:"balance"`
	Earned    int `json:"earned"`
	WatchSecs int `json:"watch_secs,omitempty"` // watched toward the next hankoWatchSecs
}

func init() {
	registerCommand(&discordgo.ApplicationCommand{Name: "hanko", Description: "Your hanko (判子), earned in games, chat and Otakase, and the richest members",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Whose (default you)"},
		}}, (*bot).hankoCommand)
	registerCommand(&discordgo.ApplicationCommand{Name: "hanko-give", Description: "Give or take a member's hanko", DefaultMemberPermissions: &permMod,
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Who", Required: true},
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "hanko", Description: "How many, minus to take", Required: true,
				MinValue: ptr(float64(-hankoMaxGive)), MaxValue: hankoMaxGive},
		}}, (*bot).hankoGiveCommand)
	registerHelp("`/hanko` your hanko (判子): earned in games, chat and Otakase, spent in giveaways")
	onHeartbeat(func(b *bot, ev heartbeatEvent) {
		if ev.Seconds > 0 {
			b.store.touch(func(d *storeData) { hankoWatch(d, ev.User, ev.Seconds) })
		}
	})
}

// earnHanko adds n hanko, n > 0.
func earnHanko(d *storeData, user string, n int) {
	if n <= 0 || user == "" {
		return
	}
	if d.Hanko == nil {
		d.Hanko = map[string]hankoWallet{}
	}
	w := d.Hanko[user]
	w.Balance += n
	w.Earned += n
	d.Hanko[user] = w
}

// hankoWatch counts watched seconds toward the next hanko.
func hankoWatch(d *storeData, user string, secs int) {
	if d.Hanko == nil {
		d.Hanko = map[string]hankoWallet{}
	}
	w := d.Hanko[user]
	w.WatchSecs += secs
	n := w.WatchSecs / hankoWatchSecs
	w.WatchSecs %= hankoWatchSecs
	d.Hanko[user] = w
	earnHanko(d, user, n*hankoPerWatch)
}

// spendHanko takes n hanko when the member has them.
func spendHanko(d *storeData, user string, n int) bool {
	w := d.Hanko[user]
	if n < 0 || w.Balance < n {
		return false
	}
	w.Balance -= n
	d.Hanko[user] = w
	return true
}

// adjustHanko adds n (or takes -n) hanko, never below zero, and returns the
// balance. Given hanko count as earned.
func adjustHanko(d *storeData, user string, n int) int {
	if n > 0 {
		earnHanko(d, user, n)
	} else if w, ok := d.Hanko[user]; ok {
		w.Balance = max(0, w.Balance+n)
		d.Hanko[user] = w
	}
	return d.Hanko[user].Balance
}

func hankoBalance(d *storeData, user string) int { return d.Hanko[user].Balance }

func (b *bot) hankoCommand(i *discordgo.InteractionCreate) {
	who := i.Member.User
	if o, ok := optionMap(i)["member"]; ok {
		who = optUser(i, o)
	}
	var w hankoWallet
	type rich struct {
		user string
		n    int
	}
	var top []rich
	b.store.view(func(d *storeData) {
		w = d.Hanko[who.ID]
		for u, x := range d.Hanko {
			if x.Balance > 0 {
				top = append(top, rich{u, x.Balance})
			}
		}
	})
	slices.SortFunc(top, func(a, b rich) int {
		if a.n != b.n {
			return b.n - a.n
		}
		return strings.Compare(a.user, b.user)
	})
	lines := []string{fmt.Sprintf("<@%s> has **%d** hanko 判 (%d earned in all).", who.ID, w.Balance, w.Earned), ""}
	if len(top) > 0 {
		lines = append(lines, "**Richest**")
		for k, r := range top[:min(10, len(top))] {
			lines = append(lines, fmt.Sprintf("%d. <@%s> · %d", k+1, r.user, r.n))
		}
		lines = append(lines, "")
	}
	lines = append(lines, fmt.Sprintf("Earn them with game points in `/games` (1 a point), chatting, and watching in Otakase with a linked app (%d every %d minutes). Spend them in giveaways.",
		hankoPerWatch, hankoWatchSecs/60))
	b.respondQuiet(i, true, &discordgo.MessageEmbed{Title: "Hanko 判子", Color: shu, Description: strings.Join(lines, "\n")})
}

func (b *bot) hankoGiveCommand(i *discordgo.InteractionCreate) {
	if !b.staffOnly(i) {
		return
	}
	o := optionMap(i)
	user := optUser(i, o["member"])
	n := int(o["hanko"].IntValue())
	if user == nil || user.Bot || n == 0 {
		b.respond(i, true, errorEmbed("Pick a member and a number other than 0."))
		return
	}
	var total int
	b.store.update(func(d *storeData) { total = adjustHanko(d, user.ID, n) })
	b.adminNote(i.Member.User.ID, user.ID, fmt.Sprintf("changed the hanko of %%s by %+d", n))
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Changed <@%s>'s hanko by %+d. They have %d now.", user.ID, n, total)})
}
