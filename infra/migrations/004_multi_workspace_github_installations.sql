-- A single GitHub App installation (one GitHub account/org) can legitimately
-- serve several OpsPilot workspaces owned by the same user. The original
-- global UNIQUE(installation_id) permanently bound an installation to whichever
-- workspace connected it first: every other workspace listed zero installations,
-- so its repository picker stayed empty and projects were created with no repo.
--
-- Scope uniqueness to the workspace instead. All installation lookups already
-- filter on workspace_id, so no read path changes.

ALTER TABLE github_installations
    DROP CONSTRAINT IF EXISTS github_installations_installation_id_key;

CREATE UNIQUE INDEX IF NOT EXISTS github_installations_workspace_installation_key
    ON github_installations (workspace_id, installation_id);
