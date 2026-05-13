from __future__ import annotations

import json
import math
import os
import urllib.error
import urllib.request
from typing import Any, Mapping, Protocol

from .config import EmbeddingConfig


class EmbeddingProvider(Protocol):
    def embed(self, text: str) -> list[float]:
        raise NotImplementedError


class OpenAICompatibleEmbeddingProvider:
    def __init__(
        self,
        config: EmbeddingConfig,
        env: Mapping[str, str] | None = None,
    ) -> None:
        self._config = config
        self._env = os.environ if env is None else env

    def embed(self, text: str) -> list[float]:
        api_key = self._env.get(self._config.api_key_env)
        if not api_key:
            raise RuntimeError(
                f"embedding API key environment variable is not set: {self._config.api_key_env}"
            )

        request = urllib.request.Request(
            self._embeddings_url(),
            data=json.dumps(
                {
                    "model": self._config.model,
                    "input": text,
                    "encoding_format": self._config.encoding_format,
                    "dimensions": self._config.dimensions,
                }
            ).encode("utf-8"),
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
            method="POST",
        )

        try:
            with urllib.request.urlopen(
                request, timeout=self._config.timeout_seconds
            ) as response:
                body = response.read()
        except urllib.error.HTTPError as exc:
            raise RuntimeError(
                f"embedding request failed with HTTP {exc.code}"
            ) from exc
        except urllib.error.URLError as exc:
            raise RuntimeError(f"embedding request failed: {exc.reason}") from exc

        try:
            payload = json.loads(body.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise ValueError("embedding response must be valid JSON") from exc

        embedding = _extract_embedding(payload)
        if len(embedding) != self._config.dimensions:
            raise ValueError(
                f"data[0].embedding expected {self._config.dimensions} dimensions, got {len(embedding)}"
            )
        return _coerce_embedding_values(embedding, "data[0].embedding")

    def _embeddings_url(self) -> str:
        return self._config.base_url.rstrip("/") + "/embeddings"


class FakeEmbeddingProvider:
    def __init__(self, embeddings: Mapping[str, list[float]] | None = None) -> None:
        self._embeddings = {} if embeddings is None else embeddings

    def embed(self, text: str) -> list[float]:
        embedding = self._embeddings.get(text)
        if embedding is None:
            raise ValueError(f"no fake embedding configured for text: {text}")
        return _coerce_embedding_values(embedding, "embedding")


def build_embedding_provider(
    config: EmbeddingConfig, env: Mapping[str, str] | None = None
) -> EmbeddingProvider:
    if config.provider == "openai-compatible":
        return OpenAICompatibleEmbeddingProvider(config, env=env)
    raise ValueError(f"unsupported embedding provider: {config.provider}")


def _extract_embedding(payload: Any) -> list[Any]:
    if not isinstance(payload, dict):
        raise ValueError("embedding response must contain data[0].embedding")
    data = payload.get("data")
    if not isinstance(data, list) or len(data) == 0:
        raise ValueError("embedding response must contain data[0].embedding")
    first = data[0]
    if not isinstance(first, dict):
        raise ValueError("embedding response must contain data[0].embedding")
    embedding = first.get("embedding")
    if not isinstance(embedding, list):
        raise ValueError("embedding response must contain data[0].embedding")
    return embedding


def _coerce_embedding_values(embedding: list[Any], path: str) -> list[float]:
    return [
        _coerce_float(value, f"{path}[{index}]")
        for index, value in enumerate(embedding)
    ]


def _coerce_float(value: Any, path: str) -> float:
    if isinstance(value, bool):
        raise ValueError(f"{path} must be a finite float")
    try:
        coerced = float(value)
    except (TypeError, ValueError):
        raise ValueError(f"{path} must be a finite float") from None
    if not math.isfinite(coerced):
        raise ValueError(f"{path} must be a finite float")
    return coerced
