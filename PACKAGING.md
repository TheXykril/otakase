# Packaging

How Otakase reaches users, and what to do when publishing a new version.

## Two packages, deliberately

| Package | Built from | Maintained |
|---|---|---|
| `otakase` | source, via the [`PKGBUILD`](PKGBUILD) in this repository | by hand |
| `otakase-bin` | the prebuilt `otakase-linux-x86_64` release asset | by [`update-aur.yml`](.github/workflows/update-aur.yml) |

They cannot share a name. AUR convention reserves the `-bin` suffix for packages
that install a prebuilt binary, and a package that skips `build()` has to carry
it. The source package is the better one — it runs the test suite in `check()`,
so a broken build fails before it installs, and it builds for `aarch64` as well
as `x86_64`.

Both install `/usr/bin/otakase`, symlink `otk` beside it, declare
`replaces=('curd')`, and ship [`otakase.install`](otakase.install) so the
Super+Shift+A keybinding is set up for the installing user.

## Releasing a version

The release workflow is gated on the literal string `release:` appearing in the
pushed commit message. Nothing else publishes, so ordinary commits are free.

1. Set the version in `VERSION.txt` **and** `pkgver` in `PKGBUILD`. They must
   match: the workflow takes the tag `v<version>` from `VERSION.txt`, and the
   PKGBUILD fetches `archive/refs/tags/v${pkgver}.tar.gz`.
2. Close the `## Unreleased` section in `CHANGELOG.md` with the version and date.
3. Commit with `release: v<version>` in the message, and push.

That builds Linux, macOS (x86_64, arm64, universal) and Windows, creates the
tag, publishes the release with its notes, and — once AUR publishing is enabled
— updates `otakase-bin`.

An ordinary push builds Linux only. That is intentional: the other platforms
take most of the run, and nothing needs them until a release.

## Publishing to the AUR

Not yet done. The workflow exists but its guard names a repository that does not
exist, so it cannot run.

### One-time setup

1. **Register** at <https://aur.archlinux.org/register>. The AUR account is
   separate from GitHub and needs an SSH *public* key.

2. **Generate a dedicated key.** Not your GitHub one — the private half goes
   into a CI secret, and you want to be able to revoke exactly one thing.

   ```bash
   ssh-keygen -t ed25519 -C "aur@otakase" -f ~/.ssh/aur_otakase -N ""
   ```

   Add `~/.ssh/aur_otakase.pub` to the AUR account under *My Account*.

3. **Point ssh at it:**

   ```
   Host aur.archlinux.org
     User aur
     IdentityFile ~/.ssh/aur_otakase
     IdentitiesOnly yes
   ```

   Check it before going further — this greets you by username:

   ```bash
   ssh aur@aur.archlinux.org help
   ```

### Publishing `otakase` (source, by hand)

Cloning a name that does not exist is how an AUR package is created. "You appear
to have cloned an empty repository" is the expected response.

```bash
git clone ssh://aur@aur.archlinux.org/otakase.git
cd otakase
cp /path/to/otakase/PKGBUILD /path/to/otakase/otakase.install .
updpkgsums                          # replaces sha256sums=('SKIP') with real hashes
makepkg --printsrcinfo > .SRCINFO   # mandatory; a push without it is rejected
makepkg -si                         # build, run the tests, install
git add PKGBUILD .SRCINFO otakase.install
git commit -m "Initial import: otakase <version>"
git push
```

`otakase.install` must be copied across. The PKGBUILD names it in `install=`,
and without it the build fails.

On every version bump: change `pkgver`, run `updpkgsums`, **regenerate
`.SRCINFO`**, then commit all three. A stale `.SRCINFO` is the most common AUR
mistake — the site reads it, not the PKGBUILD, so the package silently keeps
advertising the old version.

An AUR repository holds packaging files only. Never commit the source tree, a
built package, or `src/` and `pkg/` directories.

### Enabling `otakase-bin` (automated)

1. Register `otakase-bin` on the AUR the same way.
2. Add the **private** key (`~/.ssh/aur_otakase`) as a repository secret named
   `AUR_SSH_PRIVATE_KEY`.
3. In `.github/workflows/update-aur.yml`, change the guard from
   `'DISABLED/aur-publishing-not-configured'` to `'TheXykril/otakase'`.

Until all three are done, leave the guard alone. It exists because this workflow
was inherited from upstream, where it pushed to a package this project does not
own; publishing to somebody else's package is the failure it prevents.

## Installing without the AUR

The AUR is convenience, not the install path. Both of these work from any
release:

```bash
# Arch, from source — the full package experience
git clone https://github.com/TheXykril/otakase.git && cd otakase && makepkg -si

# Anywhere — the prebuilt binary
curl -Lo otakase https://github.com/TheXykril/otakase/releases/latest/download/otakase-linux-x86_64
chmod +x otakase && sudo install -Dm755 otakase /usr/bin/otakase
```

## Testing a package without publishing

`makepkg` needs the release tag to exist, which is awkward before a release. To
build from the working tree instead:

```bash
WORK=$(mktemp -d)
tar --exclude-vcs -czf "$WORK/otakase-<version>.tar.gz" --transform 's|^\.|otakase-<version>|' .
cp PKGBUILD otakase.install "$WORK/"
sed -i 's|source=(.*)|source=("otakase-<version>.tar.gz")|' "$WORK/PKGBUILD"
cd "$WORK" && makepkg --skipinteg
sudo pacman -U otakase-*.pkg.tar.zst
```

Bump `pkgrel` in that copy to force pacman to treat a reinstall as an upgrade,
which is the only way to exercise the `post_upgrade` hook.
