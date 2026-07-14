-- Index expires_at so the periodic GC delete (WHERE expires_at < now()) is an
-- index scan rather than a full-table scan.
CREATE INDEX IF NOT EXISTS refresh_tokens_expires_idx ON refresh_tokens(expires_at);
