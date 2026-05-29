from __future__ import annotations

import argparse
import hashlib
import json
import re
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Mapping

from .adapters.base import MirrorAdapter
from .adapters.fake import FakeMirrorAdapter
from .adapters.trivium import TriviumAdapter
from .config import load_config
from .config import QueryAnalysisConfig
from .embedding import EmbeddingCacheMiss
from .protocol import (
    build_activation_result,
    build_clear_namespace_result,
    build_candidates_result,
    build_eval_config_result,
    build_dedup_search_result,
    build_delete_candidates_result,
    build_error,
    build_query_analysis_result,
    build_rerank_result,
    build_result,
    parse_activation_request,
    parse_candidate_request,
    parse_clear_namespace_payload,
    parse_dedup_search_request,
    parse_delete_candidates_request,
    parse_eval_config_request,
    parse_operation_request,
    parse_query_analysis_request,
    parse_rerank_request,
    ProtocolError,
)
from .query_analysis import analyze_query


class AdapterClosingHTTPServer(ThreadingHTTPServer):
    def __init__(
        self,
        address: tuple[str, int],
        handler: type[BaseHTTPRequestHandler],
        adapter: MirrorAdapter,
    ) -> None:
        super().__init__(address, handler)
        self._adapter = adapter

    def server_close(self) -> None:
        try:
            close = getattr(self._adapter, "close", None)
            if callable(close):
                close()
        finally:
            super().server_close()


def create_server(
    address: tuple[str, int],
    adapter: MirrorAdapter,
    query_analysis_config: QueryAnalysisConfig | None = None,
) -> ThreadingHTTPServer:
    if query_analysis_config is None:
        adapter_config = getattr(adapter, "config", None)
        query_analysis_config = getattr(adapter_config, "query_analysis", None)
    if query_analysis_config is None:
        query_analysis_config = load_config(env={}).query_analysis

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            if self.path != "/health":
                self._write_json(HTTPStatus.NOT_FOUND, build_error("not found"))
                return
            self._write_json(HTTPStatus.OK, {"status": "ok"})

        def do_POST(self) -> None:
            try:
                request = self._read_json()
                if self.path == "/mirror/operation":
                    operation = parse_operation_request(request)
                    result = adapter.handle_operation(operation.operation, operation.payload)
                    self._write_json(
                        HTTPStatus.OK,
                        build_result(operation.operation_id, **result),
                    )
                    return
                if self.path == "/mirror/clear-namespace":
                    clear_request = parse_clear_namespace_payload(request)
                    result = adapter.clear_namespace(**clear_request)
                    self._write_json(
                        HTTPStatus.OK, build_clear_namespace_result(**result)
                    )
                    return
                if self.path == "/eval/configure":
                    config_request = parse_eval_config_request(request)
                    configure = getattr(adapter, "configure_eval", None)
                    if not callable(configure):
                        raise ProtocolError("adapter does not support eval configure")
                    result = configure(config_request)
                    self._write_json(HTTPStatus.OK, build_eval_config_result(**result))
                    return
                if self.path == "/retrieval/candidates":
                    candidate_request = parse_candidate_request(request)
                    try:
                        result = adapter.find_candidates(candidate_request)
                    except EmbeddingCacheMiss:
                        result = {
                            "candidates": [],
                            "degraded": True,
                            "fallback_reason": "embedding_cache_miss",
                        }
                    self._write_json(
                        HTTPStatus.OK,
                        build_candidates_result(
                            candidate_request["request_id"], **result
                        ),
                    )
                    return
                if self.path == "/memory/dedup-search":
                    dedup_request = parse_dedup_search_request(request)
                    result = _semantic_dedup_search(adapter, dedup_request)
                    self._write_json(
                        HTTPStatus.OK,
                        build_dedup_search_result(
                            dedup_request["request_id"], **result
                        ),
                    )
                    return
                if self.path == "/memory/delete-candidates":
                    delete_request = parse_delete_candidates_request(request)
                    result = _semantic_delete_candidates(adapter, delete_request)
                    self._write_json(
                        HTTPStatus.OK,
                        build_delete_candidates_result(
                            delete_request["request_id"], **result
                        ),
                    )
                    return
                if self.path == "/retrieval/query-analysis":
                    analysis_request = parse_query_analysis_request(request)
                    analysis = analyze_query(analysis_request, query_analysis_config)
                    self._write_json(
                        HTTPStatus.OK,
                        build_query_analysis_result(
                            analysis_request["request_id"], analysis
                        ),
                    )
                    return
                if self.path == "/retrieval/activate":
                    activation_request = parse_activation_request(request)
                    result = adapter.activate_graph(activation_request)
                    self._write_json(
                        HTTPStatus.OK,
                        build_activation_result(
                            activation_request["request_id"], **result
                        ),
                    )
                    return
                if self.path == "/retrieval/rerank":
                    rerank_request = parse_rerank_request(request)
                    result = adapter.rerank(rerank_request)
                    self._write_json(
                        HTTPStatus.OK,
                        build_rerank_result(rerank_request["request_id"], **result),
                    )
                    return
                self._write_json(HTTPStatus.NOT_FOUND, build_error("not found"))
            except ProtocolError as exc:
                self._write_json(HTTPStatus.BAD_REQUEST, build_error(str(exc)))
                return
            except Exception:
                self._write_json(HTTPStatus.BAD_REQUEST, build_error("sidecar request failed"))
                return

        def _read_json(self) -> Any:
            body = self.rfile.read(_content_length(self.headers.get("Content-Length")))
            if not body:
                return {}
            return json.loads(body.decode("utf-8"))

        def log_message(self, format: str, *args: Any) -> None:
            return

        def _write_json(self, status: HTTPStatus, body: dict[str, Any]) -> None:
            data = json.dumps(body, ensure_ascii=False).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

    return AdapterClosingHTTPServer(address, Handler, adapter)


def _semantic_dedup_search(
    adapter: MirrorAdapter, request: dict[str, Any]
) -> dict[str, Any]:
    candidate = request["candidate"]
    matches, unavailable = _memory_node_matches(
        adapter,
        persona_id=request["persona_id"],
        query_text=candidate["safe_summary"],
        limit=request["policy"]["limit"],
        allowed_node_types={"fact"},
    )
    if unavailable:
        return {
            "candidates": [],
            "degraded": True,
            "fallback_reason": "candidate_mapping_unavailable",
            "diagnostics": {"candidate_source": "unavailable"},
        }
    return {
        "candidates": [
            {
                "node_type": item["node_type"],
                "node_id": item["node_id"],
                "similarity": item["score"],
                "match_class": "near_duplicate" if item["score"] >= 0.82 else "related",
                "match_reason": "safe_summary_overlap",
                "merge_hint": "review_or_merge",
            }
            for item in matches
        ],
        "degraded": False,
        "diagnostics": {"candidate_source": "safe_summary"},
    }


def _semantic_delete_candidates(
    adapter: MirrorAdapter, request: dict[str, Any]
) -> dict[str, Any]:
    matches, unavailable = _memory_node_matches(
        adapter,
        persona_id=request["persona_id"],
        query_text=request["intent"]["raw_text"],
        limit=request["policy"]["limit"],
        allowed_node_types=_delete_candidate_node_types(request["policy"]),
    )
    if unavailable:
        return {
            "candidates": [],
            "degraded": True,
            "fallback_reason": "candidate_mapping_unavailable",
            "preview_hash_seed": _preview_hash_seed(request["request_id"], []),
            "diagnostics": {"candidate_source": "unavailable"},
        }
    candidates = []
    include_safe_summary = request["policy"]["include_safe_summary"]
    for item in matches:
        candidate = {
            "node_type": item["node_type"],
            "node_id": item["node_id"],
            "score": item["score"],
            "why_matched": ["semantic_intent"],
            "risk_flags": [],
        }
        if include_safe_summary:
            candidate["safe_summary"] = item["safe_summary"]
        candidates.append(candidate)
    return {
        "candidates": candidates,
        "degraded": False,
        "preview_hash_seed": _preview_hash_seed(request["request_id"], candidates),
        "diagnostics": {"candidate_source": "safe_summary"},
    }


def _memory_node_matches(
    adapter: MirrorAdapter,
    *,
    persona_id: str,
    query_text: str,
    limit: int,
    allowed_node_types: set[str],
) -> tuple[list[dict[str, Any]], bool]:
    nodes = getattr(adapter, "_nodes", None)
    if not isinstance(nodes, dict):
        return [], True
    matches: list[dict[str, Any]] = []
    for (node_persona_id, node_type, sqlite_node_id), node in sorted(nodes.items()):
        if node_persona_id != persona_id:
            continue
        if node_type not in allowed_node_types:
            continue
        searchable_text = str(node.get("searchable_text", ""))
        score = _text_overlap_score(query_text, searchable_text)
        if score <= 0:
            continue
        matches.append(
            {
                "node_type": str(node_type),
                "node_id": str(sqlite_node_id),
                "safe_summary": searchable_text,
                "score": score,
            }
        )
    matches.sort(key=lambda item: (-item["score"], item["node_type"], item["node_id"]))
    return matches[:limit], False


def _delete_candidate_node_types(policy: dict[str, Any]) -> set[str]:
    out: set[str] = set()
    if policy["allow_fact_candidates"]:
        out.add("fact")
    if policy["allow_episode_candidates"]:
        out.add("episode")
    return out


def _text_overlap_score(query_text: str, searchable_text: str) -> float:
    query = _normalize_text(query_text)
    target = _normalize_text(searchable_text)
    if not query or not target:
        return 0.0
    if query in target or target in query:
        return 1.0
    query_terms = _semantic_terms(query)
    target_terms = _semantic_terms(target)
    if not query_terms or not target_terms:
        return 0.0
    overlap = query_terms & target_terms
    if not overlap:
        return 0.0
    return round(min(1.0, len(overlap) / max(1, len(target_terms))), 6)


def _semantic_terms(value: str) -> set[str]:
    terms = set(re.findall(r"[0-9a-z_]+|[\u4e00-\u9fff]", value))
    cjk = [ch for ch in value if "\u4e00" <= ch <= "\u9fff"]
    for idx in range(len(cjk) - 1):
        terms.add("".join(cjk[idx : idx + 2]))
    return terms


def _normalize_text(value: str) -> str:
    return "".join(str(value).casefold().split())


def _preview_hash_seed(request_id: str, candidates: list[dict[str, Any]]) -> str:
    digest = hashlib.sha256()
    digest.update(request_id.encode("utf-8"))
    for candidate in candidates:
        digest.update(str(candidate["node_type"]).encode("utf-8"))
        digest.update(b"\x00")
        digest.update(str(candidate["node_id"]).encode("utf-8"))
        digest.update(b"\x00")
    return digest.hexdigest()


def create_adapter(
    adapter_name: str,
    config_path: str | Path | None = None,
    env: Mapping[str, str] | None = None,
) -> MirrorAdapter:
    if adapter_name == "fake":
        return FakeMirrorAdapter()
    if adapter_name == "trivium":
        return TriviumAdapter(load_config(config_path, env=env))
    raise ValueError(f"unsupported adapter: {adapter_name}")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--adapter", choices=("fake", "trivium"), default="fake")
    parser.add_argument("--config", type=Path)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8765)
    args = parser.parse_args(argv)

    config = load_config(args.config)
    adapter = create_adapter(args.adapter, args.config)
    server = create_server((args.host, args.port), adapter, config.query_analysis)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        return 130
    finally:
        server.server_close()
    return 0


def _content_length(value: str | None) -> int:
    if value is None:
        return 0
    try:
        return max(0, int(value))
    except ValueError:
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
