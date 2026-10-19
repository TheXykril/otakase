package main

import (
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// At startup the bot compares its version with the one it ran last time and
// tells staff what changed when it was updated.

type changelogSection struct {
	Version string
	Lines   string
}

var changelogHeadingRe = regexp.MustCompile(`(?m)^##\s+v?(\S+)\s*$`)

// changelogSections splits a CHANGELOG.md into its "## version" sections, in
// file order.
func changelogSections(changelog string) []changelogSection {
	changelog = strings.ReplaceAll(changelog, "\r\n", "\n")
	idx := changelogHeadingRe.FindAllStringSubmatchIndex(changelog, -1)
	var out []changelogSection
	for k, m := range idx {
		end := len(changelog)
		if k+1 < len(idx) {
			end = idx[k+1][0]
		}
		out = append(out, changelogSection{
			Version: changelog[m[2]:m[3]],
			Lines:   strings.TrimSpace(changelog[m[1]:end]),
		})
	}
	return out
}

// sectionsBetween returns the sections newer than from up to and including
// to, newest first.
func sectionsBetween(changelog, from, to string) []changelogSection {
	var out []changelogSection
	for _, s := range changelogSections(changelog) {
		if compareVersions(s.Version, from) > 0 && compareVersions(s.Version, to) <= 0 {
			out = append(out, s)
		}
	}
	return out
}

// updateEmbed tells staff the bot now runs version to instead of from.
func updateEmbed(changelog, from, to string) *discordgo.MessageEmbed {
	e := &discordgo.MessageEmbed{
		Title:  "Bot updated to " + to,
		Color:  shu,
		Footer: &discordgo.MessageEmbedFooter{Text: "Was " + from},
	}
	if compareVersions(to, from) < 0 {
		e.Title = "Bot changed back to " + to
		return e
	}
	secs := sectionsBetween(changelog, from, to)
	var parts []string
	for _, s := range secs {
		lines := embedMarkdown(s.Lines)
		if len(secs) > 1 {
			lines = "**" + s.Version + "**\n" + lines
		}
		parts = append(parts, lines)
	}
	if len(parts) == 0 {
		e.Description = "No changelog notes for this version."
	} else {
		e.Description = fitText(strings.Join(parts, "\n\n"), embedTextLimit, "More in the bot's CHANGELOG.md.")
	}
	return e
}

// noteUpdate posts the update card when the version changed since the last
// run, and remembers this version. The first run ever only remembers it.
func (b *bot) noteUpdate() {
	now := botVersion()
	if now == "dev" {
		return
	}
	var before string
	b.store.update(func(d *storeData) {
		before = d.BotVersion
		d.BotVersion = now
	})
	if before == "" || before == now {
		return
	}
	b.staffNote(updateEmbed(botChangelog, before, now))
}
