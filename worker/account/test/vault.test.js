import { afterEach, describe, expect, it, vi } from 'vitest';
import { call, callJSON, fakeOutside, signedIn } from './helpers.js';

const users = { alice: { id: '222222222222222222', username: 'alice' } };
afterEach(() => vi.restoreAllMocks());

const key = (c) => c.repeat(43);
const wrapped = (c) => c.repeat(60);
const blob = (c) => c.repeat(80);

describe('vault', () => {
  it('starts a vault, approves a second device and seals logins', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice', { public_key: key('A') });
    const b = await signedIn('discord', 'alice', { public_key: key('B') });
    let v = await callJSON('/vault', { token: a.access_token });
    expect(v.body.key_version).toBe(0);
    expect(v.body.wrapped).toBeNull();
    // First device makes the key.
    const start = await callJSON('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, keys: { [a.device_id]: wrapped('a') } } });
    expect(start.status).toBe(200);
    v = await callJSON('/vault', { token: a.access_token });
    expect(v.body.wrapped).toBe(wrapped('a'));
    expect(v.body.waiting).toEqual([{ id: b.device_id, name: expect.any(String), public_key: key('B') }]);
    // b cannot approve itself.
    const self = await callJSON('/vault/keys', { method: 'PUT', token: b.access_token, body: { key_version: 1, keys: { [b.device_id]: wrapped('b') } } });
    expect(self.status).toBe(403);
    // a approves b.
    expect((await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, keys: { [b.device_id]: wrapped('b') } } })).status).toBe(200);
    expect((await callJSON('/vault', { token: b.access_token })).body.wrapped).toBe(wrapped('b'));
    // Sealed logins with optimistic versions.
    const first = await callJSON('/vault/blob', { method: 'PUT', token: a.access_token, body: { profile: 'p1', base_version: 0, key_version: 1, ciphertext: blob('x') } });
    expect(first.body.version).toBe(1);
    const clash = await callJSON('/vault/blob', { method: 'PUT', token: b.access_token, body: { profile: 'p1', base_version: 0, key_version: 1, ciphertext: blob('y') } });
    expect(clash.status).toBe(409);
    expect(clash.body.current.ciphertext).toBe(blob('x'));
    const second = await callJSON('/vault/blob', { method: 'PUT', token: b.access_token, body: { profile: 'p1', base_version: 1, key_version: 1, ciphertext: blob('y') } });
    expect(second.body.version).toBe(2);
    v = await callJSON('/vault', { token: a.access_token });
    expect(v.body.blobs).toEqual([{ profile: 'p1', version: 2, key_version: 1, ciphertext: blob('y') }]);
  });

  it('marks the vault stale when a device goes and takes a rotation', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice', { public_key: key('A') });
    const b = await signedIn('discord', 'alice', { public_key: key('B') });
    await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, keys: { [a.device_id]: wrapped('a'), [b.device_id]: wrapped('b') } } });
    await call(`/devices/${b.device_id}`, { method: 'DELETE', token: a.access_token });
    let v = await callJSON('/vault', { token: a.access_token });
    expect(v.body.stale).toBe(true);
    // Old key version refused for blobs after rotation.
    const rot = await callJSON('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 2, rotate: true, keys: { [a.device_id]: wrapped('c') } } });
    expect(rot.status).toBe(200);
    v = await callJSON('/vault', { token: a.access_token });
    expect(v.body).toMatchObject({ key_version: 2, stale: false, wrapped: wrapped('c') });
    const old = await callJSON('/vault/blob', { method: 'PUT', token: a.access_token, body: { profile: 'p', base_version: 0, key_version: 1, ciphertext: blob('z') } });
    expect(old.status).toBe(409);
    // A second rotation to the same version loses.
    const late = await callJSON('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 2, rotate: true, keys: { [a.device_id]: wrapped('d') } } });
    expect(late.status).toBe(409);
  });

  it('refuses keys for devices of another account', async () => {
    fakeOutside({ users: { ...users, bob: { id: '333333333333333333', username: 'bob' } } });
    const a = await signedIn('discord', 'alice', { public_key: key('A') });
    const bob = await signedIn('discord', 'bob', { public_key: key('B') });
    const r = await callJSON('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, keys: { [a.device_id]: wrapped('a'), [bob.device_id]: wrapped('b') } } });
    expect(r.status).toBe(400);
  });
});
