# Writing a provider

A provider is a streaming host: given a title it finds shows, lists their
episodes, and turns an episode into stream URLs mpv can play. Everything
else (the tracker, menus, matching a search result to a list entry, sub/dub
fallback, the player, skipping) lives in `internal/` and stays out of the
provider.

Providers are plain Go packages compiled into the binary. There's no plugin
loading.

## Where things are

| Path | What it is |
|---|---|
| `internal/providers/types.go` | `Provider`, the optional interfaces, `SelectionOption`, `PlaybackConfig`, `StreamPlaybackHint`, `SubtitleTrack` |
| `internal/providers/registry.go` | `Register`, `Meta`, name lookup, `New`, `SetFactoryForTest` |
| `internal/providers/lang.go` | `NormalizeTranslationType`, `AlternateTranslationType` |
| `internal/providers/subtitles.go` | Language names and codes, `PickSubtitle` |
| `internal/providers/substyle/` | Soft vs. hard subtitle choice for hosts that offer both |
| `internal/providerhost/host.go` | What a provider may use from the app: HTTP client, logging, prompts |
| `internal/loadproviders/load.go` | Blank imports that pull every built-in provider into the binary |
| `internal/provider_bridge.go` | Fills in the `providerhost` hooks and adapts providers to the app |
| `internal/provider_disabled.go` | Which providers are switched off, and the default stack order |
| `internal/provider.go` | The provider stack: search, retries, `name::id` keys |

Current providers: `anikoto`, `anizone`, `kickassanime`, `nyaa` (which also
registers `sukebei`).

A provider package imports `providers` and `providerhost` and never
`internal` itself; `internal` imports the providers, so going the other way
is an import cycle.

## The interface

```go
type Provider interface {
	Name() string
	SearchAnime(query, mode string) ([]SelectionOption, error)
	EpisodesList(showID, mode string) ([]string, error)
	GetEpisodeURL(config PlaybackConfig, id string, epNo int) ([]string, error)
}
```

- **`Name`** returns the canonical name, the same one passed to `Register`.
- **`SearchAnime`** returns the host's matches for `query`. `mode` is `"sub"`
  or `"dub"`; run it through `providers.NormalizeTranslationType` if you use
  it, since anything other than `"dub"` means sub.
- **`EpisodesList`** returns the episode numbers the show has in `mode`, as
  strings (`"1"`, `"2"`, ...). If the host lists sub and dub separately, only
  return the ones for `mode`, and return an error when there are none. A list
  for audio the show doesn't have is worse than no list.
- **`GetEpisodeURL`** returns stream URLs for episode `epNo`, best first.
  `epNo` is the tracker's numbering, starting at 1. `id` is the `Key` from
  your own search result. `config.SubOrDub` is the audio wanted.

### SelectionOption

```go
type SelectionOption struct {
	Key, Label string // host's id for the show; the line shown in menus
	Title      string // plain title, used to match against the tracker
	Thumbnail  string // cover image URL, optional
	ExtraData  any    // anything else that helps match, optional
}
```

Return the host's raw id in `Key`. When several providers are searched at
once the app stores it as `provider::id` (for example `anikoto::one-piece-odmau`) and
strips the prefix again before calling you.

Put format, year or season in `Label` when the host has several entries with
near-identical names; people pick from that line. Keep `Title` plain.

`ExtraData` is only read when it is a `providers.SearchHit`. Its `MalID` ties a
result to a list entry (`malIDFromProviderExtraData` in
`internal/provider_mapping.go`), and its `Episodes` count is shown in the menu.
Attach one when your host's search gives either.

### PlaybackConfig

```go
type PlaybackConfig struct {
	SubOrDub string // "sub" or "dub"
	SubStyle string // "ask", "soft" or "hard": how subtitles reach the player in sub mode
}
```

Only `SubStyle` matters to hosts that serve both hardsubbed and softsubbed
streams. `substyle.Choose(hasSoft, hasHard, config.SubStyle)` makes the
decision, asking the user once per run when the setting is `ask`. No current
provider needs it.

## Optional interfaces

The app checks for these with a type assertion; there's nothing to register.

```go
type ModeResolver interface {
	GetEpisodeURLForMode(config PlaybackConfig, id string, epNo int, mode string) ([]string, error)
}

type HintResolver interface {
	GetEpisodeURLForModeWithHints(config PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]StreamPlaybackHint, error)
}

type SkipRanger interface {
	SkipRange(id, mode string, epNo int) (intro, outro []int, err error)
}
```

**`ModeResolver`**: the audio fallback asks for the other language explicitly.
Without this the app copies the config, sets `SubOrDub` to the mode it wants
and calls `GetEpisodeURL`, which is fine for most hosts.

**`HintResolver`**: when the player needs more than a URL. The map is keyed by
URL:

```go
type StreamPlaybackHint struct {
	Referrer, Subtitle string            // Referer to send; default subtitle URL
	Headers            map[string]string // extra headers, passed to mpv as-is
	Subtitles          []SubtitleTrack   // every track the host offers, if more than one
}
```

Use `Headers` when a CDN wants more than a referrer (`Origin`, a browser
`User-Agent`). With several tracks in `Subtitles` the app picks one by the
user's subtitle language; `PickSubtitle` and `TrackLanguage` in
`subtitles.go` do the same matching if you need it inside the provider.

**`SkipRanger`**: when the stream response already carries intro and outro
times. Return `[start, end]` in seconds; return `nil, nil, nil` when you don't
know and the next skip source is asked. `anikoto` does this.

Implementing `HintResolver` usually means implementing all three `GetEpisode*`
methods by calling one function, as `anikoto` does.

## Errors

- Wrap `providerhost.ErrRateLimited` when the host throttles you (some send
  200 with an error body; `providerhost.IsRateLimitedBody` checks for that).
  The user is told to try again instead of being told the show doesn't exist.
- Network failures (timeouts, resets, EOF, DNS) are retried and, after two in
  a row, the provider is skipped for five minutes. Don't swallow them into an
  empty result.
- An empty result with no error, or a plain "nothing found" error, is a
  definitive answer and is not retried.
- `providerhost.HTTPStatusOK` and `providerhost.HTTPStatusError(context,
  status, body)` give the same error shape every provider uses.
- Prefix your messages with the provider name: `anikoto: no dub for one-piece-odmau
  episode 3`.

## Adding one

### 1. Package

```
internal/providers/yoursite/
  register.go
  provider.go
  api.go
  provider_test.go
```

Look at `anikoto` for HTML scraping that resolves through an embed page (and
falls back across mirror domains), `kickassanime` for a JSON API, and `nyaa`
for something that isn't an HTTP stream at all.

### 2. Register it

```go
package yoursite

import "github.com/thexykril/otakase/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "yoursite",
		Aliases:  []string{"yoursite.to", "ys"},
		Referrer: "https://yoursite.to/",
	}, func() providers.Provider { return &Provider{} })
}
```

| `Meta` field | Meaning |
|---|---|
| `Name` | Canonical name. Lower-cased, with spaces and dashes removed. Written to the config and watch history, so don't rename it later. |
| `Aliases` | Other spellings the config and menus accept. Matching ignores case, spaces, `-` and `_`. |
| `Referrer` | Referer sent with this provider's streams, for playback and downloads, when no hint overrides it. |
| `DefaultDisabled` | Keeps the provider out of every stack. There's no config switch that turns it back on, so this is for hosts that are down. |
| `DisableReason` | Why it's off: shown by `-provider-status` and in the error when something asks for it. |
| `Adult` | 18+ only. Off unless `AdultContent=true`. |

`Register` panics on an empty name or nil factory, so a mistake shows up the
moment the binary starts. The factory is called for every `providers.New`, so
keep it cheap; create clients lazily, as `anikoto`'s `api()` does.

### 3. Implement it

```go
type Provider struct {
	client *client
}

func (p *Provider) Name() string { return "yoursite" }

func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error)       { ... }
func (p *Provider) EpisodesList(showID, mode string) ([]string, error)                        { ... }
func (p *Provider) GetEpisodeURL(cfg providers.PlaybackConfig, id string, epNo int) ([]string, error) { ... }
```

For HTTP, use `providerhost.HTTPClient()`. It's the app's shared client, so
your requests share its cookie jar and 25 second timeout.
Allow the client to be swapped in tests:

```go
func newClient() *client {
	return &client{baseURL: "https://yoursite.to/api", http: providerhost.HTTPClient()}
}
```

Other hooks:

| Hook | Use |
|---|---|
| `providerhost.Log(msg)` | Line in `otakase-debug.log`. Log what you tried and why it failed. |
| `providerhost.Out(msg)` | Message the user sees. Rarely needed. |
| `providerhost.PromptSelect(options)` | Ask the user to pick from a list, in whatever menu they use. |
| `providerhost.CurrentSubStyle()`, `PersistSubStylePreference(style)` | Read and save `SubStyle`; `substyle` uses these. |

The hooks are nil until `internal` is loaded, which matters in unit tests:
check for nil, or set them in the test.

### 4. Add it to the build

Add a blank import to `internal/loadproviders/load.go`:

```go
_ "github.com/thexykril/otakase/internal/providers/yoursite"
```

### 5. Place it in the default stack

With `Provider=stacked` (the default) providers are tried in the order of
`preferredProviderOrder` in `internal/provider_disabled.go`. Anything not in
that list goes after it, sorted by name. Add your name where it belongs: by
how reliably it resolves streams and how cheaply.

### 6. Test it

- Unit tests run against `httptest.NewServer` serving the host's real response
  shapes; see `anikoto/provider_test.go`. Keep captured pages under
  `testdata/`. No unit test may reach the real host.
- Live tests go in `live_test.go`, skipped unless an environment variable is
  set (`OTAKASE_LIVE_ANIZONE=1` and so on). Run them by hand before sending
  the change. The weekly canary workflow only runs `-provider-status` and a
  couple of stack-level live tests in `internal/`.
- To stub a provider in app-level tests, use
  `providers.SetFactoryForTest(name, factory)` and defer the restore it
  returns.

Then check it against the real site:

```bash
go test ./internal/providers/yoursite/
go run ./cmd/otakase -provider-status
```

`-provider-status` searches every registered provider (enabled or not) and
prints result counts and latency.

## Configuration

What users can set in `otakase.conf`:

```ini
# The default: every enabled provider, in the default order.
Provider=stacked
# Only this one.
Provider=["yoursite"]
# Never use these.
DisabledProviders=["anizone","yoursite"]
```

A provider is off if it's in `DisabledProviders`, has `DefaultDisabled`, or is
`Adult` while `AdultContent` is false. Off providers are dropped from
`Provider` too; if nothing usable is left the app falls back to the first
enabled one.

## Needing more from the app

Keep provider names out of `internal/`. When a host needs something new:

1. **It fits an optional interface.** Implement it.
2. **It's a fact about the provider** (a flag, a capability). Add a field to
   `Meta` and read it through `providers.MetaFor(name)`.
3. **It's an app service** (a prompt, a setting, a shared cache). Add a hook
   to `providerhost`, fill it in from `init` in `provider_bridge.go`, and
   tolerate it being nil.
4. **It's a per-play setting** every provider could use. Add it to
   `PlaybackConfig` and set it in `toPlaybackConfig`.
5. **It's a new kind of answer** (like skip times were). Add an optional
   interface in `types.go` and have `providerAdapter` pass it through.

## Checklist

- [ ] Package under `internal/providers/<name>/`, registered in `init`
- [ ] Blank import in `internal/loadproviders/load.go`
- [ ] Placed in `preferredProviderOrder`
- [ ] Imports only `providers`, `providerhost` and its own helpers
- [ ] `EpisodesList` only lists episodes in the requested audio
- [ ] Episode numbers start at 1 and match the tracker
- [ ] Streams play in mpv with only the `Referrer` / hints you return
- [ ] Throttling returns `ErrRateLimited`; network errors aren't hidden
- [ ] Unit tests with no network; live tests behind an env var
- [ ] `-provider-status` shows results
