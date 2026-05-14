from __future__ import annotations

import json
import urllib.error
from typing import Any
from urllib.parse import urlparse

from memorycore_sidecar.config import RerankConfig
from memorycore_sidecar.rerank import (
    DashScopeVLRerankProvider,
    DisabledRerankProvider,
    build_rerank_provider,
)


def test_dashscope_vl_provider_posts_safe_summaries_and_maps_clamped_results(monkeypatch):
    captured: dict[str, Any] = {}

    def fake_urlopen(request: Any, timeout: int) -> _Response:
        captured["timeout"] = timeout
        captured["path"] = urlparse(request.full_url).path
        captured["headers"] = {key.lower(): value for key, value in request.header_items()}
        captured["body"] = json.loads(request.data.decode("utf-8"))
        return _Response(
            {
                "output": {
                    "results": [
                        {
                            "index": 1,
                            "relevance_score": 1.2,
                            "document": {"text": "provider leaked text"},
                        },
                        {"index": 0, "relevance_score": "-0.5"},
                    ]
                }
            }
        )

    monkeypatch.setattr("memorycore_sidecar.rerank.urllib.request.urlopen", fake_urlopen)
    provider = DashScopeVLRerankProvider(
        _rerank_config(endpoint_url="https://dashscope.aliyuncs.com/custom/rerank", top_n=5),
        env={"DASHSCOPE_API_KEY": "test-secret"},
    )

    result = provider.rerank(
        "用户要找咖啡偏好",
        [
            _candidate("node-a", "fact", "safe summary A", raw_text="do not send A"),
            _candidate("node-b", "episode", "safe summary B", raw_text="do not send B"),
        ],
    )

    assert result == {
        "results": [
            {
                "node_id": "node-b",
                "node_type": "episode",
                "rerank_score": 1.0,
                "debug_reason": "dashscope_qwen3_vl_rerank index=1",
            },
            {
                "node_id": "node-a",
                "node_type": "fact",
                "rerank_score": 0.0,
                "debug_reason": "dashscope_qwen3_vl_rerank index=0",
            },
        ],
        "degraded": False,
    }
    assert captured["timeout"] == 30
    assert captured["path"] == "/custom/rerank"
    assert captured["headers"]["authorization"] == "Bearer test-secret"
    assert captured["headers"]["content-type"] == "application/json"
    assert captured["body"] == {
        "model": "qwen3-vl-rerank",
        "input": {
            "query": {"text": "用户要找咖啡偏好"},
            "documents": [{"text": "safe summary A"}, {"text": "safe summary B"}],
        },
        "parameters": {
            "top_n": 2,
            "return_documents": False,
            "instruct": "Retrieve semantically relevant safe memory summaries for the user's query.",
        },
    }
    assert "raw_text" not in json.dumps(captured["body"])
    assert "provider leaked text" not in json.dumps(result)


def test_dashscope_vl_provider_caps_top_n_by_config(monkeypatch):
    captured: dict[str, Any] = {}

    def fake_urlopen(request: Any, timeout: int) -> _Response:
        captured["body"] = json.loads(request.data.decode("utf-8"))
        return _Response({"output": {"results": []}})

    monkeypatch.setattr("memorycore_sidecar.rerank.urllib.request.urlopen", fake_urlopen)
    provider = DashScopeVLRerankProvider(
        _rerank_config(top_n=1),
        env={"DASHSCOPE_API_KEY": "test-secret"},
    )

    result = provider.rerank(
        "query",
        [
            _candidate("node-a", "fact", "safe summary A"),
            _candidate("node-b", "episode", "safe summary B"),
        ],
    )

    assert result == {"results": [], "degraded": False}
    assert captured["body"]["parameters"]["top_n"] == 1
    assert captured["body"]["parameters"]["return_documents"] is False


def test_dashscope_vl_provider_missing_key_returns_safe_fallback(monkeypatch):
    called = False

    def fake_urlopen(request: Any, timeout: int) -> _Response:
        nonlocal called
        called = True
        return _Response({"output": {"results": []}})

    monkeypatch.setattr("memorycore_sidecar.rerank.urllib.request.urlopen", fake_urlopen)
    provider = DashScopeVLRerankProvider(
        _rerank_config(api_key_env="MISSING_RERANK_KEY"),
        env={},
    )

    result = provider.rerank("query", [_candidate("node-a", "fact", "safe summary")])

    assert result == {
        "results": [],
        "degraded": True,
        "fallback_reason": "missing_api_key",
    }
    assert called is False


def test_dashscope_vl_provider_http_error_returns_safe_fallback(monkeypatch):
    def fake_urlopen(request: Any, timeout: int) -> _Response:
        raise urllib.error.HTTPError(request.full_url, 502, "bad gateway", {}, None)

    monkeypatch.setattr("memorycore_sidecar.rerank.urllib.request.urlopen", fake_urlopen)
    provider = DashScopeVLRerankProvider(
        _rerank_config(),
        env={"DASHSCOPE_API_KEY": "test-secret"},
    )

    result = provider.rerank("query", [_candidate("node-a", "fact", "safe summary")])

    assert result == {
        "results": [],
        "degraded": True,
        "fallback_reason": "http_error",
    }
    assert "test-secret" not in json.dumps(result)
    assert "safe summary" not in json.dumps(result)


def test_dashscope_vl_provider_url_error_returns_safe_fallback_without_details(monkeypatch):
    def fake_urlopen(request: Any, timeout: int) -> _Response:
        raise urllib.error.URLError("secret upstream detail")

    monkeypatch.setattr("memorycore_sidecar.rerank.urllib.request.urlopen", fake_urlopen)
    provider = DashScopeVLRerankProvider(
        _rerank_config(),
        env={"DASHSCOPE_API_KEY": "test-secret"},
    )

    result = provider.rerank("query", [_candidate("node-a", "fact", "safe summary")])

    assert result == {
        "results": [],
        "degraded": True,
        "fallback_reason": "url_error",
    }
    assert "secret upstream detail" not in json.dumps(result)
    assert "test-secret" not in json.dumps(result)
    assert "safe summary" not in json.dumps(result)


def test_dashscope_vl_provider_malformed_response_returns_safe_fallback(monkeypatch):
    def fake_urlopen(request: Any, timeout: int) -> _Response:
        return _Response(
            {
                "output": {
                    "results": [
                        {"index": 9, "relevance_score": 0.7},
                    ]
                }
            }
        )

    monkeypatch.setattr("memorycore_sidecar.rerank.urllib.request.urlopen", fake_urlopen)
    provider = DashScopeVLRerankProvider(
        _rerank_config(),
        env={"DASHSCOPE_API_KEY": "test-secret"},
    )

    result = provider.rerank("query", [_candidate("node-a", "fact", "safe summary")])

    assert result == {
        "results": [],
        "degraded": True,
        "fallback_reason": "malformed_response",
    }


def test_dashscope_vl_provider_json_decode_error_returns_safe_fallback(monkeypatch):
    def fake_urlopen(request: Any, timeout: int) -> _RawResponse:
        return _RawResponse(b"{not-json")

    monkeypatch.setattr("memorycore_sidecar.rerank.urllib.request.urlopen", fake_urlopen)
    provider = DashScopeVLRerankProvider(
        _rerank_config(),
        env={"DASHSCOPE_API_KEY": "test-secret"},
    )

    result = provider.rerank("query", [_candidate("node-a", "fact", "safe summary")])

    assert result == {
        "results": [],
        "degraded": True,
        "fallback_reason": "malformed_response",
    }


def test_dashscope_vl_provider_malformed_score_returns_safe_fallback(monkeypatch):
    def fake_urlopen(request: Any, timeout: int) -> _Response:
        return _Response(
            {
                "output": {
                    "results": [
                        {"index": 0, "relevance_score": "not-a-score"},
                    ]
                }
            }
        )

    monkeypatch.setattr("memorycore_sidecar.rerank.urllib.request.urlopen", fake_urlopen)
    provider = DashScopeVLRerankProvider(
        _rerank_config(),
        env={"DASHSCOPE_API_KEY": "test-secret"},
    )

    result = provider.rerank("query", [_candidate("node-a", "fact", "safe summary")])

    assert result == {
        "results": [],
        "degraded": True,
        "fallback_reason": "malformed_response",
    }


def test_build_rerank_provider_returns_disabled_provider_for_none():
    provider = build_rerank_provider(_rerank_config(provider="none"))

    assert isinstance(provider, DisabledRerankProvider)
    assert provider.rerank("query", [_candidate("node-a", "fact", "safe summary")]) == {
        "results": [],
        "degraded": True,
        "fallback_reason": "rerank_not_configured",
    }


class _Response:
    def __init__(self, body: dict[str, Any]) -> None:
        self._body = json.dumps(body).encode("utf-8")

    def __enter__(self) -> "_Response":
        return self

    def __exit__(self, exc_type: Any, exc: Any, traceback: Any) -> None:
        return None

    def read(self) -> bytes:
        return self._body


class _RawResponse:
    def __init__(self, body: bytes) -> None:
        self._body = body

    def __enter__(self) -> "_RawResponse":
        return self

    def __exit__(self, exc_type: Any, exc: Any, traceback: Any) -> None:
        return None

    def read(self) -> bytes:
        return self._body


def _candidate(
    node_id: str,
    node_type: str,
    safe_summary: str,
    **extra: Any,
) -> dict[str, Any]:
    candidate = {
        "node_id": node_id,
        "node_type": node_type,
        "safe_summary": safe_summary,
    }
    candidate.update(extra)
    return candidate


def _rerank_config(**overrides: Any) -> RerankConfig:
    values = {
        "provider": "dashscope-vl",
        "endpoint_url": "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank",
        "api_key_env": "DASHSCOPE_API_KEY",
        "model": "qwen3-vl-rerank",
        "timeout_seconds": 30,
        "top_n": 30,
        "instruct": "Retrieve semantically relevant safe memory summaries for the user's query.",
    }
    values.update(overrides)
    return RerankConfig(**values)
