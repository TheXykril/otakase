// The token vault. Tracker logins are encrypted on the device with the
// account's vault key; each device holds that key wrapped with its own
// public key. The server also keeps one copy sealed with the VAULT_SECRET
// Worker secret, and hands it only to signed-in devices of the same
// account, so a new device needs no approval. Without VAULT_SECRET a new
// device gets the key when a device that has it approves it.

import { HttpError, json, readJSON, now, safeEqual, enc, b64url, fromB64url } from './util.js';

const blobPattern = /^[A-Za-z0-9_-]{16,65536}$/;
const wrappedPattern = /^[A-Za-z0-9_-]{40,512}$/;
const profilePattern = /^[A-Za-z0-9_-]{0,64}$/;
const checkPattern = /^[A-Za-z0-9_-]{40,64}$/;
const keyPattern = /^[A-Za-z0-9_-]{43}$/;

// ---- The server's copy ----------------------------------------------------

/** The AES key that seals one account's server copy, from VAULT_SECRET. */
async function serverKey(env, account) {
  const base = await crypto.subtle.importKey('raw', enc.encode(env.VAULT_SECRET), 'HKDF', false, ['deriveKey']);
  return crypto.subtle.deriveKey(
    { name: 'HKDF', hash: 'SHA-256', salt: enc.encode('otakase vault server v1'), info: enc.encode(account) },
    base,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  );
}

const serverOn = (env) => typeof env.VAULT_SECRET === 'string' && env.VAULT_SECRET.length >= 32;

/** Seals [key] for [account] at [version]: iv then ciphertext. */
async function sealServer(env, account, version, key) {
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const box = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv, additionalData: enc.encode(`${account}:${version}`) },
    await serverKey(env, account),
    key,
  );
  return b64url([...iv, ...new Uint8Array(box)]);
}

/** Opens a server copy, or null when it does not open (another secret). */
async function openServer(env, account, version, sealed) {
  try {
    const bytes = fromB64url(sealed);
    const plain = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: bytes.slice(0, 12), additionalData: enc.encode(`${account}:${version}`) },
      await serverKey(env, account),
      bytes.slice(12),
    );
    return new Uint8Array(plain);
  } catch {
    return null;
  }
}

/** The check a device computes for a vault key: HMAC-SHA256 of a label. */
async function checkOf(key) {
  const mac = await crypto.subtle.importKey('raw', key, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
  return b64url(await crypto.subtle.sign('HMAC', mac, enc.encode('otakase vault check v1')));
}

async function notify(env, account, msg) {
  const hub = env.HUB.get(env.HUB.idFromName(account));
  await hub.fetch('https://hub/notify', { method: 'POST', headers: { 'x-account': account }, body: JSON.stringify(msg) });
}

/** GET /vault: this device's wrapped key, devices still waiting, all blobs. */
export async function getVault(env, auth) {
  const [account, keys, devices, blobs] = await env.DB.batch([
    env.DB.prepare('SELECT vault_key_version, vault_stale, vault_check, vault_server FROM accounts WHERE id = ?').bind(auth.account),
    env.DB.prepare('SELECT device_id, key_version, wrapped FROM vault_keys WHERE account_id = ?').bind(auth.account),
    env.DB.prepare('SELECT id, name, public_key FROM devices WHERE account_id = ?').bind(auth.account),
    env.DB.prepare('SELECT profile_id, version, key_version, ciphertext FROM vault WHERE account_id = ?').bind(auth.account),
  ]);
  const a = account.results[0];
  if (!a) throw new HttpError(401, 'sign in again');
  const mine = keys.results.find((k) => k.device_id === auth.device && k.key_version === a.vault_key_version);
  const holders = new Set(keys.results.filter((k) => k.key_version === a.vault_key_version).map((k) => k.device_id));
  // The server's copy: 'off' without VAULT_SECRET, 'missing' until a device
  // that holds the key hands it over, then 'ready'. A device of this account
  // without its own copy gets the key itself (only ever over this signed-in
  // call) and stores its own copy through /vault/restore.
  let server = 'off';
  let serverCopy = null;
  if (serverOn(env)) {
    server = 'missing';
    if (a.vault_key_version > 0 && a.vault_server) {
      const key = await openServer(env, auth.account, a.vault_key_version, a.vault_server);
      if (key) {
        server = 'ready';
        // A removed device's access token lives up to an hour: only devices
        // still listed on the account get the key.
        const listed = devices.results.some((d) => d.id === auth.device);
        if (!mine && listed) serverCopy = b64url(key);
      }
    }
  }
  return json({
    key_version: a.vault_key_version,
    stale: a.vault_stale === 1,
    has_check: !!a.vault_check,
    server,
    ...(serverCopy ? { server_key: serverCopy } : {}),
    wrapped: mine ? mine.wrapped : null,
    holders: [...holders],
    waiting: devices.results.filter((d) => !holders.has(d.id) && d.public_key).map((d) => ({ id: d.id, name: d.name, public_key: d.public_key })),
    devices: devices.results.map((d) => ({ id: d.id, public_key: d.public_key })),
    blobs: blobs.results.map((b) => ({ profile: b.profile_id, version: b.version, key_version: b.key_version, ciphertext: b.ciphertext })),
  });
}

/**
 * PUT /vault/keys {key_version, keys: {device: wrapped}, rotate}
 * Without rotate, adds wrapped copies of the current key for more devices
 * (approving them). With rotate, key_version must be the next one: the
 * listed copies replace every old one and the blobs must be re-sealed.
 */
export async function putKeys(request, env, auth) {
  const body = await readJSON(request, 16 * 1024);
  const keys = body.keys && typeof body.keys === 'object' ? Object.entries(body.keys) : [];
  if (!keys.length || keys.length > 10) throw new HttpError(400, 'bad keys');
  for (const [d, w] of keys) if (typeof w !== 'string' || !wrappedPattern.test(w) || typeof d !== 'string') throw new HttpError(400, 'bad keys');
  const version = Number(body.key_version);
  const account = await env.DB.prepare('SELECT vault_key_version FROM accounts WHERE id = ?').bind(auth.account).first();
  if (!account) throw new HttpError(401, 'sign in again');
  const devices = new Set((await env.DB.prepare('SELECT id FROM devices WHERE account_id = ?').bind(auth.account).all()).results.map((r) => r.id));
  for (const [d] of keys) if (!devices.has(d)) throw new HttpError(400, 'unknown device');
  const statements = [];
  if (body.rotate) {
    if (version !== account.vault_key_version + 1) throw new HttpError(409, 'vault changed', { key_version: account.vault_key_version });
    if (!keys.some(([d]) => d === auth.device)) throw new HttpError(400, 'keep a copy for this device');
    const check = typeof body.check === 'string' && checkPattern.test(body.check) ? body.check : null;
    const bump = env.DB.prepare('UPDATE accounts SET vault_key_version = ?, vault_stale = 0, vault_check = ?, vault_server = NULL WHERE id = ? AND vault_key_version = ?')
      .bind(version, check, auth.account, account.vault_key_version);
    const r = await bump.run();
    if (!r.meta.changes) throw new HttpError(409, 'vault changed');
    statements.push(env.DB.prepare('DELETE FROM vault_keys WHERE account_id = ?').bind(auth.account));
  } else {
    if (version !== account.vault_key_version || version === 0) throw new HttpError(409, 'vault changed', { key_version: account.vault_key_version });
    if (!(await holdsKey(env, auth, version))) throw new HttpError(403, 'This device has no vault key yet.');
  }
  for (const [d, w] of keys) {
    statements.push(env.DB.prepare(
      'INSERT INTO vault_keys (account_id, device_id, key_version, wrapped) VALUES (?, ?, ?, ?) ON CONFLICT (account_id, device_id) DO UPDATE SET key_version = excluded.key_version, wrapped = excluded.wrapped',
    ).bind(auth.account, d, version, w));
  }
  try {
    await env.DB.batch(statements);
  } catch (e) {
    // A half-done rotation leaves nobody with the new key: ask for another.
    if (body.rotate) await env.DB.prepare('UPDATE accounts SET vault_stale = 1 WHERE id = ?').bind(auth.account).run();
    throw e;
  }
  await notify(env, auth.account, { t: 'vault', from: auth.device });
  return json({ ok: true, key_version: version });
}

async function holdsKey(env, auth, version) {
  return !!(await env.DB.prepare('SELECT 1 FROM vault_keys WHERE account_id = ? AND device_id = ? AND key_version = ?')
    .bind(auth.account, auth.device, version).first());
}

/**
 * PUT /vault/check {key_version, check}
 * A device holding the current key stores its check, for vaults made
 * before checks existed.
 */
export async function putCheck(request, env, auth) {
  const body = await readJSON(request);
  const version = Number(body.key_version);
  if (typeof body.check !== 'string' || !checkPattern.test(body.check)) throw new HttpError(400, 'bad check');
  const account = await env.DB.prepare('SELECT vault_key_version FROM accounts WHERE id = ?').bind(auth.account).first();
  if (!account) throw new HttpError(401, 'sign in again');
  if (version !== account.vault_key_version || version === 0) throw new HttpError(409, 'vault changed', { key_version: account.vault_key_version });
  if (!(await holdsKey(env, auth, version))) throw new HttpError(403, 'This device has no vault key yet.');
  await env.DB.prepare('UPDATE accounts SET vault_check = ? WHERE id = ? AND vault_key_version = ? AND vault_check IS NULL')
    .bind(body.check, auth.account, version).run();
  return json({ ok: true });
}

/**
 * POST /vault/restore {key_version, check, wrapped}
 * A device that signed out and in again still has the vault key. It proves
 * that with the key's check and stores a copy wrapped for its new entry.
 * After a device was removed the key is replaced, so an old copy fails here.
 */
export async function restoreKey(request, env, auth) {
  const body = await readJSON(request);
  const version = Number(body.key_version);
  if (typeof body.check !== 'string' || typeof body.wrapped !== 'string' || !wrappedPattern.test(body.wrapped)) throw new HttpError(400, 'bad key');
  const account = await env.DB.prepare('SELECT vault_key_version, vault_check FROM accounts WHERE id = ?').bind(auth.account).first();
  if (!account) throw new HttpError(401, 'sign in again');
  if (version !== account.vault_key_version || version === 0) throw new HttpError(409, 'vault changed', { key_version: account.vault_key_version });
  if (!account.vault_check || !safeEqual(account.vault_check, body.check)) throw new HttpError(403, 'This key is not the vault key.');
  await env.DB.prepare(
    'INSERT INTO vault_keys (account_id, device_id, key_version, wrapped) VALUES (?, ?, ?, ?) ON CONFLICT (account_id, device_id) DO UPDATE SET key_version = excluded.key_version, wrapped = excluded.wrapped',
  ).bind(auth.account, auth.device, version, body.wrapped).run();
  await notify(env, auth.account, { t: 'vault', from: auth.device });
  return json({ ok: true, key_version: version });
}

/**
 * PUT /vault/server {key_version, key}
 * A device holding the current key hands the server its copy. The key must
 * match the account's check, so a device cannot plant a wrong one.
 */
export async function putServerKey(request, env, auth) {
  if (!serverOn(env)) throw new HttpError(503, 'The server keeps no vault keys.');
  const body = await readJSON(request);
  const version = Number(body.key_version);
  if (typeof body.key !== 'string' || !keyPattern.test(body.key)) throw new HttpError(400, 'bad key');
  const account = await env.DB.prepare('SELECT vault_key_version, vault_check FROM accounts WHERE id = ?').bind(auth.account).first();
  if (!account) throw new HttpError(401, 'sign in again');
  if (version !== account.vault_key_version || version === 0) throw new HttpError(409, 'vault changed', { key_version: account.vault_key_version });
  if (!(await holdsKey(env, auth, version))) throw new HttpError(403, 'This device has no vault key yet.');
  const key = fromB64url(body.key);
  if (!account.vault_check || !safeEqual(account.vault_check, await checkOf(key))) throw new HttpError(403, 'This key is not the vault key.');
  await env.DB.prepare('UPDATE accounts SET vault_server = ? WHERE id = ? AND vault_key_version = ?')
    .bind(await sealServer(env, auth.account, version, key), auth.account, version).run();
  return json({ ok: true });
}

/**
 * PUT /vault/blob {profile, base_version, key_version, ciphertext}
 * Saves a profile's sealed logins if nobody saved a newer one meanwhile; a
 * clash answers 409 with the current blob so the device merges and retries.
 */
export async function putBlob(request, env, auth) {
  const body = await readJSON(request, 80 * 1024);
  const profile = typeof body.profile === 'string' ? body.profile : '';
  if (!profilePattern.test(profile) || typeof body.ciphertext !== 'string' || !blobPattern.test(body.ciphertext)) throw new HttpError(400, 'bad blob');
  const base = Number(body.base_version) || 0;
  const keyVersion = Number(body.key_version);
  const account = await env.DB.prepare('SELECT vault_key_version FROM accounts WHERE id = ?').bind(auth.account).first();
  if (!account) throw new HttpError(401, 'sign in again');
  if (keyVersion !== account.vault_key_version) throw new HttpError(409, 'vault changed', { key_version: account.vault_key_version });
  const at = now();
  const r = base === 0
    ? await env.DB.prepare('INSERT OR IGNORE INTO vault (account_id, profile_id, version, key_version, ciphertext, updated) VALUES (?, ?, 1, ?, ?, ?)')
      .bind(auth.account, profile, keyVersion, body.ciphertext, at).run()
    : await env.DB.prepare('UPDATE vault SET version = version + 1, key_version = ?, ciphertext = ?, updated = ? WHERE account_id = ? AND profile_id = ? AND version = ?')
      .bind(keyVersion, body.ciphertext, at, auth.account, profile, base).run();
  if (!r.meta.changes) {
    const current = await env.DB.prepare('SELECT version, key_version, ciphertext FROM vault WHERE account_id = ? AND profile_id = ?')
      .bind(auth.account, profile).first();
    throw new HttpError(409, 'vault changed', { current: current || null });
  }
  await notify(env, auth.account, { t: 'vault', from: auth.device, profile });
  return json({ ok: true, version: base + 1 });
}

/** DELETE /vault/blob?profile= : forgets a profile's sealed logins. */
export async function deleteBlob(url, env, auth) {
  const profile = url.searchParams.get('profile') || '';
  if (!profilePattern.test(profile)) throw new HttpError(400, 'bad profile');
  await env.DB.prepare('DELETE FROM vault WHERE account_id = ? AND profile_id = ?').bind(auth.account, profile).run();
  await notify(env, auth.account, { t: 'vault', from: auth.device, profile });
  return json({ ok: true });
}
