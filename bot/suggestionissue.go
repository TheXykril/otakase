package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Staff turn a suggestion into a GitHub issue with /suggestion-issue in its
// thread. A loop watches linked issues: closed as completed marks the
// suggestion done, closed as not planned marks it declined.

// issuesURL is a variable so tests can point it at a fake server.
var issuesURL = "https://api.github.com/repos/TheXykril/otakase/issues"

const suggestionIssueChecks = 30 * time.Minute

func suggestionIssueCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{Name: "suggestion-issue",
		Description: "Make a GitHub issue from the suggestion this thread is about", DefaultMemberPermissions: &permPurge}
}

func suggestionIssueField(n int) *discordgo.MessageEmbedField {
	return &discordgo.MessageEmbedField{Name: "GitHub issue", Value: fmt.Sprintf("[#%d](%s/issues/%d)", n, repoURL, n)}
}

// suggestionIssueBody is the text of the GitHub issue made from s.
func suggestionIssueBody(s suggestion, link string) string {
	body := fmt.Sprintf("Suggested by **%s** on the Otakase Discord.\n\n", s.Name)
	if s.Details != "" {
		body += s.Details + "\n\n"
	}
	body += fmt.Sprintf("Votes: %d up, %d down\n\n[Open the suggestion on Discord](%s)", len(s.Up), len(s.Down), link)
	return body
}

// issueCreateProblem explains a failed GitHub answer to staff.
func issueCreateProblem(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "GitHub didn't accept the bot's token (github_token). It may be wrong or expired."
	case http.StatusForbidden, http.StatusNotFound:
		return "The bot's GitHub token (github_token) can't write issues on TheXykril/otakase. Give it Issues: Read and write."
	case http.StatusGone:
		return "Issues are turned off on the GitHub repository."
	default:
		return fmt.Sprintf("GitHub said no (%d). Try again later.", status)
	}
}

// createIssue opens a GitHub issue and returns it with GitHub's status code.
// When the labels are refused it tries again without them.
func createIssue(token, title, body string, labels []string) (ghIssue, int, error) {
	payload := map[string]any{"title": title, "body": body}
	if len(labels) > 0 {
		payload["labels"] = labels
	}
	data, _ := json.Marshal(payload)
	req := githubRequest("POST", issuesURL, token)
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "otakase-bot (https://github.com/TheXykril/otakase)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return ghIssue{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnprocessableEntity && len(labels) > 0 {
		return createIssue(token, title, body, nil)
	}
	if resp.StatusCode >= 300 {
		return ghIssue{}, resp.StatusCode, fmt.Errorf("github: %s", resp.Status)
	}
	var is ghIssue
	err = json.NewDecoder(resp.Body).Decode(&is)
	return is, resp.StatusCode, err
}

// suggestionIssue handles /suggestion-issue.
func (b *bot) suggestionIssue(i *discordgo.InteractionCreate) {
	if i.Member.Permissions&(discordgo.PermissionManageMessages|discordgo.PermissionAdministrator) == 0 {
		b.respond(i, true, errorEmbed("Only staff can do this."))
		return
	}
	var s suggestion
	var found bool
	b.store.view(func(d *storeData) { s, found = d.Suggestions[i.ChannelID] })
	if !found {
		b.respond(i, true, errorEmbed("Use this inside a suggestion's thread."))
		return
	}
	if s.Issue > 0 {
		b.respond(i, true, errorEmbed("This suggestion already has GitHub issue #%d.", s.Issue),
			linkButtons("Open issue", fmt.Sprintf("%s/issues/%d", repoURL, s.Issue))...)
		return
	}
	if b.cfg.GitHubToken == "" {
		b.respond(i, true, errorEmbed("The bot has no GitHub token yet. Set github_token in the add-on options "+
			"to a token that can write issues on TheXykril/otakase (Issues: Read and write)."))
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); err != nil {
		log.Printf("defer: %v", err)
		return
	}
	reply := func(e *discordgo.MessageEmbed) {
		if _, err := b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{e}}); err != nil {
			log.Printf("edit: %v", err)
		}
	}
	link := messageLink(b.cfg.GuildID, s.Channel, i.ChannelID)
	is, status, err := createIssue(b.cfg.GitHubToken, clip(s.Title, 200), suggestionIssueBody(s, link), []string{"enhancement"})
	if err != nil {
		log.Printf("suggestion issue: %v", err)
		if status == 0 {
			reply(errorEmbed("Couldn't reach GitHub right now. Try again in a minute."))
		} else {
			reply(errorEmbed("%s", issueCreateProblem(status)))
		}
		return
	}
	b.store.update(func(d *storeData) {
		if cur, ok := d.Suggestions[i.ChannelID]; ok {
			cur.Issue = is.Number
			d.Suggestions[i.ChannelID] = cur
			s = cur
		}
	})
	if _, err := b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: s.Channel, ID: i.ChannelID,
		Embeds: &[]*discordgo.MessageEmbed{s.embed()}, Components: ptr(s.components())}); err != nil {
		log.Printf("suggestion card: %v", err)
	}
	if _, err := b.s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{Color: shu,
			Description: fmt.Sprintf("<@%s> made GitHub issue **#%d** from this suggestion. "+
				"When it's closed there, the suggestion is marked done or declined here.", i.Member.User.ID, is.Number)}},
		Components:      linkButtons("Open issue", is.HTMLURL),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Printf("suggestion issue note: %v", err)
	}
	reply(&discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Made issue [#%d](%s).", is.Number, is.HTMLURL)})
}

// issueOutcome maps a closed GitHub issue to a suggestion status, or "" when
// the issue is still open (or closed some other way).
func issueOutcome(is ghIssue) string {
	if is.State != "closed" {
		return ""
	}
	switch is.StateReason {
	case "not_planned", "duplicate":
		return "declined"
	case "reopened":
		return ""
	default: // "completed", or no reason on older issues
		return "done"
	}
}

func (b *bot) suggestionIssueLoop() {
	for {
		if err := b.checkSuggestionIssues(); err != nil {
			b.jobFailed("Suggestion issues", err)
		} else {
			b.jobOK("Suggestion issues")
		}
		time.Sleep(suggestionIssueChecks)
	}
}

// checkSuggestionIssues updates open suggestions whose GitHub issue closed.
func (b *bot) checkSuggestionIssues() error {
	linked := map[string]int{} // thread → issue
	b.store.view(func(d *storeData) {
		for id, s := range d.Suggestions {
			if s.Issue > 0 && !statusByKey(s.Status).Closed {
				linked[id] = s.Issue
			}
		}
	})
	var firstErr error
	for thread, n := range linked {
		var is ghIssue
		if err := getJSON(githubRequest("GET", fmt.Sprintf("%s/%d", issuesURL, n), b.cfg.GitHubToken), &is); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("issue #%d: %w", n, err)
			}
			continue
		}
		key := issueOutcome(is)
		if key == "" {
			continue
		}
		st := statusByKey(key)
		how := "completed"
		switch is.StateReason {
		case "not_planned":
			how = "not planned"
		case "duplicate":
			how = "a duplicate"
		}
		note := fmt.Sprintf("GitHub issue #%d was closed as %s.", n, how)
		by := ""
		if b.s.State != nil && b.s.State.User != nil {
			by = b.s.State.User.ID
		}
		if _, err := b.changeSuggestionStatus(thread, key, note, by); err != nil {
			log.Printf("suggestion status: %v", err)
		}
		if _, err := b.s.ChannelMessageSendComplex(thread, &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{{Color: st.Color,
				Description: fmt.Sprintf("Marked **%s**: [GitHub issue #%d](%s) was closed as %s.", st.Label, n, is.HTMLURL, how)}},
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Printf("suggestion issue note: %v", err)
		}
		b.archiveIfClosed(thread, key)
	}
	return firstErr
}
