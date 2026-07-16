-- Billing-ready subscription metadata and auditable usage counters. Provider
-- checkout is intentionally decoupled; enforcement works before billing is wired.
CREATE TABLE IF NOT EXISTS subscriptions (
    id                      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id            UUID NOT NULL UNIQUE REFERENCES workspaces(id) ON DELETE CASCADE,
    plan                    TEXT NOT NULL DEFAULT 'free' CHECK (plan IN ('free', 'pro', 'team')),
    status                  TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'trialing', 'past_due', 'cancelled')),
    billing_provider        TEXT,
    provider_customer_id    TEXT,
    provider_subscription_id TEXT,
    current_period_start    TIMESTAMPTZ,
    current_period_end      TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO subscriptions (workspace_id, plan, status)
SELECT id, CASE WHEN plan IN ('free', 'pro', 'team') THEN plan ELSE 'free' END, 'active'
FROM workspaces
ON CONFLICT (workspace_id) DO NOTHING;

CREATE OR REPLACE FUNCTION create_default_workspace_subscription()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO subscriptions (workspace_id, plan, status)
    VALUES (NEW.id, COALESCE(NEW.plan, 'free'), 'active')
    ON CONFLICT (workspace_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS create_workspace_subscription ON workspaces;
CREATE TRIGGER create_workspace_subscription
    AFTER INSERT ON workspaces
    FOR EACH ROW EXECUTE FUNCTION create_default_workspace_subscription();

DROP TRIGGER IF EXISTS update_subscriptions_updated_at ON subscriptions;
CREATE TRIGGER update_subscriptions_updated_at
    BEFORE UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE IF NOT EXISTS usage_events (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id    UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id      UUID REFERENCES projects(id) ON DELETE SET NULL,
    user_id         UUID REFERENCES users(id) ON DELETE SET NULL,
    event_type      TEXT NOT NULL,
    units           BIGINT NOT NULL DEFAULT 1 CHECK (units > 0),
    metadata        JSONB NOT NULL DEFAULT '{}',
    idempotency_key TEXT UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_usage_workspace_type_created
    ON usage_events(workspace_id, event_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usage_project_created
    ON usage_events(project_id, created_at DESC);

DO $$ BEGIN
    RAISE NOTICE 'OpsPilot usage and billing foundation migration complete';
END $$;
