from __future__ import annotations

import json
import sqlite3
import threading
from datetime import datetime, timedelta, timezone
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

import pytest

from memorycore_sidecar.config import EmbeddingCacheConfig, EmbeddingConfig
from memorycore_sidecar.embedding import (
    CachedEmbeddingProvider,
    EmbeddingCacheMiss,
    FakeEmbeddingProvider,
    OpenAICompatibleEmbeddingProvider,
    build_embedding_provider,
)


def test_openai_compatible_provider_posts_dashscope_default_body_headers_and_path():
    server, captured = _start_embedding_server(
        HTTPStatus.OK,
        {"data": [{"embedding": [0.1, 0.2, 0.3]}]},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(
                base_url=f"http://127.0.0.1:{server.server_address[1]}/compatible-mode/v1/"
            ),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        result = provider.embed("用户喜欢咖啡")

        assert result == [0.1, 0.2, 0.3]
        assert captured["path"] == "/compatible-mode/v1/embeddings"
        assert captured["headers"]["authorization"] == "Bearer test-secret"
        assert captured["headers"]["content-type"] == "application/json"
        assert captured["body"] == {
            "model": "text-embedding-v4",
            "input": "用户喜欢咖啡",
            "encoding_format": "float",
            "dimensions": 3,
        }
    finally:
        _stop_server(server)


def test_openai_compatible_provider_missing_key_names_env_var_without_secret_value():
    provider = OpenAICompatibleEmbeddingProvider(
        _embedding_config(api_key_env="MISSING_DASHSCOPE_KEY"),
        env={},
    )

    with pytest.raises(RuntimeError) as err:
        provider.embed("text")

    assert "MISSING_DASHSCOPE_KEY" in str(err.value)


def test_openai_compatible_provider_http_failure_has_clear_error():
    server, _captured = _start_embedding_server(
        HTTPStatus.BAD_GATEWAY,
        {"error": {"message": "upstream unavailable"}},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        with pytest.raises(RuntimeError, match="embedding request failed with HTTP 502"):
            provider.embed("text")
    finally:
        _stop_server(server)


@pytest.mark.parametrize(
    "body",
    [
        {},
        {"data": []},
        {"data": [{}]},
        {"data": [{"embedding": "not-a-list"}]},
    ],
)
def test_openai_compatible_provider_rejects_malformed_response(body):
    server, _captured = _start_embedding_server(HTTPStatus.OK, body)
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        with pytest.raises(ValueError, match="data\\[0\\]\\.embedding"):
            provider.embed("text")
    finally:
        _stop_server(server)


def test_openai_compatible_provider_rejects_wrong_dimension():
    server, _captured = _start_embedding_server(
        HTTPStatus.OK,
        {"data": [{"embedding": [0.1, 0.2]}]},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        with pytest.raises(ValueError, match="expected 3 dimensions"):
            provider.embed("text")
    finally:
        _stop_server(server)


def test_openai_compatible_provider_coerces_successful_embedding_values_to_floats():
    server, _captured = _start_embedding_server(
        HTTPStatus.OK,
        {"data": [{"embedding": [1, "2.5", 3.0]}]},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        result = provider.embed("text")

        assert result == [1.0, 2.5, 3.0]
        assert all(type(value) is float for value in result)
    finally:
        _stop_server(server)


def test_openai_compatible_provider_rejects_malformed_embedding_value_with_index_without_value():
    server, _captured = _start_embedding_server(
        HTTPStatus.OK,
        {"data": [{"embedding": [0.1, "secret-value", 0.3]}]},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        with pytest.raises(ValueError, match=r"data\[0\]\.embedding\[1\]") as err:
            provider.embed("text")

        assert "secret-value" not in str(err.value)
    finally:
        _stop_server(server)


@pytest.mark.parametrize("value", ["nan", "inf", "-inf"])
def test_openai_compatible_provider_rejects_non_finite_embedding_values_with_index_without_value(
    value,
):
    server, _captured = _start_embedding_server(
        HTTPStatus.OK,
        {"data": [{"embedding": [0.1, value, 0.3]}]},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        with pytest.raises(ValueError, match=r"data\[0\]\.embedding\[1\]") as err:
            provider.embed("text")

        assert value not in str(err.value)
    finally:
        _stop_server(server)


def test_build_embedding_provider_accepts_openai_compatible_and_validates_provider():
    provider = build_embedding_provider(
        _embedding_config(provider="openai-compatible"),
        env={"DASHSCOPE_API_KEY": "test-secret"},
    )

    assert isinstance(provider, OpenAICompatibleEmbeddingProvider)

    with pytest.raises(ValueError, match="unsupported embedding provider"):
        build_embedding_provider(
            _embedding_config(provider="other"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )


def test_fake_embedding_provider_returns_configured_vectors():
    provider = FakeEmbeddingProvider({"hello": [1, 2.0]})

    assert provider.embed("hello") == [1.0, 2.0]


@pytest.mark.parametrize("value", ["nan", "inf", "-inf"])
def test_fake_embedding_provider_rejects_non_finite_values_with_index_without_value(value):
    provider = FakeEmbeddingProvider({"hello": [1, value]})

    with pytest.raises(ValueError, match=r"embedding\[1\]") as err:
        provider.embed("hello")

    assert value not in str(err.value)


def test_cached_embedding_provider_read_write_miss_then_hit_writes_only_vector_metadata_and_refs(
    tmp_path: Path,
):
    inner = _CountingEmbeddingProvider({"hello": [0.1, 0.2, 0.3]})
    provider = CachedEmbeddingProvider(
        inner,
        _embedding_config(),
        _cache_config(tmp_path, mode="read_write"),
    )

    assert provider.embed("hello", ref={"kind": "node", "id": "fact-1"}) == [
        0.1,
        0.2,
        0.3,
    ]
    assert provider.embed("hello", ref={"kind": "node", "id": "fact-1"}) == [
        0.1,
        0.2,
        0.3,
    ]

    assert inner.calls == ["hello"]
    assert provider.stats == {
        "hits": 1,
        "misses": 1,
        "live_call_count": 1,
        "writes": 1,
    }
    with sqlite3.connect(tmp_path / "embedding-cache.sqlite3") as db:
        embedding_columns = {
            row[1] for row in db.execute("PRAGMA table_info(embeddings)").fetchall()
        }
        assert "raw_text" not in embedding_columns
        row = db.execute(
            "SELECT vector_json, provider_kind, model, dimensions FROM embeddings"
        ).fetchone()
        assert json.loads(row[0]) == [0.1, 0.2, 0.3]
        assert row[1:] == ("openai-compatible", "text-embedding-v4", 3)
        refs = db.execute(
            "SELECT ref_kind, ref_id FROM embedding_cache_refs"
        ).fetchall()
        assert refs == [("node", "fact-1")]


def test_cached_embedding_provider_read_only_hit_returns_cached_vector_without_live_call(
    tmp_path: Path,
):
    writer = CachedEmbeddingProvider(
        _CountingEmbeddingProvider({"hello": [0.1, 0.2, 0.3]}),
        _embedding_config(),
        _cache_config(tmp_path, mode="read_write"),
    )
    writer.embed("hello")
    reader = CachedEmbeddingProvider(
        _ExplodingEmbeddingProvider(),
        _embedding_config(),
        _cache_config(tmp_path, mode="read_only"),
    )

    assert reader.embed("hello") == [0.1, 0.2, 0.3]
    assert reader.stats == {
        "hits": 1,
        "misses": 0,
        "live_call_count": 0,
        "writes": 0,
    }


def test_cached_embedding_provider_read_only_miss_raises_without_live_call(tmp_path: Path):
    inner = _CountingEmbeddingProvider({"missing": [0.1, 0.2, 0.3]})
    provider = CachedEmbeddingProvider(
        inner,
        _embedding_config(),
        _cache_config(tmp_path, mode="read_only"),
    )

    with pytest.raises(EmbeddingCacheMiss, match="embedding cache miss"):
        provider.embed("missing")

    assert inner.calls == []
    assert provider.stats == {
        "hits": 0,
        "misses": 1,
        "live_call_count": 0,
        "writes": 0,
    }


def test_cached_embedding_provider_refresh_ignores_old_row_and_overwrites_vector(
    tmp_path: Path,
):
    writer = CachedEmbeddingProvider(
        _CountingEmbeddingProvider({"hello": [0.1, 0.2, 0.3]}),
        _embedding_config(),
        _cache_config(tmp_path, mode="read_write"),
    )
    writer.embed("hello")
    refresh_inner = _CountingEmbeddingProvider({"hello": [0.4, 0.5, 0.6]})
    refresher = CachedEmbeddingProvider(
        refresh_inner,
        _embedding_config(),
        _cache_config(tmp_path, mode="refresh"),
    )

    assert refresher.embed("hello") == [0.4, 0.5, 0.6]

    assert refresh_inner.calls == ["hello"]
    assert refresher.stats == {
        "hits": 0,
        "misses": 1,
        "live_call_count": 1,
        "writes": 1,
    }
    reader = CachedEmbeddingProvider(
        _ExplodingEmbeddingProvider(),
        _embedding_config(),
        _cache_config(tmp_path, mode="read_only"),
    )
    assert reader.embed("hello") == [0.4, 0.5, 0.6]


def test_cached_embedding_provider_off_bypasses_cache(tmp_path: Path):
    inner = _CountingEmbeddingProvider({"hello": [0.1, 0.2, 0.3]})
    provider = CachedEmbeddingProvider(
        inner,
        _embedding_config(),
        _cache_config(tmp_path, mode="off"),
    )

    provider.embed("hello")
    provider.embed("hello")

    assert inner.calls == ["hello", "hello"]
    assert provider.stats == {
        "hits": 0,
        "misses": 0,
        "live_call_count": 2,
        "writes": 0,
    }
    assert not (tmp_path / "embedding-cache.sqlite3").exists()


def test_cached_embedding_provider_rejects_unknown_mode(tmp_path: Path):
    provider = CachedEmbeddingProvider(
        _CountingEmbeddingProvider({"hello": [0.1, 0.2, 0.3]}),
        _embedding_config(),
        _cache_config(tmp_path, mode="typo"),
    )

    with pytest.raises(ValueError, match="embedding_cache.mode"):
        provider.embed("hello")


@pytest.mark.parametrize(
    "override",
    [
        {"provider": "alternate-provider"},
        {"base_url": "https://alternate.example.test/v1"},
        {"model": "alternate-model"},
        {"dimensions": 4},
        {"encoding_format": "alternate-format"},
    ],
)
def test_cached_embedding_provider_key_separates_provider_metadata(
    tmp_path: Path, override: dict[str, Any]
):
    base_inner = _CountingEmbeddingProvider({"hello": [0.1, 0.2, 0.3]})
    base = CachedEmbeddingProvider(
        base_inner,
        _embedding_config(),
        _cache_config(tmp_path, mode="read_write"),
    )
    other_dimensions = int(override.get("dimensions", 3))
    other_inner = _CountingEmbeddingProvider(
        {"hello": [float(index) for index in range(other_dimensions)]}
    )
    other = CachedEmbeddingProvider(
        other_inner,
        _embedding_config(**override),
        _cache_config(tmp_path, mode="read_write"),
    )

    base.embed("hello")
    other.embed("hello")

    assert base_inner.calls == ["hello"]
    assert other_inner.calls == ["hello"]
    with sqlite3.connect(tmp_path / "embedding-cache.sqlite3") as db:
        assert db.execute("SELECT COUNT(*) FROM embeddings").fetchone()[0] == 2


def test_cached_embedding_provider_key_separates_text_versions(tmp_path: Path):
    first_inner = _CountingEmbeddingProvider({"hello": [0.1, 0.2, 0.3]})
    first = CachedEmbeddingProvider(
        first_inner,
        _embedding_config(),
        _cache_config(
            tmp_path,
            mode="read_write",
            text_normalization_version="norm-v1",
            searchable_text_version="search-v1",
        ),
    )
    second_inner = _CountingEmbeddingProvider({"hello": [0.4, 0.5, 0.6]})
    second = CachedEmbeddingProvider(
        second_inner,
        _embedding_config(),
        _cache_config(
            tmp_path,
            mode="read_write",
            text_normalization_version="norm-v2",
            searchable_text_version="search-v1",
        ),
    )

    first.embed("hello")
    second.embed("hello")

    assert first_inner.calls == ["hello"]
    assert second_inner.calls == ["hello"]
    with sqlite3.connect(tmp_path / "embedding-cache.sqlite3") as db:
        rows = db.execute(
            "SELECT text_normalization_version, searchable_text_version FROM embeddings"
        ).fetchall()
    assert sorted(rows) == [("norm-v1", "search-v1"), ("norm-v2", "search-v1")]


def test_cached_embedding_provider_cleanup_expired_query_refs_removes_orphans(
    tmp_path: Path,
):
    inner = _CountingEmbeddingProvider(
        {
            "old query": [0.1, 0.2, 0.3],
            "new query": [0.4, 0.5, 0.6],
        }
    )
    provider = CachedEmbeddingProvider(
        inner,
        _embedding_config(),
        _cache_config(tmp_path, mode="read_write", ttl_days_for_query=7),
    )
    provider.embed("old query", ref={"kind": "query", "id": "old", "persona_id": "alice"})
    provider.embed("new query", ref={"kind": "query", "id": "new", "persona_id": "alice"})
    expired = (datetime.now(timezone.utc) - timedelta(days=8)).isoformat(
        timespec="seconds"
    )

    with sqlite3.connect(tmp_path / "embedding-cache.sqlite3") as db:
        db.execute(
            "UPDATE embedding_cache_refs SET created_at = ? WHERE ref_id = 'old'",
            (expired,),
        )

    provider.cleanup_expired_query_refs()

    with sqlite3.connect(tmp_path / "embedding-cache.sqlite3") as db:
        refs = db.execute(
            "SELECT ref_kind, ref_id FROM embedding_cache_refs ORDER BY ref_id"
        ).fetchall()
        embedding_count = db.execute("SELECT COUNT(*) FROM embeddings").fetchone()[0]

    assert refs == [("query", "new")]
    assert embedding_count == 1


def _embedding_config(**overrides: Any) -> EmbeddingConfig:
    values = {
        "provider": "openai-compatible",
        "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
        "api_key_env": "DASHSCOPE_API_KEY",
        "model": "text-embedding-v4",
        "dimensions": 3,
        "timeout_seconds": 2,
        "encoding_format": "float",
    }
    values.update(overrides)
    return EmbeddingConfig(**values)


def _cache_config(tmp_path: Path, **overrides: Any) -> EmbeddingCacheConfig:
    values = {
        "mode": "read_write",
        "db_path": tmp_path / "embedding-cache.sqlite3",
        "text_normalization_version": "norm-v1",
        "searchable_text_version": "search-v1",
        "ttl_days_for_query": 14,
        "store_raw_text": False,
    }
    values.update(overrides)
    return EmbeddingCacheConfig(**values)


class _CountingEmbeddingProvider:
    def __init__(self, embeddings: dict[str, list[float]]) -> None:
        self.embeddings = embeddings
        self.calls: list[str] = []

    def embed(self, text: str) -> list[float]:
        self.calls.append(text)
        return self.embeddings[text]


class _ExplodingEmbeddingProvider:
    def embed(self, text: str) -> list[float]:
        raise AssertionError("live embedding provider should not be called")


def _start_embedding_server(
    status: HTTPStatus, body: dict[str, Any]
) -> tuple[ThreadingHTTPServer, dict[str, Any]]:
    captured: dict[str, Any] = {}

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:
            request_body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
            captured["path"] = self.path
            captured["headers"] = {key.lower(): value for key, value in self.headers.items()}
            captured["body"] = json.loads(request_body.decode("utf-8"))
            data = json.dumps(body).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, format: str, *args: Any) -> None:
            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever)
    thread.start()
    server._test_thread = thread  # type: ignore[attr-defined]
    return server, captured


def _stop_server(server: ThreadingHTTPServer) -> None:
    server.shutdown()
    server.server_close()
    server._test_thread.join(timeout=2)  # type: ignore[attr-defined]
