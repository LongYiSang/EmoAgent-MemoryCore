from memorycore_sidecar.adapters.fake import FakeMirrorAdapter
from memorycore_sidecar.adapters.trivium import TriviumAdapter


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
        {"persona_id": "default", "query_text": "咖啡", "limit": 8}
    )

    assert result == {
        "candidates": [
            {
                "trivium_node_id": upserted["trivium_node_id"],
                "score": 1.0,
                "source": "fake_sparse",
            }
        ],
        "degraded": False,
    }


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
        "fallback_reason": "rerank_not_implemented",
    }


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
