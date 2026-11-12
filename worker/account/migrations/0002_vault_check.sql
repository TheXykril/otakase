-- A keyed hash of the current vault key (never the key itself). A device
-- that was signed out but kept the key proves it with this to get its copy
-- back without another device approving it.
ALTER TABLE accounts ADD COLUMN vault_check TEXT;
