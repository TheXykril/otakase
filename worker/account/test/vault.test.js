import { afterEach, describe, expect, it, vi } from 'vitest';
import { env } from 'cloudflare:test';
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
  it('lets a device that signed out get its key back with the check', async () => {
    fakeOutside({ users });
    const check = 'C'.repeat(43);
    const a = await signedIn('discord', 'alice', { public_key: key('A') });
    await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, check, keys: { [a.device_id]: wrapped('a') } } });
    expect((await callJSON('/vault', { token: a.access_token })).body.has_check).toBe(true);
    // Signing out keeps the key: nobody has to replace it.
    expect((await call('/logout', { method: 'POST', token: a.access_token })).status).toBe(200);
    const again = await signedIn('discord', 'alice', { public_key: key('B') });
    let v = await callJSON('/vault', { token: again.access_token });
    expect(v.body).toMatchObject({ key_version: 1, stale: false, wrapped: null });
    // A wrong check is refused, the right one stores the new copy.
    const wrong = await callJSON('/vault/restore', { method: 'POST', token: again.access_token, body: { key_version: 1, check: 'D'.repeat(43), wrapped: wrapped('b') } });
    expect(wrong.status).toBe(403);
    const right = await callJSON('/vault/restore', { method: 'POST', token: again.access_token, body: { key_version: 1, check, wrapped: wrapped('b') } });
    expect(right.status).toBe(200);
    v = await callJSON('/vault', { token: again.access_token });
    expect(v.body.wrapped).toBe(wrapped('b'));
  });

  it('refuses an old key after the device was removed elsewhere', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice', { public_key: key('A') });
    const b = await signedIn('discord', 'alice', { public_key: key('B') });
    await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, check: 'C'.repeat(43), keys: { [a.device_id]: wrapped('a'), [b.device_id]: wrapped('b') } } });
    await call(`/devices/${b.device_id}`, { method: 'DELETE', token: a.access_token });
    expect((await callJSON('/vault', { token: a.access_token })).body.stale).toBe(true);
    await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 2, rotate: true, check: 'E'.repeat(43), keys: { [a.device_id]: wrapped('c') } } });
    const back = await signedIn('discord', 'alice', { public_key: key('B') });
    const old = await callJSON('/vault/restore', { method: 'POST', token: back.access_token, body: { key_version: 1, check: 'C'.repeat(43), wrapped: wrapped('b') } });
    expect(old.status).toBe(409);
    const guess = await callJSON('/vault/restore', { method: 'POST', token: back.access_token, body: { key_version: 2, check: 'C'.repeat(43), wrapped: wrapped('b') } });
    expect(guess.status).toBe(403);
  });

  it('takes a check only from a device holding the key, once', async () => {
    fakeOutside({ users });
    const a = await signedIn('discord', 'alice', { public_key: key('A') });
    const b = await signedIn('discord', 'alice', { public_key: key('B') });
    await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, keys: { [a.device_id]: wrapped('a') } } });
    expect((await callJSON('/vault', { token: a.access_token })).body.has_check).toBe(false);
    expect((await call('/vault/check', { method: 'PUT', token: b.access_token, body: { key_version: 1, check: 'B'.repeat(43) } })).status).toBe(403);
    expect((await call('/vault/check', { method: 'PUT', token: a.access_token, body: { key_version: 1, check: 'C'.repeat(43) } })).status).toBe(200);
    await call('/vault/check', { method: 'PUT', token: a.access_token, body: { key_version: 1, check: 'D'.repeat(43) } });
    const r = await callJSON('/vault/restore', { method: 'POST', token: b.access_token, body: { key_version: 1, check: 'C'.repeat(43), wrapped: wrapped('b') } });
    expect(r.status).toBe(200);
  });

  describe('server copy', () => {
    // A real key and its check, the way the app makes them.
    async function realKey() {
      const raw = crypto.getRandomValues(new Uint8Array(32));
      const mac = await crypto.subtle.importKey('raw', raw, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
      const b64 = (b) => btoa(String.fromCharCode(...new Uint8Array(b))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
      return { key: b64(raw), check: b64(await crypto.subtle.sign('HMAC', mac, new TextEncoder().encode('otakase vault check v1'))) };
    }

    it('gives a new device of the same account the key, no approval', async () => {
      fakeOutside({ users });
      const { key: vk, check } = await realKey();
      const a = await signedIn('discord', 'alice', { public_key: key('A') });
      await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, check, keys: { [a.device_id]: wrapped('a') } } });
      expect((await callJSON('/vault', { token: a.access_token })).body.server).toBe('missing');
      expect((await call('/vault/server', { method: 'PUT', token: a.access_token, body: { key_version: 1, key: vk } })).status).toBe(200);
      const raw = await env.DB.prepare('SELECT vault_server FROM accounts').first('vault_server');
      expect(raw).not.toContain(vk);
      // The device that already has its copy is not sent the key again.
      let v = await callJSON('/vault', { token: a.access_token });
      expect(v.body.server).toBe('ready');
      expect(v.body.server_key).toBeUndefined();
      const b = await signedIn('discord', 'alice', { public_key: key('B') });
      v = await callJSON('/vault', { token: b.access_token });
      expect(v.body.server_key).toBe(vk);
      expect((await call('/vault/restore', { method: 'POST', token: b.access_token, body: { key_version: 1, check, wrapped: wrapped('b') } })).status).toBe(200);
      expect((await callJSON('/vault', { token: b.access_token })).body.server_key).toBeUndefined();
    });

    it('never gives the key to another account', async () => {
      fakeOutside({ users: { ...users, bob: { id: '333333333333333333', username: 'bob' } } });
      const { key: vk, check } = await realKey();
      const a = await signedIn('discord', 'alice', { public_key: key('A') });
      await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, check, keys: { [a.device_id]: wrapped('a') } } });
      await call('/vault/server', { method: 'PUT', token: a.access_token, body: { key_version: 1, key: vk } });
      const bob = await signedIn('discord', 'bob', { public_key: key('B') });
      const v = await callJSON('/vault', { token: bob.access_token });
      expect(v.body.key_version).toBe(0);
      expect(v.body.server_key).toBeUndefined();
      expect(JSON.stringify(v.body)).not.toContain(vk);
      // Another account's sealed copy does not open as this one's.
      const sealed = await env.DB.prepare('SELECT vault_server FROM accounts WHERE vault_server IS NOT NULL').first('vault_server');
      await env.DB.prepare('UPDATE accounts SET vault_key_version = 1, vault_server = ? WHERE vault_server IS NULL').bind(sealed).run();
      const stolen = await callJSON('/vault', { token: bob.access_token });
      expect(stolen.body.server).toBe('missing');
      expect(stolen.body.server_key).toBeUndefined();
    });

    it('takes only the real key, from a device holding it', async () => {
      fakeOutside({ users });
      const { key: vk, check } = await realKey();
      const other = await realKey();
      const a = await signedIn('discord', 'alice', { public_key: key('A') });
      const b = await signedIn('discord', 'alice', { public_key: key('B') });
      await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, check, keys: { [a.device_id]: wrapped('a') } } });
      expect((await call('/vault/server', { method: 'PUT', token: b.access_token, body: { key_version: 1, key: vk } })).status).toBe(403);
      expect((await call('/vault/server', { method: 'PUT', token: a.access_token, body: { key_version: 1, key: other.key } })).status).toBe(403);
      expect((await call('/vault/server', { method: 'PUT', token: a.access_token, body: { key_version: 2, key: vk } })).status).toBe(409);
      expect((await call('/vault/server', { method: 'PUT', token: a.access_token, body: { key_version: 1, key: 'short' } })).status).toBe(400);
    });

    it('drops the copy when the key is replaced, and a removed device gets nothing', async () => {
      fakeOutside({ users });
      const first = await realKey();
      const a = await signedIn('discord', 'alice', { public_key: key('A') });
      const b = await signedIn('discord', 'alice', { public_key: key('B') });
      await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, check: first.check, keys: { [a.device_id]: wrapped('a') } } });
      await call('/vault/server', { method: 'PUT', token: a.access_token, body: { key_version: 1, key: first.key } });
      await call(`/devices/${b.device_id}`, { method: 'DELETE', token: a.access_token });
      // b's access token still verifies for a while, but b is gone.
      expect((await callJSON('/vault', { token: b.access_token })).body.server_key).toBeUndefined();
      const second = await realKey();
      await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 2, rotate: true, check: second.check, keys: { [a.device_id]: wrapped('c') } } });
      expect((await callJSON('/vault', { token: a.access_token })).body.server).toBe('missing');
      await call('/vault/server', { method: 'PUT', token: a.access_token, body: { key_version: 2, key: second.key } });
      const c = await signedIn('discord', 'alice', { public_key: key('C') });
      expect((await callJSON('/vault', { token: c.access_token })).body.server_key).toBe(second.key);
    });

    it('is off without VAULT_SECRET', async () => {
      fakeOutside({ users });
      const secret = env.VAULT_SECRET;
      env.VAULT_SECRET = '';
      try {
        const { key: vk, check } = await realKey();
        const a = await signedIn('discord', 'alice', { public_key: key('A') });
        await call('/vault/keys', { method: 'PUT', token: a.access_token, body: { key_version: 1, rotate: true, check, keys: { [a.device_id]: wrapped('a') } } });
        expect((await callJSON('/vault', { token: a.access_token })).body.server).toBe('off');
        expect((await call('/vault/server', { method: 'PUT', token: a.access_token, body: { key_version: 1, key: vk } })).status).toBe(503);
      } finally {
        env.VAULT_SECRET = secret;
      }
    });
  });
});
