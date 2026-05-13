from __future__ import annotations

from dataclasses import dataclass
from typing import Any

REQUEST_SCHEMA_VERSION = "memory_mirror_operation.v0.1"
RESPONSE_SCHEMA_VERSION = "memory_mirror_operation_result.v0.1"
CLEAR_NAMESPACE_REQUEST_SCHEMA_VERSION = "memory_mirror_clear_namespace.v0.1"
CLEAR_NAMESPACE_RESPONSE_SCHEMA_VERSION = "memory_mirror_clear_namespace_result.v0.1"
CANDIDATE_REQUEST_SCHEMA_VERSION = "memory_mirror_candidate_request.v0.1"
CANDIDATE_RESPONSE_SCHEMA_VERSION = "memory_mirror_candidates.v0.1"

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
    "delete_edge": ("persona_id", "sqlite_edge_id"),
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
) -> dict[str, Any]:
    result = {
        "schema_version": CANDIDATE_RESPONSE_SCHEMA_VERSION,
        "request_id": request_id,
        "candidates": candidates or [],
        "degraded": degraded,
    }
    if fallback_reason:
        result["fallback_reason"] = fallback_reason
    return result
