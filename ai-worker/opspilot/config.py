from __future__ import annotations

import os
from dataclasses import dataclass


def _bool_env(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


@dataclass(frozen=True)
class Settings:
    database_url: str
    nats_url: str
    github_app_id: str
    github_private_key_path: str
    github_private_key_base64: str
    github_api_version: str
    gemini_api_key: str
    embedding_provider: str
    embedding_model: str
    embedding_dimensions: int
    groq_api_key: str
    groq_model: str
    max_file_bytes: int
    max_repository_files: int
    max_repository_bytes: int
    chunk_chars: int
    chunk_overlap_chars: int
    retrieval_min_score: float
    run_background_worker: bool
    recovery_poll_seconds: float
    retention_cleanup_seconds: float
    internal_token: str
    sandbox_runner_url: str
    sandbox_runner_token: str

    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            database_url=os.getenv("DATABASE_URL", ""),
            nats_url=os.getenv("NATS_URL", "nats://localhost:4222"),
            github_app_id=os.getenv("GITHUB_APP_ID", ""),
            github_private_key_path=os.getenv("GITHUB_APP_PRIVATE_KEY_PATH", ""),
            github_private_key_base64=os.getenv("GITHUB_APP_PRIVATE_KEY_BASE64", ""),
            github_api_version=os.getenv("GITHUB_API_VERSION", "2026-03-10"),
            gemini_api_key=os.getenv("GEMINI_API_KEY", ""),
            embedding_provider=os.getenv("EMBEDDING_PROVIDER", "local").lower(),
            embedding_model=os.getenv("GEMINI_EMBEDDING_MODEL", "gemini-embedding-2"),
            embedding_dimensions=int(os.getenv("EMBEDDING_DIMENSIONS", "768")),
            groq_api_key=os.getenv("GROQ_API_KEY", ""),
            groq_model=os.getenv("GROQ_MODEL", "openai/gpt-oss-120b"),
            max_file_bytes=int(os.getenv("INGESTION_MAX_FILE_BYTES", str(512 * 1024))),
            max_repository_files=int(os.getenv("INGESTION_MAX_REPOSITORY_FILES", "3000")),
            max_repository_bytes=int(os.getenv("INGESTION_MAX_REPOSITORY_BYTES", str(30 * 1024 * 1024))),
            chunk_chars=int(os.getenv("RAG_CHUNK_CHARS", "4000")),
            chunk_overlap_chars=int(os.getenv("RAG_CHUNK_OVERLAP_CHARS", "400")),
            retrieval_min_score=float(os.getenv("RAG_MIN_SCORE", "0.16")),
            run_background_worker=_bool_env("RUN_BACKGROUND_WORKER", False),
            recovery_poll_seconds=float(os.getenv("RECOVERY_POLL_SECONDS", "5")),
            retention_cleanup_seconds=max(60.0, float(os.getenv("RETENTION_CLEANUP_SECONDS", "3600"))),
            internal_token=os.getenv("AI_WORKER_INTERNAL_TOKEN", ""),
            sandbox_runner_url=os.getenv("SANDBOX_RUNNER_URL", ""),
            sandbox_runner_token=os.getenv("SANDBOX_RUNNER_INTERNAL_TOKEN", ""),
        )

    def validate_worker(self) -> None:
        missing = []
        if not self.database_url:
            missing.append("DATABASE_URL")
        if not self.github_app_id:
            missing.append("GITHUB_APP_ID")
        if not (self.github_private_key_path or self.github_private_key_base64):
            missing.append("GITHUB_APP_PRIVATE_KEY_PATH or GITHUB_APP_PRIVATE_KEY_BASE64")
        if self.embedding_provider == "gemini" and not self.gemini_api_key:
            missing.append("GEMINI_API_KEY")
        if missing:
            raise ValueError("missing worker configuration: " + ", ".join(missing))
