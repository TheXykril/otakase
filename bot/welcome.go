package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// /post-welcome replaces the bot's post in the welcome channel with the
// current one, so links to renamed or new channels stay right.

//go:embed assets/welcome-header.png
var welcomeHeader []byte

var permManageServer = int64(discordgo.PermissionManageGuild)

func welcomeCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{Name: "post-welcome", Description: "Post the welcome message again with current channels",
		DefaultMemberPermissions: &permManageServer}
}

// welcomeLinks are the "Start here" lines, by channel name without its
// prefix. Channels that don't exist are left out.
var welcomeLinks = []struct{ name, text string }{
	{"rules", "Read before posting"},
	{"announcements", "News"},
	{"stable-builds", "New releases"},
	{"faq", "Common questions"},
	{"support", "Get help, one post per problem"},
	{"suggestions", "Suggest features with `/suggest`"},
	{"general", "Chat"},
	{"create room", "Join to get your own voice room"},
}

// welcomePayload builds the Components V2 message from the server's channels.
func welcomePayload(chans []*discordgo.Channel) map[string]any {
	ids := map[string]string{}
	for _, c := range chans {
		n := strings.ToLower(channelBase(c.Name))
		if c.Type == discordgo.ChannelTypeGuildVoice && isCreateChannel(c.Name) {
			n = "create room"
		}
		if _, seen := ids[n]; !seen && c.Type != discordgo.ChannelTypeGuildCategory {
			ids[n] = c.ID
		}
	}
	var lines []string
	for _, l := range welcomeLinks {
		if id, ok := ids[l.name]; ok {
			lines = append(lines, fmt.Sprintf("<#%s>  %s", id, l.text))
		}
	}
	text := func(s string) map[string]any { return map[string]any{"type": 10, "content": s} }
	sep := map[string]any{"type": 14, "divider": true, "spacing": 1}
	button := func(label, url string) map[string]any {
		return map[string]any{"type": 2, "style": 5, "label": label, "url": url}
	}
	return map[string]any{
		"flags":       1 << 15, // components v2
		"attachments": []map[string]any{{"id": 0, "filename": "welcome-header.png"}},
		"components": []map[string]any{
			{"type": 12, "items": []map[string]any{{"media": map[string]any{"url": "attachment://welcome-header.png"}}}},
			{"type": 17, "accent_color": shu, "components": []map[string]any{
				text("## Welcome to Otakase\nWatch anime from the command line, with your list kept in sync. Otakase finds the episode, " +
					"plays it in mpv or casts it to your TV, skips openings, endings, filler and recaps, and updates AniList or MyAnimeList when you're done."),
				sep,
				text("### Start here\n" + strings.Join(lines, "\n")),
				sep,
				text("-# Pick your system and notifications in **Channels & Roles** at the top of the channel list. Type `/help` to see what the bot can do."),
			}},
			{"type": 1, "components": []map[string]any{
				button("Website", siteURL), button("Download", repoURL+"/releases/latest"), button("Wiki", wikiURL), button("GitHub", repoURL),
			}},
		},
	}
}

func (b *bot) postWelcome(i *discordgo.InteractionCreate) {
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		chans, err := b.s.GuildChannels(b.cfg.GuildID)
		if err != nil {
			return errorEmbed("Couldn't list the channels."), nil
		}
		welcome := ""
		for _, c := range chans {
			if c.Type == discordgo.ChannelTypeGuildText && strings.ToLower(channelBase(c.Name)) == "welcome" {
				welcome = c.ID
			}
		}
		if welcome == "" {
			return errorEmbed("No channel called welcome."), nil
		}
		ctype, body, err := discordgo.MultipartBodyWithJSON(welcomePayload(chans), []*discordgo.File{
			{Name: "welcome-header.png", ContentType: "image/png", Reader: bytes.NewReader(welcomeHeader)}})
		if err != nil {
			return errorEmbed("Couldn't build the message: %v", err), nil
		}
		// Old posts go first, so the new one is the only bot post there.
		if old, err := b.s.ChannelMessages(welcome, 50, "", "", ""); err == nil {
			for _, m := range old {
				if m.Author != nil && m.Author.ID == b.s.State.User.ID {
					_ = b.s.ChannelMessageDelete(welcome, m.ID)
				}
			}
		}
		url := discordgo.EndpointChannelMessages(welcome)
		if _, err := b.s.RequestWithLockedBucket("POST", url, ctype, body, b.s.Ratelimiter.LockBucket(url), 0); err != nil {
			log.Printf("post welcome: %v", err)
			return errorEmbed("Couldn't post it: %v", err), nil
		}
		return &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Posted a fresh welcome message in <#%s>.", welcome)}, nil
	})
}
