package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// /post-info replaces the bot's posts in the welcome, rules and FAQ
// channels with the current ones, so new texts and renamed or new channels
// show up without editing messages by hand.

var (
	//go:embed assets/welcome-header.png
	welcomeHeader []byte
	//go:embed assets/rules-header.png
	rulesHeader []byte
	//go:embed assets/faq-header.png
	faqHeader []byte
)

var permManageServer = int64(discordgo.PermissionManageGuild)

// infoPosts are the channels /post-info fills, in posting order.
var infoPosts = []string{"welcome", "rules", "faq"}

func infoPostCommand() *discordgo.ApplicationCommand {
	choices := []*discordgo.ApplicationCommandOptionChoice{{Name: "All of them", Value: "all"}}
	for _, p := range infoPosts {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: strings.ToUpper(p[:1]) + p[1:], Value: p})
	}
	return &discordgo.ApplicationCommand{Name: "post-info", Description: "Post the welcome, rules and FAQ messages again with the current text",
		DefaultMemberPermissions: &permManageServer,
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "post", Description: "Which post (default: all of them)", Choices: choices},
		}}
}

// channelIDs maps lower-case channel names without their kanji prefix to
// ids, first one wins. The "Create room" voice channel is "create room".
func channelIDs(chans []*discordgo.Channel) map[string]string {
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
	return ids
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

const welcomeIntro = "## Welcome to Otakase\nWatch anime on your phone, TV and computer, with your list kept in sync. " +
	"Otakase finds the episode, plays it, skips openings, endings and filler, and updates AniList or MyAnimeList when you're done. " +
	"One app for Android phones, Android TV, Windows, macOS and Linux."

// rulesList is the rules post. A rule's text may name a channel as
// {support}; it becomes a link when that channel exists.
var rulesList = []struct{ title, text string }{
	{"Be respectful", "No harassment, hate speech, slurs or personal attacks."},
	{"Keep it in English", "Shared channels are in English so everyone can follow."},
	{"Use the right channel", "Help requests go in {support}, one post per problem, with your Otakase version, your device and what happened."},
	{"No streaming or download links", "Don't post or ask for links to streaming, download or torrent sites."},
	{"Keep it safe for work", "No NSFW content anywhere on the server."},
	{"No spam or advertising", "No self-promotion, unsolicited DMs or invite links."},
	{"Mark spoilers", "Use ||spoiler tags|| for anything past the latest episode."},
	{"Report bugs in support", "Post a bug in {support} with clear steps; **Report a bug** in the post (or `/bug`) passes it on and you hear back when it's fixed."},
}

func cv2Text(s string) map[string]any { return map[string]any{"type": 10, "content": s} }

var cv2Sep = map[string]any{"type": 14, "divider": true, "spacing": 1}

func cv2Button(label, url string) map[string]any {
	return map[string]any{"type": 2, "style": 5, "label": label, "url": url}
}

// infoPayload is a Components V2 message: a header picture, a Shu card and
// an optional row of link buttons.
func infoPayload(image string, card []map[string]any, buttons ...map[string]any) map[string]any {
	comps := []map[string]any{
		{"type": 12, "items": []map[string]any{{"media": map[string]any{"url": "attachment://" + image}}}},
		{"type": 17, "accent_color": shu, "components": card},
	}
	if len(buttons) > 0 {
		comps = append(comps, map[string]any{"type": 1, "components": buttons})
	}
	return map[string]any{
		"flags":       1 << 15, // components v2
		"attachments": []map[string]any{{"id": 0, "filename": image}},
		"components":  comps,
	}
}

// welcomePayload builds the welcome message from the server's channels.
func welcomePayload(chans []*discordgo.Channel) map[string]any {
	ids := channelIDs(chans)
	var lines []string
	for _, l := range welcomeLinks {
		if id, ok := ids[l.name]; ok {
			lines = append(lines, fmt.Sprintf("<#%s>  %s", id, l.text))
		}
	}
	return infoPayload("welcome-header.png", []map[string]any{
		cv2Text(welcomeIntro),
		cv2Sep,
		cv2Text("### Start here\n" + strings.Join(lines, "\n")),
		cv2Sep,
		cv2Text("-# Pick your system and notifications in **Channels & Roles** at the top of the channel list. Type `/help` to see what the bot can do."),
	}, cv2Button("Website", siteURL), cv2Button("Download", downloadPage), cv2Button("Help", wikiURL))
}

// rulesPayload builds the rules message.
func rulesPayload(chans []*discordgo.Channel) map[string]any {
	ids := channelIDs(chans)
	support := "the support forum"
	if id, ok := ids["support"]; ok {
		support = "<#" + id + ">"
	}
	var card []map[string]any
	for n, r := range rulesList {
		card = append(card, cv2Text(fmt.Sprintf("**%d. %s**\n%s", n+1, r.title, strings.ReplaceAll(r.text, "{support}", support))))
	}
	card = append(card, cv2Sep,
		cv2Text("-# Moderators have the final say. Breaking the rules leads to a warning, then a timeout, then a ban."))
	return infoPayload("rules-header.png", card)
}

// faqPayload builds the FAQ message from the given answers, so answers
// staff changed on the dashboard show up too.
func faqPayload(list []faqEntry) map[string]any {
	var card []map[string]any
	for n, f := range list {
		if n > 0 {
			card = append(card, cv2Sep)
		}
		card = append(card, cv2Text("### "+f.Question+"\n"+f.Answer))
	}
	card = append(card, cv2Sep, cv2Text("-# Ask the bot any of these with `/faq`."))
	return infoPayload("faq-header.png", card,
		cv2Button("Troubleshooting", wikiURL+"/Troubleshooting"), cv2Button("Help", wikiURL))
}

// infoPost builds one post: its payload and header picture.
func (b *bot) infoPost(name string, chans []*discordgo.Channel) (map[string]any, []byte) {
	switch name {
	case "welcome":
		return welcomePayload(chans), welcomeHeader
	case "rules":
		return rulesPayload(chans), rulesHeader
	default:
		var list []faqEntry
		for _, f := range faqs {
			list = append(list, b.faqAnswer(f))
		}
		return faqPayload(list), faqHeader
	}
}

// postInfo posts the chosen info posts again, one line of outcome each.
func (b *bot) postInfo(i *discordgo.InteractionCreate) {
	if !b.staffOnly(i) {
		return
	}
	which := "all"
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "post" {
			which = o.StringValue()
		}
	}
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		chans, err := b.s.GuildChannels(b.cfg.GuildID)
		if err != nil {
			return errorEmbed("Couldn't list the channels."), nil
		}
		ids := map[string]string{}
		for _, c := range chans {
			n := strings.ToLower(channelBase(c.Name))
			if _, seen := ids[n]; !seen && c.Type == discordgo.ChannelTypeGuildText {
				ids[n] = c.ID
			}
		}
		var out []string
		for _, name := range infoPosts {
			if which != "all" && which != name {
				continue
			}
			ch, ok := ids[name]
			if !ok {
				out = append(out, fmt.Sprintf("**%s**: no text channel called %s.", name, name))
				continue
			}
			payload, header := b.infoPost(name, chans)
			if err := b.replaceBotPosts(ch, payload, header); err != nil {
				log.Printf("post %s: %v", name, err)
				out = append(out, fmt.Sprintf("**%s**: couldn't post it (%v).", name, err))
				continue
			}
			out = append(out, fmt.Sprintf("**%s**: posted fresh in <#%s>.", name, ch))
		}
		return &discordgo.MessageEmbed{Color: shu, Description: strings.Join(out, "\n")}, nil
	})
}

// replaceBotPosts deletes the bot's recent posts in a channel and posts
// the payload with its header picture, so the new one is the only bot post
// there. Posts by other accounts are left alone.
func (b *bot) replaceBotPosts(channel string, payload map[string]any, header []byte) error {
	image := payload["attachments"].([]map[string]any)[0]["filename"].(string)
	ctype, body, err := discordgo.MultipartBodyWithJSON(payload, []*discordgo.File{
		{Name: image, ContentType: "image/png", Reader: bytes.NewReader(header)}})
	if err != nil {
		return err
	}
	if old, err := b.s.ChannelMessages(channel, 50, "", "", ""); err == nil {
		for _, m := range old {
			if m.Author != nil && m.Author.ID == b.s.State.User.ID {
				_ = b.s.ChannelMessageDelete(channel, m.ID)
			}
		}
	}
	url := discordgo.EndpointChannelMessages(channel)
	_, err = b.s.RequestWithLockedBucket("POST", url, ctype, body, b.s.Ratelimiter.LockBucket(url), 0)
	return err
}
