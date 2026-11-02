package main

const (
	repoURL = "https://github.com/TheXykril/otakase"
	siteURL = "https://otakase.xyverion.com/"
	wikiURL = repoURL + "/wiki"
	dlURL   = repoURL + "/releases/latest/download/"
)

type installGuide struct {
	Title string
	Body  string
}

// installGuides mirror the README's Install section.
var installGuides = map[string]installGuide{
	"android": {"Android phone and Android TV", "Install [otakase-android.apk](" + dlURL + "otakase-android.apk) (Android 7.0 or newer). " +
		"Android asks once to allow installing apps from your browser or file manager. One app works on phones and on Android TV."},
	"windows": {"Windows", "Run [otakase-app-windows-setup.exe](" + dlURL + "otakase-app-windows-setup.exe). " +
		"It installs for your user only, needs no administrator rights and adds Otakase to the Start menu."},
	"mac": {"macOS", "Download [otakase-app-macos.zip](" + dlURL + "otakase-app-macos.zip), unzip it and move **Otakase** to Applications. " +
		"It works on Apple Silicon and Intel. It is not signed by Apple yet, so the first time, right-click it and choose **Open**."},
	"linux": {"Linux", "Download [otakase-app-linux-x86_64.AppImage](" + dlURL + "otakase-app-linux-x86_64.AppImage), then:\n```bash\n" +
		"chmod +x otakase-app-linux-x86_64.AppImage\n./otakase-app-linux-x86_64.AppImage\n```\nThe player is inside, so there is nothing else to install."},
}

type faqEntry struct {
	Key, Question, Answer string
}

// faqs match the #faq channel post.
var faqs = []faqEntry{
	{"install", "How do I install or update?", "Install steps for every system are on the [website](" + siteURL + ") and in `/install`. The app checks for updates itself; on Android use **More → About & updates**."},
	{"mpv", "Playback never starts", "Try another copy or source from the player, then see [Troubleshooting](" + wikiURL + "/Troubleshooting). **Diagnostics** in the app shows which sources answer."},
	{"nothing-found", "Nothing is found for a show", "Run `/provider-status` to see how many sources respond. If the show itself is wrong, use **Wrong show?** on its page. Then share what you see in the support forum."},
	{"sync", "My progress didn't sync", "See [Tracking](" + wikiURL + "/Tracking). Signing in again under **Accounts** fixes an expired sign-in."},
	{"cast", "Casting can't find my TV", "Usually a firewall on the computer. See [Casting](" + wikiURL + "/Casting)."},
	{"dev-builds", "Can I try new features early?", "Test builds are posted in the test-builds channel. Install them by hand over your current app."},
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
	"- Your Otakase version (under **About & updates** in the app)\n" +
	"- Your device (Android phone or TV, Windows, macOS, Linux)\n" +
	"- What you did and what happened\n" +
	"- For *nothing found* problems: what `/provider-status` says\n\n" +
	"When it's fixed, press **Mark solved** below."

// welcomeDMText is the card in the DM new members get, under the header
// picture.
const welcomeDMText = "## Welcome to Otakase!\n" +
	"Anime, movies and shows on every screen you own, with your list kept in sync. Glad you're here."

var welcomeDMSteps = []string{
	"**Install** · `/install` shows how for your device",
	"**Rules and roles** · read the rules, then pick your system and notifications in **Channels & Roles**",
	"**Help** · `/faq` answers common questions; for anything else, one post per problem in the support forum",
	"**New episodes** · `/follow` a show and I'll DM you when the next episode is out",
}
