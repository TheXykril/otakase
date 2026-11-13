-- The server's own copy of the vault key, sealed with the VAULT_SECRET
-- Worker secret. A device of the same account that signs in gets the key
-- from it without another device approving it. Replaced keys clear it.
ALTER TABLE accounts ADD COLUMN vault_server TEXT;
