package internal

// What a watch party needs from the Discord link (discord_link.go): the app
// token and where the bot is.

// discordToken is the token this app was linked with, if it was and the
// Discord features are on.
func discordToken() (string, bool) {
	config := GetGlobalConfig()
	if !discordEnabled(config) {
		return "", false
	}
	token := loadDiscordLink(config).Token
	return token, token != ""
}

// discordBotURL is the bot's base URL, without a trailing slash.
func discordBotURL() string {
	return discordBaseURL(GetGlobalConfig())
}
