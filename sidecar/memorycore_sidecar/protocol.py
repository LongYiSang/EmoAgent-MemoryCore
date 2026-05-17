from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Any

REQUEST_SCHEMA_VERSION = "memory_mirror_operation.v0.1"
RESPONSE_SCHEMA_VERSION = "memory_mirror_operation_result.v0.1"
CLEAR_NAMESPACE_REQUEST_SCHEMA_VERSION = "memory_mirror_clear_namespace.v0.1"
CLEAR_NAMESPACE_RESPONSE_SCHEMA_VERSION = "memory_mirror_clear_namespace_result.v0.1"
CANDIDATE_REQUEST_SCHEMA_VERSION = "memory_mirror_candidate_request.v0.1"
CANDIDATE_RESPONSE_SCHEMA_VERSION = "memory_mirror_candidates.v0.1"
ACTIVATION_REQUEST_SCHEMA_VERSION = "memory_graph_activation_request.v0.1"
ACTIVATION_RESPONSE_SCHEMA_VERSION = "memory_graph_activation_result.v0.1"
RERANK_REQUEST_SCHEMA_VERSION = "memory_rerank_request.v0.1"
RERANK_RESPONSE_SCHEMA_VERSION = "memory_rerank_result.v0.1"
EVAL_CONFIG_REQUEST_SCHEMA_VERSION = "memory_eval_sidecar_config.v0.1"
EVAL_CONFIG_RESPONSE_SCHEMA_VERSION = "memory_eval_sidecar_config_result.v0.1"
VALID_EMBEDDING_CACHE_MODES = frozenset(
    {"off", "read_write", "read_only", "refresh"}
)

SUPPORTED_OPERATIONS = frozenset(
    {"upsert_node", "delete_node", "upsert_edge", "delete_edge"}
)

_PAYLOAD_KEYS = {
    "upsert_node": "node",
    "delete_node": "node",
    "upsert_edge": "edge",
    "delete_edge": "edge",
}

_REQUIRED_FIELDS = {
    "upsert_node": ("persona_id", "node_type", "sqlite_node_id", "searchable_text"),
    "delete_node": ("persona_id", "node_type", "sqlite_node_id"),
    "upsert_edge": (
        "persona_id",
        "sqlite_edge_id",
        "from_node_type",
        "from_node_id",
        "to_node_type",
        "to_node_id",
    ),
    "delete_edge": (
        "persona_id",
        "sqlite_edge_id",
        "link_type",
        "from_node_type",
        "from_node_id",
        "to_node_type",
        "to_node_id",
    ),
}


class ProtocolError(ValueError):
    """Raised when a sidecar protocol request is invalid."""


@dataclass(frozen=True)
class MirrorOperation:
    operation_id: str
    persona_id: str
    operation: str
    payload: dict[str, Any]


def parse_operation_request(data: Any) -> MirrorOperation:
    if not isinstance(data, dict):
        raise ProtocolError("request body must be a JSON object")
    schema_version = data.get("schema_version")
    if schema_version != REQUEST_SCHEMA_VERSION:
        raise ProtocolError(f"schema_version must be {REQUEST_SCHEMA_VERSION}")
    operation_id = data.get("operation_id")
    if not isinstance(operation_id, str) or not operation_id.strip():
        raise ProtocolError("operation_id is required")
    persona_id = data.get("persona_id")
    if not isinstance(persona_id, str) or not persona_id.strip():
        raise ProtocolError("persona_id is required")
    operation = data.get("operation")
    if operation not in SUPPORTED_OPERATIONS:
        raise ProtocolError(f"unsupported operation: {operation}")

    payload_key = _PAYLOAD_KEYS[operation]
    payload = data.get(payload_key)
    if not isinstance(payload, dict):
        raise ProtocolError(f"{payload_key} payload must be a JSON object")

    missing = [
        field
        for field in _REQUIRED_FIELDS[operation]
        if _is_blank_required_value(payload.get(field))
    ]
    if missing:
        raise ProtocolError(f"{payload_key} payload missing fields: {', '.join(missing)}")

    return MirrorOperation(
        operation_id=operation_id.strip(),
        persona_id=persona_id.strip(),
        operation=operation,
        payload=payload,
    )


def _is_blank_required_value(value: Any) -> bool:
    if isinstance(value, str):
        return not value.strip()
    return not value


def parse_clear_namespace_request(data: Any) -> str:
    if not isinstance(data, dict):
        raise ProtocolError("request body must be a JSON object")
    schema_version = data.get("schema_version")
    if schema_version != CLEAR_NAMESPACE_REQUEST_SCHEMA_VERSION:
        raise ProtocolError(
            f"schema_version must be {CLEAR_NAMESPACE_REQUEST_SCHEMA_VERSION}"
        )
    persona_id = data.get("persona_id")
    if not isinstance(persona_id, str) or not persona_id.strip():
        raise ProtocolError("persona_id is required")
    return persona_id.strip()


def parse_clear_namespace_payload(data: Any) -> dict[str, Any]:
    persona_id = parse_clear_namespace_request(data)
    purge_embedding_cache = False
    if isinstance(data, dict):
        purge_embedding_cache = bool(data.get("purge_embedding_cache", False))
    return {
        "persona_id": persona_id,
        "purge_embedding_cache": purge_embedding_cache,
    }


def parse_eval_config_request(data: Any) -> dict[str, Any]:
    if not isinstance(data, dict):
        raise ProtocolError("request body must be a JSON object")
    schema_version = data.get("schema_version")
    if schema_version != EVAL_CONFIG_REQUEST_SCHEMA_VERSION:
        raise ProtocolError(
            f"schema_version must be {EVAL_CONFIG_REQUEST_SCHEMA_VERSION}"
        )
    result: dict[str, Any] = {}
    for field in (
        "trivium_dir",
        "embedding_cache_mode",
        "embedding_cache_db_path",
        "searchable_text_version",
        "text_normalization_version",
    ):
        value = data.get(field)
        if value is None:
            continue
        if not isinstance(value, str):
            raise ProtocolError(f"{field} must be a string")
        value = value.strip()
        if value:
            if field == "embedding_cache_mode" and value not in VALID_EMBEDDING_CACHE_MODES:
                raise ProtocolError(
                    "embedding_cache_mode must be one of off, read_write, read_only, refresh"
                )
            result[field] = value
    return result


def parse_candidate_request(data: Any) -> dict[str, Any]:
    if not isinstance(data, dict):
        raise ProtocolError("request body must be a JSON object")
    schema_version = data.get("schema_version")
    if schema_version != CANDIDATE_REQUEST_SCHEMA_VERSION:
        raise ProtocolError(f"schema_version must be {CANDIDATE_REQUEST_SCHEMA_VERSION}")
    request_id = data.get("request_id")
    if not isinstance(request_id, str) or not request_id.strip():
        raise ProtocolError("request_id is required")
    persona_id = data.get("persona_id")
    if not isinstance(persona_id, str) or not persona_id.strip():
        raise ProtocolError("persona_id is required")
    query_text = data.get("query_text")
    if not isinstance(query_text, str) or not query_text.strip():
        raise ProtocolError("query_text is required")
    limit = data.get("limit", 8)
    if not isinstance(limit, int) or limit <= 0:
        raise ProtocolError("limit must be a positive integer")
    return {
        "request_id": request_id.strip(),
        "persona_id": persona_id.strip(),
        "query_text": query_text,
        "limit": limit,
    }


def parse_activation_request(data: Any) -> dict[str, Any]:
    if not isinstance(data, dict):
        raise ProtocolError("request body must be a JSON object")
    schema_version = data.get("schema_version")
    if schema_version != ACTIVATION_REQUEST_SCHEMA_VERSION:
        raise ProtocolError(f"schema_version must be {ACTIVATION_REQUEST_SCHEMA_VERSION}")
    request_id = data.get("request_id")
    if not isinstance(request_id, str) or not request_id.strip():
        raise ProtocolError("request_id is required")
    persona_id = data.get("persona_id")
    if not isinstance(persona_id, str) or not persona_id.strip():
        raise ProtocolError("persona_id is required")
    seeds = data.get("seeds")
    if not isinstance(seeds, list):
        raise ProtocolError("seeds must be a JSON array")
    parsed_seeds = [_parse_activation_seed(seed) for seed in seeds]
    params = _parse_activation_params(data.get("params", {}))
    return {
        "request_id": request_id.strip(),
        "persona_id": persona_id.strip(),
        "seeds": parsed_seeds,
        "params": params,
        "anchor_debug": data.get("anchor_debug", []),
    }


def parse_rerank_request(data: Any) -> dict[str, Any]:
    if not isinstance(data, dict):
        raise ProtocolError("request body must be a JSON object")
    schema_version = data.get("schema_version")
    if schema_version != RERANK_REQUEST_SCHEMA_VERSION:
        raise ProtocolError(f"schema_version must be {RERANK_REQUEST_SCHEMA_VERSION}")
    request_id = data.get("request_id")
    if not isinstance(request_id, str) or not request_id.strip():
        raise ProtocolError("request_id is required")
    persona_id = data.get("persona_id")
    if not isinstance(persona_id, str) or not persona_id.strip():
        raise ProtocolError("persona_id is required")
    query_text = data.get("query_text")
    if not isinstance(query_text, str) or not query_text.strip():
        raise ProtocolError("query_text is required")
    candidates = data.get("candidates")
    if not isinstance(candidates, list):
        raise ProtocolError("candidates must be a JSON array")
    return {
        "request_id": request_id.strip(),
        "persona_id": persona_id.strip(),
        "query_text": query_text,
        "candidates": [
            _parse_rerank_candidate(candidate, idx)
            for idx, candidate in enumerate(candidates)
        ],
    }


def _parse_rerank_candidate(candidate: Any, idx: int) -> dict[str, Any]:
    if not isinstance(candidate, dict):
        raise ProtocolError(f"candidate[{idx}] must be a JSON object")
    parsed: dict[str, Any] = {}
    for field in ("node_id", "node_type", "safe_summary"):
        value = candidate.get(field)
        if not isinstance(value, str) or not value.strip():
            raise ProtocolError(f"candidate[{idx}].{field} is required")
        parsed[field] = value.strip() if field != "safe_summary" else value
    for field in ("current_score", "anchor_energy", "graph_energy", "configured_score"):
        if field in candidate:
            parsed[field] = _candidate_score(candidate[field], f"candidate[{idx}].{field}")
    if "source_scores" in candidate:
        parsed["source_scores"] = _source_scores(
            candidate["source_scores"], f"candidate[{idx}].source_scores"
        )
    return parsed


def _source_scores(value: Any, field_name: str) -> dict[str, float]:
    if not isinstance(value, dict):
        raise ProtocolError(f"{field_name} must be a JSON object")
    parsed = {}
    for source, score in value.items():
        if not isinstance(source, str) or not source.strip():
            raise ProtocolError(f"{field_name} keys must be nonblank strings")
        parsed[source.strip()] = _candidate_score(score, f"{field_name}.{source}")
    return parsed


def _candidate_score(value: Any, field_name: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ProtocolError(f"{field_name} must be a finite nonnegative number")
    score = float(value)
    if not math.isfinite(score) or score < 0:
        raise ProtocolError(f"{field_name} must be a finite nonnegative number")
    return score


def _parse_activation_seed(seed: Any) -> dict[str, Any]:
    if not isinstance(seed, dict):
        raise ProtocolError("activation seed must be a JSON object")
    trivium_node_id = seed.get("trivium_node_id")
    if not isinstance(trivium_node_id, int) or trivium_node_id <= 0:
        raise ProtocolError("activation seed trivium_node_id must be a positive integer")
    sqlite_node_id = seed.get("sqlite_node_id")
    if not isinstance(sqlite_node_id, str) or not sqlite_node_id.strip():
        raise ProtocolError("activation seed sqlite_node_id is required")
    node_type = seed.get("node_type")
    if not isinstance(node_type, str) or not node_type.strip():
        raise ProtocolError("activation seed node_type is required")
    seed_energy = seed.get("seed_energy")
    if (
        not isinstance(seed_energy, (int, float))
        or not math.isfinite(float(seed_energy))
        or seed_energy <= 0
    ):
        raise ProtocolError("activation seed seed_energy must be positive")
    return {
        "trivium_node_id": trivium_node_id,
        "sqlite_node_id": sqlite_node_id.strip(),
        "node_type": node_type.strip(),
        "seed_energy": float(seed_energy),
    }


def _parse_activation_params(params: Any) -> dict[str, Any]:
    if params is None:
        params = {}
    if not isinstance(params, dict):
        raise ProtocolError("params must be a JSON object")
    return {
        "max_hops": _positive_int(params.get("max_hops"), 2),
        "hop_decay": _positive_float(params.get("hop_decay"), 0.70),
        "min_energy": _positive_float(params.get("min_energy"), 0.01),
        "max_active_nodes": _positive_int(params.get("max_active_nodes"), 80),
        "hub_suppression_power": _non_negative_float(
            params.get("hub_suppression_power"), 0.50
        ),
        "include_paths": bool(params.get("include_paths", True)),
        "include_provenance_edges": bool(params.get("include_provenance_edges", False)),
        "max_edges_scanned_per_request": _positive_int(
            params.get("max_edges_scanned_per_request"), 10000
        ),
        "max_neighbors_per_node": _positive_int(
            params.get("max_neighbors_per_node"), 100
        ),
        "max_activation_wall_ms": _positive_float(
            params.get("max_activation_wall_ms"), 120.0
        ),
    }


def _positive_int(value: Any, default: int) -> int:
    if not isinstance(value, int) or value <= 0:
        return default
    return value


def _positive_float(value: Any, default: float) -> float:
    if (
        not isinstance(value, (int, float))
        or not math.isfinite(float(value))
        or value <= 0
    ):
        return default
    return float(value)


def _non_negative_float(value: Any, default: float) -> float:
    if (
        not isinstance(value, (int, float))
        or not math.isfinite(float(value))
        or value < 0
    ):
        return default
    return float(value)


def build_result(operation_id: str, **fields: Any) -> dict[str, Any]:
    result = {
        "schema_version": RESPONSE_SCHEMA_VERSION,
        "operation_id": operation_id,
        "status": "ok",
    }
    result.update(fields)
    return result


def build_error(message: str) -> dict[str, Any]:
    return {
        "schema_version": RESPONSE_SCHEMA_VERSION,
        "status": "error",
        "error": message,
    }


def build_clear_namespace_result(**fields: Any) -> dict[str, Any]:
    result = {
        "schema_version": CLEAR_NAMESPACE_RESPONSE_SCHEMA_VERSION,
        "status": "ok",
    }
    result.update(fields)
    return result


def build_candidates_result(
    request_id: str,
    candidates: list[dict[str, Any]] | None = None,
    degraded: bool = False,
    fallback_reason: str | None = None,
    embedding_cache_stats: dict[str, int] | None = None,
) -> dict[str, Any]:
    result = {
        "schema_version": CANDIDATE_RESPONSE_SCHEMA_VERSION,
        "request_id": request_id,
        "candidates": candidates or [],
        "degraded": degraded,
    }
    if fallback_reason:
        result["fallback_reason"] = fallback_reason
    if embedding_cache_stats is not None:
        result["embedding_cache_stats"] = embedding_cache_stats
    return result


def build_eval_config_result(**fields: Any) -> dict[str, Any]:
    result = {
        "schema_version": EVAL_CONFIG_RESPONSE_SCHEMA_VERSION,
        "status": "ok",
    }
    result.update(fields)
    return result


def build_activation_result(
    request_id: str,
    candidates: list[dict[str, Any]] | None = None,
    degraded: bool = False,
    fallback_reason: str | None = None,
) -> dict[str, Any]:
    result = {
        "schema_version": ACTIVATION_RESPONSE_SCHEMA_VERSION,
        "request_id": request_id,
        "candidates": candidates or [],
        "degraded": degraded,
    }
    if fallback_reason:
        result["fallback_reason"] = fallback_reason
    return result


def build_rerank_result(
    request_id: str,
    results: list[dict[str, Any]] | None = None,
    degraded: bool = False,
    fallback_reason: str | None = None,
) -> dict[str, Any]:
    result = {
        "schema_version": RERANK_RESPONSE_SCHEMA_VERSION,
        "request_id": request_id,
        "results": results or [],
        "degraded": degraded,
    }
    if fallback_reason:
        result["fallback_reason"] = fallback_reason
    return result
