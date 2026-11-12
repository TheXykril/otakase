// otakase-account: Otakase accounts on Cloudflare's free plan.
//
// Served at account.otakase.com, and at otakase.xyverion.com/account for
// networks that still block the new domain.

import { AccountHub } from './hub.js';
import { loginPage, loginStart, callback, providers } from './login.js';
import { authorize, requireRole, token, pairStart, pairApprove, pairPoll, removeDevice, roles } from './sessions.js';
import { pull } from './sync.js';
import { getVault, putKeys, putCheck, restoreKey, putBlob, deleteBlob } from './vault.js';
import { relay, relayRoute, news, botRoutes, backup } from './bot.js';
import { sign } from './jwt.js';
import { HttpError, json, errorResponse, readJSON, clip } from './util.js';
import { page } from './pages.js';

export { AccountHub };

const prefix = '/account';

function hubFor(env, account) {
  return env.HUB.get(env.HUB.idFromName(account));
}

/** Account, logins and devices for the app. */
async function me(env, auth) {
  const [account, identities, devices] = await env.DB.batch([
    env.DB.prepare('SELECT id, name, avatar, role, created, bytes FROM accounts WHERE id = ?').bind(auth.account),
    env.DB.prepare('SELECT provider, name FROM identities WHERE account_id = ?').bind(auth.account),
    env.DB.prepare('SELECT id, name, os, is_tv, app_version, created, last_seen FROM devices WHERE account_id = ? ORDER BY last_seen DESC').bind(auth.account),
  ]);
  const a = account.results[0];
  if (!a) throw new HttpError(401, 'sign in again');
  return { account: a, identities: identities.results, devices: devices.results, device: auth.device };
}

async function deleteAccount(env, auth) {
  const id = auth.account;
  await env.DB.batch([
    'DELETE FROM sync_docs WHERE account_id = ?',
    'DELETE FROM vault WHERE account_id = ?',
    'DELETE FROM vault_keys WHERE account_id = ?',
    'DELETE FROM sessions WHERE account_id = ?',
    'DELETE FROM devices WHERE account_id = ?',
    'DELETE FROM identities WHERE account_id = ?',
    'DELETE FROM pairings WHERE account_id = ?',
    'DELETE FROM accounts WHERE id = ?',
  ].map((sql) => env.DB.prepare(sql).bind(id)));
  await hubFor(env, id).fetch('https://hub/kick', { method: 'POST', headers: { 'x-account': id }, body: JSON.stringify({ all: true }) });
}

async function exportAccount(env, auth) {
  const data = await me(env, auth);
  const [docs, vault] = await env.DB.batch([
    env.DB.prepare('SELECT profile_id, kind, body, updated FROM sync_docs WHERE account_id = ?').bind(auth.account),
    env.DB.prepare('SELECT profile_id, version, updated FROM vault WHERE account_id = ?').bind(auth.account),
  ]);
  return {
    exported: new Date().toISOString(),
    ...data,
    synced: docs.results.map((d) => ({ profile: d.profile_id, kind: d.kind, data: JSON.parse(d.body), updated: d.updated })),
    // Sealed on your devices; only they can open it.
    vault: vault.results.map((v) => ({ profile: v.profile_id, version: v.version, updated: v.updated, sealed: true })),
  };
}

async function admin(request, env, auth, path) {
  requireRole(auth, 'admin');
  // The role in a token can be an hour old: check it again for admin calls.
  const fresh = await env.DB.prepare('SELECT role FROM accounts WHERE id = ?').bind(auth.account).first('role');
  if (fresh !== 'admin') throw new HttpError(403, 'not allowed');
  if (path === '/admin/accounts' && request.method === 'GET') {
    const rows = await env.DB.prepare(
      'SELECT a.id, a.name, a.role, a.banned, a.created, a.bytes, (SELECT COUNT(*) FROM devices d WHERE d.account_id = a.id) AS devices FROM accounts a ORDER BY a.created DESC LIMIT 200',
    ).all();
    return json({ accounts: rows.results });
  }
  if (path === '/admin/role' && request.method === 'POST') {
    const body = await readJSON(request);
    if (!roles.includes(body.role) || typeof body.account !== 'string') throw new HttpError(400, 'bad request');
    if (body.account === auth.account && body.role !== 'admin') throw new HttpError(400, 'keep your own admin role');
    const r = await env.DB.prepare('UPDATE accounts SET role = ? WHERE id = ?').bind(body.role, body.account).run();
    return json({ ok: r.meta.changes > 0 });
  }
  if (path === '/admin/ban' && request.method === 'POST') {
    const body = await readJSON(request);
    if (typeof body.account !== 'string' || body.account === auth.account) throw new HttpError(400, 'bad request');
    const banned = body.banned ? 1 : 0;
    const statements = [env.DB.prepare('UPDATE accounts SET banned = ? WHERE id = ?').bind(banned, body.account)];
    if (banned) statements.push(env.DB.prepare('DELETE FROM sessions WHERE account_id = ?').bind(body.account));
    await env.DB.batch(statements);
    return json({ ok: true });
  }
  throw new HttpError(404, 'not found');
}

async function route(request, env, ctx, url, path, base) {
  const method = request.method;
  // Sign-in pages and token exchange.
  if (path === '/' && method === 'GET') return Response.redirect('https://otakase.com/', 302);
  if (path === '/login' && method === 'GET') return loginPage(url, env, base);
  if (path === '/login/start' && method === 'POST') return loginStart(request, env, base);
  const cb = path.match(/^\/callback\/([a-z]+)$/);
  if (cb && method === 'GET') return callback(url, env, base, cb[1]);
  if (path === '/token' && method === 'POST') return token(request, env);
  if (path === '/pair' && method === 'GET') {
    const code = url.searchParams.get('code') || '';
    if (!/^\d{6}$/.test(code)) return page(env, 'Add TV', '<h1>Scan the code on the TV</h1><p>Open Accounts on the TV and scan its QR code with your phone.</p>', 400);
    return Response.redirect(`${base}/login?return=pair&code=${code}`, 302);
  }
  if (path === '/pair/start' && method === 'POST') return pairStart(request, env, base);
  if (path === '/pair/approve' && method === 'POST') return pairApprove(request, env);
  if (path === '/pair/poll' && method === 'POST') return pairPoll(request, env);
  if (path === '/backup' && method === 'GET') return backup(request, env);
  if (path.startsWith('/bot/')) return botRoutes(request, env, path);
  if (path === '/v1/news' && method === 'GET') return news(request, env, ctx);
  const relayed = relayRoute(method, path);
  if (relayed) return relay(request, env, path, relayed);

  const auth = await authorize(request, env);
  if (path === '/me' && method === 'GET') return json(await me(env, auth));
  if (path === '/me' && method === 'DELETE') {
    await deleteAccount(env, auth);
    return json({ ok: true });
  }
  if (path === '/me/export' && method === 'GET') {
    return json(await exportAccount(env, auth), 200, { 'content-disposition': 'attachment; filename="otakase-account.json"' });
  }
  if (path === '/logout' && method === 'POST') {
    // Signing out keeps the vault key on the device, so the key stays.
    await removeDevice(env, auth.account, auth.device, { replaceKey: false });
    return json({ ok: true });
  }
  if (path === '/attach' && method === 'POST') {
    // A short ticket so the browser can add Discord or Google to this account.
    const body = await readJSON(request);
    if (!providers[body.provider]) throw new HttpError(400, 'bad provider');
    const ticket = await sign(env.JWT_SECRET, 'attach', { sub: auth.account }, 600);
    return json({ url: `${base}/login?return=attach&provider=${body.provider}&attach=${encodeURIComponent(ticket)}` });
  }
  if (path === '/devices' && method === 'GET') return json({ devices: (await me(env, auth)).devices, online: await hubFor(env, auth.account).fetch('https://hub/online', { headers: { 'x-account': auth.account } }).then((r) => r.json()).then((d) => d.online) });
  const dev = path.match(/^\/devices\/([A-Za-z0-9-]{1,64})$/);
  if (dev && method === 'PATCH') {
    const body = await readJSON(request);
    const name = clip(body.name, 48);
    if (!name) throw new HttpError(400, 'name missing');
    const r = await env.DB.prepare('UPDATE devices SET name = ? WHERE id = ? AND account_id = ?').bind(name, dev[1], auth.account).run();
    if (!r.meta.changes) throw new HttpError(404, 'no such device');
    await hubFor(env, auth.account).fetch('https://hub/notify', { method: 'POST', headers: { 'x-account': auth.account }, body: JSON.stringify({ t: 'devices' }) });
    return json({ ok: true });
  }
  if (dev && method === 'DELETE') {
    if (!(await removeDevice(env, auth.account, dev[1]))) throw new HttpError(404, 'no such device');
    return json({ ok: true });
  }
  if (path === '/sync' && method === 'GET') {
    const since = Number(url.searchParams.get('since') || 0);
    return json(await pull(env, auth.account, Number.isInteger(since) && since >= 0 ? since : 0));
  }
  if (path === '/sync' && method === 'POST') {
    const body = await request.text();
    if (body.length > 1024 * 1024) throw new HttpError(413, 'too large');
    let docs;
    try { docs = JSON.parse(body).docs; } catch { throw new HttpError(400, 'bad request'); }
    return hubFor(env, auth.account).fetch('https://hub/sync', {
      method: 'POST', headers: { 'x-account': auth.account }, body: JSON.stringify({ device: auth.device, docs }),
    });
  }
  if (path === '/vault' && method === 'GET') return getVault(env, auth);
  if (path === '/vault/keys' && method === 'PUT') return putKeys(request, env, auth);
  if (path === '/vault/check' && method === 'PUT') return putCheck(request, env, auth);
  if (path === '/vault/restore' && method === 'POST') return restoreKey(request, env, auth);
  if (path === '/vault/blob' && method === 'PUT') return putBlob(request, env, auth);
  if (path === '/vault/blob' && method === 'DELETE') return deleteBlob(url, env, auth);
  if (path === '/live' && method === 'GET') {
    if (request.headers.get('upgrade') !== 'websocket') throw new HttpError(426, 'websocket only');
    const device = await env.DB.prepare('SELECT name, is_tv, os FROM devices WHERE id = ? AND account_id = ?').bind(auth.device, auth.account).first();
    if (!device) throw new HttpError(401, 'sign in again');
    const headers = new Headers(request.headers);
    headers.set('x-account', auth.account);
    headers.set('x-device', auth.device);
    headers.set('x-name', device.name);
    headers.set('x-tv', device.is_tv ? '1' : '0');
    headers.set('x-os', device.os);
    headers.set('x-discord', auth.discord);
    // The phone remote finds TVs and computers by their remote id.
    const remote = url.searchParams.get('remote') || '';
    headers.set('x-remote', /^[A-Za-z0-9_-]{1,64}$/.test(remote) ? remote : '');
    headers.set('x-host', url.searchParams.get('host') === '1' ? '1' : '0');
    return hubFor(env, auth.account).fetch(new Request('https://hub/live', { headers }));
  }
  if (path.startsWith('/admin/')) return admin(request, env, auth, path);
  throw new HttpError(404, 'not found');
}

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    let path = url.pathname;
    let base = url.origin;
    // otakase.xyverion.com/account/* reaches the same Worker.
    if (path === prefix || path.startsWith(`${prefix}/`)) {
      path = path.slice(prefix.length) || '/';
      base = `${url.origin}${prefix}`;
    }
    // WebSockets cannot send headers from every client: accept the access
    // token as ?access= on /live too.
    if (path === '/live' && !request.headers.get('authorization') && url.searchParams.get('access')) {
      const headers = new Headers(request.headers);
      headers.set('authorization', `Bearer ${url.searchParams.get('access')}`);
      request = new Request(request, { headers });
    }
    try {
      return await route(request, env, ctx, url, path, base);
    } catch (e) {
      if (e instanceof HttpError) return errorResponse(e.status, e.message, e.extra);
      console.error('account worker error', e && e.message);
      return errorResponse(500, 'Something went wrong. Try again.');
    }
  },
};

