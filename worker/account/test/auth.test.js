import { env } from 'cloudflare:test';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { call, callJSON, fakeOutside, pkcePair, browserSignIn, signedIn, device } from './helpers.js';

const users = {
  alice: { id: '222222222222222222', username: 'alice', global_name: 'Alice', avatar: 'abc' },
  admin: { id: '111111111111111111', username: 'xy', global_name: 'Xy', avatar: null },
  bob: { id: '333333333333333333', username: 'bob' },
  galice: { sub: 'g-1', given_name: 'Alice G', email: 'alice@example.com', picture: 'https://pic' },
  gcarol: { sub: 'g-2', name: 'Carol', email: 'carol@example.com' },
};

afterEach(() => vi.restoreAllMocks());

describe('sign-in', () => {
  it('shows the login page with Turnstile and both providers', async () => {
    const { challenge } = await pkcePair();
    const r = await call(`/login?challenge=${challenge}`);
    expect(r.status).toBe(200);
    const html = await r.text();
    expect(html).toContain('Continue with Discord');
    expect(html).toContain('Continue with Google');
    expect(html).toContain('data-sitekey="1x00000000000000000000AA"');
    expect(r.headers.get('content-security-policy')).toContain('challenges.cloudflare.com');
  });

  it('refuses the login page without a challenge', async () => {
    expect((await call('/login')).status).toBe(400);
  });

  it('signs in with Discord, makes an account and a device', async () => {
    fakeOutside({ users });
    const s = await signedIn('discord', 'alice');
    expect(s.account.name).toBe('Alice');
    expect(s.account.role).toBe('user');
    expect(s.refresh_token).toBeTruthy();
    const me = await callJSON('/me', { token: s.access_token });
    expect(me.status).toBe(200);
    expect(me.body.identities).toEqual([{ provider: 'discord', name: 'Alice' }]);
    expect(me.body.devices).toHaveLength(1);
    expect(me.body.account.avatar).toContain('cdn.discordapp.com/avatars/222222222222222222/abc.png');
  });

  it('gives the admin role to the admin Discord id', async () => {
    fakeOutside({ users });
    const s = await signedIn('discord', 'admin');
    expect(s.account.role).toBe('admin');
  });

  it('finds the same account on the second sign-in', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    const b = await signedIn('discord', 'alice');
    expect(b.account.id).toBe(a.account.id);
    expect(b.device_id).not.toBe(a.device_id);
  });

  it('signs in with Google', async () => {
    fakeOutside({ users });
    const s = await signedIn('google', 'gcarol');
    expect(s.account.name).toBe('Carol');
    const me = await callJSON('/me', { token: s.access_token });
    expect(me.body.identities).toEqual([{ provider: 'google', name: 'carol@example.com' }]);
  });

  it('stops when Turnstile fails', async () => {
    fakeOutside({ users, turnstile: false });
    const { challenge } = await pkcePair();
    const { start } = await browserSignIn('discord', 'alice', { challenge });
    expect(start.status).toBe(403);
  });

  it('needs the PKCE verifier that matches', async () => {
    fakeOutside({ users });
    const { challenge } = await pkcePair();
    const other = await pkcePair();
    const { code } = await browserSignIn('discord', 'alice', { challenge });
    const r = await callJSON('/token', { method: 'POST', body: { grant_type: 'code', code, verifier: other.verifier, device: device() } });
    expect(r.status).toBe(400);
  });

  it('sends the desktop app to its local port', async () => {
    fakeOutside({ users });
    const { challenge } = await pkcePair();
    const r = await browserSignIn('discord', 'alice', { challenge, ret: 'desktop' });
    expect(r.html).toContain('http://127.0.0.1:47613/account?code=');
    expect(r.code).toBeTruthy();
  });

  it('says why Discord refused the code', async () => {
    const spy = fakeOutside({ users });
    const real = spy.getMockImplementation();
    spy.mockImplementation(async (input, init) => {
      const url = new Request(input, init).url;
      if (url === 'https://discord.com/api/oauth2/token') return Response.json({ error: 'invalid_client' }, { status: 401 });
      return real(input, init);
    });
    const r = await browserSignIn('discord', 'alice', { challenge: (await pkcePair()).challenge });
    expect(r.status).toBe(502);
    expect(r.html).toContain('Discord sign-in failed (401: invalid_client)');
  });

  it('rejects a forged state', async () => {
    const r = await call('/callback/discord?code=x&state=nope');
    expect(r.status).toBe(400);
  });

  it('serves the same routes under /account on the fallback host', async () => {
    fakeOutside({ users });
    const s = await signedIn('discord', 'alice');
    const r = await callJSON('https://otakase.xyverion.com/account/me', { token: s.access_token });
    expect(r.status).toBe(200);
    const { challenge } = await pkcePair();
    const start = await callJSON('https://otakase.xyverion.com/account/login/start', { method: 'POST', body: { provider: 'discord', challenge, ret: 'app', turnstile: 't' } });
    expect(new URL(start.body.url).searchParams.get('redirect_uri')).toBe('https://otakase.xyverion.com/account/callback/discord');
  });
});

describe('tokens', () => {
  it('rotates the refresh token and refuses the old one', async () => {
    fakeOutside({ users });
    const s = await signedIn('discord', 'alice');
    const r1 = await callJSON('/token', { method: 'POST', body: { grant_type: 'refresh', refresh_token: s.refresh_token, app_version: '26.6.0' } });
    expect(r1.status).toBe(200);
    expect(r1.body.refresh_token).not.toBe(s.refresh_token);
    const again = await callJSON('/token', { method: 'POST', body: { grant_type: 'refresh', refresh_token: s.refresh_token } });
    expect(again.status).toBe(401);
    const dev = await env.DB.prepare('SELECT app_version FROM devices WHERE id = ?').bind(s.device_id).first();
    expect(dev.app_version).toBe('26.6.0');
  });

  it('refuses calls without a valid token', async () => {
    expect((await call('/me')).status).toBe(401);
    expect((await call('/me', { token: 'x.y.z' })).status).toBe(401);
  });

  it('signs out a device and kills its refresh token', async () => {
    fakeOutside({ users });
    const s = await signedIn('discord', 'alice');
    expect((await call('/logout', { method: 'POST', token: s.access_token })).status).toBe(200);
    const r = await callJSON('/token', { method: 'POST', body: { grant_type: 'refresh', refresh_token: s.refresh_token } });
    expect(r.status).toBe(401);
  });

  it('caps an account at ten devices', async () => {
    fakeOutside({ users });
    for (let i = 0; i < 10; i++) await signedIn('discord', 'bob');
    await expect(signedIn('discord', 'bob')).rejects.toThrow(/10 devices/);
  });
});

describe('attaching a second login', () => {
  it('adds Google to a Discord account', async () => {
    fakeOutside({ users });
    const s = await signedIn('discord', 'alice');
    const attach = await callJSON('/attach', { method: 'POST', token: s.access_token, body: { provider: 'google' } });
    const ticket = new URL(attach.body.url).searchParams.get('attach');
    const r = await browserSignIn('google', 'galice', { ret: 'attach', attach: ticket });
    expect(r.html).toContain('Google added');
    const g = await signedIn('google', 'galice');
    expect(g.account.id).toBe(s.account.id);
  });

  it('refuses a login that belongs to another account', async () => {
    fakeOutside({ users });
    await signedIn('google', 'gcarol');
    const s = await signedIn('discord', 'alice');
    const attach = await callJSON('/attach', { method: 'POST', token: s.access_token, body: { provider: 'google' } });
    const ticket = new URL(attach.body.url).searchParams.get('attach');
    const r = await browserSignIn('google', 'gcarol', { ret: 'attach', attach: ticket });
    expect(r.status).toBe(409);
  });
});

describe('TV pairing', () => {
  it('signs a TV in from the signed-in app', async () => {
    fakeOutside({ users });
    const phone = await signedIn('discord', 'alice');
    const start = await callJSON('/pair/start', { method: 'POST', body: { device: { name: 'Living room', os: 'android', is_tv: true } } });
    expect(start.body.code).toMatch(/^\d{6}$/);
    expect(start.body.url).toBe(`https://account.otakase.com/pair?code=${start.body.code}`);
    const waiting = await callJSON('/pair/poll', { method: 'POST', body: { code: start.body.code, secret: start.body.secret } });
    expect(waiting.status).toBe(202);
    const ok = await callJSON('/pair/approve', { method: 'POST', token: phone.access_token, body: { code: start.body.code } });
    expect(ok.body.name).toBe('Living room');
    const tv = await callJSON('/pair/poll', { method: 'POST', body: { code: start.body.code, secret: start.body.secret } });
    expect(tv.status).toBe(200);
    expect(tv.body.account.id).toBe(phone.account.id);
    const gone = await callJSON('/pair/poll', { method: 'POST', body: { code: start.body.code, secret: start.body.secret } });
    expect(gone.status).toBe(404);
    const me = await callJSON('/me', { token: tv.body.access_token });
    expect(me.body.devices.find((d) => d.id === tv.body.device_id).is_tv).toBe(1);
  });

  it('signs a TV in from a phone browser without the app', async () => {
    fakeOutside({ users });
    const start = await callJSON('/pair/start', { method: 'POST', body: { device: { name: 'Bedroom TV', is_tv: true } } });
    const qr = await call(`/pair?code=${start.body.code}`);
    expect(qr.status).toBe(302);
    expect(qr.headers.get('location')).toContain(`return=pair&code=${start.body.code}`);
    const page = await call(`/login?return=pair&code=${start.body.code}`);
    expect(await page.text()).toContain(`${start.body.code.slice(0, 3)} ${start.body.code.slice(3)}`);
    const r = await browserSignIn('google', 'gcarol', { ret: 'pair', pair: start.body.code });
    expect(r.html).toContain('Add this TV?');
    const web = r.html.match(/"web":"([^"]+)"/) || r.html.match(/web: "([^"]+)"/);
    expect(web).toBeTruthy();
    const ok = await callJSON('/pair/approve', { method: 'POST', body: { code: start.body.code, web: web[1] } });
    expect(ok.status).toBe(200);
    const tv = await callJSON('/pair/poll', { method: 'POST', body: { code: start.body.code, secret: start.body.secret } });
    expect(tv.body.account.name).toBe('Carol');
  });

  it('lets one TV hold several accounts', async () => {
    fakeOutside({ users });
    const alice = await signedIn('discord', 'alice');
    const carol = await signedIn('google', 'gcarol');
    const tvs = [];
    for (const who of [alice, carol]) {
      const start = await callJSON('/pair/start', { method: 'POST', body: { device: { name: 'Family TV', is_tv: true } } });
      await callJSON('/pair/approve', { method: 'POST', token: who.access_token, body: { code: start.body.code } });
      tvs.push((await callJSON('/pair/poll', { method: 'POST', body: { code: start.body.code, secret: start.body.secret } })).body);
    }
    expect(tvs[0].account.id).toBe(alice.account.id);
    expect(tvs[1].account.id).toBe(carol.account.id);
  });

  it('refuses the wrong secret and expired codes', async () => {
    const start = await callJSON('/pair/start', { method: 'POST', body: { device: { name: 'TV', is_tv: true } } });
    const wrong = await callJSON('/pair/poll', { method: 'POST', body: { code: start.body.code, secret: 'nope' } });
    expect(wrong.status).toBe(404);
    await env.DB.prepare('UPDATE pairings SET expires = 1 WHERE code = ?').bind(start.body.code).run();
    const old = await callJSON('/pair/poll', { method: 'POST', body: { code: start.body.code, secret: start.body.secret } });
    expect(old.status).toBe(410);
  });

  it('does not let a second account take an approved code', async () => {
    fakeOutside({ users });
    const alice = await signedIn('discord', 'alice');
    const carol = await signedIn('google', 'gcarol');
    const start = await callJSON('/pair/start', { method: 'POST', body: { device: { name: 'TV', is_tv: true } } });
    await callJSON('/pair/approve', { method: 'POST', token: alice.access_token, body: { code: start.body.code } });
    const r = await callJSON('/pair/approve', { method: 'POST', token: carol.access_token, body: { code: start.body.code } });
    expect(r.status).toBe(409);
  });
});

describe('devices and account', () => {
  it('renames and removes devices', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    const b = await signedIn('discord', 'alice');
    expect((await call(`/devices/${b.device_id}`, { method: 'PATCH', token: a.access_token, body: { name: 'Kitchen tablet' } })).status).toBe(200);
    let list = await callJSON('/devices', { token: a.access_token });
    expect(list.body.devices.map((d) => d.name)).toContain('Kitchen tablet');
    expect((await call(`/devices/${b.device_id}`, { method: 'DELETE', token: a.access_token })).status).toBe(200);
    list = await callJSON('/devices', { token: a.access_token });
    expect(list.body.devices).toHaveLength(1);
    const r = await callJSON('/token', { method: 'POST', body: { grant_type: 'refresh', refresh_token: b.refresh_token } });
    expect(r.status).toBe(401);
  });

  it('cannot touch another account’s device', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    const c = await signedIn('google', 'gcarol');
    expect((await call(`/devices/${c.device_id}`, { method: 'DELETE', token: a.access_token })).status).toBe(404);
  });

  it('exports and deletes everything', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    await callJSON('/sync', { method: 'POST', token: a.access_token, body: { docs: [{ profile: 'p1', kind: 'settings', fields: { SubOrDub: 'dub' }, times: { SubOrDub: 1000 } }] } });
    const ex = await call('/me/export', { token: a.access_token });
    expect(ex.headers.get('content-disposition')).toContain('otakase-account.json');
    const data = await ex.json();
    expect(data.synced[0].data.SubOrDub).toBe('dub');
    expect((await call('/me', { method: 'DELETE', token: a.access_token })).status).toBe(200);
    for (const t of ['accounts', 'identities', 'devices', 'sessions', 'sync_docs']) {
      const n = await env.DB.prepare(`SELECT COUNT(*) AS n FROM ${t}`).first('n');
      expect(n, t).toBe(0);
    }
  });
});

describe('roles', () => {
  it('keeps admin calls to admins and checks the stored role', async () => {
    fakeOutside({ users });
    const admin = await signedIn('discord', 'admin');
    const alice = await signedIn('discord', 'alice');
    expect((await call('/admin/accounts', { token: alice.access_token })).status).toBe(403);
    const list = await callJSON('/admin/accounts', { token: admin.access_token });
    expect(list.body.accounts).toHaveLength(2);
    expect((await call('/admin/role', { method: 'POST', token: admin.access_token, body: { account: alice.account.id, role: 'tester' } })).status).toBe(200);
    const r = await callJSON('/token', { method: 'POST', body: { grant_type: 'refresh', refresh_token: alice.refresh_token } });
    expect(r.body.account.role).toBe('tester');
    // A token minted while admin stops working for admin calls once demoted.
    await env.DB.prepare("UPDATE accounts SET role = 'user' WHERE id = ?").bind(admin.account.id).run();
    expect((await call('/admin/accounts', { token: admin.access_token })).status).toBe(403);
  });

  it('bans: refresh stops working', async () => {
    fakeOutside({ users });
    const admin = await signedIn('discord', 'admin');
    const alice = await signedIn('discord', 'alice');
    await call('/admin/ban', { method: 'POST', token: admin.access_token, body: { account: alice.account.id, banned: true } });
    const r = await callJSON('/token', { method: 'POST', body: { grant_type: 'refresh', refresh_token: alice.refresh_token } });
    expect(r.status).toBe(401);
    const again = await browserSignIn('discord', 'alice', { challenge: (await pkcePair()).challenge });
    expect(again.status).toBe(403);
  });
});
