import { env, createExecutionContext, waitOnExecutionContext } from 'cloudflare:test';
import { vi } from 'vitest';
import worker from '../src/index.js';

export const base = 'https://account.otakase.com';

/** Calls the Worker like a client would. */
export async function call(path, { method = 'GET', body, token, headers = {} } = {}) {
  const h = new Headers(headers);
  // A fresh address per call, so the per-address limits stay out of the way.
  if (!h.has('cf-connecting-ip')) h.set('cf-connecting-ip', `10.0.${Math.floor(Math.random() * 250)}.${Math.floor(Math.random() * 250)}`);
  if (body !== undefined) h.set('content-type', 'application/json');
  if (token) h.set('authorization', `Bearer ${token}`);
  const request = new Request(path.startsWith('http') ? path : base + path, {
    method,
    headers: h,
    body: body === undefined ? undefined : typeof body === 'string' ? body : JSON.stringify(body),
  });
  const ctx = createExecutionContext();
  const response = await worker.fetch(request, env, ctx);
  await waitOnExecutionContext(ctx);
  return response;
}

export async function callJSON(path, opts) {
  const r = await call(path, opts);
  return { status: r.status, body: await r.json() };
}

/** Fakes Discord, Google, Turnstile and the bot. Returns the spy. */
export function fakeOutside({ users = {}, turnstile = true, bot } = {}) {
  const real = globalThis.fetch;
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    if (url.hostname === 'challenges.cloudflare.com') return Response.json({ success: turnstile });
    if (url.href === 'https://discord.com/api/oauth2/token' || url.href === 'https://oauth2.googleapis.com/token') {
      const form = new URLSearchParams(await request.text());
      return Response.json({ access_token: `at-${form.get('code')}` });
    }
    if (url.href === 'https://discord.com/api/users/@me' || url.href === 'https://openidconnect.googleapis.com/v1/userinfo') {
      const code = request.headers.get('authorization').replace('Bearer at-', '');
      const u = users[code];
      if (!u) return new Response('no', { status: 401 });
      return Response.json(u);
    }
    if (url.hostname === 'bot.test' && bot) return bot(request, url);
    return real(input, init);
  });
}

export async function pkcePair() {
  const verifier = 'v'.repeat(20) + crypto.randomUUID().replace(/-/g, '');
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier));
  let s = '';
  for (const b of new Uint8Array(digest)) s += String.fromCharCode(b);
  const challenge = btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  return { verifier, challenge };
}

/** Runs the whole browser sign-in and returns the code from the redirect. */
export async function browserSignIn(provider, code, { ret = 'app', challenge, pair, attach } = {}) {
  const start = await callJSON('/login/start', {
    method: 'POST',
    body: { provider, challenge, ret, pair, attach, turnstile: 'token' },
  });
  if (start.status !== 200) return { start };
  const state = new URL(start.body.url).searchParams.get('state');
  const r = await call(`/callback/${provider}?code=${code}&state=${encodeURIComponent(state)}`);
  const html = await r.text();
  const m = html.match(/otakase:\/\/account\?code=([^"&<]+)/) || html.match(/127\.0\.0\.1:47613\/account\?code=([^"&<]+)/);
  return { status: r.status, html, code: m ? decodeURIComponent(m[1]) : null };
}

let deviceCount = 0;
export function device(extra = {}) {
  deviceCount++;
  return { name: `Phone ${deviceCount}`, os: 'android', app_version: '26.5.0', ...extra };
}

/** Signs a new device in to the account of a Discord or Google user. */
export async function signedIn(provider = 'discord', code = 'alice', extra = {}) {
  const { verifier, challenge } = await pkcePair();
  const { code: signInCode } = await browserSignIn(provider, code, { challenge });
  const r = await callJSON('/token', { method: 'POST', body: { grant_type: 'code', code: signInCode, verifier, device: device(extra) } });
  if (r.status !== 200) throw new Error(`sign in failed: ${JSON.stringify(r.body)}`);
  return r.body;
}
