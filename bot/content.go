package main

import "strings"

// Everything the bot links to is on the website: the help pages (built from
// the repo's wiki/ folder), the changelog and the downloads, which the
// site's /download/ route serves from the latest release. Members never get
// a link to the repository itself.
const (
	siteURL      = "https://otakase.xyverion.com/"
	wikiURL      = siteURL + "help"
	dlURL        = siteURL + "download/"
	downloadPage = siteURL + "#download"
	changelogURL = siteURL + "changelog/"
)

// versionChangelogURL is the changelog section of one version (26.2.0 or
// v26.2.0).
func versionChangelogURL(version string) string {
	return changelogURL + "#" + strings.TrimPrefix(version, "v")
}

type installGuide struct {
	Title string
	Body  string
}

// installGuides mirror the README's Install section.
var installGuides = map[string]installGuide{
	"android": {"Android phone and Android TV", "Install [otakase-android.apk](" + dlURL + "otakase-android.apk) (Android 7.0 or newer). " +
		"Android asks once to allow installing apps from your browser or file manager.\n\n" +
		"On a TV, open the link in a browser app or a file manager, or send the file over with an app like *Send files to TV*."},
	"windows": {"Windows", "Run [otakase-app-windows-setup.exe](" + dlURL + "otakase-app-windows-setup.exe). " +
		"It installs for your user only, so it needs no administrator rights, and adds Otakase to the Start menu."},
	"mac": {"macOS", "Download [otakase-app-macos.zip](" + dlURL + "otakase-app-macos.zip), unzip it and move **Otakase** to Applications. " +
		"It works on Apple Silicon and Intel. The app is not signed by Apple yet, so the first time, right-click it and choose **Open**."},
	"linux": {"Linux", "Download [otakase-app-linux-x86_64.AppImage](" + dlURL + "otakase-app-linux-x86_64.AppImage), make it executable and run it:\n```bash\n" +
		"chmod +x otakase-app-linux-x86_64.AppImage\n./otakase-app-linux-x86_64.AppImage\n```"},
}

// installSystems are the /install choices, in menu order.
var installSystems = []struct{ Name, Key string }{
	{"Android phone / Android TV", "android"},
	{"Windows", "windows"},
	{"macOS", "mac"},
	{"Linux", "linux"},
}

// updateHowTo is how members update, for every system.
const updateHowTo = "The app updates itself: it offers new releases when it starts, or check under **About & updates** (in More or Settings), then try again."

type faqEntry struct {
	Key, Question, Answer string
}

// faqs are the #faq channel post (`/post-info`) and the `/faq` answers.
var faqs = []faqEntry{
	{"install", "How do I install or update?", "`/install` shows the download for your system, and the [website](" + siteURL + ") has them all. " +
		"The app updates itself: it offers new releases when it starts, or check under **About & updates** (in More or Settings)."},
	{"mpv", "An episode doesn't play", "The app has its own player, so there is nothing else to install. Usually the streaming site is down: " +
		"open **Diagnostics** (in More or Settings) to see which sources answer, or try the next source. " +
		"See [Troubleshooting](" + wikiURL + "/Troubleshooting)."},
	{"nothing-found", "Nothing is found for a show", "Streaming sites change and go down often. Check **Diagnostics** in the app or `/provider-status` here. " +
		"With **Torrent sources** on (Settings → Playback) the app can also play from a torrent. Still nothing? Share what Diagnostics shows in the support forum."},
	{"tv", "My TV can't finish signing in", "Phone and TV need to be on the same home network, and guest Wi-Fi often keeps devices apart. " +
		"Check that the code on the phone matches the one on the TV. See [Android TV](" + wikiURL + "/Android-TV)."},
	{"sync", "My progress didn't sync", "Writes to AniList and MyAnimeList happen in the background and can take a moment. " +
		"Check the account under **Accounts**: one that needs signing in again shows it there. See [Tracking](" + wikiURL + "/Tracking)."},
	{"cast", "Casting can't find my TV", "Phone or computer and TV need to be on the same network. On a computer, a firewall often drops the TV's answer. " +
		"See [Casting](" + wikiURL + "/Casting)."},
	{"mac", "macOS says the app can't be opened", "It is not signed by Apple yet. Right-click **Otakase** in Applications and choose **Open** the first time."},
	{"discord", "How do I link the app to Discord?", "Open **More → Discord** in the app and follow it, or run `/link-app` here. " +
		"Linking lets you join watch rooms, follow shows and report problems straight from the app."},
	{"dev-builds", "Can I try new features early?", "Test builds are posted in the test-builds channel. They may break, so keep the normal release if you want things to just work."},
}

func faqByKey(k string) (faqEntry, bool) {
	for _, f := range faqs {
		if f.Key == k {
			return f, true
		}
	}
	return faqEntry{}, false
}

const supportChecklist = "Thanks for the post! To help us help you, please add:\n" +
	"- Your Otakase version (**About & updates** in More or Settings)\n" +
	"- Your device (Android phone, Android TV, Windows, macOS or Linux)\n" +
	"- The show and episode, what you did and what happened\n" +
	"- For playback or *nothing found* problems: what **Diagnostics** shows\n\n" +
	"When it's fixed, press **Mark solved** below."

// welcomeDMText is the card in the DM new members get, under the header
// picture.
const welcomeDMText = "## Welcome to Otakase!\n" +
	"Watch anime on your phone, TV and computer, with your list kept in sync. Glad you're here."

var welcomeDMSteps = []string{
	"**Install** · `/install` shows the download for your system",
	"**Rules and roles** · read the rules, then pick your system and notifications in **Channels & Roles**",
	"**Help** · `/faq` answers common questions; for anything else, one post per problem in the support forum",
	"**New episodes** · `/follow` a show and I'll DM you when the next episode is out",
}
