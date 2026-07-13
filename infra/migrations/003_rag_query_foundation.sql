-- OpsPilot repository retrieval and query audit foundation.

CREATE INDEX IF NOT EXISTS idx_document_chunks_text_search
    ON document_chunks USING GIN (to_tsvector('simple', chunk_text));

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_repo_path_unique
    ON documents(project_id, source_type, file_path)
    WHERE file_path IS NOT NULL;

CREATE TABLE IF NOT EXISTS rag_query_events (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id          UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    query_text          TEXT NOT NULL,
    enough_context      BOOLEAN NOT NULL,
    source_count        INT NOT NULL DEFAULT 0,
    feedback            TEXT CHECK (feedback IS NULL OR feedback IN ('positive', 'negative')),
    metadata            JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rag_query_events_project_created
    ON rag_query_events(project_id, created_at DESC);

DO $$ BEGIN
    RAISE NOTICE 'OpsPilot RAG query foundation migration complete';
END $$;
