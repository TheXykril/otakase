// Signed tokens (HS256 JWT). Access tokens carry the account, device and
// role, so most calls never touch the database to learn who is asking.

import { b64url, fromB64url, enc, dec } from './util.js';

const keys = new Map();

async function hmacKey(secret) {
  let key = keys.get(secret);
  if (!key) {
    key = await crypto.subtle.importKey('raw', enc.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign', 'verify']);
    keys.set(secret, key);
  }
  return key;
}

const header = b64url(enc.encode(JSON.stringify({ alg: 'HS256', typ: 'JWT' })));

/** Signs claims for ttl seconds. typ keeps token kinds apart. */
export async function sign(secret, typ, claims, ttl) {
  const iat = Math.floor(Date.now() / 1000);
  const body = b64url(enc.encode(JSON.stringify({ ...claims, typ, iat, exp: iat + ttl })));
  const data = `${header}.${body}`;
  const sig = await crypto.subtle.sign('HMAC', await hmacKey(secret), enc.encode(data));
  return `${data}.${b64url(sig)}`;
}

/** Returns the claims of a valid, unexpired token of kind typ, else null. */
export async function verify(secret, typ, token) {
  if (typeof token !== 'string' || token.length > 4096) return null;
  const parts = token.split('.');
  if (parts.length !== 3 || parts[0] !== header) return null;
  let ok = false;
  try {
    ok = await crypto.subtle.verify('HMAC', await hmacKey(secret), fromB64url(parts[2]), enc.encode(`${parts[0]}.${parts[1]}`));
  } catch {
    return null;
  }
  if (!ok) return null;
  let claims;
  try {
    claims = JSON.parse(dec.decode(fromB64url(parts[1])));
  } catch {
    return null;
  }
  if (claims.typ !== typ || typeof claims.exp !== 'number' || claims.exp * 1000 < Date.now()) return null;
  return claims;
}
