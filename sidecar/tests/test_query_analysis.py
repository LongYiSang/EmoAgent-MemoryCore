import json
import urllib.error

import pytest

from memorycore_sidecar.config import QueryAnalysisConfig
from memorycore_sidecar.protocol import (
    QUERY_ANALYSIS_REQUEST_SCHEMA_VERSION,
    QUERY_ANALYSIS_RESPONSE_SCHEMA_VERSION,
    ProtocolError,
    build_query_analysis_result,
    parse_query_analysis_request,
)
from memorycore_sidecar.query_analysis import analyze_query


def test_parse_query_analysis_request_accepts_optional_rationale_flag():
    request = parse_query_analysis_request(
        {
            "schema_version": QUERY_ANALYSIS_REQUEST_SCHEMA_VERSION,
            "request_id": " qa-1 ",
            "persona_id": " default ",
            "query_text": "昨天我说过的咖啡偏好是什么？",
            "include_rationale": True,
        }
    )

    assert request == {
        "request_id": "qa-1",
        "persona_id": "default",
        "query_text": "昨天我说过的咖啡偏好是什么？",
        "input_language": "zh-Hans",
        "now": "",
        "timezone": "",
        "rule_analysis": {},
        "allowed_enums": {},
        "visible_entity_hints": [],
        "retrieval_policy": {},
        "conversation_window": [],
        "include_rationale": True,
    }


def test_parse_query_analysis_request_rejects_blank_query_text():
    with pytest.raises(ProtocolError, match="query_text"):
        parse_query_analysis_request(
            {
                "schema_version": QUERY_ANALYSIS_REQUEST_SCHEMA_VERSION,
                "request_id": "qa-1",
                "persona_id": "default",
                "query_text": " ",
            }
        )


def test_build_query_analysis_result_uses_expected_schema_and_omits_rationale_by_default():
    result = build_query_analysis_result(
        "qa-1",
        {
            "status": "ok",
            "degraded": False,
            "provider": "none",
            "model": "",
            "prompt_version": "query-analysis-v0.1",
            "time_mode": "unspecified",
            "memory_domain": "general",
            "memory_ability": "recall",
            "evidence_need": "medium",
            "signals": [],
            "confidence": 0.1,
            "field_confidence": {"time_mode": 0.1},
            "entity_mentions": [],
            "query_rewrites": [],
            "semantic_anchors": [],
            "context_block_hints": [],
            "policy_hints": {},
        },
    )

    assert result["schema_version"] == QUERY_ANALYSIS_RESPONSE_SCHEMA_VERSION
    assert result["request_id"] == "qa-1"
    assert result["status"] == "ok"
    assert "query_rewrites" not in result
    assert result["analysis"]["query_rewrites"] == []
    assert result["analysis"]["policy_hints"] == {}
    assert "rationale_summary" not in result


def test_analyze_query_provider_none_returns_bounded_degraded_fallback():
    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "forget/delete all cafe related memories",
            "include_rationale": False,
        },
        QueryAnalysisConfig(
            provider="none",
            base_url="https://example.test/v1",
            api_key_env="QUERY_KEY",
            model="test-model",
            timeout_seconds=2,
            temperature=0.0,
            response_format="json_object",
            prompt_version="query-analysis-v0.1",
        ),
        env={},
    )

    assert result["status"] == "degraded"
    assert result["degraded"] is True
    assert result["fallback_reason"] == "provider_none"
    assert result["provider"] == "none"
    assert result["model"] == "test-model"
    assert result["signals"] == ["forget_delete"]
    assert result["entity_mentions"] == []
    assert result["policy_hints"] == {}
    assert "delete" in result["query_rewrites"][0]["text"].casefold()
    assert len(result["fallback_reason"]) <= 64


def test_analyze_query_missing_api_key_does_not_call_provider_or_leak_env_name(monkeypatch):
    def fail_urlopen(*args, **kwargs):
        raise AssertionError("provider must not be called without an API key")

    monkeypatch.setattr("urllib.request.urlopen", fail_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "coffee preference",
            "include_rationale": False,
        },
        QueryAnalysisConfig(
            provider="openai-compatible",
            base_url="https://example.test/v1",
            api_key_env="SECRET_QUERY_KEY",
            model="test-model",
            timeout_seconds=2,
            temperature=0.0,
            response_format="json_object",
            prompt_version="query-analysis-v0.1",
        ),
        env={},
    )

    assert result["degraded"] is True
    assert result["fallback_reason"] == "missing_api_key"
    assert "SECRET_QUERY_KEY" not in str(result)


def test_analyze_query_retries_once_after_schema_validation_failure(monkeypatch):
    responses = [
        {"choices": [{"message": {"content": json.dumps({"time_mode": "recent"})}}]},
        {
            "choices": [
                {
                    "message": {
                        "content": json.dumps(
                            {
                                "time_mode": "recent",
                                "memory_domain": "preference",
                                "memory_ability": "recall",
                                "evidence_need": "medium",
                                "signals": ["preference"],
                                "confidence": 0.8,
                                "field_confidence": {"time_mode": 0.7},
                                "entity_mentions": [
                                    {
                                        "entity_id": "ent_coffee",
                                        "canonical_name": "Coffee",
                                        "match_text": "coffee",
                                        "match_kind": "alias",
                                        "confidence": 0.8,
                                    }
                                ],
                                "query_rewrites": [
                                    {
                                        "text": "coffee preference",
                                        "weight": 0.7,
                                        "purpose": "semantic_recall",
                                    }
                                ],
                                "semantic_anchors": ["coffee"],
                                "context_block_hints": ["facts"],
                                "policy_hints": {
                                    "prefer_evidenced_by_links": True,
                                    "max_hops_hint": 2,
                                },
                                "rationale_summary": "User asks about a preference.",
                            }
                        )
                    }
                }
            ]
        },
    ]
    calls = []

    class Response:
        def __init__(self, payload):
            self.payload = payload

        def __enter__(self):
            return self

        def __exit__(self, *exc):
            return False

        def read(self):
            return json.dumps(self.payload).encode("utf-8")

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return Response(responses.pop(0))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "coffee preference",
            "include_rationale": True,
        },
        QueryAnalysisConfig(
            provider="openai-compatible",
            base_url="https://example.test/v1",
            api_key_env="QUERY_KEY",
            model="test-model",
            timeout_seconds=2,
            temperature=0.0,
            response_format="json_object",
            prompt_version="query-analysis-v0.1",
        ),
        env={"QUERY_KEY": "secret"},
    )

    assert len(calls) == 2
    assert calls[0]["response_format"] == {"type": "json_object"}
    assert result["degraded"] is False
    assert result["provider"] == "openai-compatible"
    assert result["entity_mentions"][0]["entity_id"] == "ent_coffee"
    assert result["policy_hints"]["prefer_evidenced_by_links"] is True
    assert result["policy_hints"]["max_hops_hint"] == 2
    assert result["query_rewrites"][0]["text"] == "coffee preference"
    assert result["rationale_summary"] == "User asks about a preference."
    assert "secret" not in str(result)


def test_analyze_query_retries_invalid_json_then_ok(monkeypatch):
    calls = []
    responses = [
        _provider_response("{not-json"),
        _provider_response(json.dumps(_valid_analysis(query_rewrites=["咖啡偏好"]))),
    ]

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return _Response(responses.pop(0))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "咖啡偏好是什么？",
        },
        _query_config(),
        env={"QUERY_KEY": "secret"},
    )

    assert len(calls) == 2
    assert result["status"] == "ok"
    assert result["degraded"] is False
    assert _user_payload(calls[0])["retry_schema_validation"] is False
    assert _user_payload(calls[1])["retry_schema_validation"] is True


def test_analyze_query_retries_missing_field_then_ok(monkeypatch):
    calls = []
    responses = [
        _provider_response(json.dumps({"time_mode": "recent"})),
        _provider_response(json.dumps(_valid_analysis())),
    ]

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return _Response(responses.pop(0))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "coffee preference",
        },
        _query_config(),
        env={"QUERY_KEY": "secret"},
    )

    assert len(calls) == 2
    assert result["status"] == "ok"
    assert result["diagnostics"]["first_failure_reason"] == "validation_failed"
    assert len(result["diagnostics"]["first_failure_reason"]) <= 64


def test_analyze_query_retries_invalid_enum_then_ok(monkeypatch):
    calls = []
    responses = [
        _provider_response(json.dumps(_valid_analysis(time_mode="NOT_ALLOWED"))),
        _provider_response(json.dumps(_valid_analysis(time_mode="historical"))),
    ]

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return _Response(responses.pop(0))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "我以前喜欢什么音乐？",
            "allowed_enums": {"time_mode": ["historical"]},
        },
        _query_config(),
        env={"QUERY_KEY": "secret"},
    )

    assert len(calls) == 2
    assert result["status"] == "ok"
    assert result["time_mode"] == "historical"
    assert _user_payload(calls[1])["retry_schema_validation"] is True
    assert result["diagnostics"]["first_failure_reason"] == "validation_failed"
    assert "NOT_ALLOWED" not in str(result)
    assert "secret" not in str(result)


def test_analyze_query_retries_go_shaped_invalid_enums_then_ok(monkeypatch):
    calls = []
    responses = [
        _provider_response(
            json.dumps(
                _valid_analysis(
                    time_mode="NOT_ALLOWED",
                    memory_domain="relationship_memory",
                    memory_ability="relationship_arc",
                    evidence_need="state_transition",
                )
            )
        ),
        _provider_response(
            json.dumps(
                _valid_analysis(
                    time_mode="historical",
                    memory_domain="relationship_memory",
                    memory_ability="relationship_arc",
                    evidence_need="state_transition",
                )
            )
        ),
    ]

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return _Response(responses.pop(0))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "我和AI助手的关系发生了什么变化？",
            "allowed_enums": _go_shaped_allowed_enums(),
        },
        _query_config(),
        env={"QUERY_KEY": "secret"},
    )

    assert len(calls) == 2
    assert result["status"] == "ok"
    assert result["time_mode"] == "historical"
    assert result["memory_domain"] == "relationship_memory"
    assert result["memory_ability"] == "relationship_arc"
    assert result["evidence_need"] == "state_transition"
    assert _user_payload(calls[1])["retry_schema_validation"] is True
    assert result["diagnostics"]["first_failure_reason"] == "validation_failed"
    assert "NOT_ALLOWED" not in str(result)


def test_analyze_query_two_invalid_json_falls_back(monkeypatch):
    calls = []
    responses = [
        _provider_response("{not-json"),
        _provider_response("also not json"),
    ]

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return _Response(responses.pop(0))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "coffee preference",
        },
        _query_config(),
        env={"QUERY_KEY": "secret"},
    )

    assert len(calls) == 2
    assert result["status"] == "degraded"
    assert result["fallback_reason"] == "invalid_json"
    assert result["diagnostics"]["final_fallback_reason"] == "invalid_json"


def test_analyze_query_two_validation_failures_falls_back(monkeypatch):
    calls = []
    responses = [
        _provider_response(json.dumps({"time_mode": "recent"})),
        _provider_response(json.dumps({"time_mode": "recent"})),
    ]

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return _Response(responses.pop(0))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "coffee preference",
        },
        _query_config(),
        env={"QUERY_KEY": "secret"},
    )

    assert len(calls) == 2
    assert result["status"] == "degraded"
    assert result["fallback_reason"] == "validation_failed"
    assert result["diagnostics"]["first_failure_reason"] == "validation_failed"
    assert result["diagnostics"]["final_fallback_reason"] == "validation_failed"


def test_analyze_query_two_invalid_enum_failures_falls_back(monkeypatch):
    calls = []
    responses = [
        _provider_response(json.dumps(_valid_analysis(time_mode="NOT_ALLOWED"))),
        _provider_response(json.dumps(_valid_analysis(time_mode="ALSO_NOT_ALLOWED"))),
    ]

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return _Response(responses.pop(0))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "我以前喜欢什么音乐？",
            "allowed_enums": {"time_mode": ["historical"]},
        },
        _query_config(),
        env={"QUERY_KEY": "secret"},
    )

    assert len(calls) == 2
    assert result["status"] == "degraded"
    assert result["fallback_reason"] == "validation_failed"
    assert result["diagnostics"]["final_fallback_reason"] == "validation_failed"
    assert "NOT_ALLOWED" not in str(result)
    assert "ALSO_NOT_ALLOWED" not in str(result)


def test_analyze_query_two_go_shaped_invalid_enum_failures_falls_back(monkeypatch):
    calls = []
    responses = [
        _provider_response(
            json.dumps(
                _valid_analysis(
                    time_mode="NOT_ALLOWED",
                    memory_domain="relationship_memory",
                    memory_ability="relationship_arc",
                    evidence_need="state_transition",
                )
            )
        ),
        _provider_response(
            json.dumps(
                _valid_analysis(
                    time_mode="historical",
                    memory_domain="NOT_A_DOMAIN",
                    memory_ability="relationship_arc",
                    evidence_need="state_transition",
                )
            )
        ),
    ]

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return _Response(responses.pop(0))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "我和AI助手的关系发生了什么变化？",
            "allowed_enums": _go_shaped_allowed_enums(),
        },
        _query_config(),
        env={"QUERY_KEY": "secret"},
    )

    assert len(calls) == 2
    assert result["status"] == "degraded"
    assert result["fallback_reason"] == "validation_failed"
    assert result["diagnostics"]["first_failure_reason"] == "validation_failed"
    assert result["diagnostics"]["final_fallback_reason"] == "validation_failed"
    assert "NOT_ALLOWED" not in str(result)
    assert "NOT_A_DOMAIN" not in str(result)


def test_analyze_query_sends_rich_request_payload_and_strict_prompt(monkeypatch):
    calls = []

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return _Response(_provider_response(json.dumps(_valid_analysis(time_mode="historical"))))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "我一开始喜欢什么音乐？",
            "now": "2026-05-19T12:00:00+08:00",
            "timezone": "Asia/Shanghai",
            "rule_analysis": {"time_mode": "historical", "signals": ["transition"]},
            "allowed_enums": {"time_mode": ["historical", "bitemporal"]},
            "visible_entity_hints": [{"entity_id": "ent_laufey", "canonical_name": "Laufey"}],
            "retrieval_policy": {"allow_historical": True},
        },
        _query_config(),
        env={"QUERY_KEY": "secret"},
    )

    assert result["status"] == "ok"
    user_payload = _user_payload(calls[0])
    assert user_payload["query_text"] == "我一开始喜欢什么音乐？"
    assert user_payload["input_language"] == "zh-Hans"
    assert user_payload["now"] == "2026-05-19T12:00:00+08:00"
    assert user_payload["timezone"] == "Asia/Shanghai"
    assert user_payload["rule_analysis"] == {"time_mode": "historical", "signals": ["transition"]}
    assert user_payload["allowed_enums"] == {"time_mode": ["historical", "bitemporal"]}
    assert user_payload["visible_entity_hints"] == [
        {"entity_id": "ent_laufey", "canonical_name": "Laufey"}
    ]
    assert user_payload["retrieval_policy"] == {"allow_historical": True}
    assert user_payload["conversation_window"] == []
    assert user_payload["include_rationale"] is False
    assert user_payload["output_contract"] == {
        "return_only": "analysis_object",
        "rewrite_language": "same_as_query",
        "max_query_rewrites": 3,
        "max_semantic_anchors": 4,
    }
    prompt = calls[0]["messages"][0]["content"]
    assert "Return strict JSON object only" in prompt
    assert "Do not translate Chinese queries into English" in prompt
    assert "premise_counterexample" in prompt
    assert "causal" in prompt


def test_analyze_query_public_diagnostics_do_not_include_raw_provider_response(monkeypatch):
    secret_raw = "raw provider response with api key secret-token"

    def fake_urlopen(request, timeout):
        return _Response(_provider_response(secret_raw))

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "coffee preference",
        },
        _query_config(),
        env={"QUERY_KEY": "secret"},
    )

    assert result["status"] == "degraded"
    assert result["diagnostics"]["final_fallback_reason"] == "invalid_json"
    assert "raw provider response" not in str(result)
    assert "secret-token" not in str(result)


def test_analyze_query_provider_payload_always_uses_zero_temperature(monkeypatch):
    calls = []

    class Response:
        def __enter__(self):
            return self

        def __exit__(self, *exc):
            return False

        def read(self):
            return json.dumps(
                {
                    "choices": [
                        {
                            "message": {
                                "content": json.dumps(
                                    {
                                        "time_mode": "unspecified",
                                        "memory_domain": "general",
                                        "memory_ability": "recall",
                                        "evidence_need": "medium",
                                        "signals": [],
                                        "confidence": 0.5,
                                        "field_confidence": {"time_mode": 0.5},
                                        "entity_mentions": [],
                                        "query_rewrites": [],
                                        "semantic_anchors": [],
                                        "context_block_hints": [],
                                        "policy_hints": [],
                                    }
                                )
                            }
                        }
                    ]
                }
            ).encode("utf-8")

    def fake_urlopen(request, timeout):
        calls.append(json.loads(request.data.decode("utf-8")))
        return Response()

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "coffee preference",
            "include_rationale": False,
        },
        QueryAnalysisConfig(
            provider="openai-compatible",
            base_url="https://example.test/v1",
            api_key_env="QUERY_KEY",
            model="test-model",
            timeout_seconds=2,
            max_tokens=384,
            temperature=0.7,
            response_format="json_object",
            prompt_version="query-analysis-v0.1",
        ),
        env={"QUERY_KEY": "secret"},
    )

    assert result["degraded"] is False
    assert calls[0]["temperature"] == 0
    assert calls[0]["max_tokens"] == 384


def test_analyze_query_invalid_json_provider_wrapper_retries_then_falls_back(monkeypatch):
    calls = 0

    class Response:
        def __enter__(self):
            return self

        def __exit__(self, *exc):
            return False

        def read(self):
            return b"{not-json"

    def fake_urlopen(request, timeout):
        nonlocal calls
        calls += 1
        return Response()

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "coffee preference",
            "include_rationale": False,
        },
        QueryAnalysisConfig(
            provider="openai-compatible",
            base_url="https://example.test/v1",
            api_key_env="QUERY_KEY",
            model="test-model",
            timeout_seconds=2,
            temperature=0.0,
            response_format="json_object",
            prompt_version="query-analysis-v0.1",
        ),
        env={"QUERY_KEY": "secret"},
    )

    assert calls == 2
    assert result["degraded"] is True
    assert result["fallback_reason"] == "invalid_json"


@pytest.mark.parametrize(
    "provider_payload",
    [
        {},
        {"choices": []},
        {"choices": [{"message": {"content": ""}}]},
        {"choices": [{"message": {"content": 42}}]},
    ],
)
def test_analyze_query_retries_malformed_provider_wrapper_then_falls_back(
    monkeypatch, provider_payload
):
    calls = 0

    class Response:
        def __enter__(self):
            return self

        def __exit__(self, *exc):
            return False

        def read(self):
            return json.dumps(provider_payload).encode("utf-8")

    def fake_urlopen(request, timeout):
        nonlocal calls
        calls += 1
        return Response()

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-1",
            "persona_id": "default",
            "query_text": "coffee preference",
            "include_rationale": False,
        },
        QueryAnalysisConfig(
            provider="openai-compatible",
            base_url="https://example.test/v1",
            api_key_env="QUERY_KEY",
            model="test-model",
            timeout_seconds=2,
            temperature=0.0,
            response_format="json_object",
            prompt_version="query-analysis-v0.1",
        ),
        env={"QUERY_KEY": "secret"},
    )

    assert calls == 2
    assert result["degraded"] is True
    assert result["fallback_reason"] == "validation_failed"
    assert result["diagnostics"]["final_fallback_reason"] == "validation_failed"


@pytest.mark.parametrize(
    "provider_error",
    [
        urllib.error.HTTPError(
            url="https://example.test/v1/chat/completions",
            code=500,
            msg="provider failed",
            hdrs=None,
            fp=None,
        ),
        urllib.error.URLError("connection refused"),
    ],
)
def test_analyze_query_provider_error_returns_bounded_fallback_without_retry(
    monkeypatch, provider_error
):
    calls = 0

    def fake_urlopen(request, timeout):
        nonlocal calls
        calls += 1
        raise provider_error

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-provider-error",
            "persona_id": "default",
            "query_text": "coffee preference",
            "include_rationale": False,
        },
        QueryAnalysisConfig(
            provider="openai-compatible",
            base_url="https://example.test/v1",
            api_key_env="QUERY_KEY",
            model="test-model",
            timeout_seconds=2,
            temperature=0.0,
            response_format="json_object",
            prompt_version="query-analysis-v0.1",
        ),
        env={"QUERY_KEY": "secret"},
    )

    assert calls == 1
    assert result["degraded"] is True
    assert result["fallback_reason"] == "provider_error"
    assert len(result["fallback_reason"]) <= 64


def test_analyze_query_provider_timeout_returns_distinct_fallback_without_retry(
    monkeypatch,
):
    calls = 0

    def fake_urlopen(request, timeout):
        nonlocal calls
        calls += 1
        raise TimeoutError("provider timed out")

    monkeypatch.setattr("urllib.request.urlopen", fake_urlopen)

    result = analyze_query(
        {
            "request_id": "qa-provider-timeout",
            "persona_id": "default",
            "query_text": "coffee preference",
            "include_rationale": False,
        },
        QueryAnalysisConfig(
            provider="openai-compatible",
            base_url="https://example.test/v1",
            api_key_env="QUERY_KEY",
            model="test-model",
            timeout_seconds=2,
            temperature=0.0,
            response_format="json_object",
            prompt_version="query-analysis-v0.1",
        ),
        env={"QUERY_KEY": "secret"},
    )

    assert calls == 1
    assert result["degraded"] is True
    assert result["fallback_reason"] == "provider_timeout"


class _Response:
    def __init__(self, payload):
        self.payload = payload

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False

    def read(self):
        return json.dumps(self.payload).encode("utf-8")


def _provider_response(content: str) -> dict:
    return {"choices": [{"message": {"content": content}}]}


def _valid_analysis(**overrides):
    analysis = {
        "time_mode": "unspecified",
        "memory_domain": "general",
        "memory_ability": "recall",
        "evidence_need": "medium",
        "signals": [],
        "confidence": 0.5,
        "field_confidence": {"time_mode": 0.5},
        "entity_mentions": [],
        "query_rewrites": [],
        "semantic_anchors": [],
        "context_block_hints": [],
        "policy_hints": {},
    }
    analysis.update(overrides)
    return analysis


def _query_config():
    return QueryAnalysisConfig(
        provider="openai-compatible",
        base_url="https://example.test/v1",
        api_key_env="QUERY_KEY",
        model="test-model",
        timeout_seconds=2,
        temperature=0.0,
        response_format="json_object",
        prompt_version="query-analysis-v0.1",
    )


def _go_shaped_allowed_enums():
    return {
        "time_modes": ["historical"],
        "memory_abilities": ["relationship_arc"],
        "evidence_needs": ["state_transition"],
        "memory_domains": ["relationship_memory"],
    }


def _user_payload(call: dict):
    return json.loads(call["messages"][1]["content"])
