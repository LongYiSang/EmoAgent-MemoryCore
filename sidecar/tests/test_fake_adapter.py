from memorycore_sidecar.adapters.fake import FakeMirrorAdapter


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
    assert adapter.upsert_edge(
        {
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
    ) == {}
    assert adapter.delete_edge({"persona_id": "default", "sqlite_edge_id": "edge-1"}) == {}


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
