from __future__ import annotations

from dataclasses import dataclass


PLAN_LIMITS = {
    "free": {
        "ai_query": 200,
        "incident_analysis": 50,
        "fix_proposal": 10,
        "deployment_analysis": 50,
        "projects": 3,
        "indexed_storage_bytes": 100 * 1024 * 1024,
    },
    "pro": {
        "ai_query": 5_000,
        "incident_analysis": 1_000,
        "fix_proposal": 250,
        "deployment_analysis": 1_000,
        "projects": 25,
        "indexed_storage_bytes": 5 * 1024 * 1024 * 1024,
    },
    "team": {
        "ai_query": 20_000,
        "incident_analysis": 5_000,
        "fix_proposal": 1_000,
        "deployment_analysis": 5_000,
        "projects": 50,
        "indexed_storage_bytes": 50 * 1024 * 1024 * 1024,
    },
}


class UsageLimitExceeded(ValueError):
    def __init__(self, event_type: str, used: int, limit: int):
        super().__init__(f"{event_type} monthly limit reached ({used}/{limit})")
        self.event_type = event_type
        self.used = used
        self.limit = limit


@dataclass(frozen=True)
class UsageReservation:
    plan: str
    event_type: str
    used: int
    limit: int
    remaining: int


class PostgresUsageLimiter:
    def __init__(self, database_url: str):
        self.database_url = database_url

    def consume(self, workspace_id: str, project_id: str | None, user_id: str | None, event_type: str, units: int = 1) -> UsageReservation:
        import psycopg
        from psycopg.rows import dict_row

        if units < 1 or event_type not in PLAN_LIMITS["free"] or event_type in {"projects", "indexed_storage_bytes"}:
            raise ValueError("unsupported metered event")
        with psycopg.connect(self.database_url, row_factory=dict_row) as connection:
            with connection.transaction():
                # Serialize counters per workspace without creating a mutable
                # counter row that can drift from the append-only usage ledger.
                connection.execute("SELECT pg_advisory_xact_lock(hashtextextended(%s, 0))", (workspace_id,))
                row = connection.execute(
                    "SELECT CASE WHEN plan IN ('free','pro','team') THEN plan ELSE 'free' END AS plan FROM workspaces WHERE id = %s",
                    (workspace_id,),
                ).fetchone()
                if row is None:
                    raise ValueError("workspace was not found")
                plan = row["plan"]
                used = int(connection.execute(
                    """
                    SELECT COALESCE(SUM(units), 0) AS used
                    FROM usage_events
                    WHERE workspace_id = %s AND event_type = %s
                      AND created_at >= date_trunc('month', NOW())
                    """,
                    (workspace_id, event_type),
                ).fetchone()["used"])
                limit = PLAN_LIMITS[plan][event_type]
                if used + units > limit:
                    raise UsageLimitExceeded(event_type, used, limit)
                connection.execute(
                    """
                    INSERT INTO usage_events (workspace_id, project_id, user_id, event_type, units)
                    VALUES (%s, %s, %s, %s, %s)
                    """,
                    (workspace_id, project_id, user_id, event_type, units),
                )
        current = used + units
        return UsageReservation(plan, event_type, current, limit, max(0, limit - current))

    def summary(self, workspace_id: str) -> dict:
        import psycopg
        from psycopg.rows import dict_row

        with psycopg.connect(self.database_url, row_factory=dict_row) as connection:
            workspace = connection.execute(
                "SELECT CASE WHEN plan IN ('free','pro','team') THEN plan ELSE 'free' END AS plan FROM workspaces WHERE id = %s",
                (workspace_id,),
            ).fetchone()
            if workspace is None:
                raise ValueError("workspace was not found")
            plan = workspace["plan"]
            rows = connection.execute(
                """
                SELECT event_type, COALESCE(SUM(units), 0) AS used
                FROM usage_events
                WHERE workspace_id = %s AND created_at >= date_trunc('month', NOW())
                GROUP BY event_type
                """,
                (workspace_id,),
            ).fetchall()
            used_by_type = {row["event_type"]: int(row["used"]) for row in rows}
            projects = int(connection.execute(
                "SELECT COUNT(*) AS count FROM projects WHERE workspace_id = %s AND status != 'archived'",
                (workspace_id,),
            ).fetchone()["count"])
            storage = int(connection.execute(
                "SELECT COALESCE(SUM(octet_length(chunk_text)), 0) AS bytes FROM document_chunks WHERE workspace_id = %s",
                (workspace_id,),
            ).fetchone()["bytes"])
        usage = {}
        for event_type in ("ai_query", "incident_analysis", "fix_proposal", "deployment_analysis"):
            limit = PLAN_LIMITS[plan][event_type]
            used = used_by_type.get(event_type, 0)
            usage[event_type] = {"used": used, "limit": limit, "remaining": max(0, limit - used), "period": "month"}
        usage["projects"] = {"used": projects, "limit": PLAN_LIMITS[plan]["projects"], "remaining": max(0, PLAN_LIMITS[plan]["projects"] - projects), "period": "current"}
        usage["indexed_storage_bytes"] = {"used": storage, "limit": PLAN_LIMITS[plan]["indexed_storage_bytes"], "remaining": max(0, PLAN_LIMITS[plan]["indexed_storage_bytes"] - storage), "period": "current"}
        return {"plan": plan, "usage": usage}


def storage_limit_for_plan(plan: str) -> int:
    return PLAN_LIMITS.get(plan, PLAN_LIMITS["free"])["indexed_storage_bytes"]
