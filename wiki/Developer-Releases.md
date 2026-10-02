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

CI keys off the `release:` prefix: it builds every platform, tags
`vYY.DROP.HOTFIX`, publishes the GitHub release and builds the Windows
installer with that version.

## Where users see the version

The running version is at the right of the menu header in the terminal and of
the search bar in rofi, and `otakase -v` prints it.
