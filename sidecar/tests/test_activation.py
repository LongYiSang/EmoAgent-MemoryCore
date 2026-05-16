import pytest

from memorycore_sidecar.activation import ActivationEdge, activate_graph


def test_activate_graph_propagates_two_hops_with_unique_path():
    graph = {
        1: [ActivationEdge(target_id=2, link_type="CAUSED_BY", weight=0.8)],
        2: [ActivationEdge(target_id=3, link_type="SUPPORTS", weight=0.5)],
    }

    result = activate_graph(
        [{"trivium_node_id": 1, "seed_energy": 1.0}],
        neighbors=lambda node_id: graph.get(node_id, []),
        degree=lambda node_id: 1,
        params={"max_hops": 2, "hop_decay": 0.5, "include_paths": True},
    )
    by_id = {candidate["trivium_node_id"]: candidate for candidate in result}

    assert by_id[1]["score"] == pytest.approx(1.0)
    assert by_id[2]["score"] == pytest.approx(0.4)
    assert by_id[3]["score"] == pytest.approx(0.085)
    assert by_id[3]["paths"] == [
        {"trivium_node_ids": [1, 2, 3], "link_types": ["CAUSED_BY", "SUPPORTS"]}
    ]


def test_activate_graph_excludes_provenance_edges_by_default():
    graph = {1: [ActivationEdge(target_id=2, link_type="EVIDENCED_BY", weight=1.0)]}

    result = activate_graph(
        [{"trivium_node_id": 1, "seed_energy": 1.0}],
        neighbors=lambda node_id: graph.get(node_id, []),
        degree=lambda node_id: 1,
        params={"max_hops": 1, "hop_decay": 1.0},
    )

    assert [candidate["trivium_node_id"] for candidate in result] == [1]


def test_activate_graph_includes_provenance_edges_when_enabled():
    graph = {1: [ActivationEdge(target_id=2, link_type="EVIDENCED_BY", weight=1.0)]}

    result = activate_graph(
        [{"trivium_node_id": 1, "seed_energy": 1.0}],
        neighbors=lambda node_id: graph.get(node_id, []),
        degree=lambda node_id: 1,
        params={
            "max_hops": 1,
            "hop_decay": 1.0,
            "include_provenance_edges": True,
        },
    )
    by_id = {candidate["trivium_node_id"]: candidate for candidate in result}

    assert by_id[2]["score"] == pytest.approx(0.2)


def test_activate_graph_hub_suppression_uses_target_degree():
    graph = {
        1: [
            ActivationEdge(target_id=2, link_type="ABOUT_ENTITY", weight=1.0),
            ActivationEdge(target_id=3, link_type="ABOUT_ENTITY", weight=1.0),
        ]
    }

    def degree(node_id):
        return {2: 1, 3: 9}.get(node_id, 1)

    result = activate_graph(
        [{"trivium_node_id": 1, "seed_energy": 1.0}],
        neighbors=lambda node_id: graph.get(node_id, []),
        degree=degree,
        params={"max_hops": 1, "hop_decay": 1.0, "hub_suppression_power": 1.0},
    )
    by_id = {candidate["trivium_node_id"]: candidate for candidate in result}

    assert by_id[2]["score"] == pytest.approx(0.8)
    assert by_id[3]["score"] == pytest.approx(0.8 / 9.0)
    assert by_id[3]["score"] < by_id[2]["score"]


def test_activate_graph_sorts_by_score_then_node_id_and_caps_active_nodes():
    graph = {
        1: [
            ActivationEdge(target_id=5, link_type="CAUSED_BY", weight=0.5),
            ActivationEdge(target_id=4, link_type="CAUSED_BY", weight=0.5),
            ActivationEdge(target_id=6, link_type="CAUSED_BY", weight=0.5),
        ]
    }

    result = activate_graph(
        [{"trivium_node_id": 1, "seed_energy": 1.0}],
        neighbors=lambda node_id: graph.get(node_id, []),
        degree=lambda node_id: 1,
        params={"max_hops": 1, "hop_decay": 1.0, "max_active_nodes": 3},
    )

    assert [candidate["trivium_node_id"] for candidate in result] == [1, 4, 5]
    assert [candidate["rank"] for candidate in result] == [1, 2, 3]
