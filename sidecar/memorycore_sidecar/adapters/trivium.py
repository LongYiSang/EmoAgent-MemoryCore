from __future__ import annotations

from typing import Any

from .base import MirrorAdapter


class TriviumAdapter(MirrorAdapter):
    def __init__(self) -> None:
        try:
            import triviumdb  # noqa: F401
        except ImportError as exc:
            raise RuntimeError("TriviumAdapter requires the triviumdb package") from exc

    def upsert_node(self, node: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError("TriviumDB node upsert is deferred to Phase 4C")

    def delete_node(self, node: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError("TriviumDB node delete is deferred to Phase 4C")

    def upsert_edge(self, edge: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError("TriviumDB edge upsert is deferred to Phase 4C")

    def delete_edge(self, edge: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError("TriviumDB edge delete is deferred to Phase 4C")

    def clear_namespace(self, persona_id: str) -> dict[str, Any]:
        raise NotImplementedError("TriviumDB namespace clear is deferred to Phase 4C")

    def find_candidates(self, request: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError("TriviumDB candidate retrieval is deferred to Phase 4D")
