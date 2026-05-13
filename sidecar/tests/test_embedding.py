from __future__ import annotations

import json
import threading
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

import pytest

from memorycore_sidecar.config import EmbeddingConfig
from memorycore_sidecar.embedding import (
    FakeEmbeddingProvider,
    OpenAICompatibleEmbeddingProvider,
    build_embedding_provider,
)


def test_openai_compatible_provider_posts_dashscope_default_body_headers_and_path():
    server, captured = _start_embedding_server(
        HTTPStatus.OK,
        {"data": [{"embedding": [0.1, 0.2, 0.3]}]},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(
                base_url=f"http://127.0.0.1:{server.server_address[1]}/compatible-mode/v1/"
            ),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        result = provider.embed("用户喜欢咖啡")

        assert result == [0.1, 0.2, 0.3]
        assert captured["path"] == "/compatible-mode/v1/embeddings"
        assert captured["headers"]["authorization"] == "Bearer test-secret"
        assert captured["headers"]["content-type"] == "application/json"
        assert captured["body"] == {
            "model": "text-embedding-v4",
            "input": "用户喜欢咖啡",
            "encoding_format": "float",
            "dimensions": 3,
        }
    finally:
        _stop_server(server)


def test_openai_compatible_provider_missing_key_names_env_var_without_secret_value():
    provider = OpenAICompatibleEmbeddingProvider(
        _embedding_config(api_key_env="MISSING_DASHSCOPE_KEY"),
        env={},
    )

    with pytest.raises(RuntimeError) as err:
        provider.embed("text")

    assert "MISSING_DASHSCOPE_KEY" in str(err.value)


def test_openai_compatible_provider_http_failure_has_clear_error():
    server, _captured = _start_embedding_server(
        HTTPStatus.BAD_GATEWAY,
        {"error": {"message": "upstream unavailable"}},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        with pytest.raises(RuntimeError, match="embedding request failed with HTTP 502"):
            provider.embed("text")
    finally:
        _stop_server(server)


@pytest.mark.parametrize(
    "body",
    [
        {},
        {"data": []},
        {"data": [{}]},
        {"data": [{"embedding": "not-a-list"}]},
    ],
)
def test_openai_compatible_provider_rejects_malformed_response(body):
    server, _captured = _start_embedding_server(HTTPStatus.OK, body)
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        with pytest.raises(ValueError, match="data\\[0\\]\\.embedding"):
            provider.embed("text")
    finally:
        _stop_server(server)


def test_openai_compatible_provider_rejects_wrong_dimension():
    server, _captured = _start_embedding_server(
        HTTPStatus.OK,
        {"data": [{"embedding": [0.1, 0.2]}]},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        with pytest.raises(ValueError, match="expected 3 dimensions"):
            provider.embed("text")
    finally:
        _stop_server(server)


def test_openai_compatible_provider_coerces_successful_embedding_values_to_floats():
    server, _captured = _start_embedding_server(
        HTTPStatus.OK,
        {"data": [{"embedding": [1, "2.5", 3.0]}]},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        result = provider.embed("text")

        assert result == [1.0, 2.5, 3.0]
        assert all(type(value) is float for value in result)
    finally:
        _stop_server(server)


def test_openai_compatible_provider_rejects_malformed_embedding_value_with_index_without_value():
    server, _captured = _start_embedding_server(
        HTTPStatus.OK,
        {"data": [{"embedding": [0.1, "secret-value", 0.3]}]},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        with pytest.raises(ValueError, match=r"data\[0\]\.embedding\[1\]") as err:
            provider.embed("text")

        assert "secret-value" not in str(err.value)
    finally:
        _stop_server(server)


@pytest.mark.parametrize("value", ["nan", "inf", "-inf"])
def test_openai_compatible_provider_rejects_non_finite_embedding_values_with_index_without_value(
    value,
):
    server, _captured = _start_embedding_server(
        HTTPStatus.OK,
        {"data": [{"embedding": [0.1, value, 0.3]}]},
    )
    try:
        provider = OpenAICompatibleEmbeddingProvider(
            _embedding_config(base_url=f"http://127.0.0.1:{server.server_address[1]}"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )

        with pytest.raises(ValueError, match=r"data\[0\]\.embedding\[1\]") as err:
            provider.embed("text")

        assert value not in str(err.value)
    finally:
        _stop_server(server)


def test_build_embedding_provider_accepts_openai_compatible_and_validates_provider():
    provider = build_embedding_provider(
        _embedding_config(provider="openai-compatible"),
        env={"DASHSCOPE_API_KEY": "test-secret"},
    )

    assert isinstance(provider, OpenAICompatibleEmbeddingProvider)

    with pytest.raises(ValueError, match="unsupported embedding provider"):
        build_embedding_provider(
            _embedding_config(provider="other"),
            env={"DASHSCOPE_API_KEY": "test-secret"},
        )


def test_fake_embedding_provider_returns_configured_vectors():
    provider = FakeEmbeddingProvider({"hello": [1, 2.0]})

    assert provider.embed("hello") == [1.0, 2.0]


@pytest.mark.parametrize("value", ["nan", "inf", "-inf"])
def test_fake_embedding_provider_rejects_non_finite_values_with_index_without_value(value):
    provider = FakeEmbeddingProvider({"hello": [1, value]})

    with pytest.raises(ValueError, match=r"embedding\[1\]") as err:
        provider.embed("hello")

    assert value not in str(err.value)


def _embedding_config(**overrides: Any) -> EmbeddingConfig:
    values = {
        "provider": "openai-compatible",
        "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
        "api_key_env": "DASHSCOPE_API_KEY",
        "model": "text-embedding-v4",
        "dimensions": 3,
        "timeout_seconds": 2,
        "encoding_format": "float",
    }
    values.update(overrides)
    return EmbeddingConfig(**values)


def _start_embedding_server(
    status: HTTPStatus, body: dict[str, Any]
) -> tuple[ThreadingHTTPServer, dict[str, Any]]:
    captured: dict[str, Any] = {}

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:
            request_body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
            captured["path"] = self.path
            captured["headers"] = {key.lower(): value for key, value in self.headers.items()}
            captured["body"] = json.loads(request_body.decode("utf-8"))
            data = json.dumps(body).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, format: str, *args: Any) -> None:
            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever)
    thread.start()
    server._test_thread = thread  # type: ignore[attr-defined]
    return server, captured


def _stop_server(server: ThreadingHTTPServer) -> None:
    server.shutdown()
    server.server_close()
    server._test_thread.join(timeout=2)  # type: ignore[attr-defined]
