package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// /post-roles posts a card with buttons members press to add or remove
// roles: their system, release pings, tester and watch party pings. Roles
// are found by name; the watch party one is made when missing, the others
// are left off the card. Running it again replaces the old card. The
// buttons carry the role id, so they keep working after a restart.

// selfRole is one button on the card.
type selfRole struct {
	Name   string // role name, matched ignoring case
	Text   string // what it is for
	Create bool   // made when the server doesn't have it
}

var selfRoleList = []selfRole{
	{Name: "Linux", Text: "you use Otakase on Linux"},
	{Name: "Windows", Text: "you use Otakase on Windows"},
	{Name: "macOS", Text: "you use Otakase on a Mac"},
	{Name: "Release pings", Text: "a ping when a new version is out"},
	{Name: "Tester", Text: "try new builds early and say what breaks"},
	{Name: "Watch party pings", Text: "a ping when a watch party is planned", Create: true},
}

// rolesCard is where the card is, so a new one replaces it.
type rolesCard struct {
	Channel string `json:"channel"`
	Message string `json:"message"`
}

func selfRoleCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{Name: "post-roles", Description: "Post the pick-your-roles card in this channel (replaces the old one)",
		DefaultMemberPermissions: &permManageServer}
}

// findSelfRoles matches the list to the server's roles: name → role.
func findSelfRoles(roles []*discordgo.Role) map[string]*discordgo.Role {
	out := map[string]*discordgo.Role{}
	for _, s := range selfRoleList {
		for _, r := range roles {
			if strings.EqualFold(strings.TrimSpace(r.Name), s.Name) {
				out[s.Name] = r
				break
			}
		}
	}
	return out
}

// selfRolesCard is the components v2 card for the roles found.
func selfRolesCard(found map[string]*discordgo.Role) []discordgo.MessageComponent {
	color := shu
	small, divider := discordgo.SeparatorSpacingSizeSmall, true
	var lines []string
	var rows []discordgo.MessageComponent
	var row discordgo.ActionsRow
	for _, s := range selfRoleList {
		r := found[s.Name]
		if r == nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("**%s** · %s", r.Name, s.Text))
		if len(row.Components) == 5 {
			rows = append(rows, row)
			row = discordgo.ActionsRow{}
		}
		row.Components = append(row.Components, discordgo.Button{Label: r.Name, Style: discordgo.SecondaryButton, CustomID: "selfrole:" + r.ID})
	}
	if len(row.Components) > 0 {
		rows = append(rows, row)
	}
	parts := []discordgo.MessageComponent{
		discordgo.TextDisplay{Content: "## Pick your roles\n" + strings.Join(lines, "\n")},
		discordgo.Separator{Divider: &divider, Spacing: &small},
	}
	parts = append(parts, rows...)
	parts = append(parts, discordgo.TextDisplay{Content: "-# Press a button to add the role, press it again to remove it."})
	return []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: parts}}
}

func (b *bot) guildRoles() []*discordgo.Role {
	if g, err := b.s.State.Guild(b.cfg.GuildID); err == nil && len(g.Roles) > 0 {
		return g.Roles
	}
	roles, err := b.s.GuildRoles(b.cfg.GuildID)
	if err != nil {
		log.Printf("listing roles: %v", err)
	}
	return roles
}

func (b *bot) postRoles(i *discordgo.InteractionCreate) {
	channel := i.ChannelID
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		found := findSelfRoles(b.guildRoles())
		var made []string
		for _, s := range selfRoleList {
			if found[s.Name] != nil || !s.Create {
				continue
			}
			yes, none := true, int64(0)
			r, err := b.s.GuildRoleCreate(b.cfg.GuildID, &discordgo.RoleParams{Name: s.Name, Mentionable: &yes, Permissions: &none},
				discordgo.WithAuditLogReason("Role for the pick-your-roles card"))
			if err != nil {
				log.Printf("create role %s: %v", s.Name, err)
				continue
			}
			found[s.Name] = r
			made = append(made, r.Name)
		}
		if len(found) == 0 {
			return errorEmbed("None of the roles exist yet: %s.", selfRoleNames()), nil
		}
		msg, err := b.s.ChannelMessageSendComplex(channel, &discordgo.MessageSend{
			Flags: discordgo.MessageFlagsIsComponentsV2, Components: selfRolesCard(found),
			AllowedMentions: &discordgo.MessageAllowedMentions{}})
		if err != nil {
			log.Printf("post roles: %v", err)
			return errorEmbed("Couldn't post the card here: %v", err), nil
		}
		var old *rolesCard
		b.store.update(func(d *storeData) {
			old = d.RolesCard
			d.RolesCard = &rolesCard{Channel: msg.ChannelID, Message: msg.ID}
		})
		if old != nil && old.Message != msg.ID {
			if err := b.s.ChannelMessageDelete(old.Channel, old.Message); err != nil {
				log.Printf("old roles card: %v", err)
			}
		}
		text := "Posted the roles card."
		if len(made) > 0 {
			text += " Made the role " + strings.Join(made, ", ") + "."
		}
		var missing []string
		for _, s := range selfRoleList {
			if found[s.Name] == nil {
				missing = append(missing, s.Name)
			}
		}
		if len(missing) > 0 {
			text += " Left off (no such role): " + strings.Join(missing, ", ") + "."
		}
		return &discordgo.MessageEmbed{Color: shu, Description: text}, nil
	})
}

func selfRoleNames() string {
	var names []string
	for _, s := range selfRoleList {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

// toggleSelfRole handles a button on the card.
func (b *bot) toggleSelfRole(i *discordgo.InteractionCreate, roleID string) {
	var role *discordgo.Role
	for _, r := range findSelfRoles(b.guildRoles()) {
		if r.ID == roleID {
			role = r
		}
	}
	if role == nil {
		b.respond(i, true, errorEmbed("That role is gone. Ask a moderator to post the card again."))
		return
	}
	user := i.Member.User.ID
	var err error
	text := "Added **" + role.Name + "**."
	if contains(i.Member.Roles, roleID) {
		err = b.s.GuildMemberRoleRemove(b.cfg.GuildID, user, roleID, discordgo.WithAuditLogReason("Roles card"))
		text = "Removed **" + role.Name + "**."
	} else {
		err = b.s.GuildMemberRoleAdd(b.cfg.GuildID, user, roleID, discordgo.WithAuditLogReason("Roles card"))
	}
	if err != nil {
		log.Printf("self role: %v", err)
		b.respond(i, true, errorEmbed("Couldn't change your roles. The bot's role has to be above **%s**.", role.Name))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: text})
}
