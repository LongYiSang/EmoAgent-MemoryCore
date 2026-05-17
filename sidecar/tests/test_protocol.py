import json
import tempfile
import threading
import urllib.error
import urllib.request
from pathlib import Path

import pytest

from memorycore_sidecar.adapters.fake import FakeMirrorAdapter
from memorycore_sidecar.config import SidecarConfig
from memorycore_sidecar.protocol import (
    ACTIVATION_REQUEST_SCHEMA_VERSION,
    ACTIVATION_RESPONSE_SCHEMA_VERSION,
    CANDIDATE_REQUEST_SCHEMA_VERSION,
    CANDIDATE_RESPONSE_SCHEMA_VERSION,
    CLEAR_NAMESPACE_REQUEST_SCHEMA_VERSION,
    CLEAR_NAMESPACE_RESPONSE_SCHEMA_VERSION,
    EVAL_CONFIG_REQUEST_SCHEMA_VERSION,
    RERANK_REQUEST_SCHEMA_VERSION,
    RERANK_RESPONSE_SCHEMA_VERSION,
    REQUEST_SCHEMA_VERSION,
    RESPONSE_SCHEMA_VERSION,
    ProtocolError,
    build_activation_result,
    build_rerank_result,
    build_result,
    parse_activation_request,
    parse_candidate_request,
    parse_clear_namespace_request,
    parse_eval_config_request,
    parse_operation_request,
    parse_rerank_request,
)
from memorycore_sidecar.server import create_server, create_adapter


def test_parse_operation_request_requires_schema_version():
    with pytest.raises(ProtocolError, match=REQUEST_SCHEMA_VERSION):
        parse_operation_request(
            {
                "schema_version": "wrong",
                "operation": "upsert_node",
                "node": {
                    "persona_id": "default",
                    "node_type": "fact",
                    "sqlite_node_id": "fact-1",
                },
            }
        )


def test_parse_operation_request_rejects_unknown_operation():
    with pytest.raises(ProtocolError, match="unsupported operation"):
        parse_operation_request(
            {
                "schema_version": REQUEST_SCHEMA_VERSION,
                "operation_id": "queue-1",
                "persona_id": "default",
                "operation": "rebuild_persona",
            }
        )


def test_parse_operation_request_requires_upsert_node_searchable_text():
    with pytest.raises(ProtocolError, match="searchable_text"):
        parse_operation_request(
            {
                "schema_version": REQUEST_SCHEMA_VERSION,
                "operation_id": "queue-1",
                "persona_id": "default",
                "operation": "upsert_node",
                "node": {
                    "persona_id": "default",
                    "node_type": "fact",
                    "sqlite_node_id": "fact-1",
                    "searchable_text": "  ",
                },
            }
        )


def test_parse_operation_request_requires_upsert_edge_endpoints():
    with pytest.raises(
        ProtocolError,
        match="from_node_type, from_node_id, to_node_type, to_node_id",
    ):
        parse_operation_request(
            {
                "schema_version": REQUEST_SCHEMA_VERSION,
                "operation_id": "queue-1",
                "persona_id": "default",
                "operation": "upsert_edge",
                "edge": {
                    "persona_id": "default",
                    "sqlite_edge_id": "edge-1",
                    "from_node_type": "",
                    "from_node_id": "",
                    "to_node_type": "",
                    "to_node_id": "",
                },
            }
        )


def test_parse_operation_request_requires_delete_edge_endpoints_and_link_type():
    with pytest.raises(
        ProtocolError,
        match="link_type, from_node_type, from_node_id, to_node_type, to_node_id",
    ):
        parse_operation_request(
            {
                "schema_version": REQUEST_SCHEMA_VERSION,
                "operation_id": "queue-1",
                "persona_id": "default",
                "operation": "delete_edge",
                "edge": {
                    "persona_id": "default",
                    "sqlite_edge_id": "edge-1",
                },
            }
        )


def test_build_result_uses_result_schema():
    result = build_result("queue-1", trivium_node_id=42)

    assert result == {
        "schema_version": RESPONSE_SCHEMA_VERSION,
        "operation_id": "queue-1",
        "status": "ok",
        "trivium_node_id": 42,
    }


def test_parse_clear_namespace_request_requires_persona_id():
    with pytest.raises(ProtocolError, match="persona_id"):
        parse_clear_namespace_request(
            {
                "schema_version": CLEAR_NAMESPACE_REQUEST_SCHEMA_VERSION,
            }
        )


def test_parse_candidate_request_requires_query_text():
    with pytest.raises(ProtocolError, match="query_text"):
        parse_candidate_request(
            {
                "schema_version": CANDIDATE_REQUEST_SCHEMA_VERSION,
                "request_id": "req-1",
                "persona_id": "default",
            }
        )


def test_parse_eval_config_request_rejects_unknown_embedding_cache_mode():
    with pytest.raises(ProtocolError, match="embedding_cache_mode"):
        parse_eval_config_request(
            {
                "schema_version": EVAL_CONFIG_REQUEST_SCHEMA_VERSION,
                "trivium_dir": "/tmp/mirror",
                "embedding_cache_mode": "typo",
            }
        )


def test_parse_activation_request_applies_defaults_and_keeps_anchor_debug_out_of_seeds():
    request = parse_activation_request(
        {
            "schema_version": ACTIVATION_REQUEST_SCHEMA_VERSION,
            "request_id": "act-1",
            "persona_id": "default",
            "seeds": [
                {
                    "trivium_node_id": 42,
                    "sqlite_node_id": "fact-1",
                    "node_type": "fact",
                    "seed_energy": 0.75,
                    "source_breakdown": [{"source": "sqlite_fts", "rank": 1}],
                }
            ],
            "anchor_debug": [{"node_id": "fact-1", "source_breakdown": []}],
        }
    )

    assert request["request_id"] == "act-1"
    assert request["persona_id"] == "default"
    assert request["seeds"] == [
        {
            "trivium_node_id": 42,
            "sqlite_node_id": "fact-1",
            "node_type": "fact",
            "seed_energy": 0.75,
        }
    ]
    assert request["params"]["max_hops"] == 2
    assert request["params"]["hop_decay"] == 0.70
    assert request["params"]["min_energy"] == 0.01
    assert request["params"]["max_active_nodes"] == 80
    assert request["params"]["hub_suppression_power"] == 0.50
    assert request["params"]["include_paths"] is True
    assert request["params"]["max_edges_scanned_per_request"] == 10000
    assert request["params"]["max_neighbors_per_node"] == 100
    assert request["params"]["max_activation_wall_ms"] == 120.0


def test_parse_activation_request_rejects_bad_seed():
    with pytest.raises(ProtocolError, match="trivium_node_id"):
        parse_activation_request(
            {
                "schema_version": ACTIVATION_REQUEST_SCHEMA_VERSION,
                "request_id": "act-1",
                "persona_id": "default",
                "seeds": [
                    {
                        "trivium_node_id": 0,
                        "sqlite_node_id": "fact-1",
                        "node_type": "fact",
                        "seed_energy": 0.5,
                    }
                ],
            }
        )


def test_parse_activation_request_rejects_non_finite_seed_energy():
    with pytest.raises(ProtocolError, match="seed_energy"):
        parse_activation_request(
            {
                "schema_version": ACTIVATION_REQUEST_SCHEMA_VERSION,
                "request_id": "act-1",
                "persona_id": "default",
                "seeds": [
                    {
                        "trivium_node_id": 42,
                        "sqlite_node_id": "fact-1",
                        "node_type": "fact",
                        "seed_energy": float("nan"),
                    }
                ],
            }
        )


def test_build_activation_result_uses_activation_schema():
    result = build_activation_result(
        "act-1",
        candidates=[
            {
                "trivium_node_id": 42,
                "score": 0.7,
                "source": "graph_activation",
                "rank": 1,
            }
        ],
    )

    assert result == {
        "schema_version": ACTIVATION_RESPONSE_SCHEMA_VERSION,
        "request_id": "act-1",
        "candidates": [
            {
                "trivium_node_id": 42,
                "score": 0.7,
                "source": "graph_activation",
                "rank": 1,
            }
        ],
        "degraded": False,
    }


def test_parse_rerank_request_validates_and_sanitizes_candidates():
    request = parse_rerank_request(
        {
            "schema_version": RERANK_REQUEST_SCHEMA_VERSION,
            "request_id": " rerank-1 ",
            "persona_id": " default ",
            "query_text": "coffee preference",
            "candidates": [
                {
                    "node_id": " fact-1 ",
                    "node_type": " fact ",
                    "safe_summary": "The user likes coffee.",
                    "current_score": 0.25,
                    "anchor_energy": 0.3,
                    "graph_energy": 0.1,
                    "configured_score": 0.75,
                    "source_scores": {"sqlite_fts": 0.4, "graph": 0.2},
                    "raw_episode_content": "must not be propagated",
                }
            ],
        }
    )

    assert request == {
        "request_id": "rerank-1",
        "persona_id": "default",
        "query_text": "coffee preference",
        "candidates": [
            {
                "node_id": "fact-1",
                "node_type": "fact",
                "safe_summary": "The user likes coffee.",
                "current_score": 0.25,
                "anchor_energy": 0.3,
                "graph_energy": 0.1,
                "configured_score": 0.75,
                "source_scores": {"sqlite_fts": 0.4, "graph": 0.2},
            }
        ],
    }


@pytest.mark.parametrize(
    ("field", "value", "match"),
    [
        ("request_id", " ", "request_id"),
        ("persona_id", "", "persona_id"),
        ("query_text", " ", "query_text"),
    ],
)
def test_parse_rerank_request_rejects_blank_required_fields(field, value, match):
    request = {
        "schema_version": RERANK_REQUEST_SCHEMA_VERSION,
        "request_id": "rerank-1",
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
    request[field] = value
    with pytest.raises(ProtocolError, match=match):
        parse_rerank_request(request)


@pytest.mark.parametrize("field", ["node_id", "node_type", "safe_summary"])
def test_parse_rerank_request_rejects_blank_candidate_fields(field):
    candidate = {
        "node_id": "fact-1",
        "node_type": "fact",
        "safe_summary": "coffee",
    }
    candidate[field] = " "
    with pytest.raises(ProtocolError, match=field):
        parse_rerank_request(
            {
                "schema_version": RERANK_REQUEST_SCHEMA_VERSION,
                "request_id": "rerank-1",
                "persona_id": "default",
                "query_text": "coffee",
                "candidates": [candidate],
            }
        )


def test_parse_rerank_request_rejects_invalid_candidate_scores():
    with pytest.raises(ProtocolError, match="configured_score"):
        parse_rerank_request(
            {
                "schema_version": RERANK_REQUEST_SCHEMA_VERSION,
                "request_id": "rerank-1",
                "persona_id": "default",
                "query_text": "coffee",
                "candidates": [
                    {
                        "node_id": "fact-1",
                        "node_type": "fact",
                        "safe_summary": "coffee",
                        "configured_score": float("inf"),
                    }
                ],
            }
        )


def test_parse_rerank_request_rejects_negative_source_scores():
    with pytest.raises(ProtocolError, match="source_scores"):
        parse_rerank_request(
            {
                "schema_version": RERANK_REQUEST_SCHEMA_VERSION,
                "request_id": "rerank-1",
                "persona_id": "default",
                "query_text": "coffee",
                "candidates": [
                    {
                        "node_id": "fact-1",
                        "node_type": "fact",
                        "safe_summary": "coffee",
                        "source_scores": {"sqlite_fts": -0.1},
                    }
                ],
            }
        )


def test_build_rerank_result_uses_rerank_schema():
    result = build_rerank_result(
        "rerank-1",
        results=[{"node_id": "fact-1", "rerank_score": 0.5, "debug_reason": "token_overlap=1"}],
    )

    assert result == {
        "schema_version": RERANK_RESPONSE_SCHEMA_VERSION,
        "request_id": "rerank-1",
        "results": [
            {"node_id": "fact-1", "rerank_score": 0.5, "debug_reason": "token_overlap=1"}
        ],
        "degraded": False,
    }


def test_server_health_and_mirror_operation_roundtrip():
    server = create_server(("127.0.0.1", 0), FakeMirrorAdapter())
    thread = threading.Thread(target=server.serve_forever)
    thread.start()
    try:
        base_url = f"http://127.0.0.1:{server.server_address[1]}"

        with urllib.request.urlopen(base_url + "/health", timeout=2) as response:
            assert response.status == 200
            assert json.load(response)["status"] == "ok"

        clear_response = urllib.request.urlopen(
            urllib.request.Request(
                base_url + "/mirror/clear-namespace",
                data=json.dumps(
                    {
                        "schema_version": CLEAR_NAMESPACE_REQUEST_SCHEMA_VERSION,
                        "persona_id": "default",
                    }
                ).encode("utf-8"),
                headers={"Content-Type": "application/json"},
                method="POST",
            ),
            timeout=2,
        )
        clear_body = json.load(clear_response)
        assert clear_response.status == 200
        assert clear_body["schema_version"] == CLEAR_NAMESPACE_RESPONSE_SCHEMA_VERSION
        assert clear_body["status"] == "ok"

        request = {
            "schema_version": REQUEST_SCHEMA_VERSION,
            "operation_id": "queue-1",
            "persona_id": "default",
            "operation": "upsert_node",
            "node": {
                "persona_id": "default",
                "node_type": "fact",
                "sqlite_node_id": "fact-1",
                "searchable_text": "coffee safe text",
                "payload": {"node_type": "fact"},
            },
        }
        response = urllib.request.urlopen(
            urllib.request.Request(
                base_url + "/mirror/operation",
                data=json.dumps(request).encode("utf-8"),
                headers={"Content-Type": "application/json"},
                method="POST",
            ),
            timeout=2,
        )

        body = json.load(response)
        assert response.status == 200
        assert body["schema_version"] == RESPONSE_SCHEMA_VERSION
        assert body["operation_id"] == "queue-1"
        assert body["status"] == "ok"
        assert body["trivium_node_id"] > 0

        candidate_response = urllib.request.urlopen(
            urllib.request.Request(
                base_url + "/retrieval/candidates",
                data=json.dumps(
                    {
                        "schema_version": CANDIDATE_REQUEST_SCHEMA_VERSION,
                        "request_id": "req-1",
                        "persona_id": "default",
                        "query_text": "coffee",
                        "limit": 8,
                    }
                ).encode("utf-8"),
                headers={"Content-Type": "application/json"},
                method="POST",
            ),
            timeout=2,
        )
        candidate_body = json.load(candidate_response)
        assert candidate_response.status == 200
        assert candidate_body["schema_version"] == CANDIDATE_RESPONSE_SCHEMA_VERSION
        assert candidate_body["request_id"] == "req-1"
        assert candidate_body["candidates"] == [
            {"trivium_node_id": body["trivium_node_id"], "score": 1.0, "source": "fake_sparse"}
        ]

        rerank_response = urllib.request.urlopen(
            urllib.request.Request(
                base_url + "/retrieval/rerank",
                data=json.dumps(
                    {
                        "schema_version": RERANK_REQUEST_SCHEMA_VERSION,
                        "request_id": "rerank-1",
                        "persona_id": "default",
                        "query_text": "coffee",
                        "candidates": [
                            {
                                "node_id": "fact-1",
                                "node_type": "fact",
                                "safe_summary": "coffee safe text",
                                "configured_score": 0.5,
                            }
                        ],
                    }
                ).encode("utf-8"),
                headers={"Content-Type": "application/json"},
                method="POST",
            ),
            timeout=2,
        )
        rerank_body = json.load(rerank_response)
        assert rerank_response.status == 200
        assert rerank_body["schema_version"] == RERANK_RESPONSE_SCHEMA_VERSION
        assert rerank_body["request_id"] == "rerank-1"
        assert rerank_body["degraded"] is False
        assert rerank_body["results"][0]["node_id"] == "fact-1"
        assert rerank_body["results"][0]["node_type"] == "fact"
        assert 0 <= rerank_body["results"][0]["rerank_score"] <= 1
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


def test_server_trivium_adapter_rerank_provider_none_returns_degraded():
    with tempfile.TemporaryDirectory() as tmp:
        config_path = Path(tmp) / "sidecar.toml"
        config_path.write_text(
            "[trivium]\n"
            f"dir = '{(Path(tmp) / 'trivium').as_posix()}'\n"
            "[embedding]\n"
            "dimensions = 3\n"
            "[rerank]\n"
            "provider = 'none'\n",
            encoding="utf-8",
        )
        adapter = create_adapter("trivium", config_path, env={})
        server = create_server(("127.0.0.1", 0), adapter)
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        try:
            base_url = f"http://127.0.0.1:{server.server_address[1]}"
            rerank_response = urllib.request.urlopen(
                urllib.request.Request(
                    base_url + "/retrieval/rerank",
                    data=json.dumps(
                        {
                            "schema_version": RERANK_REQUEST_SCHEMA_VERSION,
                            "request_id": "rerank-none",
                            "persona_id": "default",
                            "query_text": "coffee",
                            "candidates": [
                                {
                                    "node_id": "fact-1",
                                    "node_type": "fact",
                                    "safe_summary": "coffee safe text",
                                }
                            ],
                        }
                    ).encode("utf-8"),
                    headers={"Content-Type": "application/json"},
                    method="POST",
                ),
                timeout=2,
            )
            rerank_body = json.load(rerank_response)
            assert rerank_response.status == 200
            assert rerank_body == {
                "schema_version": RERANK_RESPONSE_SCHEMA_VERSION,
                "request_id": "rerank-none",
                "results": [],
                "degraded": True,
                "fallback_reason": "rerank_not_configured",
            }
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)


def test_server_rejects_bad_schema_with_http_400():
    server = create_server(("127.0.0.1", 0), FakeMirrorAdapter())
    thread = threading.Thread(target=server.serve_forever)
    thread.start()
    try:
        base_url = f"http://127.0.0.1:{server.server_address[1]}"
        request = {
            "schema_version": "wrong",
            "operation": "delete_node",
            "node": {
                "persona_id": "default",
                "node_type": "fact",
                "sqlite_node_id": "fact-1",
            },
        }

        with pytest.raises(urllib.error.HTTPError) as err:
            urllib.request.urlopen(
                urllib.request.Request(
                    base_url + "/mirror/operation",
                    data=json.dumps(request).encode("utf-8"),
                    headers={"Content-Type": "application/json"},
                    method="POST",
                ),
                timeout=2,
            )

        assert err.value.code == 400
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


def test_server_close_closes_adapter_when_supported():
    class ClosingAdapter(FakeMirrorAdapter):
        def __init__(self) -> None:
            super().__init__()
            self.closed = False

        def close(self) -> None:
            self.closed = True

    adapter = ClosingAdapter()
    server = create_server(("127.0.0.1", 0), adapter)

    server.server_close()

    assert adapter.closed is True


def test_create_adapter_loads_config_for_trivium(monkeypatch, tmp_path):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text("[trivium]\ndir = \"mirror\"\n", encoding="utf-8")
    captured = {}

    class CapturingTriviumAdapter:
        def __init__(self, config: SidecarConfig) -> None:
            captured["config"] = config

    monkeypatch.setattr("memorycore_sidecar.server.TriviumAdapter", CapturingTriviumAdapter)

    adapter = create_adapter("trivium", config_path, env={})

    assert isinstance(adapter, CapturingTriviumAdapter)
    assert captured["config"].trivium.dir == (tmp_path / "mirror").resolve()


def test_create_adapter_does_not_load_config_for_fake(monkeypatch, tmp_path):
    def fail_load_config(*args, **kwargs):
        raise AssertionError("fake adapter should not load config")

    monkeypatch.setattr("memorycore_sidecar.server.load_config", fail_load_config)

    adapter = create_adapter("fake", tmp_path / "missing.toml", env={})

    assert isinstance(adapter, FakeMirrorAdapter)
