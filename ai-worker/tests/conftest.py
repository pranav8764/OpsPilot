from __future__ import annotations

from dataclasses import replace

from opspilot.config import Settings


def make_settings(**overrides) -> Settings:
    base = Settings(
        database_url="postgresql://test:test@localhost/test",
        nats_url="nats://localhost:4222",
        github_app_id="1",
        github_private_key_path="",
        github_private_key_base64="unused-in-tests",
        github_api_version="2026-03-10",
        gemini_api_key="",
        embedding_provider="local",
        embedding_model="gemini-embedding-2",
        embedding_dimensions=768,
        groq_api_key="",
        groq_model="openai/gpt-oss-120b",
        max_file_bytes=512 * 1024,
        max_repository_files=3000,
        max_repository_bytes=30 * 1024 * 1024,
        chunk_chars=4000,
        chunk_overlap_chars=400,
        retrieval_min_score=0.16,
        run_background_worker=False,
        recovery_poll_seconds=0.01,
        retention_cleanup_seconds=3600,
        internal_token="",
        sandbox_runner_url="",
        sandbox_runner_token="",
    )
    return replace(base, **overrides)
