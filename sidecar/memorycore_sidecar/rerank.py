from __future__ import annotations

import json
import math
import os
import urllib.error
import urllib.request
from typing import Any, Mapping, Protocol

from .config import RerankConfig


class RerankProvider(Protocol):
    def rerank(self, query_text: str, candidates: list[dict[str, Any]]) -> dict[str, Any]:
        raise NotImplementedError


class DisabledRerankProvider:
    def rerank(self, query_text: str, candidates: list[dict[str, Any]]) -> dict[str, Any]:
        return _fallback("rerank_not_configured")


class DashScopeVLRerankProvider:
    def __init__(
        self,
        config: RerankConfig,
        env: Mapping[str, str] | None = None,
    ) -> None:
        self._config = config
        self._env = os.environ if env is None else env

    def rerank(self, query_text: str, candidates: list[dict[str, Any]]) -> dict[str, Any]:
        api_key = self._env.get(self._config.api_key_env)
        if not api_key:
            return _fallback("missing_api_key")

        request = urllib.request.Request(
            self._config.endpoint_url,
            data=json.dumps(
                {
                    "model": self._config.model,
                    "input": {
                        "query": {"text": query_text},
                        "documents": [
                            {"text": candidate.get("safe_summary", "")}
                            for candidate in candidates
                        ],
                    },
                    "parameters": {
                        "top_n": min(self._config.top_n, len(candidates)),
                        "return_documents": False,
                        "instruct": self._config.instruct,
                    },
                }
            ).encode("utf-8"),
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
            method="POST",
        )

        try:
            with urllib.request.urlopen(
                request, timeout=self._config.timeout_seconds
            ) as response:
                body = response.read()
        except urllib.error.HTTPError:
            return _fallback("http_error")
        except urllib.error.URLError:
            return _fallback("url_error")

        try:
            payload = json.loads(body.decode("utf-8"))
            results = _parse_results(payload, candidates)
        except (UnicodeDecodeError, json.JSONDecodeError, ValueError):
            return _fallback("malformed_response")

        return {"results": results, "degraded": False}


def build_rerank_provider(
    config: RerankConfig, env: Mapping[str, str] | None = None
) -> RerankProvider:
    if config.provider == "none":
        return DisabledRerankProvider()
    if config.provider == "dashscope-vl":
        return DashScopeVLRerankProvider(config, env=env)
    raise ValueError(f"unsupported rerank provider: {config.provider}")


def _parse_results(
    payload: Any, candidates: list[dict[str, Any]]
) -> list[dict[str, Any]]:
    if not isinstance(payload, dict):
        raise ValueError("rerank response must be a JSON object")
    output = payload.get("output")
    if not isinstance(output, dict):
        raise ValueError("rerank response must contain output.results")
    provider_results = output.get("results")
    if not isinstance(provider_results, list):
        raise ValueError("rerank response must contain output.results")

    results: list[dict[str, Any]] = []
    for result in provider_results:
        if not isinstance(result, dict):
            raise ValueError("rerank result must be a JSON object")
        index = result.get("index")
        if isinstance(index, bool) or not isinstance(index, int):
            raise ValueError("rerank result index must be an integer")
        if index < 0 or index >= len(candidates):
            raise ValueError("rerank result index is out of range")
        score = _score(result.get("relevance_score"))
        candidate = candidates[index]
        node_id = candidate.get("node_id")
        node_type = candidate.get("node_type")
        if not isinstance(node_id, str) or not node_id.strip():
            raise ValueError("rerank candidate node_id is required")
        if not isinstance(node_type, str) or not node_type.strip():
            raise ValueError("rerank candidate node_type is required")
        results.append(
            {
                "node_id": node_id,
                "node_type": node_type,
                "rerank_score": score,
                "debug_reason": f"dashscope_qwen3_vl_rerank index={index}",
            }
        )
    return results


def _score(value: Any) -> float:
    if isinstance(value, bool):
        raise ValueError("relevance_score must be finite")
    try:
        score = float(value)
    except (TypeError, ValueError):
        raise ValueError("relevance_score must be finite") from None
    if not math.isfinite(score):
        raise ValueError("relevance_score must be finite")
    return min(max(score, 0.0), 1.0)


def _fallback(reason: str) -> dict[str, Any]:
    return {"results": [], "degraded": True, "fallback_reason": reason}
