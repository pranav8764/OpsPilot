export interface IngestionBadge {
  label: string;
  className: string;
}

// Mirrors the CHECK constraint at
// infra/migrations/002_github_ingestion_foundation.sql.
// 'dead_letter' means retries are exhausted — it is terminal, never in-flight.
export const INGESTION_BADGE: Record<string, IngestionBadge> = {
  succeeded:   { label: 'Indexed',     className: 'badge-success' },
  running:     { label: 'Indexing',    className: 'badge-warning' },
  queued:      { label: 'Queued',      className: 'badge-warning' },
  failed:      { label: 'Failed',      className: 'badge-error'   },
  dead_letter: { label: 'Failed',      className: 'badge-error'   },
  cancelled:   { label: 'Cancelled',   className: 'badge-neutral' },
  none:        { label: 'Not indexed', className: 'badge-neutral' },
};

export function ingestionBadge(status?: string | null): IngestionBadge {
  return INGESTION_BADGE[status ?? 'none'] ?? INGESTION_BADGE.none;
}
