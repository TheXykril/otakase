package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var zero = 0.0

func (b *bot) registerCommands() error {
	osChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: "Debian / Ubuntu / other Linux", Value: "linux"},
		{Name: "Arch / Manjaro", Value: "arch"},
		{Name: "Windows", Value: "windows"},
		{Name: "macOS", Value: "mac"},
	}
	var faqChoices []*discordgo.ApplicationCommandOptionChoice
	for _, f := range faqs {
		faqChoices = append(faqChoices, &discordgo.ApplicationCommandOptionChoice{Name: f.Question, Value: f.Key})
	}
	cmds := []*discordgo.ApplicationCommand{
		{Name: "help", Description: "What this bot can do"},
		{Name: "install", Description: "How to install Otakase", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "system", Description: "Your system", Required: true, Choices: osChoices},
		}},
		{Name: "faq", Description: "Answer to a common question", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "topic", Description: "Question", Required: true, Choices: faqChoices},
		}},
		{Name: "latest", Description: "The latest Otakase release"},
		{Name: "anime", Description: "Look up an anime on AniList", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "title", Description: "Anime title", Required: true},
		}},
		{Name: "provider-status", Description: "Which anime sources work right now"},
		{Name: "bug", Description: "Turn this support post into a GitHub issue"},
		{Name: "watchparty", Description: "Plan a watch party in a voice channel", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "anime", Description: "What you'll watch (pick from the suggestions)", Required: true, Autocomplete: true},
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "starts_in", Description: "Minutes from now (default 15)", MinValue: &zero, MaxValue: 10080},
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "limit", Description: "Max people in the voice channel (0 = no limit)", MinValue: &zero, MaxValue: 99},
			{Type: discordgo.ApplicationCommandOptionString, Name: "voice", Description: "Who can talk and screen share (default: everyone talks, host shares)", Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "Everyone talks, only the host screen shares", Value: partyTalk},
				{Name: "Everyone talks and can screen share", Value: partyShare},
				{Name: "Only the host talks and screen shares", Value: partyHostOnly},
			}},
			{Type: discordgo.ApplicationCommandOptionBoolean, Name: "sync", Description: "Everyone watches in their own Otakase, synced to the host (default off)"},
		}},
	}
	cmds = append(cmds, modCommands()...)
	cmds = append(cmds, roomCommands()...)
	cmds = append(cmds, welcomeCommand())
	cmds = append(cmds, suggestionCommands()...)
	cmds = append(cmds, followCommands()...)
	cmds = append(cmds, anilistCommands()...)
	cmds = append(cmds, changelogCommand(), suggestionIssueCommand())
	cmds = append(cmds, botStatusCommand(), settingsCommand(), previewCommand())
	cmds = append(cmds, supportQueueCommand())
	cmds = append(cmds, spoilerCommand(), selfRoleCommand())
	cmds = append(cmds, gameCommands()...)
	cmds = append(cmds, levelCommands()...)
	cmds = append(cmds, verificationCommand())
	cmds = append(cmds, socialCommands()...)
	cmds = append(cmds, supportExtraCommands()...)
	cmds = append(cmds, appCommands()...)
	cmds = append(cmds, registeredCommands...)
	if b.cfg.GitHubClientID != "" && b.cfg.ContributorRole != "" {
		cmds = append(cmds, &discordgo.ApplicationCommand{
			Name: "link-github", Description: "Get the Contributor role for a merged pull request"})
	}
	addShareOptions(cmds)
	_, err := b.s.ApplicationCommandBulkOverwrite(b.s.State.User.ID, b.cfg.GuildID, cmds)
	return err
}

func (b *bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Member == nil && i.Type == discordgo.InteractionMessageComponent {
		if h, rest, ok := findPrefixed(dmComponentHandlers, i.MessageComponentData().CustomID); ok {
			h(b, i, rest)
			return
		}
	}
	if i.Member == nil && b.dmInteraction(i) {
		return
	}
	if i.Member == nil {
		return // DMs
	}
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		d := i.ApplicationCommandData()
		opt := func(name string) string {
			for _, o := range d.Options {
				if o.Name == name {
					return o.StringValue()
				}
			}
			return ""
		}
		if b.gameCommand(i, d.Name) {
			return
		}
		if h, ok := commandHandlers[d.Name]; ok {
			h(b, i)
			return
		}
		switch d.Name {
		case "help":
			b.respond(i, onlyAsker(i), helpEmbed())
		case "install":
			g := installGuides[opt("system")]
			b.respond(i, onlyAsker(i), &discordgo.MessageEmbed{Title: "Install on " + g.Title, Description: g.Body, Color: shu,
				Footer: &discordgo.MessageEmbedFooter{Text: "Otakase needs mpv · more on the website"}})
		case "faq":
			f, _ := faqByKey(opt("topic"))
			b.respond(i, onlyAsker(i), &discordgo.MessageEmbed{Title: f.Question, Description: b.faqAnswer(f).Answer, Color: shu})
		case "latest":
			b.deferThen(i, onlyAsker(i), func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
				return latestRelease(b.cfg.GitHubToken)
			})
		case "anime":
			b.deferThen(i, onlyAsker(i), func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) { return searchAnime(opt("title")) })
		case "link-github":
			b.startGitHubLink(i)
		case "provider-status":
			b.deferThen(i, true, b.providerStatusFor(interactionUser(i).ID))
		case "bug":
			b.reportBug(i)
		case "watchparty":
			b.watchparty(i)
		case "Report to mods":
			b.reportMessage(i)
		case "warn":
			b.warn(i)
		case "warnings":
			b.warnings(i)
		case "purge":
			b.purge(i)
		case "slowmode":
			b.slowmode(i)
		case "verification":
			b.verification(i)
		case "room":
			b.roomCommand(i)
		case "post-welcome":
			b.postWelcome(i)
		case "room-invite":
			b.roomInvite(i)
		case "suggest":
			b.suggestForm(i)
		case "suggestion-status":
			b.setSuggestionStatus(i)
		case "follow":
			b.follow(i, opt("anime"))
		case "unfollow":
			b.unfollow(i, opt("anime"))
		case "following":
			b.following(i)
		case "link-anilist":
			b.linkAniList(i)
		case "unlink-anilist":
			b.unlinkAniList(i)
		case "profile":
			b.profile(i)
		case "changelog":
			b.changelog(i)
		case "suggestion-issue":
			b.suggestionIssue(i)
		case "bot-status":
			b.botStatus(i)
		case "settings":
			b.settingsCmd(i)
		case "preview":
			b.previewCmd(i)
		case "support-queue":
			b.supportQueue(i)
		case "spoiler-guard":
			b.setSpoilerGuard(i)
		case "rank":
			b.rank(i)
		case "levels":
			b.levels(i)
		case "post-roles":
			b.postRoles(i)
		case "season-preview", "server-top", "compare", "recommend":
			b.socialCommand(i, d.Name)
		case "support", "diagnose", "thank", "helpers", "answer-stats":
			b.supportExtraCommand(i, d.Name)
		case "link-app", "unlink-app", "linked-apps", "whos-watching", "watch-time":
			b.appCommand(i)
		}
	case discordgo.InteractionApplicationCommandAutocomplete:
		if b.gameAutocomplete(i, i.ApplicationCommandData().Name) {
			return
		}
		if h, ok := autocompleteHandlers[i.ApplicationCommandData().Name]; ok {
			h(b, i)
			return
		}
		switch i.ApplicationCommandData().Name {
		case "watchparty", "follow":
			b.suggestTitles(i)
		case "unfollow":
			b.suggestFollowed(i)
		case "changelog":
			b.suggestReleases(i)
		}
	case discordgo.InteractionModalSubmit:
		if h, rest, ok := findPrefixed(modalHandlers, i.ModalSubmitData().CustomID); ok {
			h(b, i, rest)
			return
		}
		switch id := i.ModalSubmitData().CustomID; id {
		case "suggest-form":
			b.postSuggestion(i)
		case "anilist-user-form", "anilist-token-form":
			b.finishAniListLink(i, id)
		case "rc-rename-form", "rc-limit-form":
			b.roomControlForm(i, id)
		case "support-form":
			b.submitSupportForm(i)
		}
	case discordgo.InteractionMessageComponent:
		id := i.MessageComponentData().CustomID
		if h, rest, ok := findPrefixed(componentHandlers, id); ok {
			h(b, i, rest)
			return
		}
		if eventID, ok := strings.CutPrefix(id, "party-cancel:"); ok {
			b.cancelParty(i, eventID)
			return
		}
		if eventID, ok := strings.CutPrefix(id, "party-in:"); ok {
			b.partyRSVP(i, eventID, true)
			return
		}
		if eventID, ok := strings.CutPrefix(id, "party-out:"); ok {
			b.partyRSVP(i, eventID, false)
			return
		}
		if appID, ok := strings.CutPrefix(id, "app-revoke:"); ok {
			b.revokeApp(i, appID)
			return
		}
		if strings.HasPrefix(id, "rc-") {
			b.roomControl(i, id)
			return
		}
		if showID, ok := strings.CutPrefix(id, "airing-follow:"); ok {
			b.followAiring(i, showID)
			return
		}
		if showID, ok := strings.CutPrefix(id, "follow-stop:"); ok {
			b.stopFollowButton(i, showID)
			return
		}
		if showID, ok := strings.CutPrefix(id, "airing-party:"); ok {
			b.partyAiring(i, showID)
			return
		}
		if roleID, ok := strings.CutPrefix(id, "selfrole:"); ok {
			b.toggleSelfRole(i, roleID)
			return
		}
		if strings.HasPrefix(id, "appeal-") {
			b.appealDecision(i, id)
			return
		}
		if strings.HasPrefix(id, "verify-") {
			b.verifyButton(i, id)
			return
		}
		if showID, ok := strings.CutPrefix(id, "plan-watch:"); ok {
			b.planToWatch(i, showID)
			return
		}
		if b.supportExtraComponent(i, id) {
			return
		}
		if b.gameComponent(i, id) {
			return
		}
		if strings.HasPrefix(id, "mod-") {
			b.modAction(i, id)
			return
		}
		switch id {
		case "solved":
			b.markSolved(i)
		case "bug":
			b.reportBug(i)
		case "sug-up", "sug-down":
			b.voteSuggestion(i, id == "sug-up")
		case "anilist-paste":
			b.anilistPasteForm(i)
		case "airing-pick":
			b.pickAiring(i)
		case "airing-mine":
			b.myAiring(i)
		case "season-pick", "rec-pick":
			b.pickPlanShow(i)
		}
	}
}

func helpEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: "Otakase bot", Color: shu, Description: strings.Join(append([]string{
		"`/install` how to install on your system",
		"`/faq` answers to common questions",
		"`/latest` the newest release",
		"`/changelog` what changed in a release, or `since:` your version for everything newer",
		"`/anime` look up a show on AniList",
		"`/provider-status` which anime sources work right now",
		"`/follow` get a DM when a new episode of a show airs (`/following`, `/unfollow`)",
		"`/watchparty` plan a watch party in a voice channel",
		"`/room` your own voice channel: public, private or 18+ invite only (or join **Create room**)",
		"Your room's chat has buttons to rename, lock, hide, limit, invite or hand it over",
		"`/suggest` suggest an idea; vote on ideas with 👍 and 👎",
		"`/bug` turn a support post into a GitHub issue",
		"`/support` ask for help with a short form; `/diagnose` how to share your diagnostics",
		"`/helpers` the top helpers; thank someone in your post with `/thank`",
		"Type `#123` to show a GitHub issue or pull request",
		"`/link-github` Contributor role if you have a merged pull request",
		"`/link-anilist` link your AniList, then `/profile` shows what you watch",
		"`/activity` opens Watch Together and games; `/leaderboard` shows points, `/riddle-add` creates emoji riddles",
		"`/rank` your level from chatting, `/levels` the most active members",
		"`/compare` your AniList next to another member's",
		"`/recommend` five shows for you; `/server-top` what members here watch",
		"`/link-app` link the Otakase app (`/linked-apps`, `/unlink-app`), then `/whos-watching` and `/watch-time`",
		"Right-click a message → **Apps → Report to mods** to flag it",
		"",
		"In the support forum, press **Mark solved** when your problem is fixed.",
		"The anime channel has a live board of today's episodes: pick a show there for its link, a DM when it airs or a watch party.",
	}, helpLines...), "\n")}
}

func (b *bot) respond(i *discordgo.InteractionCreate, ephemeral bool, e *discordgo.MessageEmbed, comps ...discordgo.MessageComponent) {
	data := &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}
	if ephemeral {
		data.Flags = discordgo.MessageFlagsEphemeral
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource, Data: data}); err != nil {
		log.Printf("respond: %v", err)
	}
}

// deferThen acknowledges at once (lookups can take longer than Discord's
// three seconds) and fills the answer in when fn returns.
func (b *bot) deferThen(i *discordgo.InteractionCreate, ephemeral bool, fn func() (*discordgo.MessageEmbed, []discordgo.MessageComponent)) {
	var flags discordgo.MessageFlags
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: flags}}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	e, comps := fn()
	if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{e}, Components: &comps}); err != nil {
		log.Printf("edit: %v", err)
	}
}

func linkButtons(pairs ...string) []discordgo.MessageComponent {
	var row discordgo.ActionsRow
	for k := 0; k+1 < len(pairs); k += 2 {
		row.Components = append(row.Components, discordgo.Button{Label: pairs[k], URL: pairs[k+1], Style: discordgo.LinkButton})
	}
	return []discordgo.MessageComponent{row}
}

func errorEmbed(format string, a ...any) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Description: fmt.Sprintf(format, a...), Color: 0x8A8276}
}
