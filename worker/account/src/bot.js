// The Discord bot side.
//
// The app no longer links to the bot itself: an account with Discord
// attached is the link. Discord features the app uses (sharing a score,
// support posts, source reports, watch parties) go through this Worker,
// which calls the bot with a shared secret and the member's Discord id.
// Support posts and source reports wait in the outbox while the bot is
// offline. News is kept here too, so the app shows it with the bot down.
//
// The bot calls /bot/* with the same secret to find a member's account and
// to drop items into their inbox.

import { authorize } from './sessions.js';
import { HttpError, json, readJSON, safeEqual, now } from './util.js';

/** App calls passed to the bot: [method, path pattern, queue when offline]. */
const relayed = [
  ['POST', /^\/v1\/rating$/, false],
  ['POST', /^\/v1\/report$/, true],
  ['POST', /^\/v1\/provider-report$/, true],
  ['GET', /^\/v1\/parties$/, false],
  ['POST', /^\/v1\/party\/[A-Za-z0-9_-]{1,64}\/rsvp$/, false],
  ['POST', /^\/v1\/party\/start$/, false],
  ['GET', /^\/v1\/party\/[A-Za-z0-9_-]{1,64}\/ws$/, false],
  ['GET', /^\/v1\/club$/, false],
];

export function relayRoute(method, path) {
  return relayed.find(([m, p]) => m === method && p.test(path)) || null;
}

const maxRelayBody = 10 * 1024 * 1024;
const maxQueuedBody = 768 * 1024;

function b64(bytes) {
  let s = '';
  const view = new Uint8Array(bytes);
  for (let i = 0; i < view.length; i += 0x8000) s += String.fromCharCode(...view.subarray(i, i + 0x8000));
  return btoa(s);
}

const newsTTL = 10 * 60 * 1000;
const newsSaveEvery = 60 * 60 * 1000;

function botHeaders(env, discord, device) {
  return {
    'x-otakase-account': env.BOT_SECRET,
    'x-discord-user': discord,
    'x-otakase-device': device,
  };
}

/** /v1/... from the app: passed to the bot as the member. */
export async function relay(request, env, path, route) {
  const auth = await authorize(request, env);
  if (!auth.discord) throw new HttpError(403, 'Add Discord to your Otakase account first (Accounts → Otakase account).');
  if (!env.BOT_URL || !env.BOT_SECRET) throw new HttpError(503, 'The Discord bot is not set up.');
  const url = new URL(request.url);
  const target = `${env.BOT_URL}${path}${url.search}`;
  const headers = new Headers(botHeaders(env, auth.discord, auth.device));
  const type = request.headers.get('content-type');
  if (type) headers.set('content-type', type);
  if (request.headers.get('upgrade') === 'websocket') {
    headers.set('upgrade', 'websocket');
    return fetch(target, { headers });
  }
  // Support posts can carry a screenshot, so bodies stay bytes.
  const body = request.method === 'GET' ? undefined : await request.arrayBuffer();
  if (body && body.byteLength > maxRelayBody) throw new HttpError(413, 'too large');
  try {
    const r = await fetch(target, { method: request.method, headers, body, signal: AbortSignal.timeout(15000) });
    if (r.status < 500 || !route[2]) return new Response(r.body, { status: r.status, headers: { 'content-type': r.headers.get('content-type') || 'application/json', 'cache-control': 'no-store' } });
  } catch {
    if (!route[2]) throw new HttpError(503, 'The Discord bot is offline. Try again later.');
  }
  if (body && body.byteLength > maxQueuedBody) throw new HttpError(503, 'The Discord bot is offline. Try again later.');
  await env.DB.prepare('INSERT INTO outbox (method, path, discord, device, content_type, body, created) VALUES (?, ?, ?, ?, ?, ?, ?)')
    .bind(request.method, path, auth.discord, auth.device, type || '', body ? b64(body) : '', now()).run();
  return json({ queued: true }, 202);
}

/** GET /v1/news: the bot's news, cached, and kept for when it is offline. */
export async function news(request, env, ctx) {
  const cache = caches.default;
  const key = new Request('https://account.otakase.com/v1/news');
  const hit = await cache.match(key);
  if (hit) return hit;
  let body = null;
  if (env.BOT_URL) {
    try {
      const r = await fetch(`${env.BOT_URL}/v1/news`, { signal: AbortSignal.timeout(8000) });
      if (r.ok) body = await r.text();
    } catch { /* offline: use the saved copy */ }
  }
  const saved = await env.DB.prepare('SELECT body, updated FROM news WHERE id = 1').first();
  if (body !== null) {
    if (!saved || (saved.body !== body && now() - saved.updated > newsSaveEvery)) {
      await env.DB.prepare('INSERT INTO news (id, body, updated) VALUES (1, ?, ?) ON CONFLICT (id) DO UPDATE SET body = excluded.body, updated = excluded.updated')
        .bind(body, now()).run();
    }
  } else {
    body = saved ? saved.body : '{"items":[]}';
  }
  const response = new Response(body, { headers: { 'content-type': 'application/json', 'cache-control': `public, max-age=${newsTTL / 1000}` } });
  ctx.waitUntil(cache.put(key, response.clone()));
  return response;
}

// ---- Called by the bot -----------------------------------------------------

export function requireBot(request, env) {
  const header = request.headers.get('authorization') || '';
  if (!env.BOT_SECRET || !safeEqual(header, `Bearer ${env.BOT_SECRET}`)) throw new HttpError(401, 'unauthorized');
}

async function accountByDiscord(env, discord) {
  if (!/^\d{5,24}$/.test(discord || '')) throw new HttpError(400, 'bad discord id');
  const row = await env.DB.prepare(
    "SELECT a.id, a.name, a.role FROM identities i JOIN accounts a ON a.id = i.account_id WHERE i.provider = 'discord' AND i.provider_user_id = ?",
  ).bind(discord).first();
  return row || null;
}

export async function botRoutes(request, env, path) {
  requireBot(request, env);
  const url = new URL(request.url);
  if (path === '/bot/account' && request.method === 'GET') {
    const account = await accountByDiscord(env, url.searchParams.get('discord'));
    if (!account) return json({ account: null });
    const devices = await env.DB.prepare('SELECT id, name, os, is_tv, app_version, last_seen FROM devices WHERE account_id = ? ORDER BY last_seen DESC')
      .bind(account.id).all();
    return json({ account: { id: account.id, name: account.name, role: account.role }, devices: devices.results });
  }
  if (path === '/bot/inbox' && request.method === 'POST') {
    const body = await readJSON(request, 8192);
    const account = await accountByDiscord(env, body.discord);
    if (!account) return json({ delivered: 0, account: false });
    const hub = env.HUB.get(env.HUB.idFromName(account.id));
    const r = await hub.fetch('https://hub/inbox', { method: 'POST', headers: { 'x-account': account.id }, body: JSON.stringify({ item: body.item }) });
    return json({ ...(await r.json()), account: true }, r.status);
  }
  if (path === '/bot/outbox' && request.method === 'GET') {
    const rows = await env.DB.prepare('SELECT id, method, path, discord, device, content_type, body, created FROM outbox ORDER BY id LIMIT 20').all();
    return json({ items: rows.results });
  }
  if (path === '/bot/outbox' && request.method === 'DELETE') {
    const ids = (url.searchParams.get('ids') || '').split(',').map(Number).filter((n) => Number.isInteger(n) && n > 0).slice(0, 50);
    if (ids.length) await env.DB.prepare(`DELETE FROM outbox WHERE id IN (${ids.map(() => '?').join(',')})`).bind(...ids).run();
    return json({ ok: true, removed: ids.length });
  }
  if (path === '/bot/role' && request.method === 'POST') {
    // The bot keeps the Tester role and the account role in step.
    const body = await readJSON(request);
    if (!['user', 'tester'].includes(body.role)) throw new HttpError(400, 'bad role');
    const account = await accountByDiscord(env, body.discord);
    if (!account) return json({ ok: false, account: false });
    if (account.role === 'dev' || account.role === 'admin') return json({ ok: true, role: account.role });
    await env.DB.prepare('UPDATE accounts SET role = ? WHERE id = ?').bind(body.role, account.id).run();
    return json({ ok: true, role: body.role });
  }
  if (path === '/bot/stats' && request.method === 'GET') {
    const [accounts, devices, week] = await env.DB.batch([
      env.DB.prepare('SELECT COUNT(*) AS n, COALESCE(SUM(bytes), 0) AS bytes FROM accounts'),
      env.DB.prepare('SELECT COUNT(*) AS n FROM devices'),
      env.DB.prepare('SELECT COUNT(*) AS n FROM devices WHERE last_seen > ?').bind(now() - 7 * 24 * 60 * 60 * 1000),
    ]);
    return json({
      accounts: accounts.results[0].n,
      bytes: accounts.results[0].bytes,
      devices: devices.results[0].n,
      active_devices_7d: week.results[0].n,
    });
  }
  throw new HttpError(404, 'not found');
}

/** The tables a backup holds, in restore order. */
export const backupTables = ['accounts', 'identities', 'devices', 'sync_docs', 'vault', 'vault_keys'];

/**
 * GET /backup?table=&after=: one page of a table for the weekly backup at
 * home. Sessions and pairings are left out: they expire anyway, and a
 * restore signs every device in again.
 */
export async function backup(request, env) {
  const header = request.headers.get('authorization') || '';
  if (!env.BACKUP_TOKEN || !safeEqual(header, `Bearer ${env.BACKUP_TOKEN}`)) throw new HttpError(401, 'unauthorized');
  const url = new URL(request.url);
  const table = url.searchParams.get('table');
  if (!backupTables.includes(table)) return json({ tables: backupTables });
  const after = Number(url.searchParams.get('after') || 0);
  const rows = await env.DB.prepare(`SELECT rowid AS _row, * FROM ${table} WHERE rowid > ? ORDER BY rowid LIMIT 500`).bind(after).all();
  const last = rows.results.length ? rows.results[rows.results.length - 1]._row : null;
  return json({ table, rows: rows.results.map(({ _row, ...r }) => r), next: rows.results.length === 500 ? last : null, at: now() });
}
