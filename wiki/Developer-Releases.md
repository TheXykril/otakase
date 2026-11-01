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
3. adds a `## YY.DROP.HOTFIX - YYYY-MM-DD` section at the top of
   `CHANGELOG.md`.

The update prompt shows these `CHANGELOG.md` sections to people updating, read
from the new release's tag: every section between their version and the new
one, with the bold opening sentence of each entry as its headline. Write
entries for users, opening with a bold sentence that stands on its own. A
`## Unreleased` section at the top, if there is one, is what a dev build
offers.

Push that commit, then start the **Build and release** workflow with
*release* ticked (Actions tab, or `/github workflow:build release:true` in
Discord). It runs `app-build.yml` from `main`: Go vet and tests, Flutter
analysis and tests, the signed Android APK and the desktop app on one runner
per OS. Then it tags `vYY.DROP.HOTFIX`, publishes the GitHub release and posts
it in Discord. Workflows never run on a push or pull request by themselves.

A release carries these files; the names are what the in-app updaters look
for:

| File | What |
| --- | --- |
| `otakase-android.apk` + `.json` | Phones and Android TV |
| `otakase-app-windows-setup.exe` + `otakase-app-windows.json` | Windows (per-user installer) |
| `otakase-app-macos.zip` + `.json` | macOS (universal `Otakase.app`, ad hoc signed) |
| `otakase-app-linux-x86_64.AppImage` + `otakase-app-linux-x86_64.json` | Linux (bundles libmpv) |

The **Dev Build** workflow builds the same files from `main` with the version
`<version>-dev.<run number>` and replaces the `dev` pre-release. The apps never
offer a pre-release as an update.

## winget

Off for now: releases do not call the `Winget` workflow. It still submits the
old command-line installer, so before turning it back on, point it at
`otakase-app-windows-setup.exe` (the package id `TheXykril.Otakase` can stay).
How it works when on:

- It uses [komac](https://github.com/russellbanks/Komac) to open a pull
  request on `microsoft/winget-pkgs` from the owner's fork; winget's moderators
  merge it, usually within a few days.
- It needs a `WINGET_TOKEN` repository secret: a classic personal access
  token of the repo owner with the `public_repo` scope.
- The first submission is built from the manifests in `Build/winget/`. After
  that each release uses `komac update`, which copies the last accepted
  manifest and changes only the version, URL and hash.

## Discord

After a release, CI posts an announcement card to the Discord server's
stable-builds channel through the `Discord release` workflow
(`.github/workflows/discord-release.yml`). The card has a header image with
the version (`.github/discord/release-header.html`, screenshotted with the
runner's Chrome), the bold opening sentence of each `CHANGELOG.md` entry for
that version, a ping for the Release pings role, and buttons to the release,
the changelog and the website.

- The Otakase bot sends the post, so it carries the bot's profile. It needs a
  `DISCORD_BOT_TOKEN` repository secret, the bot's token from the Discord
  developer portal; without it the workflow does nothing. The bot needs to
  view, send messages, attach files and mention roles in both channels.
- The channel and pinged role are `DISCORD_RELEASE_CHANNEL` and
  `DISCORD_RELEASE_ROLE` in the workflow; empty the role for no ping. A
  channel is an id or a name looked up in the `DISCORD_GUILD` server, with or
  without its `版・` style prefix, so renaming the prefix doesn't break it. In
  an announcement channel the post is also published to following servers.
- To post a release again, run the `Discord release` workflow from the
  Actions tab with its version.
- Each dev build (the `Dev Build` workflow) posts a card to the
  test-builds channel too: its version, the `feat`/`fix` commits on `main`
  since the last release commit, a ping for the Tester role and a link to the
  `dev` pre-release. The channel and role are `DISCORD_DEV_CHANNEL` and
  `DISCORD_DEV_ROLE` in `dev-build.yml`.
- To preview a post, run `python3 .github/discord/post_release.py post
  VERSION header.png` with `DISCORD_BOT_TOKEN` set and
  `DISCORD_RELEASE_CHANNEL` pointing at a test channel.

## Where users see the version

In the app, under About & updates.
