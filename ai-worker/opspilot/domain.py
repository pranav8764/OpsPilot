from __future__ import annotations

from dataclasses import dataclass, field
from typing import Protocol


@dataclass(frozen=True)
class IngestionJob:
    id: str
    workspace_id: str
    project_id: str
    repository_connection_id: str
    owner: str
    repository: str
    branch: str
    installation_id: int
    attempt_count: int
    max_attempts: int


@dataclass(frozen=True)
class RepositoryFile:
    path: str
    sha: str
    size: int
    content: str


@dataclass(frozen=True)
class Chunk:
    text: str
    index: int
    start_line: int
    end_line: int


@dataclass
class IndexedDocument:
    path: str
    sha: str
    content_hash: str
    file_type: str
    language: str
    chunks: list[Chunk]
    embeddings: list[list[float]]
    redactions: dict[str, int] = field(default_factory=dict)


@dataclass(frozen=True)
class RetrievedChunk:
    id: str
    file_path: str
    text: str
    score: float
    start_line: int
    end_line: int

    @property
    def citation(self) -> str:
        return f"{self.file_path}:L{self.start_line}-L{self.end_line}"


@dataclass(frozen=True)
class IngestionResult:
    job_id: str
    commit_sha: str
    files_seen: int
    files_indexed: int
    files_skipped: int
    chunks_created: int
    redactions: int


class EmbeddingProvider(Protocol):
    def embed_documents(self, texts: list[str]) -> list[list[float]]: ...

    def embed_query(self, text: str) -> list[float]: ...


class RepositorySource(Protocol):
    def fetch(self, owner: str, repository: str, branch: str) -> tuple[str, list[RepositoryFile]]: ...


class IngestionStore(Protocol):
    def claim_job(self, job_id: str | None = None) -> IngestionJob | None: ...

    def complete_job(self, job: IngestionJob, commit_sha: str, documents: list[IndexedDocument], stats: dict[str, int]) -> None: ...

    def fail_job(self, job: IngestionJob, code: str, message: str) -> None: ...


class RetrievalStore(Protocol):
    def search(self, workspace_id: str, project_id: str, query: str, embedding: list[float], limit: int) -> list[RetrievedChunk]: ...

    def record_query(self, workspace_id: str, project_id: str, query: str, enough_context: bool, source_count: int) -> None: ...
