from __future__ import annotations

import math
import re
from dataclasses import dataclass
from typing import Any, Callable


SOURCE_PRIORITY = {
    "raw_dense": 0,
    "semantic_rewrite_dense": 1,
    "semantic_anchor_dense": 2,
}

DEFAULT_RRF_K = 60
DEFAULT_SUPPORT_BETA = 0.18
DEFAULT_MAX_SUPPORT_BONUS = 0.12


@dataclass(frozen=True)
class DenseQuery:
    text: str
    source: str
    purpose: str
    weight: float
    top_k: int


@dataclass(frozen=True)
class DenseHit:
    trivium_node_id: int
    score: float


def build_dense_queries(request: dict[str, Any]) -> list[DenseQuery]:
    query = request["query"]
    limit = int(request["limit"])
    signals = set(query.get("signals", []))
    forget_delete = "forget_delete" in signals
    queries = [
        DenseQuery(
            text=query["raw_text"],
            source="raw_dense",
            purpose="raw_query",
            weight=1.0,
            top_k=max(limit * 2, 32),
        )
    ]
    generated_seen: set[str] = set()

    for item in query.get("rewrites", [])[:5]:
        text = str(item.get("text", ""))
        normalized = normalize_text(text)
        if not normalized or normalized in generated_seen:
            continue
        purpose = str(item.get("purpose", "semantic_recall")).strip() or "semantic_recall"
        if forget_delete and purpose != "operation_target":
            continue
        if is_generic_generated_text(normalized):
            continue
        generated_seen.add(normalized)
        queries.append(
            DenseQuery(
                text=text,
                source="semantic_rewrite_dense",
                purpose=purpose,
                weight=clamp_float(item.get("weight"), 0.1, 0.9, 0.5),
                top_k=max(limit * 2, 32),
            )
        )

    if not forget_delete:
        anchor_count = 0
        for item in query.get("semantic_anchors", [])[:8]:
            if anchor_count >= 4:
                break
            text = str(item.get("text", ""))
            normalized = normalize_text(text)
            if not normalized or normalized in generated_seen:
                continue
            if is_generic_generated_text(normalized):
                continue
            generated_seen.add(normalized)
            anchor_count += 1
            queries.append(
                DenseQuery(
                    text=text,
                    source="semantic_anchor_dense",
                    purpose=str(item.get("purpose", "semantic_anchor")).strip()
                    or "semantic_anchor",
                    weight=clamp_float(item.get("weight"), 0.1, 0.65, 0.45),
                    top_k=max(limit, 16),
                )
            )
    return queries


def fuse_dense_results(
    request: dict[str, Any],
    search: Callable[[DenseQuery], list[DenseHit]],
) -> dict[str, Any]:
    queries = build_dense_queries(request)
    debug_scores = bool(request.get("debug_scores", False))
    limit = int(request["limit"])
    per_query_counts: list[dict[str, Any]] = []
    merged: dict[int, dict[str, Any]] = {}

    for query in queries:
        hits = search(query)
        per_query_counts.append(
            {"source": query.source, "purpose": query.purpose, "count": len(hits)}
        )
        for rank, hit in enumerate(hits, start=1):
            if hit.trivium_node_id <= 0:
                continue
            weighted_score = clamp_float(hit.score, 0.0, 1.0, 0.0) * query.weight
            item = merged.setdefault(
                hit.trivium_node_id,
                {
                    "trivium_node_id": hit.trivium_node_id,
                    "primary_score": -1.0,
                    "primary_source": query.source,
                    "primary_purpose": query.purpose,
                    "hit_count": 0,
                    "rrf_support": 0.0,
                },
            )
            item["hit_count"] += 1
            item["rrf_support"] += query.weight / (DEFAULT_RRF_K + rank)
            if _is_better_primary(weighted_score, query, item):
                item["primary_score"] = weighted_score
                item["primary_source"] = query.source
                item["primary_purpose"] = query.purpose

    query_count = max(len(queries), 1)
    candidates = []
    for item in merged.values():
        rrf_norm = min(
            1.0,
            item["rrf_support"] / (query_count / (DEFAULT_RRF_K + 1)),
        )
        primary_score = max(0.0, item["primary_score"])
        support_bonus = (
            min(
                DEFAULT_MAX_SUPPORT_BONUS,
                DEFAULT_SUPPORT_BETA * rrf_norm * (1 - primary_score),
            )
            if item["hit_count"] > 1
            else 0.0
        )
        fused_score = min(1.0, primary_score + support_bonus)
        candidate = {
            "trivium_node_id": item["trivium_node_id"],
            "fused_score": round(fused_score, 6),
            "primary_source": item["primary_source"],
            "primary_purpose": item["primary_purpose"],
            "rank": 0,
            "hit_count": item["hit_count"],
            "_primary_score": round(primary_score, 6),
        }
        if debug_scores:
            candidate["score_breakdown"] = {
                "primary_score": candidate["_primary_score"],
                "rrf_support_norm": round(rrf_norm, 6),
                "support_bonus": round(support_bonus, 6),
                "score_norm_method": "weighted_max_rrf",
            }
        candidates.append(candidate)

    candidates.sort(
        key=lambda item: (
            -float(item["fused_score"]),
            -float(item["_primary_score"]),
            -int(item["hit_count"]),
            SOURCE_PRIORITY.get(str(item["primary_source"]), 99),
            int(item["trivium_node_id"]),
        )
    )
    candidates = candidates[:limit]
    for rank, candidate in enumerate(candidates, start=1):
        candidate["rank"] = rank
        candidate.pop("_primary_score", None)

    diagnostics = {
        "query_count": len(queries),
        "raw_query_count": sum(1 for query in queries if query.source == "raw_dense"),
        "rewrite_query_count": sum(
            1 for query in queries if query.source == "semantic_rewrite_dense"
        ),
        "anchor_query_count": sum(
            1 for query in queries if query.source == "semantic_anchor_dense"
        ),
        "merged_candidate_count": len(merged),
        "dense_results_label": "operation_target_candidates"
        if "forget_delete" in set(request["query"].get("signals", []))
        else "dense_candidates",
        "per_query_counts": per_query_counts,
    }
    result: dict[str, Any] = {"candidates": candidates, "degraded": False}
    if debug_scores:
        result["diagnostics"] = diagnostics
    return result


def normalize_text(text: str) -> str:
    return " ".join(str(text).casefold().strip().split())


def is_generic_generated_text(normalized_text: str) -> bool:
    if not normalized_text:
        return True
    generic = {
        "memory",
        "memories",
        "remembered context",
        "context",
        "semantic memory",
        "relationship expansion",
        "记忆",
        "相关记忆",
        "上下文",
    }
    if normalized_text in generic:
        return True
    tokens = re.findall(r"[0-9A-Za-z_]+|[\u4e00-\u9fff]", normalized_text)
    return len(tokens) == 1 and tokens[0] in generic


def clamp_float(value: Any, lower: float, upper: float, default: float) -> float:
    try:
        number = float(value)
    except (TypeError, ValueError):
        return default
    if not math.isfinite(number):
        return default
    return min(max(number, lower), upper)


def _is_better_primary(
    weighted_score: float,
    query: DenseQuery,
    current: dict[str, Any],
) -> bool:
    current_score = float(current["primary_score"])
    if weighted_score != current_score:
        return weighted_score > current_score
    return SOURCE_PRIORITY.get(query.source, 99) < SOURCE_PRIORITY.get(
        str(current["primary_source"]), 99
    )
