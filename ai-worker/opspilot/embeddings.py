from __future__ import annotations

import hashlib
import math
import random
import re
import time
from typing import Iterable

from .config import Settings


TOKEN_RE = re.compile(r"[A-Za-z_][A-Za-z0-9_./:-]{1,80}")
STOPWORDS = {"a", "an", "and", "are", "as", "at", "be", "by", "for", "from", "how", "in", "is", "it", "of", "on", "or", "that", "the", "this", "to", "what", "where", "which", "with"}


class LocalHashEmbeddings:
    """Deterministic, dependency-free lexical embeddings for local/test mode."""

    def __init__(self, dimensions: int = 768):
        self.dimensions = dimensions

    def _embed(self, text: str) -> list[float]:
        vector = [0.0] * self.dimensions
        tokens = [token.lower() for token in TOKEN_RE.findall(text)]
        for token in tokens:
            if token in STOPWORDS:
                continue
            digest = hashlib.blake2b(token.encode("utf-8"), digest_size=8).digest()
            slot = int.from_bytes(digest[:4], "big") % self.dimensions
            sign = 1.0 if digest[4] & 1 else -1.0
            vector[slot] += sign
        norm = math.sqrt(sum(value * value for value in vector))
        return [value / norm for value in vector] if norm else vector

    def embed_documents(self, texts: list[str]) -> list[list[float]]:
        return [self._embed(text) for text in texts]

    def embed_query(self, text: str) -> list[float]:
        return self._embed(text)


# Provider-side failures that are worth waiting out rather than failing the job.
_TRANSIENT_MARKERS = (
    "RESOURCE_EXHAUSTED",  # 429 rate limit
    "UNAVAILABLE",         # 503 upstream blip
    "INTERNAL",            # 500
    "DEADLINE_EXCEEDED",   # 504
    "429",
    "500",
    "503",
    "504",
)


def _is_transient(exc: Exception) -> bool:
    message = str(exc)
    return any(marker in message for marker in _TRANSIENT_MARKERS)


class GeminiEmbeddings:
    def __init__(self, api_key: str, model: str, dimensions: int):
        from google import genai

        self._client = genai.Client(api_key=api_key)
        self._model = model
        self._dimensions = dimensions

    def _embed(self, texts: Iterable[str], task_type: str) -> list[list[float]]:
        from google.genai import types

        values = list(texts)
        if not values:
            return []
        # Passing a plain list[str] as `contents` makes the SDK treat it as ONE
        # Content with many parts, so the API returns a single embedding for the
        # whole batch — silently, without erroring. Each text needs its own
        # Content object to get one embedding per input. Getting this wrong also
        # forces a call per chunk, which exhausts the per-minute quota on a
        # repository of any size.
        contents = [types.Content(parts=[types.Part(text=value)]) for value in values]

        # Ingestion calls this once per repository file, so a repo of any size
        # bursts well past the per-minute embedding quota. Retry the transient
        # provider failures rather than dead-lettering the whole job over one of
        # them: a rate limit or a 503 is a wait, not an error. Everything else
        # (bad key, bad model, malformed request) propagates immediately.
        delay = 2.0
        last_exc: Exception | None = None
        for attempt in range(6):
            try:
                response = self._client.models.embed_content(
                    model=self._model,
                    contents=contents,
                    config=types.EmbedContentConfig(
                        task_type=task_type,
                        output_dimensionality=self._dimensions,
                    ),
                )
                break
            except Exception as exc:  # noqa: BLE001 - provider raises broad errors
                if not _is_transient(exc):
                    raise
                last_exc = exc
                if attempt == 5:
                    raise
                time.sleep(delay + random.uniform(0, 1.0))
                delay = min(delay * 2, 60.0)
        else:  # pragma: no cover - loop always breaks or raises
            raise RuntimeError("embedding retries exhausted") from last_exc

        vectors = [list(item.values) for item in response.embeddings]
        if len(vectors) != len(values):
            raise RuntimeError(
                f"gemini returned {len(vectors)} embeddings for {len(values)} inputs"
            )
        return vectors

    def embed_documents(self, texts: list[str]) -> list[list[float]]:
        return self._embed(texts, "RETRIEVAL_DOCUMENT")

    def embed_query(self, text: str) -> list[float]:
        return self._embed([text], "RETRIEVAL_QUERY")[0]


def build_embedding_provider(settings: Settings):
    if settings.embedding_provider == "gemini":
        if not settings.gemini_api_key:
            raise ValueError("GEMINI_API_KEY is required when EMBEDDING_PROVIDER=gemini")
        return GeminiEmbeddings(settings.gemini_api_key, settings.embedding_model, settings.embedding_dimensions)
    if settings.embedding_provider != "local":
        raise ValueError(f"unsupported embedding provider: {settings.embedding_provider}")
    return LocalHashEmbeddings(settings.embedding_dimensions)
