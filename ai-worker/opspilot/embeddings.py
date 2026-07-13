from __future__ import annotations

import hashlib
import math
import re
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
        response = self._client.models.embed_content(
            model=self._model,
            contents=values,
            config=types.EmbedContentConfig(
                task_type=task_type,
                output_dimensionality=self._dimensions,
            ),
        )
        return [list(item.values) for item in response.embeddings]

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
