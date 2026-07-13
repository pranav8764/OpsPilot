from __future__ import annotations

import json
from contextlib import contextmanager
from typing import Iterator

from .domain import IndexedDocument, IngestionJob, RetrievedChunk
from .usage import storage_limit_for_plan


def _vector(values: list[float]) -> str:
    return "[" + ",".join(f"{value:.9g}" for value in values) + "]"


class PostgresStore:
    def __init__(self, database_url: str):
        if not database_url:
            raise ValueError("DATABASE_URL is required")
        self.database_url = database_url

    @contextmanager
    def _connect(self) -> Iterator:
        import psycopg
        from psycopg.rows import dict_row

        with psycopg.connect(self.database_url, row_factory=dict_row) as connection:
            yield connection

    def claim_job(self, job_id: str | None = None) -> IngestionJob | None:
        with self._connect() as connection:
            with connection.transaction():
                row = connection.execute(
                    """
                    WITH candidate AS (
                        SELECT id
                        FROM ingestion_jobs
                        WHERE (%s::uuid IS NULL OR id = %s::uuid)
                          AND status IN ('queued', 'failed')
                          AND attempt_count < max_attempts
                        ORDER BY queued_at
                        FOR UPDATE SKIP LOCKED
                        LIMIT 1
                    ), claimed AS (
                        UPDATE ingestion_jobs AS job
                        SET status = 'running',
                            attempt_count = attempt_count + 1,
                            started_at = NOW(),
                            finished_at = NULL,
                            error_code = NULL,
                            error_message = NULL,
                            updated_at = NOW()
                        FROM candidate
                        WHERE job.id = candidate.id
                        RETURNING job.*
                    )
                    SELECT claimed.id, claimed.workspace_id, claimed.project_id,
                           claimed.repository_connection_id, connection.owner,
                           connection.name AS repository,
                           COALESCE(claimed.source_branch, connection.selected_branch) AS branch,
                           installation.installation_id,
                           claimed.attempt_count, claimed.max_attempts
                    FROM claimed
                    JOIN repository_connections AS connection
                      ON connection.id = claimed.repository_connection_id
                    JOIN github_installations AS installation
                      ON installation.id = connection.github_installation_id
                    """,
                    (job_id, job_id),
                ).fetchone()
                if row is None:
                    return None
                connection.execute(
                    """
                    INSERT INTO ingestion_events
                        (ingestion_job_id, workspace_id, project_id, event_type, status, message)
                    VALUES (%s, %s, %s, 'ingestion.started', 'running', 'Repository ingestion started')
                    """,
                    (row["id"], row["workspace_id"], row["project_id"]),
                )
                return IngestionJob(
                    id=str(row["id"]),
                    workspace_id=str(row["workspace_id"]),
                    project_id=str(row["project_id"]),
                    repository_connection_id=str(row["repository_connection_id"]),
                    owner=row["owner"],
                    repository=row["repository"],
                    branch=row["branch"],
                    installation_id=row["installation_id"],
                    attempt_count=row["attempt_count"],
                    max_attempts=row["max_attempts"],
                )

    def complete_job(
        self,
        job: IngestionJob,
        commit_sha: str,
        documents: list[IndexedDocument],
        stats: dict[str, int],
    ) -> None:
        with self._connect() as connection:
            with connection.transaction():
                new_storage_bytes = sum(
                    len(chunk.text.encode("utf-8")) for document in documents for chunk in document.chunks
                )
                workspace = connection.execute(
                    "SELECT CASE WHEN plan IN ('free','pro','team') THEN plan ELSE 'free' END AS plan FROM workspaces WHERE id = %s FOR UPDATE",
                    (job.workspace_id,),
                ).fetchone()
                if workspace is None:
                    raise ValueError("workspace was not found")
                other_storage = int(connection.execute(
                    "SELECT COALESCE(SUM(octet_length(chunk_text)), 0) AS bytes FROM document_chunks WHERE workspace_id = %s AND project_id != %s",
                    (job.workspace_id, job.project_id),
                ).fetchone()["bytes"])
                if other_storage + new_storage_bytes > storage_limit_for_plan(workspace["plan"]):
                    raise ValueError("indexed storage limit would be exceeded for this workspace plan")
                stats = {**stats, "indexed_storage_bytes": new_storage_bytes}
                connection.execute(
                    "DELETE FROM documents WHERE workspace_id = %s AND project_id = %s AND source_type = 'repo'",
                    (job.workspace_id, job.project_id),
                )
                for document in documents:
                    document_id = connection.execute(
                        """
                        INSERT INTO documents
                            (workspace_id, project_id, source_type, file_path, content_hash, metadata)
                        VALUES (%s, %s, 'repo', %s, %s, %s::jsonb)
                        RETURNING id
                        """,
                        (
                            job.workspace_id,
                            job.project_id,
                            document.path,
                            document.content_hash,
                            json.dumps({"commit_sha": commit_sha, "blob_sha": document.sha}),
                        ),
                    ).fetchone()["id"]
                    if len(document.chunks) != len(document.embeddings):
                        raise ValueError(f"embedding count mismatch for {document.path}")
                    for chunk, embedding in zip(document.chunks, document.embeddings, strict=True):
                        connection.execute(
                            """
                            INSERT INTO document_chunks
                                (workspace_id, project_id, document_id, chunk_text, embedding,
                                 file_path, file_type, language, chunk_index, metadata)
                            VALUES (%s, %s, %s, %s, %s::vector, %s, %s, %s, %s, %s::jsonb)
                            """,
                            (
                                job.workspace_id,
                                job.project_id,
                                document_id,
                                chunk.text,
                                _vector(embedding),
                                document.path,
                                document.file_type,
                                document.language,
                                chunk.index,
                                json.dumps({"start_line": chunk.start_line, "end_line": chunk.end_line, "commit_sha": commit_sha}),
                            ),
                        )
                    for detector, count in document.redactions.items():
                        connection.execute(
                            """
                            INSERT INTO secret_redaction_events
                                (workspace_id, project_id, ingestion_job_id, file_path, detector, redaction_count)
                            VALUES (%s, %s, %s, %s, %s, %s)
                            """,
                            (job.workspace_id, job.project_id, job.id, document.path, detector, count),
                        )

                connection.execute(
                    """
                    UPDATE repository_connections
                    SET last_indexed_commit_sha = %s, last_indexed_at = NOW(), status = 'connected', updated_at = NOW()
                    WHERE id = %s AND workspace_id = %s
                    """,
                    (commit_sha, job.repository_connection_id, job.workspace_id),
                )
                connection.execute(
                    "UPDATE projects SET indexing_status = 'completed', status = 'active', updated_at = NOW() WHERE id = %s AND workspace_id = %s",
                    (job.project_id, job.workspace_id),
                )
                connection.execute(
                    """
                    UPDATE ingestion_jobs
                    SET status = 'succeeded', source_commit_sha = %s, finished_at = NOW(),
                        metadata = metadata || %s::jsonb, updated_at = NOW()
                    WHERE id = %s AND workspace_id = %s
                    """,
                    (commit_sha, json.dumps(stats), job.id, job.workspace_id),
                )
                connection.execute(
                    """
                    INSERT INTO ingestion_events
                        (ingestion_job_id, workspace_id, project_id, event_type, status, message, metadata)
                    VALUES (%s, %s, %s, 'repo.indexed', 'succeeded', 'Repository indexing completed', %s::jsonb)
                    """,
                    (job.id, job.workspace_id, job.project_id, json.dumps(stats)),
                )

    def fail_job(self, job: IngestionJob, code: str, message: str) -> None:
        safe_message = message[:1000]
        final_status = "dead_letter" if job.attempt_count >= job.max_attempts else "failed"
        with self._connect() as connection:
            with connection.transaction():
                connection.execute(
                    """
                    UPDATE ingestion_jobs
                    SET status = %s, error_code = %s, error_message = %s,
                        finished_at = NOW(), updated_at = NOW()
                    WHERE id = %s AND workspace_id = %s
                    """,
                    (final_status, code, safe_message, job.id, job.workspace_id),
                )
                connection.execute(
                    "UPDATE projects SET indexing_status = 'failed', status = 'error', updated_at = NOW() WHERE id = %s AND workspace_id = %s",
                    (job.project_id, job.workspace_id),
                )
                connection.execute(
                    """
                    INSERT INTO ingestion_events
                        (ingestion_job_id, workspace_id, project_id, event_type, status, message)
                    VALUES (%s, %s, %s, 'ingestion.failed', %s, %s)
                    """,
                    (job.id, job.workspace_id, job.project_id, final_status, safe_message),
                )

    def search(
        self,
        workspace_id: str,
        project_id: str,
        query: str,
        embedding: list[float],
        limit: int,
    ) -> list[RetrievedChunk]:
        with self._connect() as connection:
            rows = connection.execute(
                """
                SELECT id, file_path, chunk_text,
                       GREATEST(0, 1 - (embedding <=> %s::vector)) AS score,
                       COALESCE((metadata->>'start_line')::int, 1) AS start_line,
                       COALESCE((metadata->>'end_line')::int, 1) AS end_line
                FROM document_chunks
                WHERE workspace_id = %s AND project_id = %s AND embedding IS NOT NULL
                ORDER BY embedding <=> %s::vector
                LIMIT %s
                """,
                (_vector(embedding), workspace_id, project_id, _vector(embedding), limit),
            ).fetchall()
        return [
            RetrievedChunk(
                id=str(row["id"]),
                file_path=row["file_path"],
                text=row["chunk_text"],
                score=float(row["score"]),
                start_line=row["start_line"],
                end_line=row["end_line"],
            )
            for row in rows
        ]

    def record_query(
        self,
        workspace_id: str,
        project_id: str,
        query: str,
        enough_context: bool,
        source_count: int,
    ) -> None:
        with self._connect() as connection:
            connection.execute(
                """
                INSERT INTO rag_query_events
                    (workspace_id, project_id, query_text, enough_context, source_count)
                VALUES (%s, %s, %s, %s, %s)
                """,
                (workspace_id, project_id, query[:4000], enough_context, source_count),
            )
