from __future__ import annotations

import hashlib
import json
import math
import os
import sqlite3
import threading
import unicodedata
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone
from typing import Any, Mapping, Protocol

from .config import EmbeddingCacheConfig, EmbeddingConfig

_VALID_CACHE_MODES = {"off", "read_write", "read_only", "refresh"}


class EmbeddingProvider(Protocol):
    def embed(self, text: str) -> list[float]:
        raise NotImplementedError


class EmbeddingCacheMiss(LookupError):
    """Raised when read-only embedding cache mode has no matching vector."""


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


class CachedEmbeddingProvider:
    def __init__(
        self,
        inner: EmbeddingProvider,
        embedding_config: EmbeddingConfig,
        cache_config: EmbeddingCacheConfig,
    ) -> None:
        self._inner = inner
        self._embedding_config = embedding_config
        self._cache_config = cache_config
        self._lock = threading.RLock()
        self._hits = 0
        self._misses = 0
        self._live_call_count = 0
        self._writes = 0

    @property
    def stats(self) -> dict[str, int]:
        with self._lock:
            return {
                "hits": self._hits,
                "misses": self._misses,
                "live_call_count": self._live_call_count,
                "writes": self._writes,
            }

    def embed(self, text: str, ref: Mapping[str, str] | None = None) -> list[float]:
        mode = self._cache_config.mode
        if mode not in _VALID_CACHE_MODES:
            raise ValueError(
                "embedding_cache.mode must be one of off, read_write, read_only, refresh"
            )
        if mode == "off":
            return self._embed_live(text)

        key = _embedding_cache_key(text, self._embedding_config, self._cache_config)
        if mode != "refresh":
            cached = self._read_cached_vector(key)
            if cached is not None:
                with self._lock:
                    self._hits += 1
                if mode == "read_write":
                    self._write_ref(key, ref)
                return cached

        with self._lock:
            self._misses += 1
        if mode == "read_only":
            raise EmbeddingCacheMiss(f"embedding cache miss: {key}")

        vector = self._embed_live(text)
        self._write_embedding(key, text, vector, ref)
        with self._lock:
            self._writes += 1
        return vector

    def _embed_live(self, text: str) -> list[float]:
        with self._lock:
            self._live_call_count += 1
        return _coerce_embedding_values(self._inner.embed(text), "embedding")

    def _read_cached_vector(self, key: str) -> list[float] | None:
        if not self._cache_config.db_path.exists():
            return None
        with self._connect() as db:
            try:
                row = db.execute(
                    "SELECT vector_json FROM embeddings WHERE cache_key = ?", (key,)
                ).fetchone()
            except sqlite3.OperationalError:
                return None
        if row is None:
            return None
        try:
            payload = json.loads(row[0])
        except (TypeError, json.JSONDecodeError) as exc:
            raise ValueError("cached embedding vector must be valid JSON") from exc
        if not isinstance(payload, list):
            raise ValueError("cached embedding vector must be a JSON array")
        return _coerce_embedding_values(payload, "cached embedding")

    def _write_embedding(
        self,
        key: str,
        text: str,
        vector: list[float],
        ref: Mapping[str, str] | None,
    ) -> None:
        self._cache_config.db_path.parent.mkdir(parents=True, exist_ok=True)
        metadata = _embedding_cache_metadata(
            text, self._embedding_config, self._cache_config
        )
        now = _utc_now()
        with self._connect() as db:
            _ensure_schema(db)
            db.execute(
                """
                INSERT INTO embeddings (
                    cache_key,
                    provider_kind,
                    base_url_hash,
                    model,
                    dimensions,
                    encoding_format,
                    text_normalization_version,
                    searchable_text_version,
                    normalized_text_sha256,
                    vector_json,
                    created_at,
                    updated_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(cache_key) DO UPDATE SET
                    vector_json = excluded.vector_json,
                    updated_at = excluded.updated_at
                """,
                (
                    key,
                    metadata["provider_kind"],
                    metadata["base_url_hash"],
                    metadata["model"],
                    metadata["dimensions"],
                    metadata["encoding_format"],
                    metadata["text_normalization_version"],
                    metadata["searchable_text_version"],
                    metadata["normalized_text_sha256"],
                    json.dumps(vector, separators=(",", ":")),
                    now,
                    now,
                ),
            )
            _insert_ref(db, key, ref, now)

    def _write_ref(self, key: str, ref: Mapping[str, str] | None) -> None:
        if ref is None:
            return
        self._cache_config.db_path.parent.mkdir(parents=True, exist_ok=True)
        with self._connect() as db:
            _ensure_schema(db)
            _insert_ref(db, key, ref, _utc_now())

    def delete_node_ref(self, persona_id: str, node_type: str, node_id: str) -> None:
        if not self._cache_config.db_path.exists():
            return
        with self._connect() as db:
            _ensure_schema(db)
            db.execute(
                """
                DELETE FROM embedding_cache_refs
                WHERE ref_kind = 'node'
                  AND persona_id = ?
                  AND node_type = ?
                  AND node_id = ?
                """,
                (persona_id, node_type, node_id),
            )
            _delete_orphan_embeddings(db)

    def clear_namespace_refs(self, persona_id: str) -> None:
        if not self._cache_config.db_path.exists():
            return
        with self._connect() as db:
            _ensure_schema(db)
            db.execute(
                "DELETE FROM embedding_cache_refs WHERE persona_id = ?",
                (persona_id,),
            )
            _delete_orphan_embeddings(db)

    def cleanup_expired_query_refs(self) -> None:
        if not self._cache_config.db_path.exists():
            return
        cutoff = (
            datetime.now(timezone.utc)
            - timedelta(days=self._cache_config.ttl_days_for_query)
        ).isoformat(timespec="seconds")
        with self._connect() as db:
            _ensure_schema(db)
            db.execute(
                """
                DELETE FROM embedding_cache_refs
                WHERE ref_kind = 'query'
                  AND created_at < ?
                """,
                (cutoff,),
            )
            _delete_orphan_embeddings(db)

    def _connect(self) -> sqlite3.Connection:
        return sqlite3.connect(self._cache_config.db_path)


def build_embedding_provider(
    config: EmbeddingConfig, env: Mapping[str, str] | None = None
) -> EmbeddingProvider:
    if config.provider == "openai-compatible":
        return OpenAICompatibleEmbeddingProvider(config, env=env)
    raise ValueError(f"unsupported embedding provider: {config.provider}")


def _embedding_cache_key(
    text: str,
    embedding_config: EmbeddingConfig,
    cache_config: EmbeddingCacheConfig,
) -> str:
    metadata = _embedding_cache_metadata(text, embedding_config, cache_config)
    encoded = json.dumps(metadata, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _embedding_cache_metadata(
    text: str,
    embedding_config: EmbeddingConfig,
    cache_config: EmbeddingCacheConfig,
) -> dict[str, str | int]:
    normalized_text = _normalize_text(text)
    return {
        "provider_kind": embedding_config.provider,
        "base_url_hash": _sha256_hex(embedding_config.base_url.rstrip("/")),
        "model": embedding_config.model,
        "dimensions": embedding_config.dimensions,
        "encoding_format": embedding_config.encoding_format,
        "text_normalization_version": cache_config.text_normalization_version,
        "searchable_text_version": cache_config.searchable_text_version,
        "normalized_text_sha256": _sha256_hex(normalized_text),
    }


def _ensure_schema(db: sqlite3.Connection) -> None:
    db.execute(
        """
        CREATE TABLE IF NOT EXISTS embeddings (
            cache_key TEXT PRIMARY KEY,
            provider_kind TEXT NOT NULL,
            base_url_hash TEXT NOT NULL,
            model TEXT NOT NULL,
            dimensions INTEGER NOT NULL,
            encoding_format TEXT NOT NULL,
            text_normalization_version TEXT NOT NULL,
            searchable_text_version TEXT NOT NULL,
            normalized_text_sha256 TEXT NOT NULL,
            vector_json TEXT NOT NULL,
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL
        )
        """
    )
    _ensure_ref_schema(db)


def _ensure_ref_schema(db: sqlite3.Connection) -> None:
    row = db.execute(
        """
        SELECT name FROM sqlite_master
        WHERE type = 'table' AND name = 'embedding_cache_refs'
        """
    ).fetchone()
    if row is None:
        _create_ref_table(db)
        return
    table_info = db.execute("PRAGMA table_info(embedding_cache_refs)").fetchall()
    columns = {str(row[1]) for row in table_info}
    primary_key = [
        str(row[1])
        for row in sorted(table_info, key=lambda value: int(value[5]))
        if int(row[5]) > 0
    ]
    expected_columns = {
        "cache_key",
        "ref_kind",
        "ref_id",
        "persona_id",
        "node_type",
        "node_id",
        "created_at",
    }
    expected_primary_key = [
        "cache_key",
        "ref_kind",
        "ref_id",
        "persona_id",
        "node_type",
        "node_id",
    ]
    if expected_columns <= columns and primary_key == expected_primary_key:
        return

    existing_columns = [column for column in expected_columns if column in columns]
    rows = []
    if existing_columns:
        selected = ", ".join(existing_columns)
        rows = db.execute(f"SELECT {selected} FROM embedding_cache_refs").fetchall()

    db.execute("ALTER TABLE embedding_cache_refs RENAME TO embedding_cache_refs_old")
    _create_ref_table(db)
    for row in rows:
        item = dict(zip(existing_columns, row))
        db.execute(
            """
            INSERT OR IGNORE INTO embedding_cache_refs (
                cache_key,
                ref_kind,
                ref_id,
                persona_id,
                node_type,
                node_id,
                created_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                str(item.get("cache_key", "")),
                str(item.get("ref_kind", "")),
                str(item.get("ref_id", "")),
                str(item.get("persona_id", "")),
                str(item.get("node_type", "")),
                str(item.get("node_id", "")),
                str(item.get("created_at", _utc_now())),
            ),
        )
    db.execute("DROP TABLE embedding_cache_refs_old")


def _create_ref_table(db: sqlite3.Connection) -> None:
    db.execute(
        """
        CREATE TABLE embedding_cache_refs (
            cache_key TEXT NOT NULL,
            ref_kind TEXT NOT NULL,
            ref_id TEXT NOT NULL,
            persona_id TEXT NOT NULL DEFAULT '',
            node_type TEXT NOT NULL DEFAULT '',
            node_id TEXT NOT NULL DEFAULT '',
            created_at TEXT NOT NULL,
            PRIMARY KEY (
                cache_key,
                ref_kind,
                ref_id,
                persona_id,
                node_type,
                node_id
            ),
            FOREIGN KEY (cache_key) REFERENCES embeddings(cache_key) ON DELETE CASCADE
        )
        """
    )


def _insert_ref(
    db: sqlite3.Connection,
    key: str,
    ref: Mapping[str, str] | None,
    now: str,
) -> None:
    if ref is None:
        return
    ref_kind = ref.get("kind")
    ref_id = ref.get("id") or ref.get("node_id")
    if not ref_kind or not ref_id:
        return
    persona_id = str(ref.get("persona_id", ""))
    node_type = str(ref.get("node_type", ""))
    node_id = str(ref.get("node_id", ""))
    db.execute(
        """
        INSERT OR IGNORE INTO embedding_cache_refs (
            cache_key,
            ref_kind,
            ref_id,
            persona_id,
            node_type,
            node_id,
            created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
        """,
        (key, str(ref_kind), str(ref_id), persona_id, node_type, node_id, now),
    )


def _delete_orphan_embeddings(db: sqlite3.Connection) -> None:
    db.execute(
        """
        DELETE FROM embeddings
        WHERE cache_key NOT IN (
            SELECT DISTINCT cache_key FROM embedding_cache_refs
        )
        """
    )


def _normalize_text(text: str) -> str:
    return unicodedata.normalize("NFC", text)


def _sha256_hex(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


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
