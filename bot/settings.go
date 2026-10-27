package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Runtime settings: staff change them with /settings without editing the
// add-on options. A change is kept in the data file as an override on top
// of the add-on option, which stays the default, and takes effect at once
// because the code reads them through b.settingBool, b.settingInt and
// b.settingFloat at the moment it needs them.

type settingType int

const (
	settingBool settingType = iota
	settingInt
	settingFloat
)

// setting is one runtime setting. Min and Max bound int and float values.
// Default gives the value from the add-on options.
type setting struct {
	Name     string
	Type     settingType
	Min, Max float64
	Desc     string
	Default  func(c config) string
}

// settings lists what /settings can change. Add a line here to make
// another value changeable, then read it with b.setting...(name).
var settings = []setting{
	{Name: "daily_airing", Type: settingBool, Desc: "Post the daily airing board", Default: func(c config) string { return strconv.FormatBool(c.DailyAiring) }},
	{Name: "airing_hour_utc", Type: settingInt, Min: 0, Max: 23, Desc: "UTC hour the airing board goes up (0-23)", Default: func(c config) string { return strconv.Itoa(c.AiringHour) }},
	{Name: "welcome_dm", Type: settingBool, Desc: "DM new members a welcome", Default: func(c config) string { return strconv.FormatBool(c.WelcomeDM) }},
	{Name: "score_sharing", Type: settingBool, Desc: "Members can share scores from the app in the reviews channel", Default: func(config) string { return "true" }},
	{Name: "watch_room_max", Type: settingInt, Min: 1, Max: 100, Desc: "How many people fit in the watch room; each one costs about 2.5 Mbps of upload at 720p", Default: func(config) string { return "10" }},
	{Name: "watch_room_max_perks", Type: settingInt, Min: 1, Max: 100, Desc: "How many people fit in a watch room opened by staff (no ticket, 8 hours)", Default: func(config) string { return "30" }},
	{Name: "watch_room_max_champion", Type: settingInt, Min: 1, Max: 100, Desc: "How many people fit in a watch room opened by the Game Champion (no ticket, 8 hours)", Default: func(config) string { return "20" }},
	{Name: "watch_rooms_max", Type: settingInt, Min: 1, Max: 10, Desc: "How many watch rooms can run at once; each room is streamed on its own, so upload adds up", Default: func(config) string { return "3" }},
	{Name: "watch_cdn_daily", Type: settingInt, Min: 0, Max: 100000, Desc: "Video segments a day sent through the edge cache (watch_cdn) before the bot serves them itself again; the free Cloudflare plan stops at 100000 requests a day. 0 turns the edge cache off", Default: func(config) string { return "90000" }},
	{Name: "watch_room_progress", Type: settingBool, Desc: "The watch room moves members' AniList and MAL progress on after they watch an episode", Default: func(config) string { return "true" }},
	{Name: "capsule_threshold", Type: settingFloat, Min: 0, Max: 1, Desc: "How close in meaning to a bad example a time capsule message must be to get blocked (0-1, lower is stricter)", Default: func(config) string { return "0.6" }},
	{Name: "match_threshold", Type: settingFloat, Min: 0, Max: 1, Desc: "How close in meaning a chat question must be to get an answer (0-1, higher is stricter)", Default: func(c config) string { return formatFloat(c.AIThreshold) }},
}

func findSetting(name string) (setting, bool) {
	for _, s := range settings {
		if s.Name == name {
			return s, true
		}
	}
	return setting{}, false
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// parse checks a typed value and returns it in its stored form.
func (s setting) parse(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	switch s.Type {
	case settingBool:
		switch strings.ToLower(raw) {
		case "true", "on", "yes", "1", "enabled":
			return "true", nil
		case "false", "off", "no", "0", "disabled":
			return "false", nil
		}
		return "", fmt.Errorf("%s is on or off", s.Name)
	case settingInt:
		n, err := strconv.Atoi(raw)
		if err != nil || float64(n) < s.Min || float64(n) > s.Max {
			return "", fmt.Errorf("%s is a whole number from %g to %g", s.Name, s.Min, s.Max)
		}
		return strconv.Itoa(n), nil
	case settingFloat:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || f < s.Min || f > s.Max {
			return "", fmt.Errorf("%s is a number from %g to %g", s.Name, s.Min, s.Max)
		}
		return formatFloat(f), nil
	}
	return "", fmt.Errorf("unknown setting type")
}

// show is a stored value as staff read it.
func (s setting) show(v string) string {
	if s.Type == settingBool {
		if v == "true" {
			return "on"
		}
		return "off"
	}
	return v
}

// settingValue is a setting's current value and whether staff changed it.
func (b *bot) settingValue(name string) (string, bool) {
	s, ok := findSetting(name)
	if !ok {
		log.Printf("unknown setting %q", name)
		return "", false
	}
	var v string
	var set bool
	if b.store != nil {
		b.store.view(func(d *storeData) { v, set = d.Settings[name] })
	}
	if set {
		if _, err := s.parse(v); err == nil {
			return v, true
		}
	}
	return s.Default(b.cfg), false
}

func (b *bot) settingBool(name string) bool {
	v, _ := b.settingValue(name)
	return v == "true"
}

func (b *bot) settingInt(name string) int {
	v, _ := b.settingValue(name)
	n, _ := strconv.Atoi(v)
	return n
}

func (b *bot) settingFloat(name string) float64 {
	v, _ := b.settingValue(name)
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

func settingsCommand() *discordgo.ApplicationCommand {
	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, s := range settings {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: s.Name, Value: s.Name})
	}
	return &discordgo.ApplicationCommand{Name: "settings", Description: "View or change bot settings without restarting it", DefaultMemberPermissions: &permMod,
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Setting (leave empty to see them all)", Choices: choices},
			{Type: discordgo.ApplicationCommandOptionString, Name: "value", Description: "New value: on/off, a number…", MaxLength: 50},
			{Type: discordgo.ApplicationCommandOptionBoolean, Name: "reset", Description: "Go back to the add-on option"},
		}}
}

// settingLine is one setting in the /settings list.
func settingLine(s setting, v string, changed bool, def string) string {
	line := fmt.Sprintf("**%s** `%s`", s.Name, s.show(v))
	if changed {
		line += fmt.Sprintf(" · changed (add-on option: `%s`)", s.show(def))
	}
	return line + "\n-# " + s.Desc
}

func (b *bot) settingsEmbed() *discordgo.MessageEmbed {
	var lines []string
	for _, s := range settings {
		v, changed := b.settingValue(s.Name)
		lines = append(lines, settingLine(s, v, changed, s.Default(b.cfg)))
	}
	return &discordgo.MessageEmbed{Title: "Bot settings", Color: shu, Description: strings.Join(lines, "\n\n"),
		Footer: &discordgo.MessageEmbedFooter{Text: "/settings name value to change one · reset to go back to the add-on option"}}
}

func (b *bot) settingsCmd(i *discordgo.InteractionCreate) {
	if !b.staffOnly(i) {
		return
	}
	var name, value string
	var reset, hasValue bool
	for _, o := range i.ApplicationCommandData().Options {
		switch o.Name {
		case "name":
			name = o.StringValue()
		case "value":
			value, hasValue = o.StringValue(), true
		case "reset":
			reset = o.BoolValue()
		}
	}
	if name == "" {
		if hasValue || reset {
			b.respond(i, true, errorEmbed("Pick a setting with `name` too."))
			return
		}
		b.respond(i, true, b.settingsEmbed())
		return
	}
	s, ok := findSetting(name)
	if !ok {
		b.respond(i, true, errorEmbed("There's no setting called %s.", name))
		return
	}
	old, _ := b.settingValue(name)
	who := i.Member.User.ID
	switch {
	case reset && hasValue:
		b.respond(i, true, errorEmbed("Give a value or reset, not both."))
	case reset:
		b.store.update(func(d *storeData) { delete(d.Settings, name) })
		now := s.Default(b.cfg)
		b.staffNote(&discordgo.MessageEmbed{Title: "Setting reset", Color: shu,
			Description: fmt.Sprintf("<@%s> reset **%s** to the add-on option `%s` (was `%s`).", who, name, s.show(now), s.show(old))})
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("**%s** is back to the add-on option: `%s`.", name, s.show(now))})
	case hasValue:
		v, err := s.parse(value)
		if err != nil {
			b.respond(i, true, errorEmbed("That doesn't fit: %v.", err))
			return
		}
		b.store.update(func(d *storeData) {
			if d.Settings == nil {
				d.Settings = map[string]string{}
			}
			d.Settings[name] = v
		})
		b.staffNote(&discordgo.MessageEmbed{Title: "Setting changed", Color: shu,
			Description: fmt.Sprintf("<@%s> set **%s** to `%s` (was `%s`).", who, name, s.show(v), s.show(old))})
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("**%s** is now `%s`. It works from now on, no restart needed.", name, s.show(v))})
	default:
		v, changed := b.settingValue(name)
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: settingLine(s, v, changed, s.Default(b.cfg))})
	}
}

// staffOnly reports whether the member is staff (a staff role, Moderate
// Members or Manage Server), telling them when not.
func (b *bot) staffOnly(i *discordgo.InteractionCreate) bool {
	if i.Member.Permissions&(discordgo.PermissionManageGuild|discordgo.PermissionModerateMembers|discordgo.PermissionAdministrator) != 0 {
		return true
	}
	for _, r := range i.Member.Roles {
		if contains(b.cfg.StaffRoles, r) {
			return true
		}
	}
	b.respond(i, true, errorEmbed("Only staff can use this."))
	return false
}
