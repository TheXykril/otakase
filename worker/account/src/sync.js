// Settings, profiles and per-show choices. Each profile has a few JSON
// documents ("kinds"); every field carries the time it was changed, and the
// newer change wins field by field, like the app's own merge. A field set to
// null stays as a tombstone so an old device cannot bring it back.
//
// Pushes run inside the account's AccountHub, one at a time, so two devices
// saving at once never lose each other's fields.

import { HttpError, now } from './util.js';

export const maxDocBytes = 64 * 1024;
export const maxAccountBytes = 1024 * 1024;
export const maxProfiles = 6;
export const maxDocs = 64;
export const maxDocsPerPush = 32;

const idPattern = /^[A-Za-z0-9_-]{0,64}$/;
const kindPattern = /^[a-z][a-z_]{0,23}$/;

/** Checks one pushed document: {profile, kind, fields, times}. */
export function cleanDoc(d) {
  if (!d || typeof d !== 'object') throw new HttpError(400, 'bad document');
  const profile = typeof d.profile === 'string' ? d.profile : '';
  if (!idPattern.test(profile)) throw new HttpError(400, 'bad profile id');
  if (!kindPattern.test(d.kind || '')) throw new HttpError(400, 'bad kind');
  if (!d.fields || typeof d.fields !== 'object' || Array.isArray(d.fields)) throw new HttpError(400, 'bad fields');
  if (!d.times || typeof d.times !== 'object' || Array.isArray(d.times)) throw new HttpError(400, 'bad times');
  const fields = {};
  const times = {};
  const limit = now() + 24 * 60 * 60 * 1000; // a clock a day ahead is broken
  for (const [k, v] of Object.entries(d.fields)) {
    const t = d.times[k];
    if (k.length > 128 || typeof t !== 'number' || !Number.isFinite(t) || t <= 0) throw new HttpError(400, 'bad field time');
    fields[k] = v === undefined ? null : v;
    times[k] = Math.min(Math.floor(t), limit);
  }
  return { profile, kind: d.kind, fields, times };
}

/** Merges incoming fields into a stored document. Newer time wins. */
export function mergeDoc(stored, incoming) {
  const body = { ...(stored ? stored.body : {}) };
  const times = { ...(stored ? stored.times : {}) };
  let changed = false;
  for (const [k, v] of Object.entries(incoming.fields)) {
    const t = incoming.times[k];
    if (times[k] === undefined || t > times[k]) {
      body[k] = v;
      times[k] = t;
      changed = true;
    }
  }
  return { body, times, changed };
}

/** A profile document counts as live unless it carries _deleted: true. */
function liveProfile(doc) {
  return doc.kind === 'profile' && doc.body._deleted !== true;
}

/**
 * Applies a push for one account. Returns the new sync version (unchanged
 * when nothing was newer) and how many documents changed.
 */
export async function applyPush(env, account, docs) {
  if (!Array.isArray(docs) || docs.length > maxDocsPerPush) throw new HttpError(400, 'bad push');
  const clean = docs.map(cleanDoc);
  const rows = await env.DB.prepare('SELECT profile_id, kind, body, times FROM sync_docs WHERE account_id = ?').bind(account).all();
  const stored = new Map();
  let bytes = 0;
  for (const r of rows.results) {
    stored.set(`${r.profile_id}\u0000${r.kind}`, { profile: r.profile_id, kind: r.kind, body: JSON.parse(r.body), times: JSON.parse(r.times), size: r.body.length + r.times.length });
    bytes += r.body.length + r.times.length;
  }
  const changed = new Map();
  for (const d of clean) {
    const key = `${d.profile}\u0000${d.kind}`;
    const before = changed.get(key) || stored.get(key);
    const merged = mergeDoc(before, d);
    if (!merged.changed) continue;
    const bodyText = JSON.stringify(merged.body);
    const timesText = JSON.stringify(merged.times);
    const size = bodyText.length + timesText.length;
    if (size > maxDocBytes) throw new HttpError(413, `The ${d.kind} data is too big to sync.`);
    bytes += size - (before ? before.size : 0);
    changed.set(key, { profile: d.profile, kind: d.kind, body: merged.body, times: merged.times, bodyText, timesText, size });
  }
  const account_ = await env.DB.prepare('SELECT sync_version FROM accounts WHERE id = ?').bind(account).first();
  if (!account_) throw new HttpError(401, 'sign in again');
  if (!changed.size) return { version: account_.sync_version, changed: 0 };
  const all = new Map([...stored, ...changed]);
  if (all.size > maxDocs) throw new HttpError(413, 'Too much to sync.');
  if ([...all.values()].filter(liveProfile).length > maxProfiles) throw new HttpError(409, `An account holds ${maxProfiles} profiles.`);
  if (bytes > maxAccountBytes) throw new HttpError(413, 'This account is full. Remove some profiles or lists.');
  const version = account_.sync_version + 1;
  const at = now();
  const statements = [env.DB.prepare('UPDATE accounts SET sync_version = ?, bytes = ? WHERE id = ?').bind(version, bytes, account)];
  for (const d of changed.values()) {
    statements.push(env.DB.prepare(
      `INSERT INTO sync_docs (account_id, profile_id, kind, body, times, version, updated) VALUES (?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT (account_id, profile_id, kind) DO UPDATE SET body = excluded.body, times = excluded.times, version = excluded.version, updated = excluded.updated`,
    ).bind(account, d.profile, d.kind, d.bodyText, d.timesText, version, at));
  }
  await env.DB.batch(statements);
  return { version, changed: changed.size };
}

/** GET /sync?since=N: documents changed after version N. */
export async function pull(env, account, since) {
  const row = await env.DB.prepare('SELECT sync_version FROM accounts WHERE id = ?').bind(account).first();
  if (!row) throw new HttpError(401, 'sign in again');
  // The cheap answer: nothing new costs one row read.
  if (since === row.sync_version) return { version: row.sync_version, docs: [] };
  const from = since > row.sync_version ? 0 : since; // a reset account starts over
  const rows = await env.DB.prepare('SELECT profile_id, kind, body, times, version FROM sync_docs WHERE account_id = ? AND version > ?')
    .bind(account, from).all();
  return {
    version: row.sync_version,
    full: from === 0,
    docs: rows.results.map((r) => ({ profile: r.profile_id, kind: r.kind, fields: JSON.parse(r.body), times: JSON.parse(r.times), version: r.version })),
  };
}
