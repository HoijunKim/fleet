CREATE TABLE IF NOT EXISTS users (
    id         uuid PRIMARY KEY,
    github_id  bigint UNIQUE NOT NULL,
    login      text NOT NULL,
    email      text NOT NULL DEFAULT '',
    avatar_url text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id),
    token_hash text NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked    boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS refresh_tokens_hash_idx ON refresh_tokens(token_hash);

CREATE TABLE IF NOT EXISTS documents (
    user_id    uuid NOT NULL REFERENCES users(id),
    kind       text NOT NULL,
    doc_id     text NOT NULL,
    payload    jsonb NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted    boolean NOT NULL DEFAULT false,
    version    bigint NOT NULL,
    PRIMARY KEY (user_id, kind, doc_id)
);
CREATE INDEX IF NOT EXISTS documents_user_version_idx ON documents(user_id, version);

CREATE TABLE IF NOT EXISTS user_versions (
    user_id uuid PRIMARY KEY REFERENCES users(id),
    current bigint NOT NULL DEFAULT 0
);
