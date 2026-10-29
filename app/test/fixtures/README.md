`ani_zip_1.json` records the mappings endpoint for AniList ID 1 on 2026-10-28.
Only IDs and episode fields used by the app are retained, for episodes 1–3 and S1.
The map key is the anime episode number. Provider `episodeNumber` may differ.
Tests read this file and mock all HTTP requests. No artwork is downloaded.
# Watch rooms

`watch_room.json` is a synthetic response using the bot's `watchView` JSON shape.
It includes clock, host, subtitle and tracker fields without external media or tokens.
