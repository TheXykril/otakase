package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Warning expiry: warnings older than warning_days stop counting toward the
// timeout every third warning gives. They stay on record and /warnings
// lists them crossed out.

// warningDays reads OTAKASE_WARNING_DAYS: 90 when unset or invalid, 0 means
// warnings never expire.
func warningDays(v string) int {
	if d, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && d >= 0 {
		return d
	}
	return 90
}

// warningExpired reports whether a warning no longer counts.
func warningExpired(w warning, now time.Time, days int) bool {
	return days > 0 && now.Sub(w.At) > time.Duration(days)*24*time.Hour
}

// splitWarnings separates the warnings that still count from expired ones,
// keeping their order.
func splitWarnings(list []warning, now time.Time, days int) (active, expired []warning) {
	for _, w := range list {
		if warningExpired(w, now, days) {
			expired = append(expired, w)
		} else {
			active = append(active, w)
		}
	}
	return active, expired
}

func warningsEmbed(userID string, list []warning, now time.Time, days int) *discordgo.MessageEmbed {
	if len(list) == 0 {
		return &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("<@%s> has no warnings.", userID)}
	}
	active, expired := splitWarnings(list, now, days)
	line := func(n int, w warning) string {
		return fmt.Sprintf("**%d.** <t:%d:d> by <@%s>: %s", n, w.At.Unix(), w.By, clip(w.Reason, 120))
	}
	desc := fmt.Sprintf("<@%s>\n", userID)
	if len(active) == 0 {
		desc += "No active warnings.\n"
	}
	start := max(0, len(active)-15)
	for n, w := range active[start:] {
		desc += line(start+n+1, w) + "\n"
	}
	if len(expired) > 0 {
		desc += "\n**Expired** (no longer count)\n"
		start := max(0, len(expired)-10)
		for _, w := range expired[start:] {
			desc += fmt.Sprintf("~~<t:%d:d> by <@%s>: %s~~ expired\n", w.At.Unix(), w.By, clip(w.Reason, 100))
		}
	}
	e := &discordgo.MessageEmbed{Title: fmt.Sprintf("%d active warning(s)", len(active)), Color: shu, Description: strings.TrimSpace(desc)}
	if days > 0 {
		e.Footer = &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Warnings expire after %d days", days)}
	}
	return e
}
