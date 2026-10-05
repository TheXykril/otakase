// Command otakase-bot is the Discord bot for the Otakase community server.
//
// It answers slash commands (/install, /faq, /latest, /anime, /link-github,
// /help), greets new support posts with a checklist and a "Mark solved"
// button, answers common questions it sees in chat, removes cross-channel
// spam and scams, helps moderators, makes voice rooms and welcomes new
// members by DM.
//
// Configuration is read from the environment; see deploy/otakase-bot.env.
// On hosts without a way to set environment variables, the same lines can go
// in a .env or otakase-bot.env file next to the binary. As a Home Assistant
// add-on it reads the add-on options from /data/options.json.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
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
	AiringChannel   string // daily airing post, empty = the "anime" channel
	LoungeChannel   string // default watch party voice channel
	DailyAiring     bool
	AiringHour      int    // UTC hour the airing post goes out
	OtakaseCLI      string // otakase binary used by /provider-status
	OllamaURL       string // Ollama for matching questions by meaning, empty = off
	EmbedModel      string
	AIThreshold     float64
	CreateRoom      string   // join-to-create voice channel, found by name when empty
	StaffRoles      []string // Moderator and Maintainer, pinged on raids
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

// haOptions maps Home Assistant add-on options to environment variables.
var haOptions = map[string]string{
	"token":            "DISCORD_BOT_TOKEN",
	"guild_id":         "OTAKASE_GUILD_ID",
	"github_client_id": "OTAKASE_GITHUB_CLIENT_ID",
	"github_token":     "GITHUB_TOKEN",
	"welcome_dm":       "OTAKASE_WELCOME_DM",
	"daily_airing":     "OTAKASE_DAILY_AIRING",
	"airing_hour_utc":  "OTAKASE_AIRING_HOUR",
	"ollama_url":       "OTAKASE_OLLAMA_URL",
}

// loadHAOptions sets variables from a Home Assistant add-on options file,
// keeping any that are already set. A missing file is fine.
func loadHAOptions(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var opts map[string]any
	if err := json.Unmarshal(data, &opts); err != nil {
		log.Printf("%s: %v", path, err)
		return
	}
	for key, env := range haOptions {
		v, ok := opts[key]
		if !ok || v == nil {
			continue
		}
		if _, set := os.LookupEnv(env); !set {
			os.Setenv(env, fmt.Sprint(v))
		}
	}
}

func loadConfig() config {
	loadHAOptions("/data/options.json")
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
		AiringChannel:   os.Getenv("OTAKASE_AIRING_CHANNEL"),
		LoungeChannel:   os.Getenv("OTAKASE_LOUNGE_CHANNEL"),
		DailyAiring:     os.Getenv("OTAKASE_DAILY_AIRING") != "false",
		AiringHour:      6,
		OtakaseCLI:      os.Getenv("OTAKASE_CLI"),
		CreateRoom:      os.Getenv("OTAKASE_CREATE_ROOM"),
	}
	if h, err := strconv.Atoi(os.Getenv("OTAKASE_AIRING_HOUR")); err == nil && h >= 0 && h < 24 {
		c.AiringHour = h
	}
	c.OllamaURL = os.Getenv("OTAKASE_OLLAMA_URL")
	c.EmbedModel = os.Getenv("OTAKASE_EMBED_MODEL")
	if c.EmbedModel == "" {
		c.EmbedModel = "all-minilm"
	}
	c.AIThreshold = 0.6
	if t, err := strconv.ParseFloat(os.Getenv("OTAKASE_AI_THRESHOLD"), 64); err == nil && t > 0 && t < 1 {
		c.AIThreshold = t
	}
	if c.OtakaseCLI == "" {
		c.OtakaseCLI = "otakase"
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
	pres *presence

	issues    issueCooldown
	providers providerCache
	sem       *semantic
	store     *store
	raid      raidGuard
	empty     emptyRooms
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
		discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsMessageContent

	b := &bot{cfg: cfg, s: s, auto: newAutoReplier(), spam: newSpamGuard(), pres: &presence{},
		sem: &semantic{url: cfg.OllamaURL, model: cfg.EmbedModel, threshold: cfg.AIThreshold}, store: openStore(storePath())}
	s.AddHandler(b.onReady)
	s.AddHandler(b.onInteraction)
	s.AddHandler(b.onThreadCreate)
	s.AddHandler(b.onMessage)
	s.AddHandler(b.onMemberAdd)
	s.AddHandler(b.onVoiceState)

	if err := s.Open(); err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	b.resolveIDs()
	if err := b.registerCommands(); err != nil {
		log.Fatalf("registering commands: %v", err)
	}
	go b.pres.run(s, b.cfg.GitHubToken)
	go b.airingLoop()
	go b.sem.start()
	go b.staleLoop()
	go b.roomLoop()
	log.Print("running")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
}

func (b *bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	log.Printf("logged in as %s", r.User.Username)
	b.pres.show(s)
}

// channelBase strips a "助・" style prefix from a channel name.
func channelBase(name string) string {
	if i := strings.LastIndex(name, "・"); i >= 0 {
		return name[i+len("・"):]
	}
	return name
}

// resolveIDs fills in channel and role ids left empty in the configuration
// by finding them by name, so a fresh setup needs only the token and server.
func (b *bot) resolveIDs() {
	if b.cfg.SupportForum == "" || b.cfg.ModChannel == "" || b.cfg.AiringChannel == "" || b.cfg.LoungeChannel == "" || b.cfg.CreateRoom == "" {
		chans, err := b.s.GuildChannels(b.cfg.GuildID)
		if err != nil {
			log.Printf("listing channels: %v", err)
		}
		for _, c := range chans {
			name := channelBase(c.Name)
			if b.cfg.SupportForum == "" && c.Type == discordgo.ChannelTypeGuildForum && name == "support" {
				b.cfg.SupportForum = c.ID
			}
			if b.cfg.ModChannel == "" && name == "mod-chat" {
				b.cfg.ModChannel = c.ID
			}
			if b.cfg.AiringChannel == "" && c.Type == discordgo.ChannelTypeGuildText && name == "anime" {
				b.cfg.AiringChannel = c.ID
			}
			if b.cfg.LoungeChannel == "" && c.Type == discordgo.ChannelTypeGuildVoice && name == "lounge" {
				b.cfg.LoungeChannel = c.ID
			}
			if b.cfg.CreateRoom == "" && c.Type == discordgo.ChannelTypeGuildVoice && isCreateChannel(c.Name) {
				b.cfg.CreateRoom = c.ID
			}
		}
	}
	{
		roles, err := b.s.GuildRoles(b.cfg.GuildID)
		if err != nil {
			log.Printf("listing roles: %v", err)
		}
		for _, r := range roles {
			if r.Name == "Contributor" && b.cfg.ContributorRole == "" {
				b.cfg.ContributorRole = r.ID
			}
			if r.Name == "Moderator" || r.Name == "Maintainer" {
				b.cfg.StaffRoles = append(b.cfg.StaffRoles, r.ID)
			}
		}
	}
	log.Printf("support forum %q, mod channel %q, contributor role %q, airing channel %q, lounge %q, create room %q",
		b.cfg.SupportForum, b.cfg.ModChannel, b.cfg.ContributorRole, b.cfg.AiringChannel, b.cfg.LoungeChannel, b.cfg.CreateRoom)
}
