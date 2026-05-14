from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Callable, Iterable, Mapping, Any


@dataclass(frozen=True)
class ActivationEdge:
    target_id: int
    link_type: str
    weight: float = 1.0


NeighborProvider = Callable[[int], Iterable[ActivationEdge]]
DegreeProvider = Callable[[int], int]


def activate_graph(
    seeds: list[dict[str, Any]],
    *,
    neighbors: NeighborProvider,
    degree: DegreeProvider,
    params: Mapping[str, Any],
) -> list[dict[str, Any]]:
    max_hops = _positive_int(params.get("max_hops"), 2)
    hop_decay = _positive_float(params.get("hop_decay"), 0.70)
    min_energy = _positive_float(params.get("min_energy"), 0.01)
    max_active_nodes = _positive_int(params.get("max_active_nodes"), 80)
    hub_power = _non_negative_float(params.get("hub_suppression_power"), 0.50)
    include_paths = bool(params.get("include_paths", True))
    include_provenance_edges = bool(params.get("include_provenance_edges", False))

    energy: dict[int, float] = {}
    paths: dict[int, dict[str, Any]] = {}
    frontier: dict[int, float] = {}
    for seed in seeds:
        node_id = int(seed["trivium_node_id"])
        seed_energy = _non_negative_float(seed.get("seed_energy"), 0.0)
        if node_id <= 0 or seed_energy <= 0:
            continue
        if seed_energy > energy.get(node_id, 0.0):
            energy[node_id] = seed_energy
            paths[node_id] = {"trivium_node_ids": [node_id], "link_types": []}
        frontier[node_id] = max(frontier.get(node_id, 0.0), seed_energy)

    for _hop in range(max_hops):
        next_frontier: dict[int, float] = {}
        for source_id, source_energy in frontier.items():
            for edge in neighbors(source_id):
                edge_weight = _edge_weight(edge, include_provenance_edges)
                if edge_weight <= 0:
                    continue
                target_degree = max(1, degree(edge.target_id))
                hub_factor = 1.0 / (float(target_degree) ** hub_power)
                propagated = source_energy * hop_decay * edge_weight * hub_factor
                if propagated < min_energy:
                    continue
                current = energy.get(edge.target_id, 0.0)
                energy[edge.target_id] = current + propagated
                if propagated > next_frontier.get(edge.target_id, 0.0):
                    next_frontier[edge.target_id] = propagated
                if edge.target_id not in paths and source_id in paths:
                    previous = paths[source_id]
                    paths[edge.target_id] = {
                        "trivium_node_ids": previous["trivium_node_ids"] + [edge.target_id],
                        "link_types": previous["link_types"] + [edge.link_type],
                    }
        if not next_frontier:
            break
        frontier = next_frontier

    candidates = [
        {
            "trivium_node_id": node_id,
            "score": min(1.0, score),
            "source": "graph_activation",
        }
        for node_id, score in energy.items()
        if score >= min_energy
    ]
    candidates.sort(
        key=lambda item: (-float(item["score"]), int(item["trivium_node_id"]))
    )
    candidates = candidates[:max_active_nodes]
    for rank, candidate in enumerate(candidates, start=1):
        candidate["rank"] = rank
        if include_paths and candidate["trivium_node_id"] in paths:
            candidate["paths"] = [paths[candidate["trivium_node_id"]]]
    return candidates


def _edge_weight(edge: ActivationEdge, include_provenance_edges: bool) -> float:
    link_type = edge.link_type.strip().upper()
    if link_type == "EVIDENCED_BY" and not include_provenance_edges:
        return 0.0
    multiplier = {
        "CAUSED_BY": 1.0,
        "SUPERSEDES": 0.9,
        "SUPPORTS": 0.85,
        "ABOUT_ENTITY": 0.8,
        "CONTRIBUTED_TO": 0.8,
        "EXPLAINS": 0.85,
        "CO_OCCURS_WITH": 0.45,
        "DERIVED_FROM": 0.35,
        "EVIDENCED_BY": 0.20,
    }.get(link_type, 0.5)
    weight = _non_negative_float(edge.weight, 0.0)
    return weight * multiplier


def _positive_int(value: Any, default: int) -> int:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        return default
    return parsed if parsed > 0 else default


def _positive_float(value: Any, default: float) -> float:
    parsed = _non_negative_float(value, default)
    return parsed if parsed > 0 else default


def _non_negative_float(value: Any, default: float) -> float:
    try:
        parsed = float(value)
    except (TypeError, ValueError):
        return default
    if not math.isfinite(parsed) or parsed < 0:
        return default
    return parsed
