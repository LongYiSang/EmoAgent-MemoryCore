from __future__ import annotations

import math
import re
import time
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from typing import Any, Callable


SOURCE_PRIORITY = {
    "raw_exact": 0,
    "raw_dense": 0,
    "semantic_rewrite_dense": 1,
    "semantic_anchor_dense": 2,
}

DEFAULT_RRF_K = 60
DEFAULT_SUPPORT_BETA = 0.18
DEFAULT_MAX_SUPPORT_BONUS = 0.12
DEFAULT_MAX_DENSE_QUERIES = 6
DEFAULT_MAX_DENSE_QUERY_WORKERS = 4
DEFAULT_MAX_REWRITE_QUERIES = 3
DEFAULT_MAX_ANCHOR_QUERIES = 2
MERGE_ORDER = [
    "fused_score_desc",
    "primary_score_desc",
    "hit_count_desc",
    "source_priority_asc",
    "trivium_node_id_asc",
]


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


@dataclass(frozen=True)
class DenseSearchResult:
    hits: list[DenseHit]
    embedding_latency_ms: int = 0
    search_latency_ms: int = 0


@dataclass(frozen=True)
class DenseQueryPlan:
    queries: list[DenseQuery]
    trims: dict[str, int]


def build_dense_queries(request: dict[str, Any]) -> list[DenseQuery]:
    return _build_dense_query_plan(request).queries


def _build_dense_query_plan(request: dict[str, Any]) -> DenseQueryPlan:
    query = request["query"]
    limit = int(request["limit"])
    signals = set(query.get("signals", []))
    forget_delete = "forget_delete" in signals
    raw_normalized = normalize_text(query["raw_text"])
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
    trims = {
        "dropped_rewrite_count": 0,
        "dropped_anchor_count": 0,
        "dropped_similar_count": 0,
        "dropped_generic_count": 0,
        "dropped_duplicate_count": 0,
        "dropped_fanout_limit_count": 0,
        "max_dense_queries": DEFAULT_MAX_DENSE_QUERIES,
    }
    rewrite_count = 0

    for item in query.get("rewrites", []):
        text = str(item.get("text", ""))
        normalized = normalize_text(text)
        if not normalized:
            trims["dropped_rewrite_count"] += 1
            continue
        if is_similar_to_raw_query(normalized, raw_normalized):
            trims["dropped_rewrite_count"] += 1
            trims["dropped_similar_count"] += 1
            continue
        purpose = str(item.get("purpose", "semantic_recall")).strip() or "semantic_recall"
        if forget_delete and purpose != "operation_target":
            trims["dropped_rewrite_count"] += 1
            continue
        if is_generic_generated_text(normalized):
            trims["dropped_rewrite_count"] += 1
            trims["dropped_generic_count"] += 1
            continue
        if normalized in generated_seen:
            trims["dropped_rewrite_count"] += 1
            trims["dropped_duplicate_count"] += 1
            continue
        if (
            rewrite_count >= DEFAULT_MAX_REWRITE_QUERIES
            or len(queries) >= DEFAULT_MAX_DENSE_QUERIES
        ):
            trims["dropped_rewrite_count"] += 1
            trims["dropped_fanout_limit_count"] += 1
            continue
        generated_seen.add(normalized)
        rewrite_count += 1
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
        for item in query.get("semantic_anchors", []):
            text = str(item.get("text", ""))
            normalized = normalize_text(text)
            if not normalized:
                trims["dropped_anchor_count"] += 1
                continue
            if is_similar_to_raw_query(normalized, raw_normalized):
                trims["dropped_anchor_count"] += 1
                trims["dropped_similar_count"] += 1
                continue
            if is_generic_generated_text(normalized):
                trims["dropped_anchor_count"] += 1
                trims["dropped_generic_count"] += 1
                continue
            if normalized in generated_seen:
                trims["dropped_anchor_count"] += 1
                trims["dropped_duplicate_count"] += 1
                continue
            if (
                anchor_count >= DEFAULT_MAX_ANCHOR_QUERIES
                or len(queries) >= DEFAULT_MAX_DENSE_QUERIES
            ):
                trims["dropped_anchor_count"] += 1
                trims["dropped_fanout_limit_count"] += 1
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
    return DenseQueryPlan(queries=queries, trims=trims)


def fuse_dense_results(
    request: dict[str, Any],
    search: Callable[[DenseQuery], list[DenseHit] | DenseSearchResult],
) -> dict[str, Any]:
    plan = _build_dense_query_plan(request)
    queries = plan.queries
    debug_scores = bool(request.get("debug_scores", False))
    limit = int(request["limit"])
    per_query_counts: list[dict[str, Any]] = []
    merged: dict[int, dict[str, Any]] = {}
    dense_embedding_wall_latency_ms = 0
    dense_search_total_latency_ms = 0

    for query, search_result in _run_dense_searches(queries, search):
        hits = search_result.hits
        dense_embedding_wall_latency_ms = max(
            dense_embedding_wall_latency_ms,
            search_result.embedding_latency_ms,
        )
        dense_search_total_latency_ms += search_result.search_latency_ms
        latency_ms = search_result.embedding_latency_ms + search_result.search_latency_ms
        per_query_counts.append(
            {
                "source": query.source,
                "purpose": query.purpose,
                "count": len(hits),
                "latency_ms": latency_ms,
            }
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
                    "source_breakdown": {},
                },
            )
            item["hit_count"] += 1
            item["rrf_support"] += query.weight / (DEFAULT_RRF_K + rank)
            breakdown = item["source_breakdown"].get(query.source)
            if breakdown is None or weighted_score > breakdown["score"]:
                item["source_breakdown"][query.source] = {
                    "source": query.source,
                    "purpose": query.purpose,
                    "rank": rank,
                    "score": round(weighted_score, 6),
                    "weight": query.weight,
                }
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
        source_breakdown = sorted(
            item["source_breakdown"].values(),
            key=lambda value: (
                SOURCE_PRIORITY.get(str(value["source"]), 99),
                int(value["rank"]),
                str(value["purpose"]),
            ),
        )
        candidate = {
            "trivium_node_id": item["trivium_node_id"],
            "fused_score": round(fused_score, 6),
            "primary_source": item["primary_source"],
            "primary_purpose": item["primary_purpose"],
            "rank": 0,
            "hit_count": item["hit_count"],
            "_primary_score": round(primary_score, 6),
        }
        if debug_scores or len(source_breakdown) > 1:
            candidate["source_breakdown"] = source_breakdown
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
        "query_trims": plan.trims,
        "dense_embedding_wall_latency_ms": dense_embedding_wall_latency_ms,
        "dense_embedding_batch_latency_ms": dense_embedding_wall_latency_ms,
        "dense_search_total_latency_ms": dense_search_total_latency_ms,
        "query_count_trimmed_by_budget": int(
            plan.trims.get("dropped_fanout_limit_count", 0)
        ),
        "merge_order": MERGE_ORDER,
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


def is_similar_to_raw_query(normalized_text: str, normalized_raw: str) -> bool:
    if not normalized_text or not normalized_raw:
        return False
    if normalized_text == normalized_raw:
        return True
    text_tokens = set(_similarity_tokens(normalized_text))
    raw_tokens = set(_similarity_tokens(normalized_raw))
    if not text_tokens or not raw_tokens:
        return False
    overlap = len(text_tokens & raw_tokens)
    union = len(text_tokens | raw_tokens)
    return union > 0 and (overlap / union) >= 0.85


def clamp_float(value: Any, lower: float, upper: float, default: float) -> float:
    try:
        number = float(value)
    except (TypeError, ValueError):
        return default
    if not math.isfinite(number):
        return default
    return min(max(number, lower), upper)


def _similarity_tokens(text: str) -> list[str]:
    return re.findall(r"[0-9A-Za-z_]+|[\u4e00-\u9fff]", text)


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


def _run_dense_searches(
    queries: list[DenseQuery],
    search: Callable[[DenseQuery], list[DenseHit] | DenseSearchResult],
) -> list[tuple[DenseQuery, DenseSearchResult]]:
    if len(queries) <= 1:
        return [(query, _run_dense_search(query, search)) for query in queries]

    max_workers = min(len(queries), DEFAULT_MAX_DENSE_QUERY_WORKERS)
    results: list[DenseSearchResult | None] = [None] * len(queries)
    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = [
            executor.submit(_run_dense_search, query, search)
            for query in queries
        ]
        for index, future in enumerate(futures):
            results[index] = future.result()
    return [
        (query, result)
        for query, result in zip(queries, results)
        if result is not None
    ]


def _run_dense_search(
    query: DenseQuery,
    search: Callable[[DenseQuery], list[DenseHit] | DenseSearchResult],
) -> DenseSearchResult:
    started = time.perf_counter()
    result = search(query)
    elapsed_ms = _elapsed_ms(started)
    if isinstance(result, DenseSearchResult):
        if result.embedding_latency_ms > 0 or result.search_latency_ms > 0:
            return result
        return DenseSearchResult(hits=result.hits, search_latency_ms=elapsed_ms)
    return DenseSearchResult(hits=result, search_latency_ms=elapsed_ms)


def _elapsed_ms(started: float) -> int:
    return int(max(0.0, time.perf_counter() - started) * 1000)
