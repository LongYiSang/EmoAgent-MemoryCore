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


def test_load_config_resolves_relative_trivium_dir_from_config_file(tmp_path):
    config_path = tmp_path / "conf" / "sidecar.toml"
    config_path.parent.mkdir()
    config_path.write_text("[trivium]\ndir = \"mirror\"\n", encoding="utf-8")

    config = load_config(config_path, env={})

    assert config.trivium.dir == (config_path.parent / "mirror").resolve()


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


def test_load_config_rejects_non_loopback_http_embedding_base_url(tmp_path):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text(
        "[embedding]\nbase_url = 'http://example.test/v1'\n",
        encoding="utf-8",
    )

    with pytest.raises(ValueError, match="embedding.base_url"):
        load_config(config_path, env={})


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
    ],
)
def test_load_config_rejects_invalid_values(tmp_path, body, message):
    config_path = tmp_path / "sidecar.toml"
    config_path.write_text(body, encoding="utf-8")

    with pytest.raises(ValueError, match=message):
        load_config(config_path, env={})
