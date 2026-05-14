from __future__ import annotations

import os

import pytest


def test_dashscope_vl_rerank_provider_smoke_maps_safe_candidates():
    if os.environ.get("MEMORYCORE_RERANK_SMOKE") != "1":
        pytest.skip("set MEMORYCORE_RERANK_SMOKE=1 to run DashScope rerank smoke")
    if not os.environ.get("DASHSCOPE_API_KEY"):
        pytest.skip("set DASHSCOPE_API_KEY to run DashScope rerank smoke")

    from memorycore_sidecar.config import RerankConfig
    from memorycore_sidecar.rerank import DashScopeVLRerankProvider

    provider = DashScopeVLRerankProvider(
        RerankConfig(
            provider="dashscope-vl",
            endpoint_url="https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank",
            api_key_env="DASHSCOPE_API_KEY",
            model="qwen3-vl-rerank",
            timeout_seconds=30,
            top_n=30,
            instruct="Retrieve semantically relevant safe memory summaries for the user's query.",
        )
    )

    result = provider.rerank(
        "用户喜欢什么咖啡？",
        [
            {
                "node_id": "fact-coffee",
                "node_type": "fact",
                "safe_summary": "用户喜欢手冲咖啡。",
            },
            {
                "node_id": "fact-ci",
                "node_type": "fact",
                "safe_summary": "用户最近在部署 CI。",
            },
        ],
    )

    assert result.get("degraded") is False
    assert len(result["results"]) >= 1
    first = result["results"][0]
    assert first["node_id"] in {"fact-coffee", "fact-ci"}
    assert first["node_type"] == "fact"
    assert 0 <= first["rerank_score"] <= 1
    assert "safe_summary" not in str(result)
    assert "手冲咖啡" not in str(result)
