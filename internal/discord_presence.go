package internal

// OtakaseDiscordAppID is the Discord application profiles show as
// "Watching Otakase".
const OtakaseDiscordAppID = "1556589176164589578"

// Discord fetches presence images itself, so they must be public URLs. They
// are read from main, so a new image works without a site deploy.
const ()

// ConvertSecondsToMinutes turns a length in seconds into whole minutes.
func ConvertSecondsToMinutes(seconds int) int {
	return seconds / 60
}
