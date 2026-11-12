// Sign-in with Discord or Google. The app opens /login in a browser with a
// PKCE challenge; after the provider sends the person back, the Worker finds
// or creates the account and hands the app a short-lived code it swaps for
// tokens at /token. Phones approving a TV use the same pages and end on an
// approval page instead of returning to an app.

import { sign, verify } from './jwt.js';
import { HttpError, json, readJSON, clip, now, RateLimit, clientIP, escapeHTML } from './util.js';
import { page } from './pages.js';

export const desktopLoginPort = 47613;

const startLimit = new RateLimit(20, 10 * 60 * 1000);

export const providers = {
  discord: {
    label: 'Discord',
    authorize: 'https://discord.com/oauth2/authorize',
    token: 'https://discord.com/api/oauth2/token',
    scope: 'identify',
    clientId: (env) => env.DISCORD_CLIENT_ID,
    secret: (env) => env.DISCORD_CLIENT_SECRET,
    async user(access) {
      const r = await fetch('https://discord.com/api/users/@me', { headers: { authorization: `Bearer ${access}` } });
      if (!r.ok) throw new HttpError(502, 'Discord did not answer');
      const u = await r.json();
      const avatar = u.avatar ? `https://cdn.discordapp.com/avatars/${u.id}/${u.avatar}.png?size=128` : '';
      return { id: String(u.id), name: clip(u.global_name || u.username, 64), avatar };
    },
  },
  google: {
    label: 'Google',
    authorize: 'https://accounts.google.com/o/oauth2/v2/auth',
    token: 'https://oauth2.googleapis.com/token',
    scope: 'openid email profile',
    clientId: (env) => env.GOOGLE_CLIENT_ID,
    secret: (env) => env.GOOGLE_CLIENT_SECRET,
    async user(access) {
      const r = await fetch('https://openidconnect.googleapis.com/v1/userinfo', { headers: { authorization: `Bearer ${access}` } });
      if (!r.ok) throw new HttpError(502, 'Google did not answer');
      const u = await r.json();
      if (!u.sub) throw new HttpError(502, 'Google did not answer');
      const name = clip(u.given_name || u.name || String(u.email || '').split('@')[0], 64);
      return { id: String(u.sub), name: name || 'Otakase viewer', avatar: clip(u.picture, 512), email: clip(u.email, 254) };
    },
  },
};

const returns = new Set(['app', 'desktop', 'pair', 'attach']);

/** GET /login: the sign-in page with the Turnstile check. */
export function loginPage(url, env, base) {
  const q = url.searchParams;
  const ret = returns.has(q.get('return')) ? q.get('return') : 'app';
  const pairCode = /^\d{6}$/.test(q.get('code') || '') ? q.get('code') : '';
  const challenge = q.get('challenge') || '';
  if ((ret === 'app' || ret === 'desktop') && !/^[A-Za-z0-9_-]{43}$/.test(challenge)) {
    return page(env, 'Sign in', '<h1>Start from Otakase</h1><p>Open Accounts in the Otakase app and pick Otakase account.</p>', 400);
  }
  const attach = ret === 'attach' ? q.get('attach') || '' : '';
  const heading = ret === 'pair'
    ? `<h1>Add a TV to your account</h1><p>Check the TV shows <b>${pairCode.slice(0, 3)} ${pairCode.slice(3)}</b>, then sign in.</p>`
    : ret === 'attach'
      ? '<h1>Add a sign-in</h1><p>Pick the login to add to your Otakase account.</p>'
      : '<h1>Sign in to Otakase</h1><p>Your settings, profiles and logins follow you to every screen.</p>';
  const only = ret === 'attach' && providers[q.get('provider')] ? [q.get('provider')] : Object.keys(providers);
  const buttons = only
    .filter((p) => providers[p].clientId(env))
    .map((p) => `<button data-provider="${p}" class="${p}">Continue with ${providers[p].label}</button>`)
    .join('');
  const site = env.TURNSTILE_SITE_KEY || '';
  const body = `${heading}
<div class="turnstile" ${site ? `data-sitekey="${escapeHTML(site)}"` : 'hidden'}></div>
<div class="buttons">${buttons}</div>
<p id="status" class="small" role="status"></p>
<p class="small">Otakase keeps only your login's id and name. <a href="https://otakase.com/privacy/">Privacy</a></p>
<script>
const start = ${JSON.stringify({ challenge, ret, pair: pairCode, attach })};
const status = document.getElementById('status');
let widget = null;
window.onTurnstileReady = () => {
  const box = document.querySelector('.turnstile');
  widget = turnstile.render(box, { sitekey: box.dataset.sitekey, theme: 'dark' });
};
for (const b of document.querySelectorAll('button[data-provider]')) {
  b.addEventListener('click', async () => {
    const turnstileToken = widget === null ? '' : turnstile.getResponse(widget) || '';
    if (${site ? 'true' : 'false'} && !turnstileToken) { status.textContent = 'Finish the check above first.'; return; }
    status.textContent = 'Opening ' + b.textContent.replace('Continue with ', '') + '…';
    try {
      const r = await fetch(${JSON.stringify(base + '/login/start')}, { method: 'POST', headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ ...start, provider: b.dataset.provider, turnstile: turnstileToken }) });
      const data = await r.json();
      if (!r.ok) throw new Error(data.error || 'Could not start');
      location.href = data.url;
    } catch (e) {
      status.textContent = e.message;
      if (widget !== null) turnstile.reset(widget);
    }
  });
}
</script>
${site ? '<script src="https://challenges.cloudflare.com/turnstile/v0/api.js?onload=onTurnstileReady&render=explicit" async defer></script>' : ''}`;
  return page(env, 'Sign in', body, 200, { turnstile: Boolean(site) });
}

async function checkTurnstile(env, token, ip) {
  if (!env.TURNSTILE_SECRET) return true;
  if (typeof token !== 'string' || !token || token.length > 2048) return false;
  const form = new FormData();
  form.set('secret', env.TURNSTILE_SECRET);
  form.set('response', token);
  if (ip !== 'local') form.set('remoteip', ip);
  try {
    const r = await fetch('https://challenges.cloudflare.com/turnstile/v0/siteverify', { method: 'POST', body: form });
    const data = await r.json();
    return data.success === true;
  } catch {
    return false;
  }
}

/** POST /login/start: checks Turnstile and gives the provider's URL. */
export async function loginStart(request, env, base) {
  const ip = clientIP(request);
  if (!startLimit.allow(ip)) throw new HttpError(429, 'Too many tries. Wait a few minutes.');
  const body = await readJSON(request, 8192);
  const provider = providers[body.provider];
  if (!provider || !provider.clientId(env)) throw new HttpError(400, 'unknown sign-in');
  const ret = returns.has(body.ret) ? body.ret : 'app';
  if ((ret === 'app' || ret === 'desktop') && !/^[A-Za-z0-9_-]{43}$/.test(body.challenge || '')) throw new HttpError(400, 'bad request');
  if (ret === 'pair' && !/^\d{6}$/.test(body.pair || '')) throw new HttpError(400, 'bad request');
  let attach = '';
  if (ret === 'attach') {
    const claims = await verify(env.JWT_SECRET, 'attach', body.attach);
    if (!claims) throw new HttpError(401, 'Start again from Accounts in Otakase.');
    attach = claims.sub;
  }
  if (!(await checkTurnstile(env, body.turnstile, ip))) throw new HttpError(403, 'The check failed. Try again.');
  const state = await sign(env.JWT_SECRET, 'state', {
    p: body.provider, ch: body.challenge || '', r: ret, pc: body.pair || '', a: attach,
  }, 600);
  const u = new URL(provider.authorize);
  u.searchParams.set('client_id', provider.clientId(env));
  u.searchParams.set('response_type', 'code');
  u.searchParams.set('scope', provider.scope);
  u.searchParams.set('redirect_uri', `${base}/callback/${body.provider}`);
  u.searchParams.set('state', state);
  if (body.provider === 'discord') u.searchParams.set('prompt', 'none');
  if (body.provider === 'google') u.searchParams.set('prompt', 'select_account');
  return json({ url: u.toString() });
}

async function providerUser(env, name, code, base) {
  const provider = providers[name];
  const form = new URLSearchParams({
    client_id: provider.clientId(env),
    client_secret: provider.secret(env) || '',
    grant_type: 'authorization_code',
    code,
    redirect_uri: `${base}/callback/${name}`,
  });
  const r = await fetch(provider.token, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded', accept: 'application/json' },
    body: form,
  });
  if (!r.ok) throw new HttpError(502, `${provider.label} sign-in failed. Try again.`);
  const token = await r.json();
  if (!token.access_token) throw new HttpError(502, `${provider.label} sign-in failed. Try again.`);
  return provider.user(token.access_token);
}

/**
 * Finds the account for a provider login, or makes one. With attach, ties
 * the login to that account instead.
 */
export async function accountFor(env, provider, user, attach) {
  const db = env.DB;
  const found = await db.prepare('SELECT account_id FROM identities WHERE provider = ? AND provider_user_id = ?')
    .bind(provider, user.id).first();
  if (attach) {
    if (found && found.account_id !== attach) throw new HttpError(409, 'That login already belongs to another Otakase account.');
    if (!found) {
      await db.prepare('INSERT INTO identities (provider, provider_user_id, account_id, name, created) VALUES (?, ?, ?, ?, ?)')
        .bind(provider, user.id, attach, user.email || user.name, now()).run();
    }
    return attach;
  }
  if (found) {
    const account = await db.prepare('SELECT banned FROM accounts WHERE id = ?').bind(found.account_id).first();
    if (!account) throw new HttpError(500, 'account missing');
    if (account.banned) throw new HttpError(403, 'This account is blocked.');
    return found.account_id;
  }
  const id = crypto.randomUUID();
  const role = provider === 'discord' && env.ADMIN_DISCORD_ID && user.id === env.ADMIN_DISCORD_ID ? 'admin' : 'user';
  await db.batch([
    db.prepare('INSERT INTO accounts (id, name, avatar, role, created) VALUES (?, ?, ?, ?, ?)')
      .bind(id, user.name || 'Otakase viewer', user.avatar || '', role, now()),
    db.prepare('INSERT INTO identities (provider, provider_user_id, account_id, name, created) VALUES (?, ?, ?, ?, ?)')
      .bind(provider, user.id, id, user.email || user.name, now()),
  ]);
  return id;
}

/** GET /callback/{provider}: back from Discord or Google. */
export async function callback(url, env, base, name) {
  const state = await verify(env.JWT_SECRET, 'state', url.searchParams.get('state'));
  if (!state || state.p !== name || !providers[name]) {
    return page(env, 'Sign in', '<h1>Sign-in expired</h1><p>Start again from Otakase.</p>', 400);
  }
  if (url.searchParams.get('error') || !url.searchParams.get('code')) {
    if (name === 'discord' && url.searchParams.get('error') === 'consent_required') {
      // prompt=none skips Discord's screen only after the first approval.
      const again = new URL(providers.discord.authorize);
      again.searchParams.set('client_id', providers.discord.clientId(env));
      again.searchParams.set('response_type', 'code');
      again.searchParams.set('scope', providers.discord.scope);
      again.searchParams.set('redirect_uri', `${base}/callback/discord`);
      again.searchParams.set('state', url.searchParams.get('state'));
      return Response.redirect(again.toString(), 302);
    }
    return page(env, 'Sign in', '<h1>Sign-in cancelled</h1><p>Nothing changed. Start again from Otakase when you are ready.</p>', 400);
  }
  let account;
  try {
    const user = await providerUser(env, name, url.searchParams.get('code'), base);
    account = await accountFor(env, name, user, state.a);
    if (name === 'discord') {
      // Discord's name and picture win over Google's, and stay fresh.
      await env.DB.prepare('UPDATE accounts SET name = ?, avatar = ? WHERE id = ? AND (name != ? OR avatar != ?)')
        .bind(user.name, user.avatar, account, user.name, user.avatar).run();
    }
  } catch (e) {
    const status = e instanceof HttpError ? e.status : 500;
    return page(env, 'Sign in', `<h1>Sign-in failed</h1><p>${escapeHTML(e instanceof HttpError ? e.message : 'Something went wrong. Try again.')}</p>`, status);
  }
  if (state.r === 'attach') {
    return page(env, 'Sign in', `<h1>${providers[name].label} added</h1><p>Go back to Otakase. You can sign in with it from now on.</p>`);
  }
  if (state.r === 'pair') {
    const web = await sign(env.JWT_SECRET, 'web', { sub: account, pc: state.pc }, 600);
    return pairApprovePage(env, base, state.pc, web);
  }
  const code = await sign(env.JWT_SECRET, 'code', { sub: account, ch: state.ch }, 300);
  const target = state.r === 'desktop'
    ? `http://127.0.0.1:${desktopLoginPort}/account?code=${encodeURIComponent(code)}`
    : `otakase://account?code=${encodeURIComponent(code)}`;
  return page(env, 'Signed in', `<h1>Signed in</h1><p>Going back to Otakase…</p><p><a class="button" href="${escapeHTML(target)}">Open Otakase</a></p>
<script>location.href = ${JSON.stringify(target)};</script>`);
}

function pairApprovePage(env, base, code, web) {
  const body = `<h1>Add this TV?</h1>
<p>Only approve a TV in front of you that shows <b>${code.slice(0, 3)} ${code.slice(3)}</b>.</p>
<div class="buttons"><button id="approve">Add TV</button></div>
<p id="status" class="small" role="status"></p>
<script>
document.getElementById('approve').addEventListener('click', async (e) => {
  e.target.disabled = true;
  const status = document.getElementById('status');
  try {
    const r = await fetch(${JSON.stringify(base + '/pair/approve')}, { method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ code: ${JSON.stringify(code)}, web: ${JSON.stringify(web)} }) });
    const data = await r.json();
    if (!r.ok) throw new Error(data.error || 'Could not add the TV');
    document.querySelector('h1').textContent = 'TV added';
    status.textContent = (data.name || 'The TV') + ' is signed in. You can close this page.';
    e.target.hidden = true;
  } catch (err) {
    status.textContent = err.message;
    e.target.disabled = false;
  }
});
</script>`;
  return page(env, 'Add TV', body);
}
