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
	"arch": {"Arch Linux / Manjaro", "Build with the bundled PKGBUILD:\n```bash\n" +
		"sudo pacman -S --needed go git mpv\n" +
		"git clone https://github.com/TheXykril/otakase.git\n" +
		"cd otakase\nmakepkg -si\n```\nOr the prebuilt binary:\n```bash\n" +
		"sudo pacman -S --needed mpv\n" +
		"curl -Lo otakase " + dlURL + "otakase-linux-x86_64\n" +
		"chmod +x otakase\nsudo install -Dm755 otakase /usr/bin/otakase\nsudo ln -sf otakase /usr/bin/otk\n```"},
	"linux": {"Debian / Ubuntu and other Linux", "```bash\n" +
		"sudo apt update\nsudo apt install mpv curl rofi libnotify-bin xdg-utils\n\n" +
		"# x86_64\ncurl -Lo otakase " + dlURL + "otakase-linux-x86_64\n" +
		"# ARM64\ncurl -Lo otakase " + dlURL + "otakase-linux-arm64\n\n" +
		"chmod +x otakase\nsudo install -Dm755 otakase /usr/bin/otakase\nsudo ln -sf otakase /usr/bin/otk\n```"},
	"mac": {"macOS", "```bash\nbrew install mpv curl\n\n" +
		"# Apple Silicon\ncurl -Lo otakase " + dlURL + "otakase-macos-arm64\n" +
		"# Intel\ncurl -Lo otakase " + dlURL + "otakase-macos-x86_64\n\n" +
		"chmod +x otakase\nsudo mv otakase /usr/local/bin/\nsudo ln -sf otakase /usr/local/bin/otk\n```"},
	"windows": {"Windows", "Run the [installer](" + dlURL + "otakase-windows-installer.exe). " +
		"It comes with mpv and adds `otakase` and `otk` to your PATH.\n\n" +
		"Or with winget:\n```powershell\nwinget install TheXykril.Otakase\n```\n" +
		"The standalone [otakase-windows-x86_64.exe](" + dlURL + "otakase-windows-x86_64.exe) needs mpv installed separately."},
}

type faqEntry struct {
	Key, Question, Answer string
}

// faqs match the #faq channel post.
var faqs = []faqEntry{
	{"install", "How do I install or update?", "Install steps for every system are on the [website](" + siteURL + ") and in `/install`. Update with `otakase -u`."},
	{"mpv", "Playback never starts", "Otakase needs mpv installed and on your PATH. See [Troubleshooting](" + wikiURL + "/Troubleshooting)."},
	{"nothing-found", "Nothing is found for a show", "Run `otakase -provider-status` to see which sources respond, then share the output in the support forum."},
	{"icons", "Menu icons show as boxes", "On Linux Otakase installs a small icon font by itself. Otherwise use a Nerd Font, or set `Icons=false` in the config (`otakase -e`)."},
	{"sync", "My progress didn't sync", "See [Tracking](" + wikiURL + "/Tracking). `otakase -change-token` signs you in again."},
	{"cast", "Casting can't find my TV", "Run `otakase -cast-setup`, then see [Casting Problems](" + wikiURL + "/Casting-Problems)."},
	{"dev-builds", "Can I try new features early?", "Set `DevBuilds=true` in the config (`otakase -e`). Test builds are posted in the test-builds channel."},
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
	"- Your Otakase version (`otakase -v`)\n" +
	"- Your system (Linux distro / Windows / macOS)\n" +
	"- The command you ran and what happened\n" +
	"- For *nothing found* problems: the output of `otakase -provider-status`\n\n" +
	"When it's fixed, press **Mark solved** below."

const welcomeDM = "## Welcome to Otakase!\n" +
	"Watch anime from the command line, with your list kept in sync.\n\n" +
	"- Read the rules and pick your system in **Channels & Roles**\n" +
	"- Something broken? Post in the support forum, one post per problem\n" +
	"- `/install` and `/faq` work in any channel\n\n" +
	"Website: " + siteURL + "\nWiki: " + wikiURL
