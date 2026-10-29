# Provider replay fixtures

Recorded on 2026-10-27 for Frieren, episode 1, sub:

- anikototv.to search, watch-page id, episode list, server list and server lookup.
- megaplay.buzz embed id and getSources response reached through anikoto.
- megavid.buzz /mal/52991/1/sub/source?noax=1.

Watch/embed pages are reduced to the fields used by the parser. Playback and
subtitle URLs are replaced with loopback URLs. The megaplay `enc` field is
re-encrypted with a fixed loopback URL using the same provider algorithm.
Unused timing/server/image fields and upstream tokens are removed. No stream
media is recorded. The tests never contact a live provider.

AniList search and episode JSON are reduced regression fixtures from
internal/providers/megavid/provider_test.go. Live AniList requests returned
HTTP 403 in the build environment, so these two are not fresh recordings.
