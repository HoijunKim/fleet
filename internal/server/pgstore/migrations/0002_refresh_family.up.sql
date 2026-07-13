-- Add a rotation-chain family id so reuse detection can revoke a whole lineage.
-- Backfill each existing token as its own single-member family (its true lineage
-- is unknown), then enforce NOT NULL. gen_random_uuid() is built in on PG13+.
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS family_id uuid;
UPDATE refresh_tokens SET family_id = gen_random_uuid() WHERE family_id IS NULL;
ALTER TABLE refresh_tokens ALTER COLUMN family_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS refresh_tokens_family_idx ON refresh_tokens(family_id);
