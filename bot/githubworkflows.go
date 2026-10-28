package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// The repository's workflows only run when started by hand. /github starts
// one from Discord, so builds, releases, the site and the wiki can be kicked
// off from a phone. Only server admins can use it: a release is public.
// Winget, the Discord release post and the installer check aren't listed:
// every build tests the installer and a release runs the other two.

// workflowsURL is a variable so tests can point it at a fake server.
var workflowsURL = "https://api.github.com/repos/TheXykril/otakase/actions/workflows"

// ghWorkflow is one workflow /github can start.
type ghWorkflow struct {
	key, file, label string
	needsVersion     bool // its version input is required
	takesVersion     bool
}

var ghWorkflows = []ghWorkflow{
	{key: "build", file: "ci-cd.yml", label: "Build and release"},
	{key: "dev", file: "dev-build.yml", label: "Dev build", needsVersion: true, takesVersion: true},
	{key: "site", file: "site.yml", label: "Site"},
	{key: "wiki", file: "wiki-sync.yml", label: "Wiki sync"},
}

func workflowByKey(key string) (ghWorkflow, bool) {
	for _, w := range ghWorkflows {
		if w.key == key {
			return w, true
		}
	}
	return ghWorkflow{}, false
}

func init() {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(ghWorkflows))
	for _, w := range ghWorkflows {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: w.label, Value: w.key})
	}
	admin := int64(discordgo.PermissionAdministrator)
	registerCommand(&discordgo.ApplicationCommand{Name: "github", Description: "Start a GitHub workflow (build, release, site, wiki...)",
		DefaultMemberPermissions: &admin, Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "workflow", Description: "Which workflow to start", Required: true, Choices: choices},
			{Type: discordgo.ApplicationCommandOptionBoolean, Name: "release", Description: "Build and release: also publish a release of main (default off)"},
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "pr", Description: "Build and release: build this pull request instead of main", MinValue: ptr(1.0)},
			{Type: discordgo.ApplicationCommandOptionString, Name: "version", Description: "Dev build: the version it is heading to, e.g. 26.8.0"},
		}}, (*bot).githubCommand)
}

// workflowInputs builds the inputs a dispatch sends, or says what is wrong.
func workflowInputs(w ghWorkflow, release bool, pr int64, version string) (map[string]string, error) {
	inputs := map[string]string{}
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	switch {
	case (release || pr > 0) && w.key != "build":
		return nil, fmt.Errorf("release and pr only go with Build and release")
	case release && pr > 0:
		return nil, fmt.Errorf("a release is always made from main, so leave pr out")
	case w.needsVersion && version == "":
		return nil, fmt.Errorf("%s needs a version, e.g. 26.8.0", w.label)
	case version != "" && !w.takesVersion:
		return nil, fmt.Errorf("%s doesn't take a version", w.label)
	}
	if w.key == "build" {
		inputs["release"] = strconv.FormatBool(release)
		if pr > 0 {
			inputs["pr_number"] = strconv.FormatInt(pr, 10)
		}
	}
	if version != "" {
		inputs["version"] = version
	}
	return inputs, nil
}

// dispatchWorkflow starts a workflow on main and returns GitHub's status code.
func dispatchWorkflow(token, file string, inputs map[string]string) (int, error) {
	data, _ := json.Marshal(map[string]any{"ref": "main", "inputs": inputs})
	req := githubRequest("POST", workflowsURL+"/"+file+"/dispatches", token)
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "otakase-bot (https://github.com/TheXykril/otakase)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return resp.StatusCode, fmt.Errorf("github: %s: %s", resp.Status, bytes.TrimSpace(body))
	}
	return resp.StatusCode, nil
}

// dispatchProblem explains a refused dispatch to the admin.
func dispatchProblem(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "GitHub didn't accept the bot's token (github_token). It may be wrong or expired."
	case http.StatusForbidden, http.StatusNotFound:
		return "The bot's GitHub token (github_token) can't start workflows on TheXykril/otakase. Give it Actions: Read and write."
	case http.StatusUnprocessableEntity:
		return "GitHub refused the run. Actions may be turned off for the repository, or the workflow's inputs changed."
	default:
		return fmt.Sprintf("GitHub said no (%d). Try again later.", status)
	}
}

// githubCommand handles /github.
func (b *bot) githubCommand(i *discordgo.InteractionCreate) {
	if i.Member == nil || i.Member.Permissions&discordgo.PermissionAdministrator == 0 {
		b.respond(i, true, errorEmbed("Only server admins can start workflows."))
		return
	}
	opts := optionMap(i)
	w, ok := workflowByKey(opts["workflow"].StringValue())
	if !ok {
		b.respond(i, true, errorEmbed("Pick a workflow from the list."))
		return
	}
	var release bool
	var pr int64
	var version string
	if o := opts["release"]; o != nil {
		release = o.BoolValue()
	}
	if o := opts["pr"]; o != nil {
		pr = o.IntValue()
	}
	if o := opts["version"]; o != nil {
		version = o.StringValue()
	}
	inputs, err := workflowInputs(w, release, pr, version)
	if err != nil {
		b.respond(i, true, errorEmbed("%s.", strings.ToUpper(err.Error()[:1])+err.Error()[1:]))
		return
	}
	if b.cfg.GitHubToken == "" {
		b.respond(i, true, errorEmbed("The bot has no GitHub token yet. Set github_token in the add-on options "+
			"to a token with Actions: Read and write on TheXykril/otakase."))
		return
	}
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		status, err := dispatchWorkflow(b.cfg.GitHubToken, w.file, inputs)
		if err != nil {
			log.Printf("github dispatch %s: %v", w.file, err)
			if status == 0 {
				return errorEmbed("Couldn't reach GitHub right now. Try again in a minute."), nil
			}
			return errorEmbed("%s", dispatchProblem(status)), nil
		}
		log.Printf("github dispatch %s %v by %s", w.file, inputs, interactionUser(i).Username)
		what := w.label
		if inputs["release"] == "true" {
			what = "Release build"
		}
		desc := "It shows up in the Actions tab within a few seconds."
		if w.key == "build" {
			desc += " When it's done, the run's page has the builds to download under **Artifacts**."
		}
		return &discordgo.MessageEmbed{Title: what + " started", Color: shu, Description: desc},
			linkButtons("Open runs", repoURL+"/actions/workflows/"+w.file)
	})
}
