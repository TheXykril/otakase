// Command otakase-bot is the Discord bot for the Otakase community server.
//
// It answers slash commands (/install, /faq, /latest, /anime, /link-github,
// /link-anilist, /profile, /help), greets new support posts with a checklist and a "Mark solved"
// button, answers common questions it sees in chat, removes cross-channel
// spam and scams, helps moderators, makes voice rooms and welcomes new
// members by DM. Members post suggestions with /suggest and vote on them.
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
	"io"
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
	Token              string
	GuildID            string
	SupportForum       string
	ModChannel         string
	LogChannel         string // background job failures and crashes, else ModChannel
	ContributorRole    string
	GitHubClientID     string
	GitHubToken        string
	WelcomeDM          bool
	AiringChannel      string // daily airing post, empty = the "anime" channel
	LoungeChannel      string // default watch party voice channel
	DailyAiring        bool
	AiringHour         int    // UTC hour the airing post goes out
	OtakaseCLI         string // otakase binary used by /provider-status
	OllamaURL          string // Ollama for matching questions by meaning, empty = off
	EmbedModel         string
	AIThreshold        float64
	CreateRoom         string   // join-to-create voice channel, found by name when empty
	SuggestionsChannel string   // where /suggest posts, found by name when empty
	StaffRoles         []string // Moderator and Maintainer, pinged on raids
	AniListClientID    string   // AniList app for proven /link-anilist, empty = by username
	AniListSecret      string   // that app's client secret, needed with the id
	AniListRole        string   // given to linked members, the "AniList linked" role when empty
	WarningDays        int      // warnings older than this stop counting, 0 = never
	SeasonPreview      bool     // post the season preview when a season starts
	HelperPoints       int      // helper points for the Helper role
	DiscordSecret      string   // the Discord app's client secret, for the daily puzzle Activity
	MALClientID        string   // MyAnimeList API app for /link-mal, empty = off
	MALSecret          string   // that app's client secret
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
	"token":                 "DISCORD_BOT_TOKEN",
	"guild_id":              "OTAKASE_GUILD_ID",
	"github_client_id":      "OTAKASE_GITHUB_CLIENT_ID",
	"github_token":          "GITHUB_TOKEN",
	"welcome_dm":            "OTAKASE_WELCOME_DM",
	"daily_airing":          "OTAKASE_DAILY_AIRING",
	"airing_hour_utc":       "OTAKASE_AIRING_HOUR",
	"ollama_url":            "OTAKASE_OLLAMA_URL",
	"anilist_client_id":     "OTAKASE_ANILIST_CLIENT_ID",
	"anilist_secret":        "OTAKASE_ANILIST_SECRET",
	"public_port":           "OTAKASE_PUBLIC_PORT",
	"warning_days":          "OTAKASE_WARNING_DAYS",
	"season_preview":        "OTAKASE_SEASON_PREVIEW",
	"helper_points":         "OTAKASE_HELPER_POINTS",
	"watch_torrent_max_gib": "OTAKASE_WATCH_TORRENT_MAX_GIB",
	"watch_torrent_storage_gib": "OTAKASE_WATCH_TORRENT_STORAGE_GIB",
	"discord_client_secret": "OTAKASE_DISCORD_CLIENT_SECRET",
	"mal_client_id":         "OTAKASE_MAL_CLIENT_ID",
	"mal_client_secret":     "OTAKASE_MAL_CLIENT_SECRET",
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
		Token:              strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")),
		GuildID:            os.Getenv("OTAKASE_GUILD_ID"),
		SupportForum:       os.Getenv("OTAKASE_SUPPORT_FORUM"),
		ModChannel:         os.Getenv("OTAKASE_MOD_CHANNEL"),
		LogChannel:         os.Getenv("OTAKASE_LOG_CHANNEL"),
		ContributorRole:    os.Getenv("OTAKASE_CONTRIBUTOR_ROLE"),
		GitHubClientID:     strings.TrimSpace(os.Getenv("OTAKASE_GITHUB_CLIENT_ID")),
		GitHubToken:        os.Getenv("GITHUB_TOKEN"),
		WelcomeDM:          os.Getenv("OTAKASE_WELCOME_DM") != "false",
		AiringChannel:      os.Getenv("OTAKASE_AIRING_CHANNEL"),
		LoungeChannel:      os.Getenv("OTAKASE_LOUNGE_CHANNEL"),
		DailyAiring:        os.Getenv("OTAKASE_DAILY_AIRING") != "false",
		AiringHour:         6,
		OtakaseCLI:         os.Getenv("OTAKASE_CLI"),
		CreateRoom:         os.Getenv("OTAKASE_CREATE_ROOM"),
		SuggestionsChannel: os.Getenv("OTAKASE_SUGGESTIONS_CHANNEL"),
		AniListClientID:    strings.TrimSpace(os.Getenv("OTAKASE_ANILIST_CLIENT_ID")),
		AniListSecret:      strings.TrimSpace(os.Getenv("OTAKASE_ANILIST_SECRET")),
		AniListRole:        os.Getenv("OTAKASE_ANILIST_ROLE"),
		WarningDays:        warningDays(os.Getenv("OTAKASE_WARNING_DAYS")),
		SeasonPreview:      os.Getenv("OTAKASE_SEASON_PREVIEW") != "false",
		DiscordSecret:      strings.TrimSpace(os.Getenv("OTAKASE_DISCORD_CLIENT_SECRET")),
		MALClientID:        strings.TrimSpace(os.Getenv("OTAKASE_MAL_CLIENT_ID")),
		MALSecret:          strings.TrimSpace(os.Getenv("OTAKASE_MAL_CLIENT_SECRET")),
	}
	if h, err := strconv.Atoi(os.Getenv("OTAKASE_AIRING_HOUR")); err == nil && h >= 0 && h < 24 {
		c.AiringHour = h
	}
	c.HelperPoints = 5
	if n, err := strconv.Atoi(os.Getenv("OTAKASE_HELPER_POINTS")); err == nil && n > 0 {
		c.HelperPoints = n
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
	logs *logReader
	spam *spamGuard
	pres *presence

	issues    issueCooldown
	providers providerCache
	sem       *semantic
	wiki      wikiIndex
	store     *store
	raid      raidGuard
	empty     emptyRooms
	health    *health
	spoilers  spoilerGuard
	guess     guessGame
	chat      chatRounds
	hl        hlGames
	acts      activitySessions
	solo      soloRounds
	pz        pzBoards
	together  togetherRooms
	watch     watchRooms
	xp        xpTracker
	msgs      *msgCache
	verify    verifyRuntime
	marks     boardMarkCache
	boardPNG  boardPNGCache
}

func main() {
	log.SetOutput(io.MultiWriter(os.Stderr, botLog)) // also kept for the dashboard's Log page
	cfg := loadConfig()
	crash := newCrashWatch(dataDir())
	crash.start()
	s, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		log.Fatal(err)
	}
	s.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsGuildMessageReactions | // watch party reactions (partyfx.go)
		discordgo.IntentsMessageContent

	b := &bot{cfg: cfg, s: s, auto: newAutoReplier(), logs: newLogReader(), spam: newSpamGuard(), pres: &presence{},
		sem: &semantic{url: cfg.OllamaURL, model: cfg.EmbedModel, threshold: cfg.AIThreshold}, store: openStore(storePath()), health: newHealth()}
	b.msgs = newMsgCache()
	b.pres.custom = func() (list []string) {
		b.store.view(func(d *storeData) { list = append(list, d.Statuses...) })
		return list
	}
	b.sem.onErr, b.sem.onOK = func(err error) { b.jobFailed("Semantic matching", err) }, func() { b.jobOK("Semantic matching") }
	b.sem.minScore = func() float64 { return b.settingFloat("match_threshold") }
	s.AddHandler(b.onReady)
	s.AddHandler(b.onInteraction)
	s.AddHandler(b.onThreadCreate)
	s.AddHandler(b.onSupportPostSimilar)
	s.AddHandler(b.onMessage)
	s.AddHandler(b.onMessageDelete)
	s.AddHandler(b.cacheMessage)
	s.AddHandler(b.logMessageDelete)
	s.AddHandler(b.logMessageDeleteBulk)
	s.AddHandler(b.logMessageUpdate)
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
	if e := crash.embed(); e != nil {
		b.staffNote(e)
	}
	b.noteUpdate()
	for _, fn := range startHooks {
		fn(b)
	}
	for _, j := range backgroundJobs {
		go b.keepRunning(j.name, func() { j.run(b) })
	}
	go b.keepRunning("Status", func() { b.pres.run(s, b.cfg.GitHubToken) })
	go b.keepRunning("Daily airing post", b.airingLoop)
	go b.keepRunning("Follow DMs", b.followLoop)
	go b.keepRunning("Season preview", b.seasonLoop)
	go b.keepRunning("Semantic matching", b.sem.start)
	go b.keepRunning("Wiki answers", b.wikiLoop)
	go b.keepRunning("Stale support check", b.staleLoop)
	go b.keepRunning("Voice room cleanup", b.roomLoop)
	go b.keepRunning("Suggestion issues", b.suggestionIssueLoop)
	go b.keepRunning("Dashboard", b.serveDashboard)
	go b.keepRunning("Dashboard card", b.haCardLoop)
	go b.keepRunning("Public API", b.servePublicAPI)
	go b.keepRunning("App activity", b.appActivityLoop)
	go b.keepRunning("Known issues post", b.knownIssuesLoop)
	go b.keepRunning("Similar posts index", b.similarLoop)
	go b.keepRunning("Bug report follow-up", b.bugFollowLoop)
	go b.keepRunning("Watch party reminders", b.partyLoop)
	go b.keepRunning("Spoiler guard titles", b.spoilerTitleLoop)
	go b.keepRunning("Level saves", b.xpSaveLoop)
	go b.keepRunning("Game Champion", b.championLoop)
	go b.keepRunning("Watch bingo", b.bingoLoop)
	go b.keepRunning("Join verification", b.verifyLoop)
	log.Print("running")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	b.saveXP()
	crash.stop()
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
	if b.cfg.SupportForum == "" || b.cfg.ModChannel == "" || b.cfg.LogChannel == "" || b.cfg.AiringChannel == "" || b.cfg.LoungeChannel == "" || b.cfg.CreateRoom == "" || b.cfg.SuggestionsChannel == "" {
		chans, err := b.s.GuildChannels(b.cfg.GuildID)
		if err != nil {
			log.Printf("listing channels: %v", err)
		}
		var ideas string
		for _, c := range chans {
			name := channelBase(c.Name)
			if b.cfg.SuggestionsChannel == "" && c.Type == discordgo.ChannelTypeGuildText {
				if name == "suggestions" {
					b.cfg.SuggestionsChannel = c.ID
				} else if name == "ideas" {
					ideas = c.ID
				}
			}
			if b.cfg.SupportForum == "" && c.Type == discordgo.ChannelTypeGuildForum && name == "support" {
				b.cfg.SupportForum = c.ID
			}
			if b.cfg.LogChannel == "" && c.Type == discordgo.ChannelTypeGuildText && name == "bot-log" {
				b.cfg.LogChannel = c.ID
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
		if b.cfg.SuggestionsChannel == "" {
			b.cfg.SuggestionsChannel = ideas
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
			if strings.EqualFold(r.Name, "AniList linked") && b.cfg.AniListRole == "" {
				b.cfg.AniListRole = r.ID
			}
			if r.Name == "Moderator" || r.Name == "Maintainer" {
				b.cfg.StaffRoles = append(b.cfg.StaffRoles, r.ID)
			}
		}
	}
	log.Printf("support forum %q, mod channel %q, log channel %q, contributor role %q, airing channel %q, lounge %q, create room %q, suggestions %q, anilist role %q",
		b.cfg.SupportForum, b.cfg.ModChannel, b.cfg.LogChannel, b.cfg.ContributorRole, b.cfg.AiringChannel, b.cfg.LoungeChannel, b.cfg.CreateRoom, b.cfg.SuggestionsChannel, b.cfg.AniListRole)
}
