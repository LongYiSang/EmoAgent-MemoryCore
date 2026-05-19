from __future__ import annotations

import threading
from pathlib import Path
from typing import Any

import pytest

triviumdb = pytest.importorskip("triviumdb")

from memorycore_sidecar.adapters.trivium import TriviumAdapter
from memorycore_sidecar.config import (
    EmbeddingCacheConfig,
    EmbeddingConfig,
    QueryAnalysisConfig,
    RerankConfig,
    SidecarConfig,
    TriviumConfig,
)
from memorycore_sidecar.embedding import FakeEmbeddingProvider


def test_trivium_adapter_upserts_updates_and_searches_real_db(tmp_path: Path):
    adapter = TriviumAdapter(
        _config(tmp_path),
        FakeEmbeddingProvider(
            {
                "coffee fact": [1.0, 0.0, 0.0],
                "espresso fact": [1.0, 0.0, 0.0],
                "coffee query": [1.0, 0.0, 0.0],
            }
        ),
    )
    node = {
        "persona_id": "alice",
        "node_type": "fact",
        "sqlite_node_id": "fact-1",
        "searchable_text": "coffee fact",
        "payload": {"importance": "high"},
    }

    first = adapter.upsert_node(node)
    second = adapter.upsert_node({**node, "searchable_text": "espresso fact"})

    assert first == second
    assert first["trivium_node_id"] > 0
    result = adapter.find_candidates(
        {
            "request_id": "query-1",
            "persona_id": "alice",
            "query": {"raw_text": "coffee query"},
            "limit": 5,
            "debug_scores": False,
        }
    )
    assert result == {
        "candidates": [
            {
                "trivium_node_id": first["trivium_node_id"],
                "fused_score": pytest.approx(1.0),
                "primary_source": "raw_dense",
                "primary_purpose": "raw_query",
                "rank": 1,
                "hit_count": 1,
            }
        ],
        "degraded": False,
    }
    assert "diagnostics" not in result
    assert "score_breakdown" not in result["candidates"][0]

    stored = _stored_node(adapter, "alice", first["trivium_node_id"])
    assert stored.payload["persona_id"] == "alice"
    assert stored.payload["node_type"] == "fact"
    assert stored.payload["sqlite_node_id"] == "fact-1"
    assert stored.payload["searchable_text"] == "espresso fact"
    assert stored.payload["payload"] == {"importance": "high"}


def test_trivium_adapter_delete_node_is_idempotent(tmp_path: Path):
    adapter = TriviumAdapter(
        _config(tmp_path),
        FakeEmbeddingProvider({"safe text": [1.0, 0.0, 0.0], "query": [1.0, 0.0, 0.0]}),
    )
    node = {
        "persona_id": "alice",
        "node_type": "fact",
        "sqlite_node_id": "fact-1",
        "searchable_text": "safe text",
        "payload": {},
    }
    upserted = adapter.upsert_node(node)

    assert adapter.delete_node(
        {"persona_id": "alice", "node_type": "fact", "sqlite_node_id": "fact-1"}
    ) == {}
    assert adapter.delete_node(
        {"persona_id": "alice", "node_type": "fact", "sqlite_node_id": "fact-1"}
    ) == {}
    assert adapter.find_candidates(
        {"persona_id": "alice", "query": {"raw_text": "query"}, "limit": 5}
    ) == {
        "candidates": [],
        "degraded": False,
    }
    assert _stored_node(adapter, "alice", upserted["trivium_node_id"]) is None


def test_trivium_adapter_find_candidates_clamps_and_filters_scores():
    class Hit:
        def __init__(self, node_id: int, score: Any) -> None:
            self.id = node_id
            self.score = score

    class SearchDB:
        def search(self, *args: Any, **kwargs: Any) -> list[Hit]:
            return [
                Hit(1, 1.25),
                Hit(2, 0.42),
                Hit(3, 0),
                Hit(4, -0.1),
                Hit(5, float("nan")),
                Hit(6, float("inf")),
                Hit(7, "not-a-score"),
            ]

    adapter = TriviumAdapter.__new__(TriviumAdapter)
    adapter.embedding_provider = FakeEmbeddingProvider({"query": [1.0, 0.0, 0.0]})
    adapter._lock = threading.RLock()
    adapter._dbs = {"alice": SearchDB()}

    assert adapter.find_candidates(
        {"persona_id": "alice", "query": {"raw_text": "query"}, "limit": 10}
    ) == {
        "candidates": [
            {
                "trivium_node_id": 1,
                "fused_score": 1.0,
                "primary_source": "raw_dense",
                "primary_purpose": "raw_query",
                "rank": 1,
                "hit_count": 1,
            },
            {
                "trivium_node_id": 2,
                "fused_score": pytest.approx(0.42),
                "primary_source": "raw_dense",
                "primary_purpose": "raw_query",
                "rank": 2,
                "hit_count": 1,
            },
        ],
        "degraded": False,
    }


def test_trivium_adapter_candidate_v02_overfetches_merges_and_exposes_debug_breakdown():
    class Hit:
        def __init__(self, node_id: int, score: Any) -> None:
            self.id = node_id
            self.score = score

    class SearchDB:
        def __init__(self) -> None:
            self.calls: list[dict[str, Any]] = []

        def search(self, query_vector, *, top_k, expand_depth, min_score, payload_filter):
            self.calls.append({"vector": query_vector, "top_k": top_k})
            key = tuple(query_vector)
            if key == (1.0, 0.0, 0.0):
                return [Hit(1, 0.5)]
            if key == (0.0, 1.0, 0.0):
                return [Hit(1, 0.9), Hit(2, 0.8)]
            if key == (0.0, 0.0, 1.0):
                return [Hit(2, 0.7)]
            return []

    db = SearchDB()
    adapter = TriviumAdapter.__new__(TriviumAdapter)
    adapter.embedding_provider = FakeEmbeddingProvider(
        {
            "raw": [1.0, 0.0, 0.0],
            "rewrite": [0.0, 1.0, 0.0],
            "anchor": [0.0, 0.0, 1.0],
        }
    )
    adapter._lock = threading.RLock()
    adapter._dbs = {"alice": db}

    result = adapter.find_candidates(
        {
            "request_id": "candidate-1",
            "persona_id": "alice",
            "limit": 2,
            "debug_scores": True,
            "query": {
                "raw_text": "raw",
                "rewrites": [
                    {"text": "rewrite", "weight": 0.5, "purpose": "semantic_recall"}
                ],
                "semantic_anchors": [
                    {"text": "anchor", "weight": 0.4, "purpose": "semantic_anchor"}
                ],
            },
        }
    )

    assert [call["top_k"] for call in db.calls] == [32, 32, 16]
    assert [candidate["trivium_node_id"] for candidate in result["candidates"]] == [1, 2]
    assert result["candidates"][0]["primary_source"] == "raw_dense"
    assert result["candidates"][0]["hit_count"] == 2
    assert result["candidates"][0]["score_breakdown"]["score_norm_method"] == "weighted_max_rrf"
    assert result["diagnostics"]["query_count"] == 3
    assert result["diagnostics"]["per_query_counts"] == [
        {"source": "raw_dense", "purpose": "raw_query", "count": 1},
        {"source": "semantic_rewrite_dense", "purpose": "semantic_recall", "count": 2},
        {"source": "semantic_anchor_dense", "purpose": "semantic_anchor", "count": 1},
    ]


def test_trivium_adapter_close_clears_registry_before_closing_handles():
    close_state: list[tuple[str, list[str]]] = []

    class CloseDB:
        def __init__(self, name: str, adapter: TriviumAdapter) -> None:
            self.name = name
            self.adapter = adapter

        def close(self) -> None:
            close_state.append((self.name, list(self.adapter._dbs)))

    adapter = TriviumAdapter.__new__(TriviumAdapter)
    adapter._lock = threading.RLock()
    adapter._dbs = {}
    adapter._dbs["alice"] = CloseDB("alice", adapter)
    adapter._dbs["bob"] = CloseDB("bob", adapter)

    adapter.close()

    assert adapter._dbs == {}
    assert close_state == [("alice", []), ("bob", [])]


def test_trivium_adapter_close_attempts_remaining_handles_after_close_error():
    closed: list[str] = []

    class CloseDB:
        def __init__(self, name: str, fail: bool = False) -> None:
            self.name = name
            self.fail = fail

        def close(self) -> None:
            closed.append(self.name)
            if self.fail:
                raise RuntimeError(f"{self.name} close failed")

    adapter = TriviumAdapter.__new__(TriviumAdapter)
    adapter._lock = threading.RLock()
    adapter._dbs = {
        "alice": CloseDB("alice", fail=True),
        "bob": CloseDB("bob"),
    }

    with pytest.raises(RuntimeError, match="alice close failed"):
        adapter.close()

    assert adapter._dbs == {}
    assert closed == ["alice", "bob"]


def test_trivium_adapter_activation_reuses_filtered_neighbors_for_degree():
    class Edge:
        def __init__(self, target_id: int, label: str = "ABOUT_ENTITY") -> None:
            self.target_id = target_id
            self.label = label
            self.weight = 1.0

    class GraphDB:
        def __init__(self) -> None:
            self.edge_calls: dict[int, int] = {}

        def get(self, node_id: int):
            return {"id": node_id} if node_id in {1, 2, 4, 5} else None

        def get_edges(self, node_id: int):
            self.edge_calls[node_id] = self.edge_calls.get(node_id, 0) + 1
            return {
                1: [Edge(2)],
                2: [Edge(4), Edge(5), Edge(999)],
            }.get(node_id, [])

    db = GraphDB()
    adapter = TriviumAdapter.__new__(TriviumAdapter)
    adapter._lock = threading.RLock()
    adapter._dbs = {"alice": db}

    result = adapter.activate_graph(
        {
            "persona_id": "alice",
            "seeds": [{"trivium_node_id": 1, "seed_energy": 1.0}],
            "params": {
                "max_hops": 2,
                "hop_decay": 1.0,
                "hub_suppression_power": 1.0,
            },
        }
    )
    by_id = {candidate["trivium_node_id"]: candidate for candidate in result["candidates"]}

    assert db.edge_calls[2] == 1
    assert by_id[2]["score"] == pytest.approx(0.4)


def test_trivium_adapter_clear_namespace_removes_only_persona_files(tmp_path: Path):
    adapter = TriviumAdapter(
        _config(tmp_path),
        FakeEmbeddingProvider(
            {
                "alice text": [1.0, 0.0, 0.0],
                "bob text": [0.0, 1.0, 0.0],
            }
        ),
    )
    adapter.upsert_node(_node("alice", "fact-1", "alice text"))
    adapter.upsert_node(_node("bob", "fact-1", "bob text"))
    before = set(tmp_path.iterdir())

    assert adapter.clear_namespace("alice") == {}

    after = set(tmp_path.iterdir())
    assert after
    assert after < before
    assert len(list(tmp_path.glob("*.tdb"))) == 1
    assert adapter.find_candidates(
        {"persona_id": "alice", "query": {"raw_text": "alice text"}, "limit": 5}
    ) == {
        "candidates": [],
        "degraded": False,
    }


def test_trivium_adapter_sanitizes_persona_id_to_prevent_path_traversal(
    tmp_path: Path,
):
    adapter = TriviumAdapter(
        _config(tmp_path),
        FakeEmbeddingProvider({"safe text": [1.0, 0.0, 0.0]}),
    )
    outside = tmp_path.parent / "outside-persona.tdb"

    adapter.upsert_node(_node("../outside-persona", "fact-1", "safe text"))

    assert not outside.exists()
    assert len(list(tmp_path.glob("*.tdb"))) == 1
    assert all(path.resolve().is_relative_to(tmp_path.resolve()) for path in tmp_path.iterdir())


def test_trivium_adapter_upsert_edge_links_existing_nodes_without_duplicates(
    tmp_path: Path,
):
    adapter = TriviumAdapter(
        _config(tmp_path),
        FakeEmbeddingProvider(
            {
                "source": [1.0, 0.0, 0.0],
                "target": [0.0, 1.0, 0.0],
            }
        ),
    )
    source = adapter.upsert_node(_node("alice", "fact-1", "source"))
    target = adapter.upsert_node(_node("alice", "entity-1", "target", node_type="entity"))
    edge = {
        "persona_id": "alice",
        "sqlite_edge_id": "edge-1",
        "from_node_type": "fact",
        "from_node_id": "fact-1",
        "to_node_type": "entity",
        "to_node_id": "entity-1",
        "link_type": "ABOUT_ENTITY",
        "weight": "0.75",
    }

    assert adapter.upsert_edge(edge) == {}
    assert adapter.upsert_edge(edge) == {}

    edges = adapter._db_for_persona("alice").get_edges(source["trivium_node_id"])
    matching = [
        edge
        for edge in edges
        if edge.target_id == target["trivium_node_id"] and edge.label == "ABOUT_ENTITY"
    ]
    assert len(matching) == 1
    assert matching[0].weight == pytest.approx(0.75)


def test_trivium_adapter_upsert_edge_fails_when_endpoint_missing(tmp_path: Path):
    adapter = TriviumAdapter(_config(tmp_path), FakeEmbeddingProvider())

    with pytest.raises(RuntimeError, match="upsert_edge endpoint is not indexed"):
        adapter.upsert_edge(
            {
                "persona_id": "alice",
                "sqlite_edge_id": "edge-1",
                "from_node_type": "fact",
                "from_node_id": "missing-source",
                "to_node_type": "entity",
                "to_node_id": "missing-target",
            }
        )
    edge = {
        "persona_id": "alice",
        "sqlite_edge_id": "edge-1",
        "link_type": "ABOUT_ENTITY",
        "from_node_type": "fact",
        "from_node_id": "missing-source",
        "to_node_type": "entity",
        "to_node_id": "missing-target",
    }
    db = adapter._db_for_persona("alice")
    has_unlink_api = any(
        callable(getattr(db, name, None))
        for name in ("unlink", "delete_edge", "remove_edge")
    )
    if has_unlink_api:
        assert adapter.delete_edge(edge) == {}
    else:
        with pytest.raises(
            RuntimeError,
            match="delete_edge requires mirror rebuild: TriviumDB adapter has no unlink API",
        ):
            adapter.delete_edge(edge)


def test_trivium_adapter_delete_edge_uses_unlink_when_supported():
    calls: list[tuple[int, int, str]] = []
    flushed: list[bool] = []

    class UnlinkDB:
        def get(self, node_id: int):
            return {"id": node_id}

        def unlink(self, source_id: int, target_id: int, label: str):
            calls.append((source_id, target_id, label))
            return True

        def flush(self):
            flushed.append(True)

    adapter = TriviumAdapter.__new__(TriviumAdapter)
    adapter._lock = threading.RLock()
    adapter._dbs = {"alice": UnlinkDB()}

    assert (
        adapter.delete_edge(
            {
                "persona_id": "alice",
                "sqlite_edge_id": "edge-1",
                "link_type": "ABOUT_ENTITY",
                "from_node_type": "fact",
                "from_node_id": "fact-1",
                "to_node_type": "entity",
                "to_node_id": "entity-1",
            }
        )
        == {}
    )
    assert calls and calls[0][2] == "ABOUT_ENTITY"
    assert flushed == [True]


def test_trivium_adapter_wraps_embedding_provider_and_records_node_and_query_refs(
    tmp_path: Path,
):
    cache_path = tmp_path / "embedding-cache.sqlite3"
    adapter = TriviumAdapter(
        _config(tmp_path / "trivium", cache_mode="read_write", cache_path=cache_path),
        FakeEmbeddingProvider(
            {
                "node text": [1.0, 0.0, 0.0],
                "query text": [1.0, 0.0, 0.0],
            }
        ),
    )

    upserted = adapter.upsert_node(_node("alice", "fact-1", "node text"))
    adapter.find_candidates(
        {
            "request_id": "query-1",
            "persona_id": "alice",
            "query": {"raw_text": "query text"},
            "limit": 5,
        }
    )
    result = adapter.find_candidates(
        {
            "request_id": "query-1",
            "persona_id": "alice",
            "query": {"raw_text": "query text"},
            "limit": 5,
        }
    )

    import sqlite3

    with sqlite3.connect(cache_path) as db:
        refs = db.execute(
            "SELECT ref_kind, ref_id FROM embedding_cache_refs ORDER BY ref_kind, ref_id"
        ).fetchall()

    assert upserted["trivium_node_id"] > 0
    assert refs == [("node", "fact-1"), ("query", "query-1")]
    assert result["embedding_cache_stats"]["hits"] >= 1
    assert result["embedding_cache_stats"]["live_call_count"] >= 2


def test_trivium_adapter_delete_node_removes_only_matching_cache_ref(
    tmp_path: Path,
):
    cache_path = tmp_path / "embedding-cache.sqlite3"
    adapter = TriviumAdapter(
        _config(tmp_path / "trivium", cache_mode="read_write", cache_path=cache_path),
        FakeEmbeddingProvider({"shared text": [1.0, 0.0, 0.0]}),
    )
    adapter.upsert_node(_node("alice", "fact-1", "shared text"))
    adapter.upsert_node(_node("bob", "fact-1", "shared text"))

    assert adapter.delete_node(
        {"persona_id": "alice", "node_type": "fact", "sqlite_node_id": "fact-1"}
    ) == {}

    import sqlite3

    with sqlite3.connect(cache_path) as db:
        refs = db.execute(
            """
            SELECT ref_kind, persona_id, node_type, node_id, ref_id
            FROM embedding_cache_refs
            ORDER BY persona_id, node_type, node_id
            """
        ).fetchall()
        embedding_count = db.execute("SELECT COUNT(*) FROM embeddings").fetchone()[0]

    assert refs == [("node", "bob", "fact", "fact-1", "fact-1")]
    assert embedding_count == 1

    assert adapter.delete_node(
        {"persona_id": "bob", "node_type": "fact", "sqlite_node_id": "fact-1"}
    ) == {}
    with sqlite3.connect(cache_path) as db:
        refs = db.execute("SELECT ref_kind, ref_id FROM embedding_cache_refs").fetchall()
        embedding_count = db.execute("SELECT COUNT(*) FROM embeddings").fetchone()[0]

    assert refs == []
    assert embedding_count == 0


def test_trivium_adapter_clear_namespace_removes_cache_refs_and_orphans(
    tmp_path: Path,
):
    cache_path = tmp_path / "embedding-cache.sqlite3"
    adapter = TriviumAdapter(
        _config(tmp_path / "trivium", cache_mode="read_write", cache_path=cache_path),
        FakeEmbeddingProvider(
            {
                "alice text": [1.0, 0.0, 0.0],
                "bob text": [0.0, 1.0, 0.0],
            }
        ),
    )
    adapter.upsert_node(_node("alice", "fact-1", "alice text"))
    adapter.upsert_node(_node("bob", "fact-1", "bob text"))

    assert adapter.clear_namespace("alice", purge_embedding_cache=True) == {}

    import sqlite3

    with sqlite3.connect(cache_path) as db:
        refs = db.execute(
            """
            SELECT ref_kind, persona_id, node_type, node_id, ref_id
            FROM embedding_cache_refs
            ORDER BY persona_id, node_type, node_id
            """
        ).fetchall()
        embedding_count = db.execute("SELECT COUNT(*) FROM embeddings").fetchone()[0]

    assert refs == [("node", "bob", "fact", "fact-1", "fact-1")]
    assert embedding_count == 1


def test_trivium_adapter_clear_namespace_preserves_embedding_cache_by_default(
    tmp_path: Path,
):
    cache_path = tmp_path / "embedding-cache.sqlite3"
    adapter = TriviumAdapter(
        _config(tmp_path / "trivium", cache_mode="read_write", cache_path=cache_path),
        FakeEmbeddingProvider({"alice text": [1.0, 0.0, 0.0]}),
    )
    adapter.upsert_node(_node("alice", "fact-1", "alice text"))

    assert adapter.clear_namespace("alice") == {}

    import sqlite3

    with sqlite3.connect(cache_path) as db:
        refs = db.execute(
            "SELECT ref_kind, persona_id, node_type, node_id FROM embedding_cache_refs"
        ).fetchall()
        embedding_count = db.execute("SELECT COUNT(*) FROM embeddings").fetchone()[0]

    assert refs == [("node", "alice", "fact", "fact-1")]
    assert embedding_count == 1


def test_trivium_adapter_configure_eval_switches_trivium_dir_and_cache_mode(
    tmp_path: Path,
):
    cache_path = tmp_path / "embedding-cache.sqlite3"
    adapter = TriviumAdapter(
        _config(tmp_path / "initial", cache_mode="off", cache_path=cache_path),
        FakeEmbeddingProvider({"node text": [1.0, 0.0, 0.0]}),
    )
    artifact_dir = tmp_path / "artifact" / "trivium"
    configured = adapter.configure_eval(
        {
            "trivium_dir": str(artifact_dir),
            "embedding_cache_mode": "read_write",
            "embedding_cache_db_path": str(cache_path),
            "searchable_text_version": "memorycore_searchable_text_v1",
            "text_normalization_version": "norm-v2",
        }
    )

    upserted = adapter.upsert_node(_node("alice", "fact-1", "node text"))

    assert configured["trivium_dir"] == str(artifact_dir.resolve())
    assert configured["embedding_cache_mode"] == "read_write"
    assert configured["embedding_cache_db_path"] == str(cache_path.resolve())
    assert configured["embedding"]["model"] == "test-embedding"
    assert configured["embedding"]["dimensions"] == "3"
    assert configured["embedding"]["encoding_format"] == "float"
    assert configured["embedding"]["searchable_text_version"] == "memorycore_searchable_text_v1"
    assert configured["embedding"]["text_normalization_version"] == "norm-v2"
    assert configured["embedding"]["fingerprint"]
    assert configured["trivium_adapter_version"]
    assert configured["triviumdb_version"]
    assert upserted["trivium_node_id"] > 0
    assert any(artifact_dir.glob("*"))
    assert cache_path.exists()


def test_trivium_adapter_configure_eval_reports_rerank_capability_and_counts(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
):
    monkeypatch.setenv("TEST_RERANK_KEY", "test-key")
    adapter = TriviumAdapter(
        _config(
            tmp_path / "trivium",
            rerank_provider="dashscope-vl",
            rerank_api_key_env="TEST_RERANK_KEY",
        ),
        FakeEmbeddingProvider(
            {
                "source": [1.0, 0.0, 0.0],
                "target": [0.0, 1.0, 0.0],
            }
        ),
    )
    adapter.upsert_node(_node("alice", "fact-1", "source"))
    adapter.upsert_node(_node("alice", "fact-2", "target"))
    adapter.upsert_edge(
        {
            "persona_id": "alice",
            "sqlite_edge_id": "edge-1",
            "from_node_type": "fact",
            "from_node_id": "fact-1",
            "to_node_type": "fact",
            "to_node_id": "fact-2",
            "link_type": "CAUSES",
            "weight": 1.0,
        }
    )

    configured = adapter.configure_eval({"trivium_dir": str(tmp_path / "trivium")})

    assert configured["rerank_provider_available"] is True
    assert configured["rerank_provider_mode"] == "live"
    assert configured["rerank_cache"] is False
    assert configured["mirror_stats_available"] is True
    assert configured["mirror_node_count"] == 2
    assert configured["mirror_edge_count"] == 1


def test_trivium_adapter_configure_eval_reports_missing_rerank_provider(
    tmp_path: Path,
):
    adapter = TriviumAdapter(
        _config(tmp_path / "trivium"),
        FakeEmbeddingProvider({"node text": [1.0, 0.0, 0.0]}),
    )

    configured = adapter.configure_eval({"trivium_dir": str(tmp_path / "trivium")})

    assert configured["rerank_provider_available"] is False
    assert configured["rerank_provider_mode"] == "none"
    assert configured["rerank_capability_reason"] == "rerank_provider_none"


def test_trivium_adapter_configure_eval_rejects_unknown_embedding_cache_mode(
    tmp_path: Path,
):
    adapter = TriviumAdapter(
        _config(tmp_path / "initial", cache_mode="off"),
        FakeEmbeddingProvider({"node text": [1.0, 0.0, 0.0]}),
    )

    with pytest.raises(ValueError, match="embedding_cache.mode"):
        adapter.configure_eval({"embedding_cache_mode": "typo"})


def _config(
    tmp_path: Path,
    *,
    cache_mode: str = "off",
    cache_path: Path | None = None,
    rerank_provider: str = "none",
    rerank_api_key_env: str = "DASHSCOPE_API_KEY",
) -> SidecarConfig:
    return SidecarConfig(
        trivium=TriviumConfig(dir=tmp_path, dtype="f32", sync_mode="normal"),
        embedding=EmbeddingConfig(
            provider="openai-compatible",
            base_url="https://example.test/v1",
            api_key_env="TEST_KEY",
            model="test-embedding",
            dimensions=3,
            timeout_seconds=2,
            encoding_format="float",
        ),
        embedding_cache=EmbeddingCacheConfig(
            mode=cache_mode,
            db_path=tmp_path / "embedding-cache.sqlite3"
            if cache_path is None
            else cache_path,
            text_normalization_version="norm-v1",
            searchable_text_version="search-v1",
            ttl_days_for_query=14,
            store_raw_text=False,
        ),
        rerank=RerankConfig(
            provider=rerank_provider,
            endpoint_url="https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank",
            api_key_env=rerank_api_key_env,
            model="qwen3-vl-rerank",
            timeout_seconds=30,
            top_n=30,
            instruct="Retrieve semantically relevant safe memory summaries for the user's query.",
        ),
        query_analysis=QueryAnalysisConfig(
            provider="none",
            base_url="https://example.test/v1",
            api_key_env="TEST_QUERY_KEY",
            model="test-query",
            timeout_seconds=2,
            temperature=0.0,
            response_format="json_object",
            prompt_version="query-analysis-v0.1",
        ),
    )


def _node(
    persona_id: str,
    sqlite_node_id: str,
    searchable_text: str,
    *,
    node_type: str = "fact",
    payload: dict[str, Any] | None = None,
) -> dict[str, Any]:
    return {
        "persona_id": persona_id,
        "node_type": node_type,
        "sqlite_node_id": sqlite_node_id,
        "searchable_text": searchable_text,
        "payload": {} if payload is None else payload,
    }


def _stored_node(adapter: TriviumAdapter, persona_id: str, trivium_node_id: int):
    return adapter._db_for_persona(persona_id).get(trivium_node_id)
