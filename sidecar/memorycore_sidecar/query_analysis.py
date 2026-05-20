from __future__ import annotations

import json
import os
import re
import time
import urllib.error
import urllib.request
from typing import Any, Mapping

from .config import QueryAnalysisConfig
from .candidates import clamp_float

_LEGACY_PROVIDER_FIELDS = (
    "time_mode",
    "memory_domain",
    "memory_ability",
    "evidence_need",
)

_MINIMAL_PROVIDER_FIELDS = (
    "intent",
    "confidence",
    "rewrite",
    "language",
)

_ENUM_ANALYSIS_FIELDS = (
    "time_mode",
    "memory_domain",
    "memory_ability",
    "evidence_need",
)

_ALLOWED_ENUM_KEYS = {
    "time_mode": ("time_mode", "time_modes"),
    "memory_domain": ("memory_domain", "memory_domains"),
    "memory_ability": ("memory_ability", "memory_abilities"),
    "evidence_need": ("evidence_need", "evidence_needs"),
}

_MIN_PROVIDER_BUDGET_MS = 700
_MAX_PROVIDER_TOKENS = 512
_DEFAULT_ENUMS = {
    "time_mode": "unspecified",
    "memory_domain": "general",
    "memory_ability": "recall",
    "evidence_need": "medium",
}


def analyze_query(
    request: dict[str, Any],
    config: QueryAnalysisConfig,
    env: Mapping[str, str] | None = None,
) -> dict[str, Any]:
    started_monotonic = time.monotonic()
    env_values = os.environ if env is None else env
    if config.provider == "none":
        return _fallback(request, config, "provider_none")
    if _effective_provider_budget_ms(request, config, started_monotonic) < _MIN_PROVIDER_BUDGET_MS:
        return _fallback(
            request,
            config,
            "provider_budget_exhausted",
            final_fallback_reason="provider_budget_exhausted",
        )
    api_key = env_values.get(config.api_key_env, "")
    if not api_key.strip():
        return _fallback(request, config, "missing_api_key")

    first_failure_reason: str | None = None
    for attempt in range(2):
        provider_budget_ms = _effective_provider_budget_ms(
            request, config, started_monotonic
        )
        if provider_budget_ms < _MIN_PROVIDER_BUDGET_MS:
            return _fallback(
                request,
                config,
                "provider_budget_exhausted",
                first_failure_reason=first_failure_reason,
                final_fallback_reason="provider_budget_exhausted",
            )
        try:
            payload = _call_openai_compatible(
                request,
                config,
                api_key,
                timeout_seconds=provider_budget_ms / 1000.0,
                retry_after_validation_failure=attempt > 0,
            )
        except json.JSONDecodeError:
            failure_reason = "invalid_json"
        except UnicodeDecodeError:
            failure_reason = "invalid_response"
        except ValueError:
            failure_reason = "validation_failed"
        except (urllib.error.HTTPError, urllib.error.URLError, TimeoutError, OSError) as exc:
            if _is_provider_timeout_error(exc):
                return _fallback(
                    request,
                    config,
                    "sidecar_provider_timeout",
                    first_failure_reason=first_failure_reason,
                    final_fallback_reason="sidecar_provider_timeout",
                )
            return _fallback(
                request,
                config,
                "provider_error",
                first_failure_reason=first_failure_reason,
                final_fallback_reason="provider_error",
            )
        else:
            try:
                result = _validate_provider_analysis(
                    payload,
                    request,
                    config,
                    include_rationale=bool(request.get("include_rationale", False)),
                )
            except ValueError:
                failure_reason = "validation_failed"
            else:
                if first_failure_reason:
                    result["diagnostics"] = {
                        "first_failure_reason": first_failure_reason[:64],
                    }
                return result

        if first_failure_reason is None:
            first_failure_reason = failure_reason
        if attempt == 1:
            return _fallback(
                request,
                config,
                failure_reason,
                first_failure_reason=first_failure_reason,
                final_fallback_reason=failure_reason,
            )
    return _fallback(
        request,
        config,
        "validation_failed",
        first_failure_reason=first_failure_reason,
        final_fallback_reason="validation_failed",
    )


def _call_openai_compatible(
    request: dict[str, Any],
    config: QueryAnalysisConfig,
    api_key: str,
    *,
    timeout_seconds: float,
    retry_after_validation_failure: bool,
) -> Any:
    body = {
        "model": config.model,
        "temperature": 0,
        "max_tokens": _provider_max_tokens(config.max_tokens),
        "response_format": {"type": config.response_format},
        "messages": [
            {"role": "system", "content": _system_prompt(config.prompt_version)},
            {
                "role": "user",
                "content": json.dumps(
                    _provider_user_payload(
                        request,
                        retry_after_validation_failure=retry_after_validation_failure,
                    ),
                    ensure_ascii=False,
                    separators=(",", ":"),
                ),
            },
        ],
    }
    http_request = urllib.request.Request(
        config.base_url.rstrip("/") + "/chat/completions",
        data=json.dumps(body, ensure_ascii=False).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    with urllib.request.urlopen(http_request, timeout=timeout_seconds) as response:
        raw = response.read()
    payload = json.loads(raw.decode("utf-8"))
    content = _extract_content(payload)
    return json.loads(content)


def _system_prompt(prompt_version: str) -> str:
    return (
        f"You are EmoAgent SemanticQueryAnalyzer ({prompt_version}). "
        "Do not answer the user. Return strict JSON object only. No markdown. "
        "No code fences. Return only the provider-minimal JSON schema object. "
        "Do not wrap it. "
        "Use only allowed enum values from allowed_enums. Use only query_text, "
        "input_language, now, timezone, rule_analysis, visible_entity_hints, "
        "conversation_window, retrieval_policy, allowed_enums, and output_contract. "
        "Do not use hidden, purged, sensitive-disallowed, provider, environment, "
        "or API-key data. Query rewrites are for memory retrieval, not general "
        "encyclopedia search. Query rewrites must stay in the same language/script "
        "as the input query. Do not translate Chinese queries into English. "
        "Proper nouns may be kept as-is, e.g. Laufey. If input query is Chinese, "
        "every non-empty query_rewrite should contain Chinese characters unless it "
        "is a short entity anchor. "
        "For temporal transition questions, use evidence_need=state_transition "
        "only when the query has explicit old/new contrast or transition wording, "
        "such as 一开始...后来, 以前/之前...现在/后来/已经, "
        "之前闹矛盾...后来和好/和解/翻篇, 发生变化, 变成, 不再, or 从 X 到 Y. "
        "Bare historical lookup phrases such as 以前, 曾经, 从前, 过去, or before "
        "without old/new contrast are direct historical facts: prefer "
        "memory_ability=direct_fact and evidence_need=exact_observation, and use "
        "past_event_direct_fact only when the query asks about a specific past "
        "event/slot. "
        "For relationship development, 心路历程, 相处之后, or companionship changes, "
        "prefer memory_ability=relationship_arc or dynamic_state, prefer "
        "evidence_need=relationship_timeline or state_transition, and include "
        "relationship_arc signal when appropriate. "
        "For questionable premises, set memory_ability=premise_check and "
        "evidence_need=premise_counterexample; generate at least one "
        "counterexample-oriented rewrite, not only a restatement of the premise. "
        "Premise or counterexample questions still use premise_check when they "
        "contain past-event wording such as 上次, for example asking whether an "
        "old negative premise is still true. "
        "Ordinary yes/no questions such as 我是不是喜欢咖啡 are direct facts unless "
        "they include all-or-nothing, negative premise, exception, or "
        "counterexample wording. Bare 一直 in direct lookups such as "
        "我一直喜欢的饮料是什么 is not enough for premise_check. "
        "Conditional risk questions such as 如果 episode 被 redacted，是否还能暴露原文内容 "
        "still use premise_check and premise_counterexample because they ask "
        "whether a premise or safety guarantee can be violated. "
        "Universal structures like 所有/每个/任何/什么都/必须/从头到尾/"
        "一直都/一直没有/一直不/从来/总是/每次/永远 should trigger "
        "counterexample retrieval. For provenance questions, set "
        "memory_ability=provenance and evidence_need=provenance_source. "
        "Questions asking for the event occasion or stated reason for a celebration, "
        "treat, gathering, or ceremony are direct event-slot recall: prefer "
        "memory_ability=direct_fact and evidence_need=exact_observation instead "
        "of causal_explain unless the query truly asks why an effect happened. "
        "For causal why questions, set memory_ability=causal_explain and include "
        "a causal signal. Required provider fields are intent, confidence, rewrite, "
        "and language. Optional arrays/objects such as anchors, semantic_anchors, "
        "entity_mentions, signals, policy_hints, and context_block_hints may be "
        "omitted; the sidecar treats missing values as empty. "
        "counterexample_rewrite is optional and may be omitted when there is no "
        "counterexample query. Protocol enum fields such as "
        "time_mode, memory_domain, memory_ability, and evidence_need are optional "
        "overrides only; the sidecar fills safe protocol defaults for Go."
    )


def _provider_user_payload(
    request: dict[str, Any],
    *,
    retry_after_validation_failure: bool,
) -> dict[str, Any]:
    query_text = str(request.get("query_text", ""))
    return {
        "query_text": query_text,
        "input_language": _optional_string(request.get("input_language"))
        or _input_language(query_text),
        "now": _optional_string(request.get("now")),
        "timezone": _optional_string(request.get("timezone")),
        "rule_analysis": _optional_object(request.get("rule_analysis")),
        "allowed_enums": _optional_object(request.get("allowed_enums")),
        "visible_entity_hints": _optional_array(request.get("visible_entity_hints")),
        "retrieval_policy": _optional_object(request.get("retrieval_policy")),
        "conversation_window": _optional_array(request.get("conversation_window")),
        "include_rationale": bool(request.get("include_rationale", False)),
        "retry_schema_validation": retry_after_validation_failure,
        "output_contract": {
            "return_only": "provider_minimal_analysis_object",
            "required_fields": [
                "intent",
                "confidence",
                "rewrite",
                "language",
            ],
            "optional_fields": [
                "counterexample_rewrite",
                "anchors",
                "semantic_anchors",
                "query_rewrites",
                "signals",
                "entity_mentions",
                "context_block_hints",
                "time_mode",
                "memory_domain",
                "memory_ability",
                "evidence_need",
                "policy_hints",
                "rationale_summary",
            ],
            "sidecar_completes_protocol_fields": True,
            "rewrite_language": "same_as_query",
            "max_anchors": 4,
        },
    }


def _extract_content(payload: Any) -> str:
    if not isinstance(payload, dict):
        raise ValueError("provider response must be a JSON object")
    choices = payload.get("choices")
    if not isinstance(choices, list) or not choices:
        raise ValueError("provider response must include choices")
    first = choices[0]
    if not isinstance(first, dict):
        raise ValueError("provider choice must be an object")
    message = first.get("message")
    if not isinstance(message, dict):
        raise ValueError("provider choice must include message")
    content = message.get("content")
    if not isinstance(content, str) or not content.strip():
        raise ValueError("provider message content must be a JSON string")
    return content


def _is_provider_timeout_error(exc: BaseException) -> bool:
    if isinstance(exc, TimeoutError):
        return True
    reason = getattr(exc, "reason", None)
    if isinstance(reason, TimeoutError):
        return True
    text = str(reason if reason is not None else exc).lower()
    return "timed out" in text or "timeout" in text


def _effective_provider_budget_ms(
    request: dict[str, Any],
    config: QueryAnalysisConfig,
    started_monotonic: float,
) -> int:
    budgets = [max(0, int(config.timeout_seconds * 1000))]
    provider_timeout_ms = _request_budget_ms(request.get("provider_timeout_ms"))
    if provider_timeout_ms is not None:
        budgets.append(provider_timeout_ms)
    deadline_ms = _request_budget_ms(request.get("deadline_ms"))
    if deadline_ms is not None:
        elapsed_ms = int(max(0.0, time.monotonic() - started_monotonic) * 1000)
        budgets.append(max(0, deadline_ms - elapsed_ms))
    return min(budgets)


def _request_budget_ms(value: Any) -> int | None:
    if isinstance(value, bool) or not isinstance(value, int):
        return None
    if value <= 0:
        return 0
    return value


def _provider_max_tokens(value: Any) -> int:
    try:
        tokens = int(value)
    except (TypeError, ValueError):
        tokens = _MAX_PROVIDER_TOKENS
    if tokens <= 0:
        tokens = _MAX_PROVIDER_TOKENS
    return min(tokens, _MAX_PROVIDER_TOKENS)


def _validate_provider_analysis(
    payload: Any,
    request: dict[str, Any],
    config: QueryAnalysisConfig,
    *,
    include_rationale: bool,
) -> dict[str, Any]:
    if not isinstance(payload, dict):
        raise ValueError("analysis must be a JSON object")
    _validate_provider_shape(payload)
    parsed_enums = _analysis_enums(payload, request)
    _validate_allowed_enums(parsed_enums, request.get("allowed_enums"))
    query_rewrites = _provider_query_rewrites(payload)
    semantic_anchors = _provider_semantic_anchors(payload)

    result = _base_result(config, degraded=False, fallback_reason=None)
    result.update(
        {
            "time_mode": parsed_enums["time_mode"],
            "memory_domain": parsed_enums["memory_domain"],
            "memory_ability": parsed_enums["memory_ability"],
            "evidence_need": parsed_enums["evidence_need"],
            "signals": _provider_signals(payload)[:12],
            "confidence": round(
                clamp_float(payload.get("confidence"), 0.0, 1.0, 0.5), 6
            ),
            "field_confidence": _field_confidence(payload.get("field_confidence", {})),
            "entity_mentions": _entity_mention_list(
                payload.get("entity_mentions", [])
            )[:12],
            "query_rewrites": query_rewrites[:5],
            "semantic_anchors": semantic_anchors[:8],
            "context_block_hints": _string_list(
                payload.get("context_block_hints", []), "context_block_hints"
            )[:8],
            "policy_hints": _policy_hints(payload.get("policy_hints", {})),
        }
    )
    if include_rationale and isinstance(payload.get("rationale_summary"), str):
        result["rationale_summary"] = payload["rationale_summary"].strip()[:240]
    return result


def _validate_provider_shape(payload: dict[str, Any]) -> None:
    if all(field in payload for field in _LEGACY_PROVIDER_FIELDS):
        return
    missing = [field for field in _MINIMAL_PROVIDER_FIELDS if field not in payload]
    if missing:
        raise ValueError(f"analysis missing {missing[0]}")


def _analysis_enums(
    payload: dict[str, Any],
    request: dict[str, Any],
) -> dict[str, str]:
    return {
        field: _analysis_enum_value(payload, request, field)
        for field in _ENUM_ANALYSIS_FIELDS
    }


def _analysis_enum_value(
    payload: dict[str, Any],
    request: dict[str, Any],
    field: str,
) -> str:
    if field in payload:
        return _string(payload[field], field)
    rule_analysis = request.get("rule_analysis")
    allowed = _allowed_enum_values(request.get("allowed_enums"), field)
    if isinstance(rule_analysis, dict):
        rule_value = _optional_string(rule_analysis.get(field))[:64]
        if rule_value and (not allowed or rule_value in allowed):
            return rule_value
    default_value = _DEFAULT_ENUMS[field]
    if not allowed or default_value in allowed:
        return default_value
    return sorted(allowed)[0]


def _allowed_enum_values(allowed_enums: Any, field: str) -> set[str]:
    if not isinstance(allowed_enums, dict):
        return set()
    for enum_key in _ALLOWED_ENUM_KEYS.get(field, (field,)):
        allowed = allowed_enums.get(enum_key)
        if isinstance(allowed, list):
            return {item for item in allowed if isinstance(item, str) and item}
    return set()


def _provider_signals(payload: dict[str, Any]) -> list[str]:
    signals = _string_list(payload.get("signals", []), "signals")
    intent = _optional_string(payload.get("intent")).casefold()
    intent_signal = {
        "causal": "causal",
        "causal_explain": "causal",
        "historical": "historical",
        "premise_check": "premise_check",
        "provenance": "provenance",
        "relationship_arc": "relationship_arc",
    }.get(intent)
    if intent_signal:
        signals.append(intent_signal)
    if _optional_string(payload.get("counterexample_rewrite")):
        signals.append("premise_check")
    seen: set[str] = set()
    out: list[str] = []
    for signal in signals:
        if signal in seen:
            continue
        seen.add(signal)
        out.append(signal)
    return out


def _provider_query_rewrites(payload: dict[str, Any]) -> list[dict[str, Any]]:
    values: list[Any] = []
    if "query_rewrites" in payload:
        values.extend(_optional_array(payload.get("query_rewrites")))
    rewrite = _optional_string(payload.get("rewrite"))
    if rewrite:
        values.append({"text": rewrite, "weight": 0.7, "purpose": "semantic_recall"})
    counterexample_rewrite = _optional_string(payload.get("counterexample_rewrite"))
    if counterexample_rewrite:
        values.append(
            {
                "text": counterexample_rewrite,
                "weight": 0.7,
                "purpose": "counterexample",
            }
        )
    return _query_rewrites(values)


def _provider_semantic_anchors(payload: dict[str, Any]) -> list[dict[str, Any]]:
    if "semantic_anchors" in payload:
        return _semantic_anchors(payload.get("semantic_anchors", []))
    return _semantic_anchors(payload.get("anchors", []))


def _fallback(
    request: dict[str, Any],
    config: QueryAnalysisConfig,
    reason: str,
    *,
    first_failure_reason: str | None = None,
    final_fallback_reason: str | None = None,
) -> dict[str, Any]:
    query_text = str(request.get("query_text", ""))
    signals = []
    if re.search(r"\b(forget|delete|remove|erase)\b|删除|忘记", query_text, re.I):
        signals.append("forget_delete")
    rewrites = [
        {
            "text": query_text,
            "weight": 0.7 if signals else 0.5,
            "purpose": "operation_target" if "forget_delete" in signals else "semantic_recall",
        }
    ]
    result = _base_result(config, degraded=True, fallback_reason=reason[:64])
    result.update(
        {
            "time_mode": "unspecified",
            "memory_domain": "general",
            "memory_ability": "recall",
            "evidence_need": "medium",
            "signals": signals,
            "confidence": 0.2,
            "field_confidence": {
                "time_mode": 0.1,
                "memory_domain": 0.1,
                "memory_ability": 0.1,
                "evidence_need": 0.1,
            },
            "entity_mentions": [],
            "query_rewrites": rewrites,
            "semantic_anchors": [],
            "context_block_hints": [],
            "policy_hints": {},
        }
    )
    diagnostics: dict[str, str] = {}
    if first_failure_reason:
        diagnostics["first_failure_reason"] = first_failure_reason[:64]
    if final_fallback_reason:
        diagnostics["final_fallback_reason"] = final_fallback_reason[:64]
    if diagnostics:
        result["diagnostics"] = diagnostics
    return result


def _base_result(
    config: QueryAnalysisConfig,
    *,
    degraded: bool,
    fallback_reason: str | None,
) -> dict[str, Any]:
    result = {
        "status": "degraded" if degraded else "ok",
        "degraded": degraded,
        "provider": config.provider,
        "model": config.model,
        "prompt_version": config.prompt_version,
    }
    if fallback_reason:
        result["fallback_reason"] = fallback_reason[:64]
    return result


def _string(value: Any, field_name: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{field_name} must be a non-empty string")
    return value.strip()[:64]


def _string_list(value: Any, field_name: str) -> list[str]:
    if not isinstance(value, list):
        raise ValueError(f"{field_name} must be an array")
    out: list[str] = []
    for item in value:
        if isinstance(item, str) and item.strip():
            out.append(item.strip()[:80])
    return out


def _field_confidence(value: Any) -> dict[str, float]:
    if not isinstance(value, dict):
        raise ValueError("field_confidence must be an object")
    out = {}
    for key, score in value.items():
        if isinstance(key, str) and key.strip():
            out[key.strip()[:64]] = round(clamp_float(score, 0.0, 1.0, 0.0), 6)
    return out


def _validate_allowed_enums(values: dict[str, str], allowed_enums: Any) -> None:
    if not isinstance(allowed_enums, dict):
        return
    for field, value in values.items():
        allowed = None
        for enum_key in _ALLOWED_ENUM_KEYS.get(field, (field,)):
            if enum_key in allowed_enums:
                allowed = allowed_enums[enum_key]
                break
        if allowed is None:
            continue
        if not isinstance(allowed, list):
            raise ValueError("allowed enum list must be an array")
        allowed_values = {item for item in allowed if isinstance(item, str) and item}
        if allowed_values and value not in allowed_values:
            raise ValueError("analysis enum value is not allowed")


def _entity_mention_list(value: Any) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        raise ValueError("entity_mentions must be an array")
    out: list[dict[str, Any]] = []
    for item in value:
        if isinstance(item, str):
            text = item.strip()[:80]
            if text:
                out.append({"match_text": text, "confidence": 0.5})
            continue
        if not isinstance(item, dict):
            continue
        entity_id = _optional_string(item.get("entity_id"))[:96]
        canonical_name = _optional_string(item.get("canonical_name"))[:120]
        alias = _optional_string(item.get("alias"))[:120]
        match_text = _optional_string(item.get("match_text"))[:120]
        match_kind = _optional_string(item.get("match_kind"))[:32]
        if match_kind not in {"canonical", "alias"}:
            match_kind = "alias" if alias else "canonical"
        if not any((entity_id, canonical_name, alias, match_text)):
            continue
        out.append(
            {
                "entity_id": entity_id,
                "canonical_name": canonical_name,
                "alias": alias,
                "match_text": match_text,
                "match_kind": match_kind,
                "confidence": round(
                    clamp_float(item.get("confidence"), 0.0, 1.0, 0.5), 6
                ),
            }
        )
    return out


def _policy_hints(value: Any) -> dict[str, Any]:
    bool_fields = {
        "prefer_evidenced_by_links",
        "prefer_supersedes_links",
        "prefer_causal_links",
        "prefer_counterexamples",
        "prefer_narratives",
    }
    out: dict[str, Any] = {}
    if isinstance(value, list):
        for item in value:
            if isinstance(item, str) and item.strip() in bool_fields:
                out[item.strip()] = True
        return out
    if not isinstance(value, dict):
        raise ValueError("policy_hints must be an object")
    for field in bool_fields:
        if isinstance(value.get(field), bool):
            out[field] = value[field]
    max_hops = value.get("max_hops_hint")
    if isinstance(max_hops, int) and not isinstance(max_hops, bool) and max_hops > 0:
        out["max_hops_hint"] = min(max_hops, 8)
    return out


def _optional_string(value: Any) -> str:
    if isinstance(value, str):
        return value.strip()
    return ""


def _optional_object(value: Any) -> dict[str, Any]:
    if isinstance(value, dict):
        return value
    return {}


def _optional_array(value: Any) -> list[Any]:
    if isinstance(value, list):
        return value
    return []


def _input_language(query_text: str) -> str:
    cjk = sum(1 for ch in query_text if "\u4e00" <= ch <= "\u9fff")
    letters = sum(1 for ch in query_text if ch.isalpha())
    return "zh-Hans" if cjk > 0 and cjk >= max(1, letters // 3) else "unknown"


def _query_rewrites(value: Any) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        raise ValueError("query_rewrites must be an array")
    out: list[dict[str, Any]] = []
    for item in value:
        if isinstance(item, str):
            text = item
            weight = 0.5
            purpose = "semantic_recall"
        elif isinstance(item, dict):
            text = item.get("text", "")
            weight = item.get("weight", 0.5)
            purpose = item.get("purpose", "semantic_recall")
        else:
            continue
        if isinstance(text, str) and text.strip():
            out.append(
                {
                    "text": text.strip()[:160],
                    "weight": round(clamp_float(weight, 0.1, 0.9, 0.5), 6),
                    "purpose": str(purpose or "semantic_recall").strip()[:64],
                }
            )
    return out


def _semantic_anchors(value: Any) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        raise ValueError("semantic_anchors must be an array")
    out: list[dict[str, Any]] = []
    for item in value:
        if isinstance(item, str):
            text = item
            weight = 0.5
            anchor_type = "semantic_anchor"
            entity_id = ""
            confidence = 0.5
        elif isinstance(item, dict):
            text = item.get("text", "")
            weight = item.get("weight", 0.5)
            anchor_type = item.get("anchor_type", item.get("purpose", "semantic_anchor"))
            entity_id = item.get("entity_id", "")
            confidence = item.get("confidence", 0.5)
        else:
            continue
        if isinstance(text, str) and text.strip():
            out.append(
                {
                    "text": text.strip()[:120],
                    "weight": round(clamp_float(weight, 0.1, 0.65, 0.5), 6),
                    "anchor_type": str(anchor_type or "semantic_anchor").strip()[:64],
                    "entity_id": str(entity_id or "").strip()[:96],
                    "confidence": round(clamp_float(confidence, 0.0, 1.0, 0.5), 6),
                }
            )
    return out


def _entity_mentions(text: str) -> list[str]:
    tokens = re.findall(r"[A-Za-z][0-9A-Za-z_-]{2,}|[\u4e00-\u9fff]{2,}", text)
    return tokens[:8]
