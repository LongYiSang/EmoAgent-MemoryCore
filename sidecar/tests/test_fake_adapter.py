import threading
import time

import pytest

from memorycore_sidecar.adapters.fake import FakeMirrorAdapter
from memorycore_sidecar.adapters.trivium import TriviumAdapter
from memorycore_sidecar.candidates import (
    DEFAULT_RRF_K,
    DEFAULT_SUPPORT_BETA,
    DenseHit,
    fuse_dense_results,
)


def test_fuse_dense_results_weights_rrf_support_and_damps_by_primary_score():
    request = {
        "request_id": "candidate-1",
        "persona_id": "alice",
        "limit": 2,
        "debug_scores": True,
        "query": {
            "raw_text": "raw",
            "rewrites": [
                {"text": "rewrite", "weight": 0.1, "purpose": "semantic_recall"}
            ],
        },
    }

    def search(query):
        if query.source == "raw_dense":
            return [DenseHit(1, 0.2)]
        return [DenseHit(1, 1.0)]

    result = fuse_dense_results(request, search)

    breakdown = result["candidates"][0]["score_breakdown"]
    expected_rrf_support = (1.0 / (DEFAULT_RRF_K + 1)) + (
        0.1 / (DEFAULT_RRF_K + 1)
    )
    expected_rrf_norm = expected_rrf_support / (2 / (DEFAULT_RRF_K + 1))
    expected_bonus = DEFAULT_SUPPORT_BETA * expected_rrf_norm * (1 - 0.2)
    assert result["candidates"][0]["trivium_node_id"] == 1
    assert breakdown["primary_score"] == pytest.approx(0.2)
    assert breakdown["rrf_support_norm"] == pytest.approx(expected_rrf_norm)
    assert breakdown["support_bonus"] == pytest.approx(expected_bonus)
    assert breakdown["support_bonus"] < 0.12
    assert breakdown["score_norm_method"] == "weighted_max_rrf"


def test_fuse_dense_results_trims_fanout_and_reports_query_diagnostics():
    request = {
        "request_id": "candidate-1",
        "persona_id": "alice",
        "limit": 2,
        "debug_scores": True,
        "query": {
            "raw_text": "coffee preference",
            "rewrites": [
                {"text": "coffee preference", "weight": 0.8, "purpose": "semantic_recall"},
                {"text": "semantic memory", "weight": 0.8, "purpose": "semantic_recall"},
                {"text": "coffee", "weight": 0.8, "purpose": "semantic_recall"},
                {"text": "espresso", "weight": 0.8, "purpose": "semantic_recall"},
                {"text": "latte", "weight": 0.8, "purpose": "semantic_recall"},
                {"text": "pour over", "weight": 0.8, "purpose": "semantic_recall"},
                {"text": "cappuccino", "weight": 0.8, "purpose": "semantic_recall"},
            ],
            "semantic_anchors": [
                {"text": "coffee preference", "weight": 0.5, "purpose": "semantic_anchor"},
                {"text": "memory", "weight": 0.5, "purpose": "semantic_anchor"},
                {"text": "espresso", "weight": 0.5, "purpose": "semantic_anchor"},
                {"text": "music", "weight": 0.5, "purpose": "semantic_anchor"},
                {"text": "tea", "weight": 0.5, "purpose": "semantic_anchor"},
            ],
        },
    }
    seen_queries = []

    def search(query):
        seen_queries.append(query.text)
        return []

    result = fuse_dense_results(request, search)

    assert seen_queries == [
        "coffee preference",
        "coffee",
        "espresso",
        "latte",
        "music",
        "tea",
    ]
    diagnostics = result["diagnostics"]
    assert diagnostics["query_count"] == 6
    assert diagnostics["rewrite_query_count"] == 3
    assert diagnostics["anchor_query_count"] == 2
    assert diagnostics["query_trims"] == {
        "dropped_rewrite_count": 4,
        "dropped_anchor_count": 3,
        "dropped_similar_count": 2,
        "dropped_generic_count": 2,
        "dropped_duplicate_count": 1,
        "dropped_fanout_limit_count": 2,
        "max_dense_queries": 6,
    }
    assert diagnostics["merge_order"] == [
        "fused_score_desc",
        "primary_score_desc",
        "hit_count_desc",
        "source_priority_asc",
        "trivium_node_id_asc",
    ]
    assert all(item["latency_ms"] >= 0 for item in diagnostics["per_query_counts"])


def test_fuse_dense_results_executes_fanout_in_parallel_and_keeps_diagnostics_order():
    request = {
        "request_id": "candidate-1",
        "persona_id": "alice",
        "limit": 3,
        "debug_scores": True,
        "query": {
            "raw_text": "raw",
            "rewrites": [
                {"text": "rewrite-a", "weight": 0.5, "purpose": "semantic_recall"},
                {"text": "rewrite-b", "weight": 0.5, "purpose": "semantic_recall"},
            ],
            "semantic_anchors": [
                {"text": "anchor", "weight": 0.4, "purpose": "semantic_anchor"}
            ],
        },
    }
    lock = threading.Lock()
    active = 0
    max_active = 0

    def search(query):
        nonlocal active, max_active
        with lock:
            active += 1
            max_active = max(max_active, active)
        try:
            time.sleep(0.03)
            return [DenseHit(len(query.text), 0.7)]
        finally:
            with lock:
                active -= 1

    result = fuse_dense_results(request, search)

    assert max_active > 1
    assert [
        (item["source"], item["purpose"], item["count"])
        for item in result["diagnostics"]["per_query_counts"]
    ] == [
        ("raw_dense", "raw_query", 1),
        ("semantic_rewrite_dense", "semantic_recall", 1),
        ("semantic_rewrite_dense", "semantic_recall", 1),
        ("semantic_anchor_dense", "semantic_anchor", 1),
    ]
    assert result["diagnostics"]["dense_embedding_wall_latency_ms"] == 0
    assert result["diagnostics"]["dense_embedding_batch_latency_ms"] == 0
    assert result["diagnostics"]["dense_search_total_latency_ms"] >= 0
    assert result["diagnostics"]["query_count_trimmed_by_budget"] == 0


def test_fake_adapter_upsert_node_returns_stable_positive_trivium_id():
    adapter = FakeMirrorAdapter()
    node = {
        "persona_id": "default",
        "node_type": "fact",
        "sqlite_node_id": "fact-1",
        "searchable_text": "safe text",
        "payload": {"node_type": "fact"},
    }

    first = adapter.upsert_node(node)
    second = adapter.upsert_node(dict(node))

    assert first == second
    assert first["trivium_node_id"] > 0


def test_fake_adapter_delete_and_edge_operations_return_ok_payloads():
    adapter = FakeMirrorAdapter()

    assert adapter.delete_node(
        {"persona_id": "default", "node_type": "fact", "sqlite_node_id": "fact-1"}
    ) == {}
    edge = {
        "persona_id": "default",
        "sqlite_edge_id": "edge-1",
        "link_type": "ABOUT_ENTITY",
        "from_node_type": "fact",
        "from_node_id": "fact-1",
        "to_node_type": "entity",
        "to_node_id": "entity-1",
        "direction": "out",
        "confidence": 0.9,
        "weight": 1.0,
        "payload": {"direction": "out"},
    }
    assert adapter.upsert_edge(edge) == {}
    assert adapter.delete_edge(edge) == {}


def test_fake_adapter_tracks_edges_and_clear_namespace_clears_persona_edges():
    adapter = FakeMirrorAdapter()
    alice_edge = {
        "persona_id": "alice",
        "sqlite_edge_id": "edge-1",
        "link_type": "ABOUT_ENTITY",
        "from_node_type": "fact",
        "from_node_id": "fact-1",
        "to_node_type": "entity",
        "to_node_id": "entity-1",
    }
    bob_edge = {
        "persona_id": "bob",
        "sqlite_edge_id": "edge-1",
        "link_type": "ABOUT_ENTITY",
        "from_node_type": "fact",
        "from_node_id": "fact-1",
        "to_node_type": "entity",
        "to_node_id": "entity-1",
    }

    assert adapter.upsert_edge(alice_edge) == {}
    assert adapter.upsert_edge(bob_edge) == {}
    assert adapter.delete_edge(alice_edge) == {}
    assert adapter.delete_edge(alice_edge) == {}
    assert adapter.clear_namespace("bob") == {}
    assert adapter.delete_edge(bob_edge) == {}


def test_fake_adapter_returns_upserted_nodes_as_candidates():
    adapter = FakeMirrorAdapter()
    upserted = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "fact",
            "sqlite_node_id": "fact-1",
            "searchable_text": "用户喜欢咖啡。",
            "payload": {},
        }
    )

    result = adapter.find_candidates(
        {
            "persona_id": "default",
            "query": {"raw_text": "咖啡"},
            "limit": 8,
            "debug_scores": False,
        }
    )

    assert result == {
        "candidates": [
            {
                "trivium_node_id": upserted["trivium_node_id"],
                "fused_score": 1.0,
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


def test_fake_adapter_candidate_v02_dedupes_generated_queries_and_applies_support_bonus():
    adapter = FakeMirrorAdapter()
    first = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "fact",
            "sqlite_node_id": "fact-coffee",
            "searchable_text": "coffee espresso latte",
            "payload": {},
        }
    )
    second = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "fact",
            "sqlite_node_id": "fact-tea",
            "searchable_text": "espresso tea",
            "payload": {},
        }
    )

    result = adapter.find_candidates(
        {
            "persona_id": "default",
            "limit": 2,
            "debug_scores": True,
            "query": {
                "raw_text": "coffee",
                "signals": ["preference"],
                "rewrites": [
                    {"text": "espresso", "weight": 0.5, "purpose": "semantic_recall"},
                    {"text": " espresso ", "weight": 0.5, "purpose": "duplicate"},
                ],
                "semantic_anchors": [
                    {"text": "latte", "weight": 0.4, "purpose": "semantic_anchor"}
                ],
            },
        }
    )

    assert result["degraded"] is False
    assert [candidate["trivium_node_id"] for candidate in result["candidates"]] == [
        first["trivium_node_id"],
        second["trivium_node_id"],
    ]
    assert result["candidates"][0]["primary_source"] == "raw_dense"
    assert result["candidates"][0]["hit_count"] == 3
    assert result["candidates"][0]["score_breakdown"]["primary_score"] == 1.0
    assert result["candidates"][0]["score_breakdown"]["support_bonus"] == 0.0
    assert result["diagnostics"]["query_count"] == 3
    assert result["diagnostics"]["rewrite_query_count"] == 1
    assert result["diagnostics"]["anchor_query_count"] == 1


def test_fake_adapter_forget_delete_queries_label_operation_target_candidates():
    adapter = FakeMirrorAdapter()
    upserted = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "fact",
            "sqlite_node_id": "fact-delete",
            "searchable_text": "delete coffee",
            "payload": {},
        }
    )

    result = adapter.find_candidates(
        {
            "persona_id": "default",
            "limit": 5,
            "debug_scores": True,
            "query": {
                "raw_text": "delete coffee",
                "signals": ["forget_delete"],
                "rewrites": [
                    {"text": "delete coffee", "weight": 0.8, "purpose": "semantic_recall"},
                    {"text": "coffee", "weight": 0.8, "purpose": "operation_target"},
                ],
                "semantic_anchors": [
                    {"text": "relationship expansion", "weight": 0.65, "purpose": "relationship"}
                ],
            },
        }
    )

    assert result["candidates"][0]["trivium_node_id"] == upserted["trivium_node_id"]
    assert result["candidates"][0]["primary_purpose"] == "raw_query"
    assert result["diagnostics"]["dense_results_label"] == "operation_target_candidates"
    assert result["diagnostics"]["rewrite_query_count"] == 1
    assert result["diagnostics"]["anchor_query_count"] == 0


def test_fake_adapter_rerank_uses_overlap_and_configured_score_deterministically():
    adapter = FakeMirrorAdapter()

    result = adapter.rerank(
        {
            "persona_id": "default",
            "query_text": "coffee preference",
            "candidates": [
                {
                    "node_id": "fact-b",
                    "node_type": "fact",
                    "safe_summary": "coffee preference is stable",
                    "configured_score": 0.2,
                },
                {
                    "node_id": "fact-a",
                    "node_type": "fact",
                    "safe_summary": "coffee preference is stable",
                    "configured_score": 0.2,
                },
                {
                    "node_id": "fact-c",
                    "node_type": "fact",
                    "safe_summary": "travel plans",
                    "configured_score": 0.9,
                },
            ],
        }
    )

    assert result["degraded"] is False
    assert [item["node_id"] for item in result["results"]] == [
        "fact-a",
        "fact-b",
        "fact-c",
    ]
    assert all(item["node_type"] == "fact" for item in result["results"])
    assert all(0 <= item["rerank_score"] <= 1 for item in result["results"])
    assert all("debug_reason" in item for item in result["results"])


def test_trivium_adapter_rerank_is_degraded_without_provider():
    adapter = object.__new__(TriviumAdapter)

    result = adapter.rerank(
        {
            "persona_id": "default",
            "query_text": "coffee",
            "candidates": [
                {
                    "node_id": "fact-1",
                    "node_type": "fact",
                    "safe_summary": "coffee",
                }
            ],
        }
    )

    assert result == {
        "results": [],
        "degraded": True,
        "fallback_reason": "rerank_not_configured",
    }


def test_trivium_adapter_rerank_delegates_to_configured_provider():
    class RecordingProvider:
        def __init__(self) -> None:
            self.requests = []

        def rerank(self, query_text, candidates):
            self.requests.append((query_text, candidates))
            return {
                "results": [
                    {
                        "node_id": "fact-1",
                        "node_type": "fact",
                        "rerank_score": 0.8,
                        "debug_reason": "provider",
                    }
                ],
                "degraded": False,
            }

    provider = RecordingProvider()
    adapter = object.__new__(TriviumAdapter)
    adapter.rerank_provider = provider
    request = {
        "persona_id": "default",
        "query_text": "coffee",
        "candidates": [
            {
                "node_id": "fact-1",
                "node_type": "fact",
                "safe_summary": "coffee",
            }
        ],
    }

    result = adapter.rerank(request)

    assert provider.requests == [("coffee", request["candidates"])]
    assert result == {
        "results": [
            {
                "node_id": "fact-1",
                "node_type": "fact",
                "rerank_score": 0.8,
                "debug_reason": "provider",
            }
        ],
        "degraded": False,
    }


def test_trivium_adapter_rerank_provider_error_degrades_without_error_detail():
    class FailingProvider:
        def rerank(self, query_text, candidates):
            raise RuntimeError("secret document payload should not leak")

    adapter = object.__new__(TriviumAdapter)
    adapter.rerank_provider = FailingProvider()

    result = adapter.rerank(
        {
            "persona_id": "default",
            "query_text": "coffee",
            "candidates": [
                {
                    "node_id": "fact-1",
                    "node_type": "fact",
                    "safe_summary": "coffee",
                }
            ],
        }
    )

    assert result == {
        "results": [],
        "degraded": True,
        "fallback_reason": "rerank_provider_error",
    }
    assert "secret" not in str(result)


def test_fake_adapter_activate_graph_expands_weighted_edges_with_paths():
    adapter = FakeMirrorAdapter()
    seed = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "fact",
            "sqlite_node_id": "fact-seed",
            "searchable_text": "seed",
            "payload": {},
        }
    )
    related = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "fact",
            "sqlite_node_id": "fact-related",
            "searchable_text": "related",
            "payload": {},
        }
    )
    episode = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "episode",
            "sqlite_node_id": "ep-1",
            "searchable_text": "episode raw text",
            "payload": {},
        }
    )
    adapter.upsert_edge(
        {
            "persona_id": "default",
            "sqlite_edge_id": "edge-caused",
            "link_type": "CAUSED_BY",
            "from_node_type": "fact",
            "from_node_id": "fact-seed",
            "to_node_type": "fact",
            "to_node_id": "fact-related",
            "weight": 0.8,
        }
    )
    adapter.upsert_edge(
        {
            "persona_id": "default",
            "sqlite_edge_id": "edge-evidence",
            "link_type": "EVIDENCED_BY",
            "from_node_type": "fact",
            "from_node_id": "fact-seed",
            "to_node_type": "episode",
            "to_node_id": "ep-1",
            "weight": 1.0,
        }
    )

    result = adapter.activate_graph(
        {
            "persona_id": "default",
            "seeds": [
                {
                    "trivium_node_id": seed["trivium_node_id"],
                    "sqlite_node_id": "fact-seed",
                    "node_type": "fact",
                    "seed_energy": 1.0,
                }
            ],
            "params": {"max_hops": 1, "hop_decay": 0.5, "include_paths": True},
        }
    )

    ids = [candidate["trivium_node_id"] for candidate in result["candidates"]]
    assert seed["trivium_node_id"] in ids
    assert related["trivium_node_id"] in ids
    assert episode["trivium_node_id"] not in ids
    related_candidate = next(
        candidate
        for candidate in result["candidates"]
        if candidate["trivium_node_id"] == related["trivium_node_id"]
    )
    assert related_candidate["score"] == 0.4
    assert related_candidate["source"] == "graph_activation"
    assert related_candidate["paths"][0]["link_types"] == ["CAUSED_BY"]


def test_fake_adapter_activation_builds_persona_graph_once(monkeypatch):
    adapter = FakeMirrorAdapter()
    seed = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "fact",
            "sqlite_node_id": "seed",
            "searchable_text": "seed",
            "payload": {},
        }
    )
    for idx in range(20):
        adapter.upsert_node(
            {
                "persona_id": "default",
                "node_type": "fact",
                "sqlite_node_id": f"target-{idx}",
                "searchable_text": f"target {idx}",
                "payload": {},
            }
        )
        adapter.upsert_edge(
            {
                "persona_id": "default",
                "sqlite_edge_id": f"edge-{idx}",
                "link_type": "SUPPORTS",
                "from_node_type": "fact",
                "from_node_id": "seed",
                "to_node_type": "fact",
                "to_node_id": f"target-{idx}",
                "weight": 1.0,
            }
        )

    calls = 0
    original = adapter._build_persona_activation_graph

    def counting_build(persona_id):
        nonlocal calls
        calls += 1
        return original(persona_id)

    monkeypatch.setattr(adapter, "_build_persona_activation_graph", counting_build)
    result = adapter.activate_graph(
        {
            "persona_id": "default",
            "seeds": [
                {
                    "trivium_node_id": seed["trivium_node_id"],
                    "sqlite_node_id": "seed",
                    "node_type": "fact",
                    "seed_energy": 1.0,
                }
            ],
            "params": {"max_hops": 1, "max_neighbors_per_node": 5},
        }
    )

    assert calls == 1
    assert result["degraded"] is False


def test_fake_adapter_activate_graph_suppresses_hub_dominance():
    adapter = FakeMirrorAdapter()
    seed = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "fact",
            "sqlite_node_id": "fact-seed",
            "searchable_text": "seed",
            "payload": {},
        }
    )
    leaf = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "fact",
            "sqlite_node_id": "fact-leaf",
            "searchable_text": "leaf",
            "payload": {},
        }
    )
    hub = adapter.upsert_node(
        {
            "persona_id": "default",
            "node_type": "entity",
            "sqlite_node_id": "entity-hub",
            "searchable_text": "hub",
            "payload": {},
        }
    )
    for idx in range(6):
        adapter.upsert_node(
            {
                "persona_id": "default",
                "node_type": "fact",
                "sqlite_node_id": f"fact-hub-{idx}",
                "searchable_text": f"hub neighbor {idx}",
                "payload": {},
            }
        )
        adapter.upsert_edge(
            {
                "persona_id": "default",
                "sqlite_edge_id": f"edge-hub-{idx}",
                "link_type": "ABOUT_ENTITY",
                "from_node_type": "entity",
                "from_node_id": "entity-hub",
                "to_node_type": "fact",
                "to_node_id": f"fact-hub-{idx}",
                "weight": 1.0,
            }
        )
    adapter.upsert_edge(
        {
            "persona_id": "default",
            "sqlite_edge_id": "edge-leaf",
            "link_type": "SUPPORTS",
            "from_node_type": "fact",
            "from_node_id": "fact-seed",
            "to_node_type": "fact",
            "to_node_id": "fact-leaf",
            "weight": 1.0,
        }
    )
    adapter.upsert_edge(
        {
            "persona_id": "default",
            "sqlite_edge_id": "edge-hub",
            "link_type": "ABOUT_ENTITY",
            "from_node_type": "fact",
            "from_node_id": "fact-seed",
            "to_node_type": "entity",
            "to_node_id": "entity-hub",
            "weight": 1.0,
        }
    )

    result = adapter.activate_graph(
        {
            "persona_id": "default",
            "seeds": [
                {
                    "trivium_node_id": seed["trivium_node_id"],
                    "sqlite_node_id": "fact-seed",
                    "node_type": "fact",
                    "seed_energy": 1.0,
                }
            ],
            "params": {
                "max_hops": 1,
                "hop_decay": 1.0,
                "hub_suppression_power": 1.0,
                "include_paths": False,
            },
        }
    )
    by_id = {candidate["trivium_node_id"]: candidate for candidate in result["candidates"]}

    assert by_id[hub["trivium_node_id"]]["score"] < by_id[leaf["trivium_node_id"]]["score"]
