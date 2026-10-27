// Edge cache for the watch room's video segments, a Cloudflare Worker.
// Setup: wiki/Developer-Discord-Bot.md, "Watch room edge cache".
//
// It passes on only watch room segments (/activity/api/watch/media/<key>/<n>.ts)
// from the bot and keeps each one in Cloudflare's cache for six hours, so
// the bot sends it once per Cloudflare location however many are watching.
// Anything else gets a 404: the Worker is no open proxy.
//
// On the Workers Free plan nothing can be billed. Past 100,000 requests a
// day the Worker answers with an error until midnight UTC, and the room's
// players fall back to the bot.

// The bot's public address, the same one the Activity's root URL mapping
// points at.
const ORIGIN = "https://bot.xyverion.com";

const SEGMENT = /^\/activity\/api\/watch\/media\/[0-9a-f]{32}\/[0-9]+\.ts$/;
const KEEP = 6 * 60 * 60; // seconds

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    if (request.method === "OPTIONS") {
      return new Response(null, { status: 204, headers: { "Access-Control-Allow-Origin": "*", "Access-Control-Allow-Headers": "*" } });
    }
    if (request.method !== "GET" || !SEGMENT.test(url.pathname)) {
      return new Response("not found", { status: 404 });
    }
    const cache = caches.default;
    const key = new Request(url.origin + url.pathname);
    const hit = await cache.match(key);
    if (hit) return hit;

    const up = await fetch(ORIGIN + url.pathname);
    if (up.status !== 200) {
      // Errors aren't kept: the next request asks the bot again.
      return new Response(up.body, { status: up.status, headers: { "Access-Control-Allow-Origin": "*", "Cache-Control": "no-store" } });
    }
    const res = new Response(up.body, up);
    res.headers.set("Cache-Control", "public, max-age=" + KEEP);
    res.headers.set("Access-Control-Allow-Origin", "*");
    res.headers.delete("Set-Cookie");
    ctx.waitUntil(cache.put(key, res.clone()));
    return res;
  },
};
