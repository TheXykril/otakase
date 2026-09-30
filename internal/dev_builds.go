package internal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// devReleaseTag is the GitHub pre-release the Dev Build workflow replaces on
// every run. Its tag never changes, so the version it carries is read from
// the release's name ("Otakase 26.1.0-dev.12") instead of the tag.
const devReleaseTag = "dev"

var devVersionRe = regexp.MustCompile(`\d+\.\d+\.\d+-dev\.[0-9A-Za-z.]+`)

// releaseVersion is the version a release installs: its tag for a normal
// release, the version in its name or notes for the dev pre-release. Empty
// when a dev release names no version.
func releaseVersion(release githubReleaseAPI) string {
	if strings.TrimSpace(release.TagName) != devReleaseTag {
		return normalizeReleaseVersion(release.TagName)
	}
	if v := devVersionRe.FindString(release.Name); v != "" {
		return v
	}
	return devVersionRe.FindString(release.Body)
}

// splitPrerelease splits "26.1.0-dev.12+abc" into "26.1.0" and "dev.12".
func splitPrerelease(version string) (base, pre string) {
	version = normalizeReleaseVersion(version)
	if i := strings.Index(version, "+"); i >= 0 {
		version = version[:i]
	}
	if i := strings.Index(version, "-"); i >= 0 {
		return version[:i], version[i+1:]
	}
	return version, ""
}

// prereleaseNumber is the trailing number of "dev.12"; 0 when there is none,
// as in the first dev builds, which ended in a commit hash.
func prereleaseNumber(pre string) int {
	parts := strings.Split(pre, ".")
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return n
}

// compareUpdateVersions orders versions for the update check. It is
// compareVersions plus pre-releases: a dev build comes before the release it
// leads up to (26.1.0-dev.12 < 26.1.0) and dev builds of one version follow
// their number. Migrations keep using compareVersions, where a dev build of
// 26.1.0 counts as 26.1.0 so it gets that version's new settings.
func compareUpdateVersions(a, b string) int {
	aBase, aPre := splitPrerelease(a)
	bBase, bPre := splitPrerelease(b)
	if parseVersionParts(aBase) == nil || parseVersionParts(bBase) == nil {
		return compareVersions(normalizeReleaseVersion(a), normalizeReleaseVersion(b))
	}
	if c := compareVersions(aBase, bBase); c != 0 {
		return c
	}
	switch {
	case aPre == bPre:
		return 0
	case aPre == "":
		return 1
	case bPre == "":
		return -1
	}
	an, bn := prereleaseNumber(aPre), prereleaseNumber(bPre)
	if an < bn {
		return -1
	}
	if an > bn {
		return 1
	}
	return strings.Compare(aPre, bPre)
}

// fetchUpdateRelease returns the release to offer: the latest release, or,
// with DevBuilds on, the dev pre-release when it is newer. A missing dev
// release (between two dev builds, or none published yet) falls back to the
// latest release.
func fetchUpdateRelease(repo string, devBuilds bool) (githubReleaseAPI, string, error) {
	latest, err := fetchGitHubRelease(repo, "latest")
	if !devBuilds {
		if err != nil {
			return githubReleaseAPI{}, "", err
		}
		return latest, releaseVersion(latest), nil
	}
	dev, devErr := fetchGitHubRelease(repo, "tags/"+devReleaseTag)
	devVersion := ""
	if devErr == nil {
		devVersion = releaseVersion(dev)
	} else {
		Log(fmt.Sprintf("No dev build to offer: %v", devErr))
	}
	if err != nil {
		if devVersion == "" {
			return githubReleaseAPI{}, "", err
		}
		return dev, devVersion, nil
	}
	latestVersion := releaseVersion(latest)
	if devVersion != "" && compareUpdateVersions(latestVersion, devVersion) < 0 {
		return dev, devVersion, nil
	}
	return latest, latestVersion, nil
}

// UpdateReleaseTag is the release `otakase -u` installs: empty for the
// latest release, or the dev tag when DevBuilds is on and the dev build is
// the newer one.
func UpdateReleaseTag(config *Config) string {
	if config == nil || !config.DevBuilds {
		return ""
	}
	release, _, err := fetchUpdateRelease(DefaultUpdateRepo, true)
	if err != nil {
		Log(fmt.Sprintf("Could not look up the dev build, updating to the latest release: %v", err))
		return ""
	}
	return release.TagName
}
