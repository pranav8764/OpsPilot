-- ============================================================
-- OpsPilot - GitHub integration and ingestion foundation
-- Migration: 002_github_ingestion_foundation.sql
-- ============================================================

-- Generic integration records for external systems such as GitHub,
-- Vercel, Railway, and future providers. Credentials must be encrypted
-- before being stored here.
CREATE TABLE IF NOT EXISTS integrations (
    id                      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id            UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider                TEXT NOT NULL,
    auth_type               TEXT NOT NULL,
    status                  TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled', 'revoked', 'error')),
    external_account_id     TEXT,
    external_account_name   TEXT,
    encrypted_credentials   TEXT,
    scopes                  TEXT[] DEFAULT '{}',
    metadata                JSONB DEFAULT '{}',
    created_by              UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at              TIMESTAMPTZ DEFAULT NOW(),
    updated_at              TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (workspace_id, provider, external_account_id)
);

CREATE INDEX IF NOT EXISTS idx_integrations_workspace_id ON integrations(workspace_id);
CREATE INDEX IF NOT EXISTS idx_integrations_provider ON integrations(provider);
CREATE INDEX IF NOT EXISTS idx_integrations_status ON integrations(status);

DROP TRIGGER IF EXISTS update_integrations_updated_at ON integrations;
CREATE TRIGGER update_integrations_updated_at
    BEFORE UPDATE ON integrations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- GitHub App installations linked to a workspace. Store the stable
-- installation ID and account/repository-selection metadata, not
-- short-lived installation access tokens.
CREATE TABLE IF NOT EXISTS github_installations (
    id                      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id            UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    integration_id          UUID REFERENCES integrations(id) ON DELETE SET NULL,
    installation_id         BIGINT NOT NULL UNIQUE,
    account_id              BIGINT,
    account_login           TEXT NOT NULL,
    account_type            TEXT,
    target_type             TEXT,
    repository_selection    TEXT,
    permissions             JSONB DEFAULT '{}',
    status                  TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended', 'deleted', 'revoked', 'error')),
    installed_by            UUID REFERENCES users(id) ON DELETE SET NULL,
    installed_at            TIMESTAMPTZ,
    suspended_at            TIMESTAMPTZ,
    created_at              TIMESTAMPTZ DEFAULT NOW(),
    updated_at              TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_github_installations_workspace_id ON github_installations(workspace_id);
CREATE INDEX IF NOT EXISTS idx_github_installations_account_login ON github_installations(account_login);
CREATE INDEX IF NOT EXISTS idx_github_installations_status ON github_installations(status);

DROP TRIGGER IF EXISTS update_github_installations_updated_at ON github_installations;
CREATE TRIGGER update_github_installations_updated_at
    BEFORE UPDATE ON github_installations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Repository attached to an OpsPilot project. MVP assumes one primary
-- repository per project; additional repos can be supported later by
-- relaxing the project_id uniqueness constraint.
CREATE TABLE IF NOT EXISTS repository_connections (
    id                          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id                UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id                  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    integration_id              UUID REFERENCES integrations(id) ON DELETE SET NULL,
    github_installation_id      UUID REFERENCES github_installations(id) ON DELETE SET NULL,
    provider                    TEXT NOT NULL DEFAULT 'github',
    repository_id               BIGINT NOT NULL,
    owner                       TEXT NOT NULL,
    name                        TEXT NOT NULL,
    full_name                   TEXT NOT NULL,
    html_url                    TEXT,
    clone_url                   TEXT,
    default_branch              TEXT NOT NULL DEFAULT 'main',
    selected_branch             TEXT NOT NULL DEFAULT 'main',
    is_private                  BOOLEAN DEFAULT false,
    permissions                 JSONB DEFAULT '{}',
    status                      TEXT NOT NULL DEFAULT 'connected'
        CHECK (status IN ('connected', 'disabled', 'archived', 'error')),
    last_indexed_commit_sha     TEXT,
    last_indexed_at             TIMESTAMPTZ,
    metadata                    JSONB DEFAULT '{}',
    connected_by                UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at                  TIMESTAMPTZ DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (project_id),
    UNIQUE (workspace_id, provider, repository_id)
);

CREATE INDEX IF NOT EXISTS idx_repository_connections_workspace_id ON repository_connections(workspace_id);
CREATE INDEX IF NOT EXISTS idx_repository_connections_project_id ON repository_connections(project_id);
CREATE INDEX IF NOT EXISTS idx_repository_connections_installation_id ON repository_connections(github_installation_id);
CREATE INDEX IF NOT EXISTS idx_repository_connections_full_name ON repository_connections(full_name);
CREATE INDEX IF NOT EXISTS idx_repository_connections_status ON repository_connections(status);

DROP TRIGGER IF EXISTS update_repository_connections_updated_at ON repository_connections;
CREATE TRIGGER update_repository_connections_updated_at
    BEFORE UPDATE ON repository_connections
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Durable ingestion jobs allow async repository indexing through NATS
-- while preserving status, retry state, commit identity, and failure
-- details for the UI and audit trail.
CREATE TABLE IF NOT EXISTS ingestion_jobs (
    id                          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id                UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id                  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repository_connection_id    UUID REFERENCES repository_connections(id) ON DELETE SET NULL,
    job_type                    TEXT NOT NULL DEFAULT 'repo_index',
    status                      TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'dead_letter')),
    source_branch               TEXT,
    source_commit_sha           TEXT,
    attempt_count               INT NOT NULL DEFAULT 0,
    max_attempts                INT NOT NULL DEFAULT 3,
    queued_at                   TIMESTAMPTZ DEFAULT NOW(),
    started_at                  TIMESTAMPTZ,
    finished_at                 TIMESTAMPTZ,
    error_code                  TEXT,
    error_message               TEXT,
    metadata                    JSONB DEFAULT '{}',
    created_by                  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at                  TIMESTAMPTZ DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_workspace_id ON ingestion_jobs(workspace_id);
CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_project_id ON ingestion_jobs(project_id);
CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_repository_connection_id ON ingestion_jobs(repository_connection_id);
CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_status_created_at ON ingestion_jobs(status, created_at DESC);

DROP TRIGGER IF EXISTS update_ingestion_jobs_updated_at ON ingestion_jobs;
CREATE TRIGGER update_ingestion_jobs_updated_at
    BEFORE UPDATE ON ingestion_jobs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE IF NOT EXISTS ingestion_events (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ingestion_job_id    UUID NOT NULL REFERENCES ingestion_jobs(id) ON DELETE CASCADE,
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id          UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    event_type          TEXT NOT NULL,
    status              TEXT,
    message             TEXT,
    metadata            JSONB DEFAULT '{}',
    created_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ingestion_events_job_id ON ingestion_events(ingestion_job_id);
CREATE INDEX IF NOT EXISTS idx_ingestion_events_project_id ON ingestion_events(project_id);
CREATE INDEX IF NOT EXISTS idx_ingestion_events_created_at ON ingestion_events(created_at DESC);

-- Records what was redacted or skipped during ingestion. This provides
-- user-visible safety evidence without storing the secret values.
CREATE TABLE IF NOT EXISTS secret_redaction_events (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id          UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    ingestion_job_id    UUID REFERENCES ingestion_jobs(id) ON DELETE SET NULL,
    file_path           TEXT,
    detector            TEXT NOT NULL,
    redaction_count     INT NOT NULL DEFAULT 0,
    metadata            JSONB DEFAULT '{}',
    created_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_secret_redaction_events_project_id ON secret_redaction_events(project_id);
CREATE INDEX IF NOT EXISTS idx_secret_redaction_events_job_id ON secret_redaction_events(ingestion_job_id);

DO $$ BEGIN
    RAISE NOTICE 'OpsPilot GitHub integration and ingestion foundation migration complete';
END $$;
