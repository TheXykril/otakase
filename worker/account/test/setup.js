import { applyD1Migrations, env } from 'cloudflare:test';
import { beforeEach } from 'vitest';

await applyD1Migrations(env.DB, env.TEST_MIGRATIONS);

// Every test starts with an empty database.
beforeEach(async () => {
  const tables = ['accounts', 'identities', 'devices', 'sessions', 'sync_docs', 'vault', 'vault_keys', 'pairings', 'outbox', 'news'];
  await env.DB.batch(tables.map((t) => env.DB.prepare(`DELETE FROM ${t}`)));
});
