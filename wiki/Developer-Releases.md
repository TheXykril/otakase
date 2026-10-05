# Versions and releases

## Version numbers

Since 26.1.0, releases are numbered by year, the way Minecraft numbers its
drops: `YY.DROP.HOTFIX`.

| Part | Meaning | Example |
| --- | --- | --- |
| `YY` | The last two digits of the year the release ships in | `26` |
| `DROP` | The feature release within that year, starting at 1 | `26.2.0` is the second of 2026 |
| `HOTFIX` | A fix-only release on top of a drop, starting at 0 | `26.2.1` |

- A release with anything new bumps `DROP` and resets `HOTFIX` to 0.
- A release with only fixes bumps `HOTFIX`.
- The first release in a new year is `YY.1.0`, whatever the previous drop was.
- The number says nothing about compatibility. A change that breaks a
  setting, a flag or stored data gets its own paragraph in the changelog.

Releases before 26.1.0 were semver, ending at 2.2.2. The jump needs no
special handling: the updater, pacman's `vercmp`, git's `version:refname`
tag sort and the Windows installer all compare the three numbers left to
right, and 26 is greater than 2.

## Cutting a release

Releases are cut only when the maintainer asks. A release is one commit on
`main` titled `release: YY.DROP.HOTFIX` that:

1. sets `VERSION.txt` to the new version,
2. sets `pkgver` in `PKGBUILD` (and resets `pkgrel=1`),
3. adds a `## YY.DROP.HOTFIX — YYYY-MM-DD` section at the top of
   `CHANGELOG.md`.

The update prompt shows these `CHANGELOG.md` sections to people updating, read
from the new release's tag: every section between their version and the new
one, with the bold opening sentence of each entry as its headline. Write
entries for users, opening with a bold sentence that stands on its own. A
`## Unreleased` section at the top, if there is one, is what a dev build
offers.

CI keys off the `release:` prefix: it builds every platform, tags
`vYY.DROP.HOTFIX`, publishes the GitHub release and builds the Windows
installer with that version.

## winget

After a release, CI submits the Windows installer to winget
(`microsoft/winget-pkgs`) through the `Winget` workflow, using
[komac](https://github.com/russellbanks/Komac). It opens a pull request there
from the owner's fork; winget's moderators merge it, usually within a few
days, and then `winget install TheXykril.Otakase` and `winget upgrade` see the
new version.

- It needs a `WINGET_TOKEN` repository secret: a classic personal access
  token of the repo owner with the `public_repo` scope. Without it the
  workflow does nothing and says so in a notice.
- The first submission, while the package is not in winget yet, is built from
  the manifests in `Build/winget/`. After that each release uses
  `komac update`, which copies the last accepted manifest and changes only the
  version, URL and hash. To change the package's description or tags, edit it
  in a winget-pkgs pull request, not in `Build/winget/`.
- To submit a release again (or the first one by hand), run the `Winget`
  workflow from the Actions tab; leave the version empty for the latest
  release.

The installer adds its folder to the user's PATH (a task in
`Build/otakase-windows-build.iss`, checked by default, so a silent winget
install gets it too) and takes it out again on uninstall. The
`Windows installer check` workflow builds, installs, runs and uninstalls it on
any pull request that changes the installer script.

## Discord

After a release, CI posts an announcement card to the Discord server's
announcements channel through the `Discord release` workflow
(`.github/workflows/discord-release.yml`). The card has a header image with
the version (`.github/discord/release-header.html`, screenshotted with the
runner's Chrome), the bold opening sentence of each `CHANGELOG.md` entry for
that version, a ping for the Release pings role, and buttons to the release,
the changelog and the website.

- It needs a `DISCORD_RELEASE_WEBHOOK` repository secret: the announcements
  channel's webhook URL. Without it the workflow does nothing.
- The pinged role is `DISCORD_RELEASE_ROLE` in the workflow; empty it for no
  ping.
- To post a release again, run the `Discord release` workflow from the
  Actions tab with its version.
- To preview a post, run `python3 .github/discord/post_release.py post
  VERSION header.png` with `DISCORD_RELEASE_WEBHOOK` set to a test channel's
  webhook.

## Where users see the version

The running version is at the right of the menu header in the terminal and of
the search bar in rofi, and `otakase -v` prints it.
