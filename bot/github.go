package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// startGitHubLink gives the Contributor role to members with a merged pull
// request. It uses GitHub's device flow: the member enters a short code on
// github.com, so the bot needs no web address of its own.
func (b *bot) startGitHubLink(i *discordgo.InteractionCreate) {
	var dc struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if err := githubForm("https://github.com/login/device/code", url.Values{"client_id": {b.cfg.GitHubClientID}}, &dc); err != nil || dc.UserCode == "" {
		log.Printf("device code: %v", err)
		b.respond(i, true, errorEmbed("GitHub didn't answer. Try again in a minute."))
		return
	}
	b.respond(i, true, &discordgo.MessageEmbed{
		Title: "Link your GitHub account",
		Description: fmt.Sprintf("1. Open the link below\n2. Enter the code **`%s`**\n3. Press Authorize\n\n"+
			"Only your public profile is read. The code works for %d minutes.", dc.UserCode, dc.ExpiresIn/60),
		Color: shu,
	}, linkButtons("Open GitHub", dc.VerificationURI)...)

	go func() {
		login, err := b.pollDeviceToken(dc.DeviceCode, dc.Interval, time.Duration(dc.ExpiresIn)*time.Second)
		if err != nil {
			b.followup(i, errorEmbed("Linking didn't finish: %v", err))
			return
		}
		merged, err := hasMergedPR(login, b.cfg.GitHubToken)
		switch {
		case err != nil:
			b.followup(i, errorEmbed("Couldn't check your pull requests right now. Try again later."))
		case !merged:
			b.followup(i, errorEmbed("Linked as **%s**, but there's no merged pull request from you yet. "+
				"Once one is merged, run `/link-github` again.", login))
		default:
			if err := b.s.GuildMemberRoleAdd(b.cfg.GuildID, i.Member.User.ID, b.cfg.ContributorRole); err != nil {
				log.Printf("role add: %v", err)
				b.followup(i, errorEmbed("Couldn't give the role. Ask a moderator."))
				return
			}
			b.followup(i, &discordgo.MessageEmbed{Description: fmt.Sprintf("Thanks for contributing, **%s**! You now have the Contributor role.", login), Color: shu})
		}
	}()
}

func (b *bot) followup(i *discordgo.InteractionCreate, e *discordgo.MessageEmbed) {
	if _, err := b.s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{e}, Flags: discordgo.MessageFlagsEphemeral}); err != nil {
		log.Printf("followup: %v", err)
	}
}

func (b *bot) pollDeviceToken(deviceCode string, interval int, ttl time.Duration) (string, error) {
	if interval < 5 {
		interval = 5
	}
	deadline := time.Now().Add(ttl)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)
		var t struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		err := githubForm("https://github.com/login/oauth/access_token", url.Values{
			"client_id":   {b.cfg.GitHubClientID},
			"device_code": {deviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}, &t)
		if err != nil {
			continue
		}
		switch t.Error {
		case "":
			var u struct{ Login string }
			if err := getJSON(githubRequest("GET", "https://api.github.com/user", t.AccessToken), &u); err != nil {
				return "", err
			}
			return u.Login, nil
		case "authorization_pending":
		case "slow_down":
			interval += 5
		case "access_denied":
			return "", fmt.Errorf("you pressed Cancel on GitHub")
		default:
			return "", fmt.Errorf("the code expired")
		}
	}
	return "", fmt.Errorf("the code expired")
}

func hasMergedPR(login, token string) (bool, error) {
	q := url.QueryEscape(fmt.Sprintf("repo:TheXykril/otakase is:pr is:merged author:%s", login))
	var r struct {
		TotalCount int `json:"total_count"`
	}
	err := getJSON(githubRequest("GET", "https://api.github.com/search/issues?q="+q, token), &r)
	return r.TotalCount > 0, err
}

func githubForm(endpoint string, form url.Values, out any) error {
	req, _ := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "otakase-bot (https://github.com/TheXykril/otakase)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}
