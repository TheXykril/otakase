package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /support opens a form and turns it into a support forum post with the
// details helpers always ask for. The bot makes the post, so it remembers
// who filled the form in (storeData.FormPosts) for Mark solved and the
// stale nudge.

// supportFormCooldown is how long a member waits between /support posts.
const supportFormCooldown = 2 * time.Minute

var supportFormLast = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

func (b *bot) supportForm(i *discordgo.InteractionCreate) {
	if b.cfg.SupportForum == "" {
		b.respond(i, true, errorEmbed("The support forum isn't set up on this server yet."))
		return
	}
	input := func(id, label string, style discordgo.TextInputStyle, required bool, minLen, maxLen int, placeholder string) discordgo.MessageComponent {
		return discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{
			CustomID: id, Label: label, Style: style, Required: required, MinLength: minLen, MaxLength: maxLen, Placeholder: placeholder}}}
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{CustomID: "support-form", Title: "Ask for help", Components: []discordgo.MessageComponent{
			input("system", "System (OS)", discordgo.TextInputShort, true, 2, 80, "e.g. Arch Linux, Ubuntu 24.04, Windows 11, macOS 15"),
			input("version", "Otakase version", discordgo.TextInputShort, true, 1, 40, "What otakase -v says, e.g. 26.5.0"),
			input("install", "How you installed it", discordgo.TextInputShort, true, 2, 100, "e.g. AUR, winget, install script, Homebrew"),
			input("what", "What happened", discordgo.TextInputParagraph, true, 10, 900, "The first line becomes the title of your post"),
			input("steps", "Steps or error (optional)", discordgo.TextInputParagraph, false, 0, 900, "What you did, and any error or log lines"),
		}},
	}); err != nil {
		log.Printf("support form: %v", err)
	}
}

// supportTitle is the post title: the first line of what happened.
func supportTitle(what string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(what), "\n")
	first = strings.TrimSpace(first)
	if len([]rune(first)) < 3 {
		return "Help needed"
	}
	return clip(first, 100)
}

// supportBody is the post's first message.
func supportBody(userID string, v map[string]string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<@%s> needs help:\n\n", userID)
	fmt.Fprintf(&sb, "**System:** %s\n", oneLine(v["system"]))
	fmt.Fprintf(&sb, "**Otakase version:** %s\n", oneLine(v["version"]))
	fmt.Fprintf(&sb, "**Installed with:** %s\n", oneLine(v["install"]))
	fmt.Fprintf(&sb, "\n**What happened**\n%s\n", v["what"])
	if steps := v["steps"]; steps != "" {
		if looksLikeLog(steps) && !strings.Contains(steps, "```") {
			steps = "```\n" + steps + "\n```"
		}
		fmt.Fprintf(&sb, "\n**Steps or error**\n%s\n", steps)
	}
	return clip(sb.String(), 2000)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// osTagNames are the forum tag names that fit what someone wrote as their
// system, lower case.
func osTagNames(system string) []string {
	s := strings.ToLower(system)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(s, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("windows", "win10", "win11", "win 10", "win 11", "wsl"):
		return []string{"windows"}
	case has("mac", "osx", "os x", "darwin", "sonoma", "sequoia", "ventura", "tahoe"):
		return []string{"macos", "mac"}
	case has("linux", "ubuntu", "debian", "arch", "manjaro", "fedora", "mint", "pop!_os", "pop os", "popos", "nixos",
		"opensuse", "suse", "endeavour", "gentoo", "void", "kali", "zorin", "elementary", "cachy", "garuda", "omarchy", "steam deck", "steamos", "bazzite", "artix"):
		return []string{"linux"}
	}
	return nil
}

// osTags picks the forum's tags that match the system.
func osTags(forum *discordgo.Channel, system string) []string {
	names := osTagNames(system)
	var out []string
	for _, t := range forum.AvailableTags {
		for _, n := range names {
			if strings.EqualFold(t.Name, n) {
				out = append(out, t.ID)
			}
		}
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// submitSupportForm makes the post from a filled /support form.
func (b *bot) submitSupportForm(i *discordgo.InteractionCreate) {
	v := modalValues(i)
	user := i.Member.User.ID
	supportFormLast.Lock()
	wait := supportFormCooldown - time.Since(supportFormLast.at[user])
	if wait <= 0 {
		supportFormLast.at[user] = time.Now()
	}
	supportFormLast.Unlock()
	if wait > 0 {
		b.respond(i, true, errorEmbed("You just made a post. You can make another in a minute or two, or add to the one you have."))
		return
	}
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		forum, err := b.s.Channel(b.cfg.SupportForum)
		if err != nil {
			log.Printf("support form: %v", err)
			return errorEmbed("Couldn't make the post, please try again later."), nil
		}
		th, err := b.s.ForumThreadStartComplex(forum.ID,
			&discordgo.ThreadStart{Name: supportTitle(v["what"]), AppliedTags: osTags(forum, v["system"])},
			&discordgo.MessageSend{Content: supportBody(user, v), AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{user}}})
		if err != nil {
			log.Printf("support post: %v", err)
			supportFormLast.Lock()
			delete(supportFormLast.at, user)
			supportFormLast.Unlock()
			return errorEmbed("Couldn't make the post, please try again later."), nil
		}
		b.store.update(func(d *storeData) {
			if d.FormPosts == nil {
				d.FormPosts = map[string]string{}
			}
			d.FormPosts[th.ID] = user
		})
		// The version check and log reader only see members' messages, so
		// they get the form's text here, after the greeting is up.
		go func() {
			time.Sleep(5 * time.Second)
			m := &discordgo.MessageCreate{Message: &discordgo.Message{ID: th.ID, ChannelID: th.ID, GuildID: b.cfg.GuildID,
				Author: i.Member.User, Content: "Otakase version: " + oneLine(v["version"]) + "\n" + v["what"] + "\n" + v["steps"]}}
			b.checkVersion(m)
			b.readSupportLog(m)
		}()
		link := fmt.Sprintf("https://discord.com/channels/%s/%s", b.cfg.GuildID, th.ID)
		return &discordgo.MessageEmbed{Color: shu, Description: "Your post is up: " + link + "\nSomeone will answer there; you'll get a notification."},
			linkButtons("Open your post", link)
	})
}
