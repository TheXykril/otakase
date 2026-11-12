-- Otakase accounts. Every row belongs to one account and goes when the
-- account is deleted.

CREATE TABLE accounts (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  avatar TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT 'user',
  sync_version INTEGER NOT NULL DEFAULT 0,
  bytes INTEGER NOT NULL DEFAULT 0,
  vault_key_version INTEGER NOT NULL DEFAULT 0,
  vault_stale INTEGER NOT NULL DEFAULT 0,
  banned INTEGER NOT NULL DEFAULT 0,
  created INTEGER NOT NULL
);

-- Discord and Google logins tied to an account.
CREATE TABLE identities (
  provider TEXT NOT NULL,
  provider_user_id TEXT NOT NULL,
  account_id TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  created INTEGER NOT NULL,
  PRIMARY KEY (provider, provider_user_id)
);
CREATE INDEX identities_account ON identities (account_id);

CREATE TABLE devices (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  name TEXT NOT NULL,
  os TEXT NOT NULL DEFAULT '',
  is_tv INTEGER NOT NULL DEFAULT 0,
  public_key TEXT NOT NULL DEFAULT '',
  app_version TEXT NOT NULL DEFAULT '',
  created INTEGER NOT NULL,
  last_seen INTEGER NOT NULL
);
CREATE INDEX devices_account ON devices (account_id);

-- Refresh tokens, stored hashed and rotated on every use.
CREATE TABLE sessions (
  refresh_hash TEXT PRIMARY KEY,
  device_id TEXT NOT NULL,
  account_id TEXT NOT NULL,
  expires INTEGER NOT NULL
);
CREATE INDEX sessions_device ON sessions (device_id);
CREATE INDEX sessions_account ON sessions (account_id);

-- Synced data: one JSON document per profile and kind, with a time per
-- field so the newer change wins field by field. profile_id is '' for the
-- account itself.
CREATE TABLE sync_docs (
  account_id TEXT NOT NULL,
  profile_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  body TEXT NOT NULL,
  times TEXT NOT NULL,
  version INTEGER NOT NULL,
  updated INTEGER NOT NULL,
  PRIMARY KEY (account_id, profile_id, kind)
);
CREATE INDEX sync_docs_version ON sync_docs (account_id, version);

-- Tracker logins, encrypted on the device. The server cannot read them.
CREATE TABLE vault (
  account_id TEXT NOT NULL,
  profile_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  key_version INTEGER NOT NULL,
  ciphertext TEXT NOT NULL,
  updated INTEGER NOT NULL,
  PRIMARY KEY (account_id, profile_id)
);

-- The account's vault key, wrapped for each device with that device's
-- public key.
CREATE TABLE vault_keys (
  account_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  key_version INTEGER NOT NULL,
  wrapped TEXT NOT NULL,
  PRIMARY KEY (account_id, device_id)
);

-- QR sign-in for TVs and new devices. Codes live two minutes.
CREATE TABLE pairings (
  code TEXT PRIMARY KEY,
  secret_hash TEXT NOT NULL,
  device TEXT NOT NULL,
  account_id TEXT,
  expires INTEGER NOT NULL
);

-- Messages for the Discord bot that could not be delivered while it was
-- offline (support posts, source reports). The bot collects them. body is
-- base64.
CREATE TABLE outbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  discord TEXT NOT NULL,
  device TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL,
  created INTEGER NOT NULL
);

-- Last news answer from the bot, served when the bot is offline.
CREATE TABLE news (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  body TEXT NOT NULL,
  updated INTEGER NOT NULL
);
