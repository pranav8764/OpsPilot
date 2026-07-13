from __future__ import annotations

from collections.abc import Callable


class PostgresRetentionCleaner:
    """Deletes only expired log bodies using each workspace's policy.

    Incident summaries reference logs with ON DELETE SET NULL, so operational
    history survives without retaining the original (redacted) log content.
    """

    def __init__(self, database_url: str, connect: Callable | None = None):
        self.database_url = database_url
        self._connect = connect

    def cleanup(self) -> int:
        if self._connect is None:
            import psycopg

            connect = psycopg.connect
        else:
            connect = self._connect
        with connect(self.database_url) as connection:
            row = connection.execute(
                """
                WITH expired AS (
                    DELETE FROM logs AS log
                    USING workspaces AS workspace
                    WHERE log.workspace_id = workspace.id
                      AND log.created_at < NOW() - make_interval(days => workspace.log_retention_days)
                    RETURNING log.id
                )
                SELECT COUNT(*) FROM expired
                """
            ).fetchone()
        return int(row[0]) if row else 0
