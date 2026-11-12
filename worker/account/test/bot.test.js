import { env } from 'cloudflare:test';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { call, callJSON, fakeOutside, signedIn } from './helpers.js';

const users = {
  alice: { id: '222222222222222222', username: 'alice' },
  gcarol: { sub: 'g-2', name: 'Carol', email: 'carol@example.com' },
};
const bot = { authorization: 'Bearer bot-secret' };
afterEach(() => vi.restoreAllMocks());

describe('calls passed to the bot', () => {
  it('sends a rating as the Discord member', async () => {
    let seen;
    fakeOutside({ users, bot: async (request, url) => {
      seen = { path: url.pathname, user: request.headers.get('x-discord-user'), device: request.headers.get('x-otakase-device'), body: await request.text() };
      return Response.json({ url: 'https://discord.com/channels/1/2/3' });
    } });
    const a = await signedIn('discord', 'alice');
    const r = await callJSON('/v1/rating', { method: 'POST', token: a.access_token, body: { anilist_id: 1, score: 9 } });
    expect(r.body.url).toContain('discord.com');
    expect(seen).toEqual({ path: '/v1/rating', user: '222222222222222222', device: a.device_id, body: '{"anilist_id":1,"score":9}' });
  });

  it('needs Discord on the account', async () => {
    fakeOutside({ users });
    const c = await signedIn('google', 'gcarol');
    expect((await call('/v1/rating', { method: 'POST', token: c.access_token, body: {} })).status).toBe(403);
  });

  it('only passes known calls', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    expect((await call('/v1/backup', { method: 'POST', token: a.access_token, body: {} })).status).toBe(404);
  });

  it('queues support posts while the bot is offline and hands them over later', async () => {
    fakeOutside({ users, bot: async () => new Response('down', { status: 502 }) });
    const a = await signedIn('discord', 'alice');
    const r = await callJSON('/v1/provider-report', { method: 'POST', token: a.access_token, body: { source: 'Source 1' } });
    expect(r.status).toBe(202);
    const rating = await call('/v1/rating', { method: 'POST', token: a.access_token, body: {} });
    expect(rating.status).toBe(502);
    expect((await call('/bot/outbox')).status).toBe(401);
    const box = await callJSON('/bot/outbox', { headers: bot });
    expect(box.body.items).toHaveLength(1);
    expect(box.body.items[0]).toMatchObject({ method: 'POST', path: '/v1/provider-report', discord: '222222222222222222', body: '{"source":"Source 1"}' });
    await call(`/bot/outbox?ids=${box.body.items[0].id}`, { method: 'DELETE', headers: bot });
    expect((await callJSON('/bot/outbox', { headers: bot })).body.items).toHaveLength(0);
  });
});

describe('news', () => {
  it('serves the bot news and keeps a copy for when it is offline', async () => {
    let up = true;
    fakeOutside({ users, bot: async () => (up ? Response.json({ items: [{ title: 'Hello' }] }) : new Response('', { status: 502 })) });
    const first = await callJSON('/v1/news');
    expect(first.body.items[0].title).toBe('Hello');
    up = false;
    await caches.default.delete('https://account.otakase.com/v1/news');
    const second = await callJSON('/v1/news');
    expect(second.body.items[0].title).toBe('Hello');
  });
});

describe('bot lookups', () => {
  it('finds the account of a Discord member and sets the tester role', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    const r = await callJSON('/bot/account?discord=222222222222222222', { headers: bot });
    expect(r.body.account.id).toBe(a.account.id);
    expect(r.body.devices).toHaveLength(1);
    expect((await callJSON('/bot/account?discord=999999999', { headers: bot })).body.account).toBeNull();
    await call('/bot/role', { method: 'POST', headers: bot, body: { discord: '222222222222222222', role: 'tester' } });
    expect(await env.DB.prepare('SELECT role FROM accounts').first('role')).toBe('tester');
    expect((await call('/bot/role', { method: 'POST', headers: bot, body: { discord: '222222222222222222', role: 'admin' } })).status).toBe(400);
    const stats = await callJSON('/bot/stats', { headers: bot });
    expect(stats.body).toMatchObject({ accounts: 1, devices: 1, active_devices_7d: 1 });
  });
});

describe('backup', () => {
  it('pages through tables with the backup token only', async () => {
    fakeOutside({ users });
    await signedIn('discord', 'alice');
    expect((await call('/backup?table=accounts', { headers: bot })).status).toBe(401);
    const list = await callJSON('/backup', { headers: { authorization: 'Bearer backup-token' } });
    expect(list.body.tables).toContain('accounts');
    expect(list.body.tables).not.toContain('sessions');
    const page = await callJSON('/backup?table=accounts', { headers: { authorization: 'Bearer backup-token' } });
    expect(page.body.rows).toHaveLength(1);
    expect(page.body.next).toBeNull();
  });
});
