package main

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Features in their own files register their commands, buttons, forms,
// background jobs and heartbeat listeners here from init(), so adding one
// does not mean editing the big switches in commands.go and main.go.

type (
	interactionHandler func(b *bot, i *discordgo.InteractionCreate)
	componentHandler   func(b *bot, i *discordgo.InteractionCreate, arg string)
)

var (
	registeredCommands   []*discordgo.ApplicationCommand
	commandHandlers      = map[string]interactionHandler{}
	autocompleteHandlers = map[string]interactionHandler{}
	componentHandlers    = map[string]componentHandler{} // by custom id prefix, up to and with the ":"
	modalHandlers        = map[string]componentHandler{}
	dmComponentHandlers  = map[string]componentHandler{} // buttons in DMs, where there is no member
	helpLines            []string
	backgroundJobs       []backgroundJob
	startHooks           []func(b *bot)
)

type backgroundJob struct {
	name string
	run  func(b *bot)
}

// registerCommand adds a slash (or context menu) command and its handler.
func registerCommand(cmd *discordgo.ApplicationCommand, h interactionHandler) {
	registeredCommands = append(registeredCommands, cmd)
	commandHandlers[cmd.Name] = h
}

// registerAutocomplete answers autocomplete for a registered command.
func registerAutocomplete(name string, h interactionHandler) { autocompleteHandlers[name] = h }

// registerComponent handles buttons and menus whose custom id starts with
// prefix (e.g. "app-queue:"); the handler gets the rest of the id.
func registerComponent(prefix string, h componentHandler) { componentHandlers[prefix] = h }

// registerDMComponent handles buttons in the bot's DMs whose custom id
// starts with prefix. Use interactionUser(i): i.Member is nil there. Register
// the same handler with registerComponent too when the button can also be
// pressed in the server.
func registerDMComponent(prefix string, h componentHandler) { dmComponentHandlers[prefix] = h }

// registerModal handles modals whose custom id starts with prefix.
func registerModal(prefix string, h componentHandler) { modalHandlers[prefix] = h }

// registerHelp adds a line to /help.
func registerHelp(line string) { helpLines = append(helpLines, line) }

// registerJob runs fn for as long as the bot runs, restarted when it stops.
func registerJob(name string, fn func(b *bot)) {
	backgroundJobs = append(backgroundJobs, backgroundJob{name, fn})
}

// onStart runs fn once the bot is connected, before the jobs start: the
// place to add Discord event handlers.
func onStart(fn func(b *bot)) { startHooks = append(startHooks, fn) }

// findPrefixed is the handler whose prefix starts id, and the rest of id.
func findPrefixed(m map[string]componentHandler, id string) (componentHandler, string, bool) {
	for p, h := range m {
		if rest, ok := strings.CutPrefix(id, p); ok {
			return h, rest, true
		}
	}
	return nil, "", false
}
