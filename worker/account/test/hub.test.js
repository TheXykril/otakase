import { afterEach, describe, expect, it, vi } from 'vitest';
import { call, callJSON, fakeOutside, signedIn } from './helpers.js';

const users = { alice: { id: '222222222222222222', username: 'alice' } };
afterEach(() => vi.restoreAllMocks());

/** Opens /live and collects messages. */
async function live(token, viaQuery = false) {
  const r = await call(viaQuery ? `/live?access=${token}` : '/live', { token: viaQuery ? undefined : token, headers: { upgrade: 'websocket' } });
  expect(r.status).toBe(101);
  const ws = r.webSocket;
  const messages = [];
  const waiters = [];
  ws.addEventListener('message', (e) => {
    const m = JSON.parse(e.data);
    messages.push(m);
    for (const w of [...waiters]) if (w.test(m)) { waiters.splice(waiters.indexOf(w), 1); w.resolve(m); }
  });
  ws.accept();
  return {
    ws,
    messages,
    send: (m) => ws.send(JSON.stringify(m)),
    next(test) {
      const found = messages.find(test);
      if (found) { messages.splice(messages.indexOf(found), 1); return Promise.resolve(found); }
      return new Promise((resolve, reject) => {
        const w = { test, resolve: (m) => { messages.splice(messages.indexOf(m), 1); resolve(m); } };
        waiters.push(w);
        setTimeout(() => reject(new Error('no message')), 2000);
      });
    },
  };
}

describe('live hub', () => {
  it('refuses without a token or upgrade', async () => {
    expect((await call('/live', { headers: { upgrade: 'websocket' } })).status).toBe(401);
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    expect((await call('/live', { token: a.access_token })).status).toBe(426);
  });

  it('carries remote messages between devices and tells about presence', async () => {
    fakeOutside({ users });
    const phone = await signedIn('discord', 'alice');
    const tv = await signedIn('discord', 'alice', { name: 'TV', is_tv: true });
    const t = await live(tv.access_token, true);
    const hello = await t.next((m) => m.t === 'hello');
    expect(hello.device).toBe(tv.device_id);
    const p = await live(phone.access_token);
    const presence = await t.next((m) => m.t === 'presence');
    expect(presence.online.map((d) => d.device).sort()).toEqual([phone.device_id, tv.device_id].sort());
    p.send({ t: 'to', to: tv.device_id, id: 'c1', m: { type: 'control', action: 'pause' } });
    const got = await t.next((m) => m.t === 'msg');
    expect(got).toMatchObject({ from: phone.device_id, id: 'c1', m: { type: 'control', action: 'pause' } });
    p.send({ t: 'to', to: 'nobody', id: 'c2', m: {} });
    expect(await p.next((m) => m.t === 'offline')).toMatchObject({ to: 'nobody', id: 'c2' });
    t.ws.close();
    p.ws.close();
  });

  it('pings other devices when synced data changes', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    const b = await signedIn('discord', 'alice');
    const lb = await live(b.access_token);
    await callJSON('/sync', { method: 'POST', token: a.access_token, body: { docs: [{ profile: 'p', kind: 'settings', fields: { x: 1 }, times: { x: 1 } }] } });
    expect(await lb.next((m) => m.t === 'sync')).toMatchObject({ v: 1, from: a.device_id });
    lb.ws.close();
  });

  it('closes the socket of a removed device', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    const b = await signedIn('discord', 'alice');
    const lb = await live(b.access_token);
    const closed = new Promise((resolve) => lb.ws.addEventListener('close', (e) => resolve(e.code)));
    await call(`/devices/${b.device_id}`, { method: 'DELETE', token: a.access_token });
    expect(await closed).toBe(4001);
  });

  it('keeps inbox items from the bot until a device takes them', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    const put = await callJSON('/bot/inbox', { method: 'POST', headers: { authorization: 'Bearer bot-secret' }, body: { discord: '222222222222222222', item: { kind: 'airing', title: 'Episode 5 is out' } } });
    expect(put.body).toMatchObject({ account: true, delivered: 0 });
    const l = await live(a.access_token);
    const box = await l.next((m) => m.t === 'inbox');
    expect(box.items[0].title).toBe('Episode 5 is out');
    l.send({ t: 'ack', ids: [box.items[0].id] });
    l.send({ t: 'presence' });
    await l.next((m) => m.t === 'presence');
    l.ws.close();
    const l2 = await live(a.access_token);
    await l2.next((m) => m.t === 'hello');
    l2.send({ t: 'presence' });
    await l2.next((m) => m.t === 'presence');
    expect(l2.messages.find((m) => m.t === 'inbox')).toBeUndefined();
    l2.ws.close();
  });

  it('passes heartbeats to the bot as the Discord member', async () => {
    const beats = [];
    fakeOutside({
      users,
      bot: async (request, url) => {
        beats.push({ path: url.pathname, user: request.headers.get('x-discord-user'), secret: request.headers.get('x-otakase-account'), body: await request.json() });
        return Response.json({ ok: true });
      },
    });
    const a = await signedIn('discord', 'alice');
    const l = await live(a.access_token);
    l.send({ t: 'hb', body: { version: '26.5.0', os: 'android', watched_seconds: 60 } });
    l.send({ t: 'presence' });
    await l.next((m) => m.t === 'presence');
    expect(beats).toEqual([{ path: '/v1/heartbeat', user: '222222222222222222', secret: 'bot-secret', body: { version: '26.5.0', os: 'android', watched_seconds: 60 } }]);
    l.ws.close();
  });
});
