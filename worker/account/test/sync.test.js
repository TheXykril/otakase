import { afterEach, describe, expect, it, vi } from 'vitest';
import { callJSON, fakeOutside, signedIn } from './helpers.js';
import { mergeDoc, cleanDoc } from '../src/sync.js';

const users = { alice: { id: '222222222222222222', username: 'alice' } };
afterEach(() => vi.restoreAllMocks());

const doc = (profile, kind, fields, t) => ({
  profile, kind, fields, times: Object.fromEntries(Object.keys(fields).map((k) => [k, t])),
});

describe('merge', () => {
  it('keeps the newer field and tombstones', () => {
    const stored = { body: { a: 1, b: 2 }, times: { a: 10, b: 10 } };
    const m = mergeDoc(stored, { fields: { a: 5, b: null, c: 3 }, times: { a: 5, b: 20, c: 1 } });
    expect(m.body).toEqual({ a: 1, b: null, c: 3 });
    expect(m.times).toEqual({ a: 10, b: 20, c: 1 });
    expect(m.changed).toBe(true);
    expect(mergeDoc(m, { fields: { a: 0 }, times: { a: 10 } }).changed).toBe(false);
  });

  it('refuses bad documents', () => {
    expect(() => cleanDoc({ profile: 'p', kind: 'Bad Kind', fields: {}, times: {} })).toThrow();
    expect(() => cleanDoc({ profile: 'p', kind: 'settings', fields: { a: 1 }, times: {} })).toThrow();
    expect(() => cleanDoc({ profile: '../x', kind: 'settings', fields: {}, times: {} })).toThrow();
  });

  it('pulls a clock far in the future back', () => {
    const d = cleanDoc({ profile: 'p', kind: 'settings', fields: { a: 1 }, times: { a: Date.now() + 1e12 } });
    expect(d.times.a).toBeLessThan(Date.now() + 2 * 24 * 3600 * 1000);
  });
});

describe('sync', () => {
  it('pushes, pulls changes since a version and answers cheaply when nothing changed', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    const b = await signedIn('discord', 'alice');
    const empty = await callJSON('/sync?since=0', { token: b.access_token });
    expect(empty.body).toEqual({ version: 0, docs: [] });
    const push = await callJSON('/sync', {
      method: 'POST', token: a.access_token,
      body: { docs: [doc('p1', 'settings', { SubOrDub: 'dub', Quality: '1080' }, 100), doc('p1', 'profile', { name: 'Ana', order: 0 }, 100)] },
    });
    expect(push.body).toEqual({ version: 1, changed: 2 });
    const pulled = await callJSON('/sync?since=0', { token: b.access_token });
    expect(pulled.body.version).toBe(1);
    expect(pulled.body.docs).toHaveLength(2);
    expect(pulled.body.docs.find((d) => d.kind === 'settings').fields.SubOrDub).toBe('dub');
    const none = await callJSON('/sync?since=1', { token: b.access_token });
    expect(none.body).toEqual({ version: 1, docs: [] });
    // b changes one field, older change from a loses.
    await callJSON('/sync', { method: 'POST', token: b.access_token, body: { docs: [doc('p1', 'settings', { Quality: '720' }, 200)] } });
    const stale = await callJSON('/sync', { method: 'POST', token: a.access_token, body: { docs: [doc('p1', 'settings', { Quality: 'best' }, 150)] } });
    expect(stale.body).toEqual({ version: 2, changed: 0 });
    const since1 = await callJSON('/sync?since=1', { token: a.access_token });
    expect(since1.body.docs).toHaveLength(1);
    expect(since1.body.docs[0].fields).toEqual({ SubOrDub: 'dub', Quality: '720' });
  });

  it('caps profiles at six', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    const docs = Array.from({ length: 6 }, (_, i) => doc(`p${i}`, 'profile', { name: `P${i}` }, 10));
    expect((await callJSON('/sync', { method: 'POST', token: a.access_token, body: { docs } })).status).toBe(200);
    const seventh = await callJSON('/sync', { method: 'POST', token: a.access_token, body: { docs: [doc('p6', 'profile', { name: 'P6' }, 10)] } });
    expect(seventh.status).toBe(409);
    // Deleting one frees a place.
    await callJSON('/sync', { method: 'POST', token: a.access_token, body: { docs: [doc('p0', 'profile', { _deleted: true }, 20)] } });
    const again = await callJSON('/sync', { method: 'POST', token: a.access_token, body: { docs: [doc('p6', 'profile', { name: 'P6' }, 30)] } });
    expect(again.status).toBe(200);
  });

  it('refuses a document over 64 KB', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice');
    const r = await callJSON('/sync', { method: 'POST', token: a.access_token, body: { docs: [doc('p', 'shows', { big: 'x'.repeat(70 * 1024) }, 1)] } });
    expect(r.status).toBe(413);
  });

  it('keeps accounts apart', async () => {
    fakeOutside({ users: { ...users, bob: { id: '333333333333333333', username: 'bob' } } });
    const a = await signedIn('discord', 'alice');
    const b = await signedIn('discord', 'bob');
    await callJSON('/sync', { method: 'POST', token: a.access_token, body: { docs: [doc('p', 'settings', { x: 1 }, 1)] } });
    expect((await callJSON('/sync?since=0', { token: b.access_token })).body.docs).toHaveLength(0);
  });
});
