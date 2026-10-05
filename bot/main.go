// Command otakase-bot is the Discord bot for the Otakase community server.
//
// It answers slash commands (/install, /faq, /latest, /anime, /link-github,
// /help), greets new support posts with a checklist and a "Mark solved"
// button, answers common questions it sees in chat, removes cross-channel
// spam and welcomes new members by DM.
//
// Configuration is read from the environment; see deploy/otakase-bot.env.
// On hosts without a way to set environment variables, the same lines can go
// in a .env or otakase-bot.env file next to the binary.
package main

import (
	"bufio"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

// Shu palette accent, used for every embed.
const shu = 0xD2492F

type config struct {
	Token           string
	GuildID         string
	SupportForum    string
	ModChannel      string
	ContributorRole string
	GitHubClientID  string
	GitHubToken     string
	WelcomeDM       bool
}

// loadEnvFile sets variables from a KEY=value file, keeping any that are
// already set. A missing file is fine.
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
}

func loadConfig() config {
	loadEnvFile(".env")
	loadEnvFile("otakase-bot.env")
	c := config{
		Token:           strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")),
		GuildID:         os.Getenv("OTAKASE_GUILD_ID"),
		SupportForum:    os.Getenv("OTAKASE_SUPPORT_FORUM"),
		ModChannel:      os.Getenv("OTAKASE_MOD_CHANNEL"),
		ContributorRole: os.Getenv("OTAKASE_CONTRIBUTOR_ROLE"),
		GitHubClientID:  os.Getenv("OTAKASE_GITHUB_CLIENT_ID"),
		GitHubToken:     os.Getenv("GITHUB_TOKEN"),
		WelcomeDM:       os.Getenv("OTAKASE_WELCOME_DM") != "false",
	}
	if c.Token == "" || c.GuildID == "" {
		log.Fatal("DISCORD_BOT_TOKEN and OTAKASE_GUILD_ID must be set")
	}
	return c
}

type bot struct {
	cfg  config
	s    *discordgo.Session
	auto *autoReplier
	spam *spamGuard
}

func main() {
	cfg := loadConfig()
	s, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		log.Fatal(err)
	}
	s.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsMessageContent

	b := &bot{cfg: cfg, s: s, auto: newAutoReplier(), spam: newSpamGuard()}
	s.AddHandler(b.onReady)
	s.AddHandler(b.onInteraction)
	s.AddHandler(b.onThreadCreate)
	s.AddHandler(b.onMessage)
	s.AddHandler(b.onMemberAdd)

	if err := s.Open(); err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	b.resolveIDs()
	if err := b.registerCommands(); err != nil {
		log.Fatalf("registering commands: %v", err)
	}
	log.Print("running")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
}

func (b *bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	log.Printf("logged in as %s", r.User.Username)
	_ = s.UpdateStatusComplex(discordgo.UpdateStatusData{
		Activities: []*discordgo.Activity{{Name: "anime · /help", Type: discordgo.ActivityTypeWatching}},
	})
}

// resolveIDs fills in channel and role ids left empty in the configuration
// by finding them by name, so a fresh setup needs only the token and server.
func (b *bot) resolveIDs() {
	if b.cfg.SupportForum == "" || b.cfg.ModChannel == "" {
		chans, err := b.s.GuildChannels(b.cfg.GuildID)
		if err != nil {
			log.Printf("listing channels: %v", err)
		}
		for _, c := range chans {
			name := c.Name[strings.LastIndex(c.Name, "・")+1:]
			if b.cfg.SupportForum == "" && c.Type == discordgo.ChannelTypeGuildForum && name == "support" {
				b.cfg.SupportForum = c.ID
			}
			if b.cfg.ModChannel == "" && name == "mod-chat" {
				b.cfg.ModChannel = c.ID
			}
		}
	}
	if b.cfg.ContributorRole == "" {
		roles, err := b.s.GuildRoles(b.cfg.GuildID)
		if err != nil {
			log.Printf("listing roles: %v", err)
		}
		for _, r := range roles {
			if r.Name == "Contributor" {
				b.cfg.ContributorRole = r.ID
			}
		}
	}
	log.Printf("support forum %q, mod channel %q, contributor role %q", b.cfg.SupportForum, b.cfg.ModChannel, b.cfg.ContributorRole)
}
