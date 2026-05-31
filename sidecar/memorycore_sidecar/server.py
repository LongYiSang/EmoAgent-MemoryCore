from __future__ import annotations

import argparse
import errno
import hashlib
import json
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
            _write_json_response(self, status, body)

    return AdapterClosingHTTPServer(address, Handler, adapter)


def _write_json_response(
    handler: BaseHTTPRequestHandler, status: HTTPStatus, body: dict[str, Any]
) -> bool:
    data = json.dumps(body, ensure_ascii=False).encode("utf-8")
    try:
        handler.send_response(status)
        handler.send_header("Content-Type", "application/json; charset=utf-8")
        handler.send_header("Content-Length", str(len(data)))
        handler.end_headers()
        handler.wfile.write(data)
        return True
    except OSError as exc:
        if _is_client_disconnect(exc):
            return False
        raise


def _is_client_disconnect(exc: OSError) -> bool:
    if isinstance(exc, (BrokenPipeError, ConnectionAbortedError, ConnectionResetError)):
        return True
    if getattr(exc, "errno", None) in {
        errno.EPIPE,
        errno.ECONNABORTED,
        errno.ECONNRESET,
    }:
        return True
    return getattr(exc, "winerror", None) in {10053, 10054}


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
    adapter_matches = getattr(adapter, "memory_node_matches", None)
    if callable(adapter_matches):
        return (
            list(
                adapter_matches(
                    persona_id=persona_id,
                    query_text=query_text,
                    limit=limit,
                    allowed_node_types=allowed_node_types,
                )
            )[:limit],
            False,
        )

    return [], True


def _delete_candidate_node_types(policy: dict[str, Any]) -> set[str]:
    out: set[str] = set()
    if policy["allow_fact_candidates"]:
        out.add("fact")
    if policy["allow_episode_candidates"]:
        out.add("episode")
    return out


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
