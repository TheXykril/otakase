// Small helpers shared by the account Worker and its Durable Object.

export const enc = new TextEncoder();
export const dec = new TextDecoder();

export class HttpError extends Error {
  constructor(status, message, extra) {
    super(message);
    this.status = status;
    this.extra = extra;
  }
}

export function json(value, status = 200, headers = {}) {
  return new Response(JSON.stringify(value), {
    status,
    headers: {
      'content-type': 'application/json; charset=utf-8',
      'cache-control': 'no-store',
      ...headers,
    },
  });
}

export function errorResponse(status, message, extra) {
  return json({ error: message, ...(extra || {}) }, status);
}

/** Reads a JSON body of at most max bytes, or throws a 400. */
export async function readJSON(request, max = 16 * 1024) {
  const length = Number(request.headers.get('content-length') || 0);
  if (length > max) throw new HttpError(413, 'too large');
  const text = await request.text();
  if (text.length > max) throw new HttpError(413, 'too large');
  try {
    const value = JSON.parse(text || '{}');
    if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error();
    return value;
  } catch {
    throw new HttpError(400, 'bad request');
  }
}

export function b64url(bytes) {
  let s = '';
  for (const b of new Uint8Array(bytes)) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export function fromB64url(text) {
  const s = atob(text.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(text.length / 4) * 4, '='));
  const out = new Uint8Array(s.length);
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i);
  return out;
}

export function randomToken(bytes = 32) {
  return b64url(crypto.getRandomValues(new Uint8Array(bytes)));
}

export async function sha256(text) {
  return b64url(await crypto.subtle.digest('SHA-256', enc.encode(text)));
}

/** Compares two strings without leaking where they differ. */
export function safeEqual(a, b) {
  if (typeof a !== 'string' || typeof b !== 'string') return false;
  const x = enc.encode(a);
  const y = enc.encode(b);
  if (x.length !== y.length) return false;
  let diff = 0;
  for (let i = 0; i < x.length; i++) diff |= x[i] ^ y[i];
  return diff === 0;
}

export const now = () => Date.now();

/** Clips a free-text value from a device to a sane length. */
export function clip(value, max) {
  return String(value ?? '').replace(/[\u0000-\u001f\u007f]/g, ' ').trim().slice(0, max);
}

/**
 * A best-effort limit per Worker isolate. Cloudflare runs few isolates for a
 * small app, so this stops a single client hammering sign-in or pairing
 * without spending database writes on counters.
 */
export class RateLimit {
  constructor(count, windowMs) {
    this.count = count;
    this.windowMs = windowMs;
    this.hits = new Map();
  }

  allow(key, at = Date.now()) {
    const kept = (this.hits.get(key) || []).filter((t) => at - t < this.windowMs);
    if (kept.length >= this.count) {
      this.hits.set(key, kept);
      return false;
    }
    kept.push(at);
    this.hits.set(key, kept);
    if (this.hits.size > 5000) this.hits = new Map([[key, kept]]);
    return true;
  }
}

export function clientIP(request) {
  return request.headers.get('cf-connecting-ip') || 'local';
}

export function escapeHTML(value) {
  return String(value ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  })[c]);
}
