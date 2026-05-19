from __future__ import annotations

import argparse
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
    build_error,
    build_query_analysis_result,
    build_rerank_result,
    build_result,
    parse_activation_request,
    parse_candidate_request,
    parse_clear_namespace_payload,
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
