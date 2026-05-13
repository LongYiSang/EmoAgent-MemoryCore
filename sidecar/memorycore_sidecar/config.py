from __future__ import annotations

import os
import tomllib
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping
from urllib.parse import urlparse


DEFAULT_TRIVIUM_DIR = "../data/trivium"
DEFAULT_TRIVIUM_DTYPE = "f32"
DEFAULT_TRIVIUM_SYNC_MODE = "normal"
DEFAULT_EMBEDDING_PROVIDER = "openai-compatible"
DEFAULT_EMBEDDING_BASE_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
DEFAULT_EMBEDDING_API_KEY_ENV = "DASHSCOPE_API_KEY"
DEFAULT_EMBEDDING_MODEL = "text-embedding-v4"
DEFAULT_EMBEDDING_DIMENSIONS = 1024
DEFAULT_EMBEDDING_TIMEOUT_SECONDS = 30
DEFAULT_EMBEDDING_ENCODING_FORMAT = "float"
_SIDECAR_PROJECT_DIR = Path(__file__).resolve().parents[1]

_VALID_DTYPES = {"f32", "f16", "u64"}
_VALID_SYNC_MODES = {"full", "normal", "off"}
_LOOPBACK_HTTP_HOSTS = {"localhost", "127.0.0.1", "::1"}


@dataclass(frozen=True)
class TriviumConfig:
    dir: Path
    dtype: str
    sync_mode: str


@dataclass(frozen=True)
class EmbeddingConfig:
    provider: str
    base_url: str
    api_key_env: str
    model: str
    dimensions: int
    timeout_seconds: int
    encoding_format: str


@dataclass(frozen=True)
class SidecarConfig:
    trivium: TriviumConfig
    embedding: EmbeddingConfig


def load_config(
    path: str | Path | None = None, env: Mapping[str, str] | None = None
) -> SidecarConfig:
    config_path = Path(path) if path is not None else None
    base_dir = config_path.parent if config_path is not None else _SIDECAR_PROJECT_DIR
    data: dict[str, Any] = {}
    if config_path is not None:
        with config_path.open("rb") as file:
            parsed = tomllib.load(file)
        if not isinstance(parsed, dict):
            raise ValueError("config root must be a table")
        data = parsed

    env_values = os.environ if env is None else env
    trivium_data = _table(data, "trivium")
    embedding_data = _table(data, "embedding")

    trivium_dir_value = _env_or_value(
        env_values, "MEMORYCORE_TRIVIUM_DIR", trivium_data, "dir", DEFAULT_TRIVIUM_DIR
    )
    trivium_dir = _resolve_dir(_string("trivium.dir", trivium_dir_value), base_dir)

    config = SidecarConfig(
        trivium=TriviumConfig(
            dir=trivium_dir,
            dtype=_string(
                "trivium.dtype",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_TRIVIUM_DTYPE",
                    trivium_data,
                    "dtype",
                    DEFAULT_TRIVIUM_DTYPE,
                ),
            ),
            sync_mode=_string(
                "trivium.sync_mode",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_TRIVIUM_SYNC_MODE",
                    trivium_data,
                    "sync_mode",
                    DEFAULT_TRIVIUM_SYNC_MODE,
                ),
            ),
        ),
        embedding=EmbeddingConfig(
            provider=_string(
                "embedding.provider",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_PROVIDER",
                    embedding_data,
                    "provider",
                    DEFAULT_EMBEDDING_PROVIDER,
                ),
            ),
            base_url=_string(
                "embedding.base_url",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_BASE_URL",
                    embedding_data,
                    "base_url",
                    DEFAULT_EMBEDDING_BASE_URL,
                ),
            ),
            api_key_env=_string(
                "embedding.api_key_env",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_API_KEY_ENV",
                    embedding_data,
                    "api_key_env",
                    DEFAULT_EMBEDDING_API_KEY_ENV,
                ),
            ),
            model=_string(
                "embedding.model",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_MODEL",
                    embedding_data,
                    "model",
                    DEFAULT_EMBEDDING_MODEL,
                ),
            ),
            dimensions=_positive_int(
                "embedding.dimensions",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_DIMENSIONS",
                    embedding_data,
                    "dimensions",
                    DEFAULT_EMBEDDING_DIMENSIONS,
                ),
            ),
            timeout_seconds=_positive_int(
                "embedding.timeout_seconds",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_TIMEOUT_SECONDS",
                    embedding_data,
                    "timeout_seconds",
                    DEFAULT_EMBEDDING_TIMEOUT_SECONDS,
                ),
            ),
            encoding_format=_string(
                "embedding.encoding_format",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_ENCODING_FORMAT",
                    embedding_data,
                    "encoding_format",
                    DEFAULT_EMBEDDING_ENCODING_FORMAT,
                ),
            ),
        ),
    )
    _validate(config)
    return config


def _table(data: Mapping[str, Any], key: str) -> Mapping[str, Any]:
    value = data.get(key, {})
    if not isinstance(value, Mapping):
        raise ValueError(f"{key} must be a table")
    return value


def _env_or_value(
    env: Mapping[str, str],
    env_key: str,
    table: Mapping[str, Any],
    key: str,
    default: Any,
) -> Any:
    if env_key in env:
        return env[env_key]
    return table.get(key, default)


def _string(name: str, value: Any) -> str:
    if not isinstance(value, str):
        raise ValueError(f"{name} must be a string")
    if value.strip() == "":
        raise ValueError(f"{name} must be non-empty")
    return value


def _positive_int(name: str, value: Any) -> int:
    if isinstance(value, bool):
        raise ValueError(f"{name} must be a positive integer")
    if isinstance(value, str):
        try:
            value = int(value, 10)
        except ValueError as exc:
            raise ValueError(f"{name} must be a positive integer") from exc
    if not isinstance(value, int) or value <= 0:
        raise ValueError(f"{name} must be a positive integer")
    return value


def _resolve_dir(value: str, base_dir: Path) -> Path:
    path = Path(value)
    if path.is_absolute():
        return path.resolve()
    return (base_dir / path).resolve()


def _validate(config: SidecarConfig) -> None:
    if config.trivium.dtype not in _VALID_DTYPES:
        raise ValueError("trivium.dtype must be one of f32, f16, u64")
    if config.trivium.sync_mode not in _VALID_SYNC_MODES:
        raise ValueError("trivium.sync_mode must be one of full, normal, off")
    if config.embedding.provider != "openai-compatible":
        raise ValueError("embedding.provider must be openai-compatible")
    _validate_embedding_base_url(config.embedding.base_url)
    if config.embedding.encoding_format != "float":
        raise ValueError("embedding.encoding_format must be float")


def _validate_embedding_base_url(value: str) -> None:
    parsed = urlparse(value)
    scheme = parsed.scheme.lower()
    hostname = parsed.hostname.lower() if parsed.hostname is not None else ""
    if scheme == "https":
        return
    if scheme == "http" and hostname in _LOOPBACK_HTTP_HOSTS:
        return
    raise ValueError(
        "embedding.base_url must use https unless it is loopback http"
    )
