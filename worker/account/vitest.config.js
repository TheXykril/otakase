import { cloudflareTest, readD1Migrations } from '@cloudflare/vitest-plugin';
import { defineConfig } from 'vitest/config';

export default defineConfig(async () => {
  const migrations = await readD1Migrations('./migrations');
  return {
    plugins: [
      cloudflareTest({
        wrangler: { configPath: './wrangler.jsonc' },
        miniflare: {
          bindings: {
            TEST_MIGRATIONS: migrations,
            JWT_SECRET: 'test-secret-test-secret-test-secret',
            DISCORD_CLIENT_ID: 'discord-client',
            DISCORD_CLIENT_SECRET: 'discord-secret',
            GOOGLE_CLIENT_ID: 'google-client',
            GOOGLE_CLIENT_SECRET: 'google-secret',
            TURNSTILE_SITE_KEY: '1x00000000000000000000AA',
            TURNSTILE_SECRET: '1x0000000000000000000000000000000AA',
            BOT_URL: 'https://bot.test',
            BOT_SECRET: 'bot-secret',
            BACKUP_TOKEN: 'backup-token',
            ADMIN_DISCORD_ID: '111111111111111111',
            VAULT_SECRET: 'vault-secret-vault-secret-vault-secret',
          },
        },
      }),
    ],
    test: { setupFiles: ['./test/setup.js'] },
  };
});
