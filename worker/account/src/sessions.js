// Tokens, devices and QR pairing.
//
// Access tokens are signed JWTs that last an hour and carry account, device
// and role. Refresh tokens are random, kept hashed and replaced on every
// use. A device's last_seen moves only when it refreshes, so being signed in
// costs about one database write an hour per device.

import { sign, verify } from './jwt.js';
import { HttpError, json, readJSON, clip, now, randomToken, sha256, b64url, enc, RateLimit, clientIP } from './util.js';

export const accessTTL = 60 * 60;
export const refreshTTL = 90 * 24 * 60 * 60 * 1000;
export const pairTTL = 2 * 60 * 1000;
export const maxDevices = 10;
export const roles = ['user', 'tester', 'dev', 'admin'];

const pairLimit = new RateLimit(10, 10 * 60 * 1000);
const pollLimit = new RateLimit(120, 5 * 60 * 1000);
const approveLimit = new RateLimit(20, 10 * 60 * 1000);

/** The access token's claims, or a 401. */
export async function authorize(request, env) {
  const header = request.headers.get('authorization') || '';
  const token = header.startsWith('Bearer ') ? header.slice(7).trim() : '';
  const claims = await verify(env.JWT_SECRET, 'access', token);
  if (!claims) throw new HttpError(401, 'sign in again');
  return { account: claims.sub, device: claims.dev, role: claims.role, discord: claims.dc || '' };
}

export function requireRole(auth, ...allowed) {
  if (!allowed.includes(auth.role)) throw new HttpError(403, 'not allowed');
}

/** Checks a device description sent by the app. */
export function cleanDevice(d) {
  if (!d || typeof d !== 'object') throw new HttpError(400, 'device missing');
  const name = clip(d.name, 48);
  if (!name) throw new HttpError(400, 'device name missing');
  const publicKey = typeof d.public_key === 'string' && /^[A-Za-z0-9_-]{43}$/.test(d.public_key) ? d.public_key : '';
  return {
    name,
    os: clip(d.os, 24),
    is_tv: d.is_tv ? 1 : 0,
    public_key: publicKey,
    app_version: clip(d.app_version, 32),
  };
}

async function pkce(verifier) {
  if (typeof verifier !== 'string' || !/^[A-Za-z0-9._~-]{43,128}$/.test(verifier)) return '';
  return b64url(await crypto.subtle.digest('SHA-256', enc.encode(verifier)));
}

async function accountRow(env, id) {
  return env.DB.prepare('SELECT id, name, avatar, role, banned FROM accounts WHERE id = ?').bind(id).first();
}

async function discordId(env, account) {
  const row = await env.DB.prepare("SELECT provider_user_id FROM identities WHERE account_id = ? AND provider = 'discord'")
    .bind(account).first();
  return row ? row.provider_user_id : '';
}

async function accessToken(env, account, device) {
  return sign(env.JWT_SECRET, 'access', {
    sub: account.id,
    dev: device,
    role: roles.includes(account.role) ? account.role : 'user',
    dc: await discordId(env, account.id),
  }, accessTTL);
}

/** Adds a device to an account and gives it its first tokens. */
export async function newDevice(env, accountId, device) {
  const account = await accountRow(env, accountId);
  if (!account) throw new HttpError(401, 'sign in again');
  if (account.banned) throw new HttpError(403, 'This account is blocked.');
  const count = await env.DB.prepare('SELECT COUNT(*) AS n FROM devices WHERE account_id = ?').bind(accountId).first('n');
  if (count >= maxDevices) throw new HttpError(409, `An account holds ${maxDevices} devices. Remove one in Devices first.`);
  const id = crypto.randomUUID();
  const refresh = randomToken(32);
  const at = now();
  await env.DB.batch([
    env.DB.prepare('INSERT INTO devices (id, account_id, name, os, is_tv, public_key, app_version, created, last_seen) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)')
      .bind(id, accountId, device.name, device.os, device.is_tv, device.public_key, device.app_version, at, at),
    env.DB.prepare('INSERT INTO sessions (refresh_hash, device_id, account_id, expires) VALUES (?, ?, ?, ?)')
      .bind(await sha256(refresh), id, accountId, at + refreshTTL),
  ]);
  return {
    access_token: await accessToken(env, account, id),
    expires_in: accessTTL,
    refresh_token: refresh,
    device_id: id,
    account: { id: account.id, name: account.name, avatar: account.avatar, role: account.role },
  };
}

/** POST /token: swaps a sign-in code or a refresh token for tokens. */
export async function token(request, env) {
  const body = await readJSON(request, 8192);
  if (body.grant_type === 'code') {
    const claims = await verify(env.JWT_SECRET, 'code', body.code);
    if (!claims || !claims.ch || claims.ch !== (await pkce(body.verifier))) throw new HttpError(400, 'Sign-in expired. Try again.');
    return json(await newDevice(env, claims.sub, cleanDevice(body.device)));
  }
  if (body.grant_type === 'refresh') {
    if (typeof body.refresh_token !== 'string' || body.refresh_token.length > 128) throw new HttpError(400, 'bad request');
    const hash = await sha256(body.refresh_token);
    const session = await env.DB.prepare('SELECT device_id, account_id, expires FROM sessions WHERE refresh_hash = ?').bind(hash).first();
    if (!session || session.expires < now()) throw new HttpError(401, 'sign in again');
    const account = await accountRow(env, session.account_id);
    if (!account || account.banned) throw new HttpError(401, 'sign in again');
    const refresh = randomToken(32);
    const at = now();
    const version = clip(body.app_version, 32);
    const results = await env.DB.batch([
      env.DB.prepare('UPDATE sessions SET refresh_hash = ?, expires = ? WHERE refresh_hash = ?').bind(await sha256(refresh), at + refreshTTL, hash),
      env.DB.prepare("UPDATE devices SET last_seen = ?, app_version = CASE WHEN ? = '' THEN app_version ELSE ? END WHERE id = ?")
        .bind(at, version, version, session.device_id),
    ]);
    // Two refreshes with the same token: only the first one wins.
    if (!results[0].meta.changes) throw new HttpError(401, 'sign in again');
    return json({
      access_token: await accessToken(env, account, session.device_id),
      expires_in: accessTTL,
      refresh_token: refresh,
      device_id: session.device_id,
      account: { id: account.id, name: account.name, avatar: account.avatar, role: account.role },
    });
  }
  throw new HttpError(400, 'bad request');
}

/** Removes a device: its sessions, its vault key and its live socket. */
export async function removeDevice(env, account, device) {
  const results = await env.DB.batch([
    env.DB.prepare('DELETE FROM devices WHERE id = ? AND account_id = ?').bind(device, account),
    env.DB.prepare('DELETE FROM sessions WHERE device_id = ? AND account_id = ?').bind(device, account),
    env.DB.prepare('DELETE FROM vault_keys WHERE device_id = ? AND account_id = ?').bind(device, account),
    // Whoever still holds the vault key replaces it, so the removed device's
    // copy opens nothing new.
    env.DB.prepare('UPDATE accounts SET vault_stale = 1 WHERE id = ? AND vault_key_version > 0').bind(account),
  ]);
  if (!results[0].meta.changes) return false;
  const hub = env.HUB.get(env.HUB.idFromName(account));
  await hub.fetch('https://hub/kick', { method: 'POST', headers: { 'x-account': account }, body: JSON.stringify({ device }) });
  return true;
}

// ---- QR pairing ----------------------------------------------------------

async function newPairCode(env, secretHash, device) {
  const at = now();
  await env.DB.prepare('DELETE FROM pairings WHERE expires < ?').bind(at).run();
  for (let i = 0; i < 5; i++) {
    const n = crypto.getRandomValues(new Uint32Array(1))[0] % 1000000;
    const code = String(n).padStart(6, '0');
    const r = await env.DB.prepare('INSERT OR IGNORE INTO pairings (code, secret_hash, device, expires) VALUES (?, ?, ?, ?)')
      .bind(code, secretHash, JSON.stringify(device), at + pairTTL).run();
    if (r.meta.changes) return code;
  }
  throw new HttpError(503, 'Try again in a moment.');
}

/** POST /pair/start: a TV asks for a code to show as QR and digits. */
export async function pairStart(request, env, base) {
  if (!pairLimit.allow(clientIP(request))) throw new HttpError(429, 'Too many codes. Wait a few minutes.');
  const body = await readJSON(request, 4096);
  const device = cleanDevice(body.device);
  const secret = randomToken(24);
  const code = await newPairCode(env, await sha256(secret), device);
  return json({ code, secret, expires_in: pairTTL / 1000, url: `${base}/pair?code=${code}`, interval: 3 });
}

/**
 * POST /pair/approve: a signed-in app (Bearer token) or a phone browser
 * that signed in on the approval page (web token) gives the TV its account.
 */
export async function pairApprove(request, env) {
  if (!approveLimit.allow(clientIP(request))) throw new HttpError(429, 'Too many tries. Wait a few minutes.');
  const body = await readJSON(request, 4096);
  let account = '';
  if (request.headers.get('authorization')) {
    account = (await authorize(request, env)).account;
  } else {
    const web = await verify(env.JWT_SECRET, 'web', body.web);
    if (!web || web.pc !== body.code) throw new HttpError(401, 'Sign in again on this page.');
    account = web.sub;
  }
  if (!/^\d{6}$/.test(body.code || '')) throw new HttpError(400, 'Type the 6 digits on the TV.');
  const row = await env.DB.prepare('SELECT device, account_id, expires FROM pairings WHERE code = ?').bind(body.code).first();
  if (!row || row.expires < now()) throw new HttpError(404, 'That code ran out. Show a new one on the TV.');
  if (row.account_id && row.account_id !== account) throw new HttpError(409, 'Someone else already approved that code.');
  await env.DB.prepare('UPDATE pairings SET account_id = ? WHERE code = ? AND account_id IS NULL').bind(account, body.code).run();
  return json({ ok: true, name: JSON.parse(row.device).name });
}

/** POST /pair/poll: the TV waits for approval, then gets its tokens. */
export async function pairPoll(request, env) {
  const body = await readJSON(request, 4096);
  if (!/^\d{6}$/.test(body.code || '') || typeof body.secret !== 'string') throw new HttpError(400, 'bad request');
  if (!pollLimit.allow(body.code)) throw new HttpError(429, 'slow down');
  const row = await env.DB.prepare('SELECT secret_hash, device, account_id, expires FROM pairings WHERE code = ?').bind(body.code).first();
  if (!row || row.secret_hash !== (await sha256(body.secret))) throw new HttpError(404, 'gone');
  if (row.expires < now()) throw new HttpError(410, 'expired');
  if (!row.account_id) return json({ pending: true }, 202);
  const del = await env.DB.prepare('DELETE FROM pairings WHERE code = ? AND account_id IS NOT NULL').bind(body.code).run();
  if (!del.meta.changes) throw new HttpError(404, 'gone');
  return json(await newDevice(env, row.account_id, JSON.parse(row.device)));
}
