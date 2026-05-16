from __future__ import annotations

import threading
from pathlib import Path
from typing import Any

import pytest

triviumdb = pytest.importorskip("triviumdb")

from memorycore_sidecar.adapters.trivium import TriviumAdapter
from memorycore_sidecar.config import (
    EmbeddingConfig,
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
        {"persona_id": "alice", "query_text": "coffee query", "limit": 5}
    )
    assert result == {
        "candidates": [
            {
                "trivium_node_id": first["trivium_node_id"],
                "score": pytest.approx(1.0),
                "source": "trivium_vector",
            }
        ],
        "degraded": False,
    }

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
        {"persona_id": "alice", "query_text": "query", "limit": 5}
    ) == {"candidates": [], "degraded": False}
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
        {"persona_id": "alice", "query_text": "query", "limit": 10}
    ) == {
        "candidates": [
            {"trivium_node_id": 1, "score": 1.0, "source": "trivium_vector"},
            {"trivium_node_id": 2, "score": 0.42, "source": "trivium_vector"},
        ],
        "degraded": False,
    }


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
        {"persona_id": "alice", "query_text": "alice text", "limit": 5}
    ) == {"candidates": [], "degraded": False}


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


def _config(tmp_path: Path) -> SidecarConfig:
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
        rerank=RerankConfig(
            provider="none",
            endpoint_url="https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank",
            api_key_env="DASHSCOPE_API_KEY",
            model="qwen3-vl-rerank",
            timeout_seconds=30,
            top_n=30,
            instruct="Retrieve semantically relevant safe memory summaries for the user's query.",
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
