from __future__ import annotations

import json
import os
import re
import urllib.error
import urllib.request
from typing import Any, Mapping

from .config import QueryAnalysisConfig
from .candidates import clamp_float

_REQUIRED_ANALYSIS_FIELDS = (
    "time_mode",
    "memory_domain",
    "memory_ability",
    "evidence_need",
    "signals",
    "confidence",
    "field_confidence",
    "entity_mentions",
    "query_rewrites",
    "semantic_anchors",
    "context_block_hints",
    "policy_hints",
)


def analyze_query(
    request: dict[str, Any],
    config: QueryAnalysisConfig,
    env: Mapping[str, str] | None = None,
) -> dict[str, Any]:
    env_values = os.environ if env is None else env
    if config.provider == "none":
        return _fallback(request, config, "provider_none")
    api_key = env_values.get(config.api_key_env, "")
    if not api_key.strip():
        return _fallback(request, config, "missing_api_key")

    for attempt in range(2):
        try:
            payload = _call_openai_compatible(
                request,
                config,
                api_key,
                retry_after_validation_failure=attempt > 0,
            )
        except json.JSONDecodeError:
            return _fallback(request, config, "invalid_json")
        except (UnicodeDecodeError, ValueError):
            return _fallback(request, config, "invalid_response")
        except (urllib.error.HTTPError, urllib.error.URLError, TimeoutError, OSError) as exc:
            if _is_provider_timeout_error(exc):
                return _fallback(request, config, "provider_timeout")
            return _fallback(request, config, "provider_error")

        try:
            return _validate_provider_analysis(
                payload,
                request,
                config,
                include_rationale=bool(request.get("include_rationale", False)),
            )
        except ValueError:
            if attempt == 1:
                return _fallback(request, config, "validation_failed")
    return _fallback(request, config, "validation_failed")


def _call_openai_compatible(
    request: dict[str, Any],
    config: QueryAnalysisConfig,
    api_key: str,
    *,
    retry_after_validation_failure: bool,
) -> Any:
    body = {
        "model": config.model,
        "temperature": 0,
        "max_tokens": config.max_tokens,
        "response_format": {"type": config.response_format},
        "messages": [
            {"role": "system", "content": _system_prompt(config.prompt_version)},
            {
                "role": "user",
                "content": json.dumps(
                    {
                        "query_text": request["query_text"],
                        "include_rationale": bool(request.get("include_rationale", False)),
                        "retry_schema_validation": retry_after_validation_failure,
                    },
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
    with urllib.request.urlopen(
        http_request, timeout=config.timeout_seconds
    ) as response:
        raw = response.read()
    payload = json.loads(raw.decode("utf-8"))
    content = _extract_content(payload)
    return json.loads(content)


def _system_prompt(prompt_version: str) -> str:
    return (
        f"Memory query analysis {prompt_version}. Return compact JSON only with: "
        "time_mode,memory_domain,memory_ability,evidence_need,signals,confidence,"
        "field_confidence,entity_mentions as objects with entity_id/canonical_name/"
        "alias/match_text/match_kind/confidence,query_rewrites,semantic_anchors "
        "as objects with text/anchor_type/entity_id/weight/confidence,"
        "context_block_hints,policy_hints as an object,and optional rationale_summary."
    )


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


def _validate_provider_analysis(
    payload: Any,
    request: dict[str, Any],
    config: QueryAnalysisConfig,
    *,
    include_rationale: bool,
) -> dict[str, Any]:
    if not isinstance(payload, dict):
        raise ValueError("analysis must be a JSON object")
    for field in _REQUIRED_ANALYSIS_FIELDS:
        if field not in payload:
            raise ValueError(f"analysis missing {field}")

    result = _base_result(config, degraded=False, fallback_reason=None)
    result.update(
        {
            "time_mode": _string(payload["time_mode"], "time_mode"),
            "memory_domain": _string(payload["memory_domain"], "memory_domain"),
            "memory_ability": _string(payload["memory_ability"], "memory_ability"),
            "evidence_need": _string(payload["evidence_need"], "evidence_need"),
            "signals": _string_list(payload["signals"], "signals")[:12],
            "confidence": round(
                clamp_float(payload["confidence"], 0.0, 1.0, 0.0), 6
            ),
            "field_confidence": _field_confidence(payload["field_confidence"]),
            "entity_mentions": _entity_mention_list(payload["entity_mentions"])[:12],
            "query_rewrites": _query_rewrites(payload["query_rewrites"])[:5],
            "semantic_anchors": _semantic_anchors(payload["semantic_anchors"])[:8],
            "context_block_hints": _string_list(
                payload["context_block_hints"], "context_block_hints"
            )[:8],
            "policy_hints": _policy_hints(payload["policy_hints"]),
        }
    )
    if include_rationale and isinstance(payload.get("rationale_summary"), str):
        result["rationale_summary"] = payload["rationale_summary"].strip()[:240]
    return result


def _fallback(
    request: dict[str, Any],
    config: QueryAnalysisConfig,
    reason: str,
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
