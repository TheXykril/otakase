# Developer

Pages for people working on otakase itself. Nothing here is needed to install
or use it; that is all in the [README](https://github.com/TheXykril/otakase#readme).

## Pages

- **[Providers](Developer-Providers)** — how stream sources are built, registered and turned off, and how to add one
- **[Casting Verification](Developer-Casting-Verification)** — checking casting against a real Chromecast, DLNA TV or Kodi
- **[Versions and releases](Developer-Releases)** — the `YY.DROP.HOTFIX` numbering and how a release is cut
- **[Cast Window Prompts](Developer-Cast-Window-Prompts)** — what prompts do in a terminal opened by a rofi cast, where nobody is watching

## Building and testing

```bash
git clone https://github.com/TheXykril/otakase.git
cd otakase
go build ./cmd/otakase
go vet ./...
go test -short -race ./...
```

CI runs the same `go vet` and tests plus a Linux build on every pull request.
Dependencies are vendored (`vendor/`).

## Debugging

Everything otakase does that can fail is logged to
`<StoragePath>/otakase-debug.log` (default `~/.local/share/otakase/`),
including ffmpeg's own output during casts and downloads.
`otakase -provider-status` probes each provider and reports which respond.

## Pull requests and releases

Open pull requests against `main` with a conventional title and fill in the
template. Version numbering and how a release is cut are on
[Versions and releases](Developer-Releases).

## Editing this wiki

The wiki is kept in the [`wiki/`](https://github.com/TheXykril/otakase/tree/main/wiki)
folder of the main repository. A workflow copies it here on every push to
`main`, so edit the files there; edits made on the wiki directly are
overwritten by the next sync. Pages for users go at the top level; developer
pages are named `Developer-*.md` and listed in the sidebar's Developer section.
