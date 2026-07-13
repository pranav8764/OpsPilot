from __future__ import annotations

import hashlib
from collections.abc import Callable

from .chunking import chunk_text
from .config import Settings
from .domain import EmbeddingProvider, IndexedDocument, IngestionJob, IngestionResult, IngestionStore, RepositoryFile
from .security import file_metadata, path_policy, scan_and_redact


class JobNotAvailable(RuntimeError):
    pass


RepositoryFetcher = Callable[[IngestionJob], tuple[str, list[RepositoryFile]]]


class IngestionProcessor:
    def __init__(
        self,
        store: IngestionStore,
        embeddings: EmbeddingProvider,
        fetch_repository: RepositoryFetcher,
        settings: Settings,
    ):
        self.store = store
        self.embeddings = embeddings
        self.fetch_repository = fetch_repository
        self.settings = settings

    def process(self, job_id: str | None = None) -> IngestionResult:
        job = self.store.claim_job(job_id)
        if job is None:
            raise JobNotAvailable("no claimable ingestion job")

        try:
            commit_sha, files = self.fetch_repository(job)
            documents: list[IndexedDocument] = []
            files_skipped = 0
            redaction_count = 0
            for repository_file in files:
                allowed, _ = path_policy(repository_file.path, repository_file.size, self.settings.max_file_bytes)
                if not allowed:
                    files_skipped += 1
                    continue
                try:
                    scan = scan_and_redact(repository_file.content)
                except ValueError:
                    files_skipped += 1
                    continue
                chunks = chunk_text(
                    scan.content,
                    max_chars=self.settings.chunk_chars,
                    overlap_chars=self.settings.chunk_overlap_chars,
                )
                if not chunks:
                    files_skipped += 1
                    continue
                file_type, language = file_metadata(repository_file.path)
                embeddings = self._embed_in_batches([chunk.text for chunk in chunks])
                documents.append(
                    IndexedDocument(
                        path=repository_file.path,
                        sha=repository_file.sha,
                        content_hash=hashlib.sha256(scan.content.encode("utf-8")).hexdigest(),
                        file_type=file_type,
                        language=language,
                        chunks=chunks,
                        embeddings=embeddings,
                        redactions=scan.redactions,
                    )
                )
                redaction_count += scan.redaction_count

            stats = {
                "files_seen": len(files),
                "files_indexed": len(documents),
                "files_skipped": files_skipped,
                "chunks_created": sum(len(document.chunks) for document in documents),
                "redactions": redaction_count,
            }
            self.store.complete_job(job, commit_sha, documents, stats)
            return IngestionResult(job_id=job.id, commit_sha=commit_sha, **stats)
        except Exception as exc:
            self.store.fail_job(job, type(exc).__name__.upper(), str(exc))
            raise

    def _embed_in_batches(self, texts: list[str], batch_size: int = 50) -> list[list[float]]:
        result: list[list[float]] = []
        for offset in range(0, len(texts), batch_size):
            result.extend(self.embeddings.embed_documents(texts[offset : offset + batch_size]))
        if len(result) != len(texts):
            raise RuntimeError("embedding provider returned an unexpected result count")
        return result
