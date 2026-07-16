-- One-time, short-lived state for GitHub App installation redirects. Only a
-- SHA-256 digest is stored so a database read cannot mint a valid callback.
CREATE TABLE IF NOT EXISTS github_install_sessions (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    token_hash      TEXT NOT NULL UNIQUE,
    workspace_id    UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at      TIMESTAMPTZ NOT NULL,
    consumed_at     TIMESTAMPTZ,
    installation_id BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS idx_github_install_sessions_expiry
    ON github_install_sessions(expires_at)
    WHERE consumed_at IS NULL;

DO $$ BEGIN
    RAISE NOTICE 'OpsPilot secure GitHub installation sessions migration complete';
END $$;
