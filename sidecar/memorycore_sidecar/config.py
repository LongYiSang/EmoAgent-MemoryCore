from __future__ import annotations

import os
import tomllib
import math
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
DEFAULT_EMBEDDING_CACHE_MODE = "off"
DEFAULT_EMBEDDING_CACHE_DB_PATH = "../data/embedding_cache.sqlite3"
DEFAULT_EMBEDDING_CACHE_TEXT_NORMALIZATION_VERSION = "v1"
DEFAULT_EMBEDDING_CACHE_SEARCHABLE_TEXT_VERSION = "v1"
DEFAULT_EMBEDDING_CACHE_TTL_DAYS_FOR_QUERY = 14
DEFAULT_EMBEDDING_CACHE_STORE_RAW_TEXT = False
DEFAULT_RERANK_PROVIDER = "none"
DEFAULT_RERANK_ENDPOINT_URL = (
    "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"
)
DEFAULT_RERANK_API_KEY_ENV = "DASHSCOPE_API_KEY"
DEFAULT_RERANK_MODEL = "qwen3-vl-rerank"
DEFAULT_RERANK_TIMEOUT_SECONDS = 30
DEFAULT_RERANK_TOP_N = 30
DEFAULT_RERANK_INSTRUCT = (
    "Retrieve semantically relevant safe memory summaries for the user's query."
)
DEFAULT_QUERY_ANALYSIS_PROVIDER = "none"
DEFAULT_QUERY_ANALYSIS_BASE_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
DEFAULT_QUERY_ANALYSIS_API_KEY_ENV = "DASHSCOPE_API_KEY"
DEFAULT_QUERY_ANALYSIS_MODEL = "qwen-plus"
DEFAULT_QUERY_ANALYSIS_TIMEOUT_SECONDS = 30
DEFAULT_QUERY_ANALYSIS_TEMPERATURE = 0.0
DEFAULT_QUERY_ANALYSIS_RESPONSE_FORMAT = "json_object"
DEFAULT_QUERY_ANALYSIS_PROMPT_VERSION = "query-analysis-v0.1"
_SIDECAR_PROJECT_DIR = Path(__file__).resolve().parents[1]

_VALID_DTYPES = {"f32", "f16", "u64"}
_VALID_SYNC_MODES = {"full", "normal", "off"}
_VALID_EMBEDDING_CACHE_MODES = {"off", "read_write", "read_only", "refresh"}
_VALID_RERANK_PROVIDERS = {"none", "dashscope-vl"}
_VALID_QUERY_ANALYSIS_PROVIDERS = {"none", "openai-compatible"}
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
class EmbeddingCacheConfig:
    mode: str
    db_path: Path
    text_normalization_version: str
    searchable_text_version: str
    ttl_days_for_query: int
    store_raw_text: bool


@dataclass(frozen=True)
class RerankConfig:
    provider: str
    endpoint_url: str
    api_key_env: str
    model: str
    timeout_seconds: int
    top_n: int
    instruct: str


@dataclass(frozen=True)
class QueryAnalysisConfig:
    provider: str
    base_url: str
    api_key_env: str
    model: str
    timeout_seconds: int
    temperature: float
    response_format: str
    prompt_version: str


@dataclass(frozen=True)
class SidecarConfig:
    trivium: TriviumConfig
    embedding: EmbeddingConfig
    embedding_cache: EmbeddingCacheConfig
    rerank: RerankConfig
    query_analysis: QueryAnalysisConfig


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
    embedding_cache_data = _table(data, "embedding_cache")
    rerank_data = _table(data, "rerank")
    query_analysis_data = _table(data, "query_analysis")

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
        embedding_cache=EmbeddingCacheConfig(
            mode=_string(
                "embedding_cache.mode",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_CACHE_MODE",
                    embedding_cache_data,
                    "mode",
                    DEFAULT_EMBEDDING_CACHE_MODE,
                ),
            ),
            db_path=_resolve_path(
                _string(
                    "embedding_cache.db_path",
                    _env_or_value(
                        env_values,
                        "MEMORYCORE_EMBEDDING_CACHE_DB_PATH",
                        embedding_cache_data,
                        "db_path",
                        DEFAULT_EMBEDDING_CACHE_DB_PATH,
                    ),
                ),
                base_dir,
            ),
            text_normalization_version=_string(
                "embedding_cache.text_normalization_version",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_CACHE_TEXT_NORMALIZATION_VERSION",
                    embedding_cache_data,
                    "text_normalization_version",
                    DEFAULT_EMBEDDING_CACHE_TEXT_NORMALIZATION_VERSION,
                ),
            ),
            searchable_text_version=_string(
                "embedding_cache.searchable_text_version",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_CACHE_SEARCHABLE_TEXT_VERSION",
                    embedding_cache_data,
                    "searchable_text_version",
                    DEFAULT_EMBEDDING_CACHE_SEARCHABLE_TEXT_VERSION,
                ),
            ),
            ttl_days_for_query=_positive_int(
                "embedding_cache.ttl_days_for_query",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_CACHE_TTL_DAYS_FOR_QUERY",
                    embedding_cache_data,
                    "ttl_days_for_query",
                    DEFAULT_EMBEDDING_CACHE_TTL_DAYS_FOR_QUERY,
                ),
            ),
            store_raw_text=_bool(
                "embedding_cache.store_raw_text",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_EMBEDDING_CACHE_STORE_RAW_TEXT",
                    embedding_cache_data,
                    "store_raw_text",
                    DEFAULT_EMBEDDING_CACHE_STORE_RAW_TEXT,
                ),
            ),
        ),
        rerank=RerankConfig(
            provider=_string(
                "rerank.provider",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_RERANK_PROVIDER",
                    rerank_data,
                    "provider",
                    DEFAULT_RERANK_PROVIDER,
                ),
            ),
            endpoint_url=_string(
                "rerank.endpoint_url",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_RERANK_ENDPOINT_URL",
                    rerank_data,
                    "endpoint_url",
                    DEFAULT_RERANK_ENDPOINT_URL,
                ),
            ),
            api_key_env=_string(
                "rerank.api_key_env",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_RERANK_API_KEY_ENV",
                    rerank_data,
                    "api_key_env",
                    DEFAULT_RERANK_API_KEY_ENV,
                ),
            ),
            model=_string(
                "rerank.model",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_RERANK_MODEL",
                    rerank_data,
                    "model",
                    DEFAULT_RERANK_MODEL,
                ),
            ),
            timeout_seconds=_positive_int(
                "rerank.timeout_seconds",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_RERANK_TIMEOUT_SECONDS",
                    rerank_data,
                    "timeout_seconds",
                    DEFAULT_RERANK_TIMEOUT_SECONDS,
                ),
            ),
            top_n=_positive_int(
                "rerank.top_n",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_RERANK_TOP_N",
                    rerank_data,
                    "top_n",
                    DEFAULT_RERANK_TOP_N,
                ),
            ),
            instruct=_string(
                "rerank.instruct",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_RERANK_INSTRUCT",
                    rerank_data,
                    "instruct",
                    DEFAULT_RERANK_INSTRUCT,
                ),
            ),
        ),
        query_analysis=QueryAnalysisConfig(
            provider=_string(
                "query_analysis.provider",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_QUERY_ANALYSIS_PROVIDER",
                    query_analysis_data,
                    "provider",
                    DEFAULT_QUERY_ANALYSIS_PROVIDER,
                ),
            ),
            base_url=_string(
                "query_analysis.base_url",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_QUERY_ANALYSIS_BASE_URL",
                    query_analysis_data,
                    "base_url",
                    DEFAULT_QUERY_ANALYSIS_BASE_URL,
                ),
            ),
            api_key_env=_string(
                "query_analysis.api_key_env",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_QUERY_ANALYSIS_API_KEY_ENV",
                    query_analysis_data,
                    "api_key_env",
                    DEFAULT_QUERY_ANALYSIS_API_KEY_ENV,
                ),
            ),
            model=_string(
                "query_analysis.model",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_QUERY_ANALYSIS_MODEL",
                    query_analysis_data,
                    "model",
                    DEFAULT_QUERY_ANALYSIS_MODEL,
                ),
            ),
            timeout_seconds=_positive_int(
                "query_analysis.timeout_seconds",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_QUERY_ANALYSIS_TIMEOUT_SECONDS",
                    query_analysis_data,
                    "timeout_seconds",
                    DEFAULT_QUERY_ANALYSIS_TIMEOUT_SECONDS,
                ),
            ),
            temperature=_finite_float(
                "query_analysis.temperature",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_QUERY_ANALYSIS_TEMPERATURE",
                    query_analysis_data,
                    "temperature",
                    DEFAULT_QUERY_ANALYSIS_TEMPERATURE,
                ),
            ),
            response_format=_string(
                "query_analysis.response_format",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_QUERY_ANALYSIS_RESPONSE_FORMAT",
                    query_analysis_data,
                    "response_format",
                    DEFAULT_QUERY_ANALYSIS_RESPONSE_FORMAT,
                ),
            ),
            prompt_version=_string(
                "query_analysis.prompt_version",
                _env_or_value(
                    env_values,
                    "MEMORYCORE_QUERY_ANALYSIS_PROMPT_VERSION",
                    query_analysis_data,
                    "prompt_version",
                    DEFAULT_QUERY_ANALYSIS_PROMPT_VERSION,
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


def _finite_float(name: str, value: Any) -> float:
    if isinstance(value, bool):
        raise ValueError(f"{name} must be a finite float")
    if isinstance(value, str):
        try:
            value = float(value)
        except ValueError as exc:
            raise ValueError(f"{name} must be a finite float") from exc
    if not isinstance(value, (int, float)):
        raise ValueError(f"{name} must be a finite float")
    value = float(value)
    if not math.isfinite(value):
        raise ValueError(f"{name} must be a finite float")
    return value


def _bool(name: str, value: Any) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        normalized = value.strip().lower()
        if normalized in {"1", "true", "yes", "on"}:
            return True
        if normalized in {"0", "false", "no", "off"}:
            return False
    raise ValueError(f"{name} must be a boolean")


def _resolve_dir(value: str, base_dir: Path) -> Path:
    path = Path(value)
    if path.is_absolute():
        return path.resolve()
    return (base_dir / path).resolve()


def _resolve_path(value: str, base_dir: Path) -> Path:
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
    _validate_https_or_loopback_http_url(
        "embedding.base_url", config.embedding.base_url
    )
    if config.embedding.encoding_format != "float":
        raise ValueError("embedding.encoding_format must be float")
    if config.embedding_cache.mode not in _VALID_EMBEDDING_CACHE_MODES:
        raise ValueError(
            "embedding_cache.mode must be one of off, read_write, read_only, refresh"
        )
    if config.rerank.provider not in _VALID_RERANK_PROVIDERS:
        raise ValueError("rerank.provider must be one of none, dashscope-vl")
    _validate_https_or_loopback_http_url(
        "rerank.endpoint_url", config.rerank.endpoint_url
    )
    if config.query_analysis.provider not in _VALID_QUERY_ANALYSIS_PROVIDERS:
        raise ValueError(
            "query_analysis.provider must be one of none, openai-compatible"
        )
    _validate_https_or_loopback_http_url(
        "query_analysis.base_url", config.query_analysis.base_url
    )
    if config.query_analysis.response_format != "json_object":
        raise ValueError("query_analysis.response_format must be json_object")
    if config.query_analysis.temperature != 0.0:
        raise ValueError("query_analysis.temperature must be 0")


def _validate_https_or_loopback_http_url(name: str, value: str) -> None:
    parsed = urlparse(value)
    scheme = parsed.scheme.lower()
    hostname = parsed.hostname.lower() if parsed.hostname is not None else ""
    if scheme == "https":
        return
    if scheme == "http" and hostname in _LOOPBACK_HTTP_HOSTS:
        return
    raise ValueError(f"{name} must use https unless it is loopback http")
