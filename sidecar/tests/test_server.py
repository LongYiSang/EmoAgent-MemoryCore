import json
import threading
import urllib.request

import pytest

from memorycore_sidecar.adapters.fake import FakeMirrorAdapter
from memorycore_sidecar.protocol import (
    ACTIVATION_REQUEST_SCHEMA_VERSION,
    ACTIVATION_RESPONSE_SCHEMA_VERSION,
    RERANK_REQUEST_SCHEMA_VERSION,
    RERANK_RESPONSE_SCHEMA_VERSION,
    REQUEST_SCHEMA_VERSION,
    RESPONSE_SCHEMA_VERSION,
)
from memorycore_sidecar.server import create_server


def test_server_retrieval_activate_roundtrip_uses_trivium_seed_id():
    server = create_server(("127.0.0.1", 0), FakeMirrorAdapter())
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        base_url = f"http://127.0.0.1:{server.server_address[1]}"
        seed_id = _upsert_node(base_url, "fact-seed", "seed text")
        related_id = _upsert_node(base_url, "fact-related", "related text")
        edge_body = _post_json(
            base_url,
            "/mirror/operation",
            {
                "schema_version": REQUEST_SCHEMA_VERSION,
                "operation_id": "edge-caused",
                "persona_id": "default",
                "operation": "upsert_edge",
                "edge": {
                    "persona_id": "default",
                    "sqlite_edge_id": "edge-caused",
                    "link_type": "CAUSED_BY",
                    "from_node_type": "fact",
                    "from_node_id": "fact-seed",
                    "to_node_type": "fact",
                    "to_node_id": "fact-related",
                    "weight": 0.8,
                },
            },
        )
        assert edge_body["schema_version"] == RESPONSE_SCHEMA_VERSION
        assert edge_body["status"] == "ok"

        body = _post_json(
            base_url,
            "/retrieval/activate",
            {
                "schema_version": ACTIVATION_REQUEST_SCHEMA_VERSION,
                "request_id": "activate-1",
                "persona_id": "default",
                "seeds": [
                    {
                        "trivium_node_id": seed_id,
                        "sqlite_node_id": "fact-seed",
                        "node_type": "fact",
                        "seed_energy": 1.0,
                    }
                ],
                "params": {"max_hops": 1, "hop_decay": 0.5, "include_paths": True},
            },
        )

        assert body["schema_version"] == ACTIVATION_RESPONSE_SCHEMA_VERSION
        assert body["request_id"] == "activate-1"
        assert body["degraded"] is False
        by_id = {candidate["trivium_node_id"]: candidate for candidate in body["candidates"]}
        assert related_id in by_id
        assert by_id[related_id]["score"] == pytest.approx(0.4)
        assert by_id[related_id]["source"] == "graph_activation"
        assert by_id[related_id]["paths"] == [
            {"trivium_node_ids": [seed_id, related_id], "link_types": ["CAUSED_BY"]}
        ]
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


def test_server_retrieval_activate_returns_budget_degraded_partial_candidates():
    server = create_server(("127.0.0.1", 0), FakeMirrorAdapter())
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        base_url = f"http://127.0.0.1:{server.server_address[1]}"
        seed_id = _upsert_node(base_url, "fact-seed", "seed text")
        _upsert_node(base_url, "fact-related-a", "related a")
        _upsert_node(base_url, "fact-related-b", "related b")
        for idx in ("a", "b"):
            body = _post_json(
                base_url,
                "/mirror/operation",
                {
                    "schema_version": REQUEST_SCHEMA_VERSION,
                    "operation_id": f"edge-{idx}",
                    "persona_id": "default",
                    "operation": "upsert_edge",
                    "edge": {
                        "persona_id": "default",
                        "sqlite_edge_id": f"edge-{idx}",
                        "link_type": "SUPPORTS",
                        "from_node_type": "fact",
                        "from_node_id": "fact-seed",
                        "to_node_type": "fact",
                        "to_node_id": f"fact-related-{idx}",
                        "weight": 1.0,
                    },
                },
            )
            assert body["status"] == "ok"

        body = _post_json(
            base_url,
            "/retrieval/activate",
            {
                "schema_version": ACTIVATION_REQUEST_SCHEMA_VERSION,
                "request_id": "activate-budget",
                "persona_id": "default",
                "seeds": [
                    {
                        "trivium_node_id": seed_id,
                        "sqlite_node_id": "fact-seed",
                        "node_type": "fact",
                        "seed_energy": 1.0,
                    }
                ],
                "params": {
                    "max_hops": 1,
                    "max_edges_scanned_per_request": 1,
                    "max_neighbors_per_node": 100,
                    "max_activation_wall_ms": 120,
                },
            },
        )

        assert body["schema_version"] == ACTIVATION_RESPONSE_SCHEMA_VERSION
        assert body["request_id"] == "activate-budget"
        assert body["degraded"] is True
        assert body["fallback_reason"] == "activation_budget_exceeded"
        assert body["candidates"]
        assert "Traceback" not in str(body)
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


def test_server_retrieval_rerank_roundtrip_orders_by_fake_adapter_score():
    server = create_server(("127.0.0.1", 0), FakeMirrorAdapter())
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        base_url = f"http://127.0.0.1:{server.server_address[1]}"
        body = _post_json(
            base_url,
            "/retrieval/rerank",
            {
                "schema_version": RERANK_REQUEST_SCHEMA_VERSION,
                "request_id": "rerank-1",
                "persona_id": "default",
                "query_text": "coffee focus",
                "candidates": [
                    {
                        "node_id": "fact-tea",
                        "node_type": "fact",
                        "safe_summary": "tea backup",
                        "configured_score": 0.9,
                    },
                    {
                        "node_id": "fact-coffee",
                        "node_type": "fact",
                        "safe_summary": "coffee helps focus",
                        "configured_score": 0.1,
                    },
                ],
            },
        )

        assert body["schema_version"] == RERANK_RESPONSE_SCHEMA_VERSION
        assert body["request_id"] == "rerank-1"
        assert body["degraded"] is False
        assert [item["node_id"] for item in body["results"]] == ["fact-coffee", "fact-tea"]
        assert body["results"][0]["node_type"] == "fact"
        assert body["results"][0]["rerank_score"] == pytest.approx(1.0)
        assert "token_overlap=2/2" in body["results"][0]["debug_reason"]
        assert body["results"][1]["rerank_score"] == pytest.approx(0.9)
        assert "configured_score=0.9" in body["results"][1]["debug_reason"]
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


def _upsert_node(base_url, sqlite_node_id, searchable_text):
    body = _post_json(
        base_url,
        "/mirror/operation",
        {
            "schema_version": REQUEST_SCHEMA_VERSION,
            "operation_id": f"upsert-{sqlite_node_id}",
            "persona_id": "default",
            "operation": "upsert_node",
            "node": {
                "persona_id": "default",
                "node_type": "fact",
                "sqlite_node_id": sqlite_node_id,
                "searchable_text": searchable_text,
                "payload": {},
            },
        },
    )
    assert body["schema_version"] == RESPONSE_SCHEMA_VERSION
    assert body["status"] == "ok"
    assert body["trivium_node_id"] > 0
    return body["trivium_node_id"]


def _post_json(base_url, path, payload):
    request = urllib.request.Request(
        base_url + path,
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=2) as response:
        assert response.status == 200
        return json.load(response)
