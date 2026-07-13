DROP INDEX IF EXISTS refresh_tokens_family_idx;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS family_id;
