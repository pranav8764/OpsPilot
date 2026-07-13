-- OpsPilot incident, workflow, approval, and tool-audit foundation.

CREATE TABLE IF NOT EXISTS logs (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id          UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source              TEXT NOT NULL,
    content             TEXT NOT NULL,
    content_hash        TEXT NOT NULL,
    error_fingerprint   TEXT,
    metadata            JSONB NOT NULL DEFAULT '{}',
    created_by          UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_logs_project_created ON logs(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_logs_fingerprint ON logs(project_id, error_fingerprint);

CREATE TABLE IF NOT EXISTS workflows (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id          UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id             UUID REFERENCES users(id) ON DELETE SET NULL,
    workflow_type       TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('planned', 'running', 'waiting_approval', 'succeeded', 'failed', 'cancelled')),
    risk_level          TEXT NOT NULL DEFAULT 'low' CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),
    input               JSONB NOT NULL DEFAULT '{}',
    output              JSONB NOT NULL DEFAULT '{}',
    error_code          TEXT,
    error_message       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflows_project_created ON workflows(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(workspace_id, status);

DROP TRIGGER IF EXISTS update_workflows_updated_at ON workflows;
CREATE TRIGGER update_workflows_updated_at
    BEFORE UPDATE ON workflows
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE IF NOT EXISTS workflow_steps (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id         UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    step_order          INT NOT NULL,
    step_name           TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('planned', 'running', 'waiting_approval', 'succeeded', 'failed', 'skipped')),
    input               JSONB NOT NULL DEFAULT '{}',
    output              JSONB NOT NULL DEFAULT '{}',
    error               TEXT,
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workflow_id, step_order)
);

CREATE INDEX IF NOT EXISTS idx_workflow_steps_workflow ON workflow_steps(workflow_id, step_order);

CREATE TABLE IF NOT EXISTS incidents (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id          UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_id         UUID REFERENCES workflows(id) ON DELETE SET NULL,
    log_id              UUID REFERENCES logs(id) ON DELETE SET NULL,
    title               TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'investigating', 'fix_proposed', 'resolved', 'closed')),
    severity            TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'error', 'critical')),
    summary             TEXT NOT NULL,
    root_cause          TEXT NOT NULL,
    confidence          TEXT NOT NULL CHECK (confidence IN ('low', 'medium', 'high', 'insufficient')),
    evidence            JSONB NOT NULL DEFAULT '[]',
    fix_recommendation  TEXT,
    resolution          TEXT,
    error_fingerprint   TEXT,
    created_by          UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_incidents_project_created ON incidents(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_fingerprint ON incidents(project_id, error_fingerprint);

DROP TRIGGER IF EXISTS update_incidents_updated_at ON incidents;
CREATE TRIGGER update_incidents_updated_at
    BEFORE UPDATE ON incidents
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE IF NOT EXISTS approvals (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id          UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_id         UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    requested_by        UUID REFERENCES users(id) ON DELETE SET NULL,
    action_type         TEXT NOT NULL,
    action_payload      JSONB NOT NULL,
    payload_hash        TEXT NOT NULL,
    risk_level          TEXT NOT NULL CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),
    status              TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'executed', 'failed')),
    decided_by          UUID REFERENCES users(id) ON DELETE SET NULL,
    decision_reason     TEXT,
    expires_at          TIMESTAMPTZ NOT NULL,
    decided_at          TIMESTAMPTZ,
    executed_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_approvals_workspace_status ON approvals(workspace_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS fix_proposals (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id          UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    incident_id         UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    workflow_id         UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    base_commit_sha     TEXT NOT NULL,
    proposed_changes    JSONB NOT NULL,
    unified_diff        TEXT NOT NULL,
    verification        JSONB NOT NULL DEFAULT '{}',
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending_approval', 'approved', 'published', 'rejected', 'failed')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fix_proposals_incident ON fix_proposals(incident_id, created_at DESC);

DROP TRIGGER IF EXISTS update_fix_proposals_updated_at ON fix_proposals;
CREATE TRIGGER update_fix_proposals_updated_at
    BEFORE UPDATE ON fix_proposals
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE IF NOT EXISTS tool_calls (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE SET NULL,
    workflow_id         UUID REFERENCES workflows(id) ON DELETE SET NULL,
    approval_id         UUID REFERENCES approvals(id) ON DELETE SET NULL,
    tool_name           TEXT NOT NULL,
    risk_level          TEXT NOT NULL,
    request_summary     JSONB NOT NULL DEFAULT '{}',
    response_summary    JSONB NOT NULL DEFAULT '{}',
    status              TEXT NOT NULL CHECK (status IN ('requested', 'denied', 'running', 'succeeded', 'failed')),
    idempotency_key     TEXT UNIQUE,
    error_code          TEXT,
    error_message       TEXT,
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tool_calls_workflow ON tool_calls(workflow_id, created_at);

DO $$ BEGIN
    RAISE NOTICE 'OpsPilot incident and workflow foundation migration complete';
END $$;
