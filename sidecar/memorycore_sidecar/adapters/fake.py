from __future__ import annotations

import hashlib
import re
from typing import Any

from memorycore_sidecar.activation import ActivationEdge, activate_graph

from .base import MirrorAdapter


class FakeMirrorAdapter(MirrorAdapter):
    def __init__(self) -> None:
        self._nodes: dict[tuple[str, str, str], dict[str, Any]] = {}
        self._edges: dict[tuple[str, str, str, str, str, str, str], dict[str, Any]] = {}

    def upsert_node(self, node: dict[str, Any]) -> dict[str, Any]:
        key = (node["persona_id"], node["node_type"], node["sqlite_node_id"])
        self._nodes[key] = dict(node)
        return {
            "trivium_node_id": _stable_fake_id(
                node["persona_id"], node["node_type"], node["sqlite_node_id"]
            )
        }

    def delete_node(self, node: dict[str, Any]) -> dict[str, Any]:
        self._nodes.pop((node["persona_id"], node["node_type"], node["sqlite_node_id"]), None)
        return {}

    def upsert_edge(self, edge: dict[str, Any]) -> dict[str, Any]:
        self._edges[_edge_key(edge)] = dict(edge)
        return {}

    def delete_edge(self, edge: dict[str, Any]) -> dict[str, Any]:
        self._edges.pop(_edge_key(edge), None)
        return {}

    def clear_namespace(self, persona_id: str) -> dict[str, Any]:
        for key in list(self._nodes):
            if key[0] == persona_id:
                del self._nodes[key]
        for key in list(self._edges):
            if key[0] == persona_id:
                del self._edges[key]
        return {}

    def find_candidates(self, request: dict[str, Any]) -> dict[str, Any]:
        persona_id = request["persona_id"]
        query = request["query_text"].casefold()
        limit = request["limit"]
        candidates = []
        for (node_persona_id, node_type, sqlite_node_id), node in sorted(self._nodes.items()):
            if node_persona_id != persona_id:
                continue
            searchable_text = str(node.get("searchable_text", "")).casefold()
            if query not in searchable_text:
                continue
            candidates.append(
                {
                    "trivium_node_id": _stable_fake_id(persona_id, node_type, sqlite_node_id),
                    "score": 1.0,
                    "source": "fake_sparse",
                }
            )
            if len(candidates) >= limit:
                break
        return {"candidates": candidates, "degraded": False}

    def activate_graph(self, request: dict[str, Any]) -> dict[str, Any]:
        persona_id = str(request["persona_id"])

        def neighbors(trivium_node_id: int) -> list[ActivationEdge]:
            out: list[ActivationEdge] = []
            for edge in self._edges.values():
                if str(edge["persona_id"]) != persona_id:
                    continue
                source_id = _stable_fake_id(
                    persona_id, str(edge["from_node_type"]), str(edge["from_node_id"])
                )
                target_id = _stable_fake_id(
                    persona_id, str(edge["to_node_type"]), str(edge["to_node_id"])
                )
                direction = str(edge.get("direction", "forward"))
                weight = float(edge.get("weight", 1.0))
                link_type = str(edge.get("link_type", "related"))
                if source_id == trivium_node_id and direction in ("forward", "bidirectional"):
                    out.append(ActivationEdge(target_id=target_id, link_type=link_type, weight=weight))
                if target_id == trivium_node_id and direction in ("backward", "bidirectional"):
                    out.append(ActivationEdge(target_id=source_id, link_type=link_type, weight=weight))
            return out

        def degree(trivium_node_id: int) -> int:
            count = 0
            for edge in self._edges.values():
                if str(edge["persona_id"]) != persona_id:
                    continue
                source_id = _stable_fake_id(
                    persona_id, str(edge["from_node_type"]), str(edge["from_node_id"])
                )
                target_id = _stable_fake_id(
                    persona_id, str(edge["to_node_type"]), str(edge["to_node_id"])
                )
                if source_id == trivium_node_id or target_id == trivium_node_id:
                    count += 1
            return count

        return {
            "candidates": activate_graph(
                list(request.get("seeds", [])),
                neighbors=neighbors,
                degree=degree,
                params=request.get("params", {}),
            ),
            "degraded": False,
        }

    def rerank(self, request: dict[str, Any]) -> dict[str, Any]:
        query_tokens = _tokens(str(request["query_text"]))
        results = []
        for candidate in request.get("candidates", []):
            summary_tokens = _tokens(str(candidate["safe_summary"]))
            overlap_count = len(query_tokens & summary_tokens)
            overlap_score = overlap_count / len(query_tokens) if query_tokens else 0.0
            configured_score = min(float(candidate.get("configured_score", 0.0)), 1.0)
            rerank_score = min(1.0, overlap_score + configured_score)
            results.append(
                {
                    "node_id": str(candidate["node_id"]),
                    "node_type": str(candidate.get("node_type", "fact")),
                    "rerank_score": round(rerank_score, 6),
                    "debug_reason": (
                        f"token_overlap={overlap_count}/{len(query_tokens)} "
                        f"configured_score={configured_score:.6g}"
                    ),
                }
            )
        results.sort(key=lambda item: (-item["rerank_score"], item["node_id"]))
        return {"results": results, "degraded": False}


def _stable_fake_id(*parts: str) -> int:
    digest = hashlib.sha256()
    for part in parts:
        digest.update(part.encode("utf-8"))
        digest.update(b"\x00")
    value = int.from_bytes(digest.digest()[:8], "big") & ((1 << 63) - 1)
    return value or 1


def _tokens(text: str) -> set[str]:
    return set(re.findall(r"[0-9A-Za-z_]+|[\u4e00-\u9fff]", text.casefold()))


def _edge_key(edge: dict[str, Any]) -> tuple[str, str, str, str, str, str, str]:
    return (
        str(edge["persona_id"]),
        str(edge["sqlite_edge_id"]),
        str(edge["link_type"]),
        str(edge["from_node_type"]),
        str(edge["from_node_id"]),
        str(edge["to_node_type"]),
        str(edge["to_node_id"]),
    )
