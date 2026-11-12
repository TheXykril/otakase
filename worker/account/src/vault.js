// The token vault. Tracker logins are encrypted on the device with the
// account's vault key; each device holds that key wrapped with its own
// public key. The server only stores the blobs and cannot open them. A new
// device gets the key when a device that has it approves it.

import { HttpError, json, readJSON, now, safeEqual } from './util.js';

const blobPattern = /^[A-Za-z0-9_-]{16,65536}$/;
const wrappedPattern = /^[A-Za-z0-9_-]{40,512}$/;
const profilePattern = /^[A-Za-z0-9_-]{0,64}$/;
const checkPattern = /^[A-Za-z0-9_-]{40,64}$/;

async function notify(env, account, msg) {
  const hub = env.HUB.get(env.HUB.idFromName(account));
  await hub.fetch('https://hub/notify', { method: 'POST', headers: { 'x-account': account }, body: JSON.stringify(msg) });
}

/** GET /vault: this device's wrapped key, devices still waiting, all blobs. */
export async function getVault(env, auth) {
  const [account, keys, devices, blobs] = await env.DB.batch([
    env.DB.prepare('SELECT vault_key_version, vault_stale, vault_check FROM accounts WHERE id = ?').bind(auth.account),
    env.DB.prepare('SELECT device_id, key_version, wrapped FROM vault_keys WHERE account_id = ?').bind(auth.account),
    env.DB.prepare('SELECT id, name, public_key FROM devices WHERE account_id = ?').bind(auth.account),
    env.DB.prepare('SELECT profile_id, version, key_version, ciphertext FROM vault WHERE account_id = ?').bind(auth.account),
  ]);
  const a = account.results[0];
  if (!a) throw new HttpError(401, 'sign in again');
  const mine = keys.results.find((k) => k.device_id === auth.device && k.key_version === a.vault_key_version);
  const holders = new Set(keys.results.filter((k) => k.key_version === a.vault_key_version).map((k) => k.device_id));
  return json({
    key_version: a.vault_key_version,
    stale: a.vault_stale === 1,
    has_check: !!a.vault_check,
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
    const bump = env.DB.prepare('UPDATE accounts SET vault_key_version = ?, vault_stale = 0, vault_check = ? WHERE id = ? AND vault_key_version = ?')
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
