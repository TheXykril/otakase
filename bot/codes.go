package main

// Codes staff make for giveaways and the like. They work with /redeem like
// the cheat codes (cheathunt.go): one use, first /redeem wins, and each
// gives a watch ticket. They stay out of the cheat hall of fame.
//
//	/codes mint count note   → the new codes, only shown to the one who made them
//	/codes list              → codes staff made that nobody redeemed yet
//	/codes revoke code       → deletes an unredeemed code

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	codesMaxMint = 25
	codesMaxNote = 100
)

func init() {
	registerCommand(&discordgo.ApplicationCommand{Name: "codes", Description: "Make /redeem codes for giveaways and events",
		DefaultMemberPermissions: &permMod,
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "mint", Description: "Make new one-use codes, shown only to you",
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionInteger, Name: "count", Description: fmt.Sprintf("How many (1 to %d)", codesMaxMint),
						Required: true, MinValue: ptr(1.0), MaxValue: codesMaxMint},
					{Type: discordgo.ApplicationCommandOptionString, Name: "note", Description: "What they're for, like a giveaway's name", MaxLength: codesMaxNote},
				}},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "list", Description: "Codes staff made that nobody redeemed yet"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "revoke", Description: "Delete a code nobody redeemed yet",
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "code", Description: "The code, like ABCD-EFGH-JKLM", Required: true, MaxLength: 20},
				}},
		}}, (*bot).codesCommand)
}

// mintCodes makes n codes and returns them.
func mintCodes(d *storeData, by, note string, n int, now time.Time) []string {
	if d.CheatCodes == nil {
		d.CheatCodes = map[string]cheatCode{}
	}
	out := make([]string, 0, n)
	for range n {
		code := newCheatCode()
		for _, taken := d.CheatCodes[code]; taken; _, taken = d.CheatCodes[code] {
			code = newCheatCode()
		}
		d.CheatCodes[code] = cheatCode{Found: now, Kind: "minted", MintedBy: by, Note: note}
		out = append(out, code)
	}
	return out
}

// unusedMinted is the minted codes nobody redeemed, oldest first.
func unusedMinted(d *storeData) []string {
	var out []string
	for c, x := range d.CheatCodes {
		if x.Finder == "" && x.RedeemedBy == "" {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		if c := d.CheatCodes[a].Found.Compare(d.CheatCodes[b].Found); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return out
}

// watchTicketLine says how many watch tickets the member has.
func watchTicketLine(b *bot, user string) string {
	n := 0
	b.store.view(func(d *storeData) { n = d.WatchTickets[user] })
	return fmt.Sprintf("You have %s.", dashPlural(n, "watch ticket"))
}

func (b *bot) codesCommand(i *discordgo.InteractionCreate) {
	if !b.staffOnly(i) {
		return
	}
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return
	}
	sub := data.Options[0]
	opts := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
	for _, o := range sub.Options {
		opts[o.Name] = o
	}
	user := i.Member.User.ID
	switch sub.Name {
	case "mint":
		n := min(max(int(opts["count"].IntValue()), 1), codesMaxMint)
		note := ""
		if o, ok := opts["note"]; ok {
			note = clip(cleanText(o.StringValue(), false), codesMaxNote)
		}
		var codes []string
		b.store.update(func(d *storeData) { codes = mintCodes(d, user, note, n, appNow()) })
		lines := make([]string, len(codes))
		for k, c := range codes {
			lines[k] = "`" + formatCheatCode(c) + "`"
		}
		what := dashPlural(n, "code")
		if note != "" {
			what += " for " + note
		}
		b.staffNote(&discordgo.MessageEmbed{Title: "Codes", Color: shu, Description: fmt.Sprintf("<@%s> made %s.", user, what)})
		b.respondQuiet(i, true, &discordgo.MessageEmbed{Title: "New codes", Color: shu,
			Description: strings.Join(lines, "\n") + "\n\nEach works once with `/redeem`, for whoever is first, and gives a watch ticket. Only you see this."})
	case "list":
		var lines []string
		b.store.view(func(d *storeData) {
			for _, c := range unusedMinted(d) {
				x := d.CheatCodes[c]
				line := fmt.Sprintf("`%s` · <@%s> · <t:%d:d>", formatCheatCode(c), x.MintedBy, x.Found.Unix())
				if x.Note != "" {
					line += " · " + x.Note
				}
				lines = append(lines, line)
			}
		})
		desc := "No unused codes. Make some with `/codes mint`."
		if len(lines) > 0 {
			desc = strings.Join(lines[max(0, len(lines)-30):], "\n")
		}
		b.respondQuiet(i, true, &discordgo.MessageEmbed{Title: "Unused codes", Color: shu, Description: desc,
			Footer: &discordgo.MessageEmbedFooter{Text: dashPlural(len(lines), "code") + " waiting"}})
	case "revoke":
		code, valid := normalizeCheatCode(opts["code"].StringValue())
		msg := "No unused staff code like that."
		b.store.update(func(d *storeData) {
			if x, ok := d.CheatCodes[code]; valid && ok && x.Finder == "" && x.RedeemedBy == "" {
				delete(d.CheatCodes, code)
				msg = "Deleted `" + formatCheatCode(code) + "`. It doesn't work any more."
			}
		})
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: msg})
	}
}
