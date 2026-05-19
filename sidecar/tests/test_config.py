from pathlib import Path

import pytest

from memorycore_sidecar.config import load_config


def test_load_config_without_file_uses_defaults_relative_to_sidecar_project(monkeypatch, tmp_path):
    monkeypatch.chdir(tmp_path)

    config = load_config(env={})

    assert config.trivium.dir == (Path(__file__).parents[2] / "data/trivium").resolve()
    assert config.trivium.dtype == "f32"
    assert config.trivium.sync_mode == "normal"
    assert config.embedding.provider == "openai-compatible"
    assert config.embedding.base_url == "https://dashscope.aliyuncs.com/compatible-mode/v1"
    assert config.embedding.api_key_env == "DASHSCOPE_API_KEY"
    assert config.embedding.model == "text-embedding-v4"
    assert config.embedding.dimensions == 1024
    assert config.embedding.timeout_seconds == 30
    assert config.embedding.encoding_format == "float"
    assert config.embedding_cache.mode == "off"
    assert config.embedding_cache.db_path == (
        Path(__file__).parents[2] / "data/embedding_cache.sqlite3"
    ).resolve()
    assert config.embedding_cache.text_normalization_version == "v1"
    assert config.embedding_cache.searchable_text_version == "v1"
    assert config.embedding_cache.ttl_days_for_query == 14
    assert config.embedding_cache.store_raw_text is False
    assert config.rerank.provider == "none"
    assert (
        config.rerank.endpoint_url
        == "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"
    )
    assert config.rerank.api_key_env == "DASHSCOPE_API_KEY"
    assert config.rerank.model == "qwen3-vl-rerank"
    assert config.rerank.timeout_seconds == 30
    assert config.rerank.top_n == 30
    assert (
        config.rerank.instruct
        == "Retrieve semantically relevant safe memory summaries for the user's query."
    )
    assert config.query_analysis.provider == "none"
    assert config.query_analysis.base_url == "https://dashscope.aliyuncs.com/compatible-mode/v1"
    assert config.query_analysis.api_key_env == "DASHSCOPE_API_KEY"
    assert config.query_analysis.model == "qwen-plus"
    assert config.query_analysis.timeout_seconds == 30
    assert config.query_analysis.max_tokens == 768
    assert config.query_analysis.temperature == 0.0
    assert config.query_analysis.response_format == "json_object"
    assert config.query_analysis.prompt_version == "query-analysis-v0.1"


def test_load_config_reads_example_defaults():
    config = load_config(Path(__file__).parents[1] / "config.example.toml", env={})

    assert config.trivium.dir == (Path(__file__).parents[2] / "data/trivium").resolve()
    assert config.trivium.dtype == "f32"
    assert config.trivium.sync_mode == "normal"
    assert config.embedding.provider == "openai-compatible"
    assert config.embedding.base_url == "https://dashscope.aliyuncs.com/compatible-mode/v1"
    assert config.embedding.api_key_env == "DASHSCOPE_API_KEY"
    assert config.embedding.model == "text-embedding-v4"
    assert config.embedding.dimensions == 1024
    assert config.embedding.timeout_seconds == 30
    assert config.embedding.encoding_format == "float"
    assert config.embedding_cache.mode == "off"
    assert config.embedding_cache.db_path == (
        Path(__file__).parents[2] / "data/embedding_cache.sqlite3"
    ).resolve()
    assert config.embedding_cache.text_normalization_version == "v1"
    assert config.embedding_cache.searchable_text_version == "v1"
    assert config.embedding_cache.ttl_days_for_query == 14
    assert config.embedding_cache.store_raw_text is False
    assert config.rerank.provider == "none"
    assert (
        config.rerank.endpoint_url
        == "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"
    )
    assert config.rerank.api_key_env == "DASHSCOPE_API_KEY"
    assert config.rerank.model == "qwen3-vl-rerank"
    assert config.rerank.timeout_seconds == 30
    assert config.rerank.top_n == 30
    assert (
        config.rerank.instruct
        == "Retrieve semantically relevant safe memory summaries for the user's query."
    )
    assert config.query_analysis.provider == "none"
    assert config.query_analysis.base_url == "https://dashscope.aliyuncs.com/compatible-mode/v1"
    assert config.query_analysis.api_key_env == "DASHSCOPE_API_KEY"
    assert config.query_analysis.model == "qwen-plus"
    assert config.query_analysis.timeout_seconds == 30
    assert config.query_analysis.max_tokens == 768
    assert config.query_analysis.temperature == 0.0
    assert config.query_analysis.response_format == "json_object"
    assert config.query_analysis.prompt_version == "query-analysis-v0.1"


def test_load_config_resolves_relative_trivium_dir_from_config_file(tmp_path):
    config_path = tmp_path / "conf" / "sidecar.toml"
    config_path.parent.mkdir()
    config_path.write_text("[trivium]\ndir = \"mirror\"\n", encoding="utf-8")

    config = load_config(config_path, env={})

    assert config.trivium.dir == (config_path.parent / "mirror").resolve()


def test_load_config_resolves_relative_embedding_cache_db_path_from_config_file(
    tmp_path,
):
    config_path = tmp_path / "conf" / "sidecar.toml"
    config_path.parent.mkdir()
    config_path.write_text(
        "[embedding_cache]\ndb_path = \"cache/embeddings.sqlite3\"\n",
        encoding="utf-8",
    )

    config = load_config(config_path, env={})

    assert config.embedding_cache.db_path == (
        config_path.parent / "cache/embeddings.sqlite3"
    ).resolve()


def test_load_config_applies_environment_overrides(tmp_path):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text(
        "[trivium]\ndir = \"from-file\"\n\n"
        "[embedding]\nmodel = \"file-model\"\ndimensions = 128\n",
        encoding="utf-8",
    )
    env = {
        "MEMORYCORE_TRIVIUM_DIR": "from-env",
        "MEMORYCORE_TRIVIUM_DTYPE": "f16",
        "MEMORYCORE_TRIVIUM_SYNC_MODE": "full",
        "MEMORYCORE_EMBEDDING_PROVIDER": "openai-compatible",
        "MEMORYCORE_EMBEDDING_BASE_URL": "https://example.test/v1",
        "MEMORYCORE_EMBEDDING_API_KEY_ENV": "EXAMPLE_KEY",
        "MEMORYCORE_EMBEDDING_MODEL": "env-model",
        "MEMORYCORE_EMBEDDING_DIMENSIONS": "256",
        "MEMORYCORE_EMBEDDING_TIMEOUT_SECONDS": "45",
        "MEMORYCORE_EMBEDDING_ENCODING_FORMAT": "float",
        "MEMORYCORE_EMBEDDING_CACHE_MODE": "read_write",
        "MEMORYCORE_EMBEDDING_CACHE_DB_PATH": "cache-from-env.sqlite3",
        "MEMORYCORE_EMBEDDING_CACHE_TEXT_NORMALIZATION_VERSION": "norm-env",
        "MEMORYCORE_EMBEDDING_CACHE_SEARCHABLE_TEXT_VERSION": "search-env",
        "MEMORYCORE_EMBEDDING_CACHE_TTL_DAYS_FOR_QUERY": "3",
        "MEMORYCORE_EMBEDDING_CACHE_STORE_RAW_TEXT": "true",
        "MEMORYCORE_RERANK_PROVIDER": "dashscope-vl",
        "MEMORYCORE_RERANK_ENDPOINT_URL": "https://example.test/rerank",
        "MEMORYCORE_RERANK_API_KEY_ENV": "RERANK_KEY",
        "MEMORYCORE_RERANK_MODEL": "rerank-model",
        "MEMORYCORE_RERANK_TIMEOUT_SECONDS": "12",
        "MEMORYCORE_RERANK_TOP_N": "7",
        "MEMORYCORE_RERANK_INSTRUCT": "Find safe memories.",
        "MEMORYCORE_QUERY_ANALYSIS_PROVIDER": "openai-compatible",
        "MEMORYCORE_QUERY_ANALYSIS_BASE_URL": "https://example.test/qa/v1",
        "MEMORYCORE_QUERY_ANALYSIS_API_KEY_ENV": "QUERY_KEY",
        "MEMORYCORE_QUERY_ANALYSIS_MODEL": "query-model",
        "MEMORYCORE_QUERY_ANALYSIS_TIMEOUT_SECONDS": "13",
        "MEMORYCORE_QUERY_ANALYSIS_MAX_TOKENS": "321",
        "MEMORYCORE_QUERY_ANALYSIS_TEMPERATURE": "0",
        "MEMORYCORE_QUERY_ANALYSIS_RESPONSE_FORMAT": "json_object",
        "MEMORYCORE_QUERY_ANALYSIS_PROMPT_VERSION": "qa-v-test",
    }

    config = load_config(config_path, env=env)

    assert config.trivium.dir == (config_path.parent / "from-env").resolve()
    assert config.trivium.dtype == "f16"
    assert config.trivium.sync_mode == "full"
    assert config.embedding.provider == "openai-compatible"
    assert config.embedding.base_url == "https://example.test/v1"
    assert config.embedding.api_key_env == "EXAMPLE_KEY"
    assert config.embedding.model == "env-model"
    assert config.embedding.dimensions == 256
    assert config.embedding.timeout_seconds == 45
    assert config.embedding.encoding_format == "float"
    assert config.embedding_cache.mode == "read_write"
    assert config.embedding_cache.db_path == (
        config_path.parent / "cache-from-env.sqlite3"
    ).resolve()
    assert config.embedding_cache.text_normalization_version == "norm-env"
    assert config.embedding_cache.searchable_text_version == "search-env"
    assert config.embedding_cache.ttl_days_for_query == 3
    assert config.embedding_cache.store_raw_text is True
    assert config.rerank.provider == "dashscope-vl"
    assert config.rerank.endpoint_url == "https://example.test/rerank"
    assert config.rerank.api_key_env == "RERANK_KEY"
    assert config.rerank.model == "rerank-model"
    assert config.rerank.timeout_seconds == 12
    assert config.rerank.top_n == 7
    assert config.rerank.instruct == "Find safe memories."
    assert config.query_analysis.provider == "openai-compatible"
    assert config.query_analysis.base_url == "https://example.test/qa/v1"
    assert config.query_analysis.api_key_env == "QUERY_KEY"
    assert config.query_analysis.model == "query-model"
    assert config.query_analysis.timeout_seconds == 13
    assert config.query_analysis.max_tokens == 321
    assert config.query_analysis.temperature == 0.0
    assert config.query_analysis.response_format == "json_object"
    assert config.query_analysis.prompt_version == "qa-v-test"


def test_load_config_does_not_support_rerank_return_documents_override(tmp_path):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text("[rerank]\nprovider = 'dashscope-vl'\n", encoding="utf-8")

    config = load_config(
        config_path,
        env={"MEMORYCORE_RERANK_RETURN_DOCUMENTS": "true"},
    )

    assert not hasattr(config.rerank, "return_documents")


@pytest.mark.parametrize(
    "base_url",
    [
        "http://localhost:8000/v1",
        "http://127.0.0.1:8000/v1",
        "http://[::1]:8000/v1",
    ],
)
def test_load_config_allows_loopback_http_embedding_base_url(tmp_path, base_url):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text(f"[embedding]\nbase_url = '{base_url}'\n", encoding="utf-8")

    config = load_config(config_path, env={})

    assert config.embedding.base_url == base_url


@pytest.mark.parametrize(
    "endpoint_url",
    [
        "http://localhost:8000/rerank",
        "http://127.0.0.1:8000/rerank",
        "http://[::1]:8000/rerank",
    ],
)
def test_load_config_allows_loopback_http_rerank_endpoint_url(tmp_path, endpoint_url):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text(
        f"[rerank]\nprovider = 'dashscope-vl'\nendpoint_url = '{endpoint_url}'\n",
        encoding="utf-8",
    )

    config = load_config(config_path, env={})

    assert config.rerank.endpoint_url == endpoint_url


def test_load_config_rejects_non_loopback_http_rerank_endpoint_url(tmp_path):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text(
        "[rerank]\nprovider = 'dashscope-vl'\nendpoint_url = 'http://example.test/rerank'\n",
        encoding="utf-8",
    )

    with pytest.raises(ValueError, match="rerank.endpoint_url"):
        load_config(config_path, env={})


def test_load_config_rejects_non_loopback_http_embedding_base_url(tmp_path):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text(
        "[embedding]\nbase_url = 'http://example.test/v1'\n",
        encoding="utf-8",
    )

    with pytest.raises(ValueError, match="embedding.base_url"):
        load_config(config_path, env={})


def test_load_config_allows_loopback_http_query_analysis_base_url(tmp_path):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text(
        "[query_analysis]\nprovider = 'openai-compatible'\nbase_url = 'http://localhost:8000/v1'\n",
        encoding="utf-8",
    )

    config = load_config(config_path, env={})

    assert config.query_analysis.base_url == "http://localhost:8000/v1"


@pytest.mark.parametrize(
    ("body", "message"),
    [
        ("[trivium]\ndir = ''\n", "trivium.dir"),
        ("[trivium]\ndtype = 'f64'\n", "trivium.dtype"),
        ("[trivium]\nsync_mode = 'fast'\n", "trivium.sync_mode"),
        ("[embedding]\nprovider = 'other'\n", "embedding.provider"),
        ("[embedding]\nbase_url = ''\n", "embedding.base_url"),
        ("[embedding]\napi_key_env = ''\n", "embedding.api_key_env"),
        ("[embedding]\nmodel = ''\n", "embedding.model"),
        ("[embedding]\ndimensions = 0\n", "embedding.dimensions"),
        ("[embedding]\ntimeout_seconds = 0\n", "embedding.timeout_seconds"),
        ("[embedding]\nencoding_format = 'base64'\n", "embedding.encoding_format"),
        ("[embedding_cache]\nmode = 'invalid'\n", "embedding_cache.mode"),
        ("[embedding_cache]\ndb_path = ''\n", "embedding_cache.db_path"),
        (
            "[embedding_cache]\ntext_normalization_version = ''\n",
            "embedding_cache.text_normalization_version",
        ),
        (
            "[embedding_cache]\nsearchable_text_version = ''\n",
            "embedding_cache.searchable_text_version",
        ),
        (
            "[embedding_cache]\nttl_days_for_query = 0\n",
            "embedding_cache.ttl_days_for_query",
        ),
        (
            "[embedding_cache]\nstore_raw_text = 'maybe'\n",
            "embedding_cache.store_raw_text",
        ),
        ("[rerank]\nprovider = 'other'\n", "rerank.provider"),
        ("[rerank]\nendpoint_url = ''\n", "rerank.endpoint_url"),
        ("[rerank]\napi_key_env = ''\n", "rerank.api_key_env"),
        ("[rerank]\nmodel = ''\n", "rerank.model"),
        ("[rerank]\ntimeout_seconds = 0\n", "rerank.timeout_seconds"),
        ("[rerank]\ntop_n = 0\n", "rerank.top_n"),
        ("[rerank]\ninstruct = ''\n", "rerank.instruct"),
        ("[query_analysis]\nprovider = 'other'\n", "query_analysis.provider"),
        ("[query_analysis]\nbase_url = ''\n", "query_analysis.base_url"),
        ("[query_analysis]\napi_key_env = ''\n", "query_analysis.api_key_env"),
        ("[query_analysis]\nmodel = ''\n", "query_analysis.model"),
        ("[query_analysis]\ntimeout_seconds = 0\n", "query_analysis.timeout_seconds"),
        ("[query_analysis]\nmax_tokens = 0\n", "query_analysis.max_tokens"),
        ("[query_analysis]\ntemperature = 'hot'\n", "query_analysis.temperature"),
        ("[query_analysis]\ntemperature = 0.1\n", "query_analysis.temperature"),
        ("[query_analysis]\nresponse_format = 'text'\n", "query_analysis.response_format"),
        ("[query_analysis]\nprompt_version = ''\n", "query_analysis.prompt_version"),
    ],
)
def test_load_config_rejects_invalid_values(tmp_path, body, message):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text(body, encoding="utf-8")

    with pytest.raises(ValueError, match=message):
        load_config(config_path, env={})
