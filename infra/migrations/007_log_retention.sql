-- Per-workspace lifecycle controls for collected/manual log bodies. Incident
-- summaries and audit records remain available after the backing log expires.
ALTER TABLE workspaces
    ADD COLUMN IF NOT EXISTS log_retention_days INT NOT NULL DEFAULT 30;

DO $$ BEGIN
    ALTER TABLE workspaces
        ADD CONSTRAINT workspaces_log_retention_days_range
        CHECK (log_retention_days BETWEEN 1 AND 365);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS idx_logs_workspace_created
    ON logs(workspace_id, created_at);

DO $$ BEGIN
    RAISE NOTICE 'OpsPilot log-retention migration complete';
END $$;
