from __future__ import annotations

import hashlib
import importlib.metadata
import json
import math
import os
import re
import threading
from dataclasses import replace
from pathlib import Path
from typing import Any

from memorycore_sidecar.activation import ActivationEdge, activate_graph_with_diagnostics
from memorycore_sidecar.config import SidecarConfig, load_config
from memorycore_sidecar.embedding import (
    CachedEmbeddingProvider,
    EmbeddingProvider,
    build_embedding_provider,
)

from .base import MirrorAdapter

_MAX_TRIVIUM_ID = (1 << 63) - 1
_TRIVIUM_ADAPTER_VERSION = "memorycore_sidecar.trivium.v1"
_VALID_EMBEDDING_CACHE_MODES = {"off", "read_write", "read_only", "refresh"}


class TriviumAdapter(MirrorAdapter):
    def __init__(
        self,
        config: SidecarConfig | None = None,
        embedding_provider: EmbeddingProvider | None = None,
    ) -> None:
        self.config = load_config() if config is None else config
        embedding_provider = (
            build_embedding_provider(self.config.embedding)
            if embedding_provider is None
            else embedding_provider
        )
        self._live_embedding_provider = embedding_provider
        self.embedding_provider = self._wrap_embedding_provider(embedding_provider)
        from memorycore_sidecar.rerank import build_rerank_provider

        self.rerank_provider = build_rerank_provider(self.config.rerank)
        try:
            import triviumdb
        except ImportError as exc:
            raise RuntimeError("TriviumAdapter requires the triviumdb package") from exc
        self._triviumdb = triviumdb
        self._lock = threading.RLock()
        self._dbs: dict[str, Any] = {}
        self.config.trivium.dir.mkdir(parents=True, exist_ok=True)

    def upsert_node(self, node: dict[str, Any]) -> dict[str, Any]:
        persona_id = str(node["persona_id"])
        node_type = str(node["node_type"])
        sqlite_node_id = str(node["sqlite_node_id"])
        searchable_text = str(node.get("searchable_text", ""))
        trivium_node_id = _stable_node_id(persona_id, node_type, sqlite_node_id)
        vector = _embed_with_optional_ref(
            self.embedding_provider,
            searchable_text,
            {
                "kind": "node",
                "id": sqlite_node_id,
                "persona_id": persona_id,
                "node_type": node_type,
                "node_id": sqlite_node_id,
            },
        )
        payload = {
            "persona_id": persona_id,
            "node_type": node_type,
            "sqlite_node_id": sqlite_node_id,
            "searchable_text": searchable_text,
            "payload": node.get("payload", {}),
        }

        with self._lock:
            db = self._db_for_persona(persona_id)
            if db.get(trivium_node_id) is None:
                try:
                    db.insert_with_id(trivium_node_id, vector, payload)
                except RuntimeError:
                    db.update_payload(trivium_node_id, payload)
                    db.update_vector(vector, trivium_node_id)
            else:
                db.update_payload(trivium_node_id, payload)
                db.update_vector(vector, trivium_node_id)
            db.flush()
        return {"trivium_node_id": trivium_node_id}

    def delete_node(self, node: dict[str, Any]) -> dict[str, Any]:
        persona_id = str(node["persona_id"])
        node_type = str(node["node_type"])
        sqlite_node_id = str(node["sqlite_node_id"])
        trivium_node_id = _stable_node_id(
            persona_id,
            node_type,
            sqlite_node_id,
        )
        with self._lock:
            db = self._db_for_persona(persona_id)
            if db.get(trivium_node_id) is not None:
                db.delete(trivium_node_id)
                db.flush()
        _delete_embedding_node_ref(
            self.embedding_provider,
            persona_id,
            node_type,
            sqlite_node_id,
        )
        return {}

    def upsert_edge(self, edge: dict[str, Any]) -> dict[str, Any]:
        persona_id = str(edge["persona_id"])
        source_id = _edge_endpoint_id(edge, persona_id, "from")
        target_id = _edge_endpoint_id(edge, persona_id, "to")
        if source_id is None or target_id is None:
            return {}

        with self._lock:
            db = self._db_for_persona(persona_id)
            if db.get(source_id) is None or db.get(target_id) is None:
                raise RuntimeError("upsert_edge endpoint is not indexed in mirror")

            label = _edge_label(edge)
            for existing in db.get_edges(source_id):
                if existing.target_id == target_id and existing.label == label:
                    return {}

            db.link(source_id, target_id, label=label, weight=_edge_weight(edge))
            db.flush()
        return {}

    def delete_edge(self, edge: dict[str, Any]) -> dict[str, Any]:
        persona_id = str(edge["persona_id"])
        source_id = _edge_endpoint_id(edge, persona_id, "from")
        target_id = _edge_endpoint_id(edge, persona_id, "to")
        label = _edge_label(edge)
        if source_id is None or target_id is None:
            return {}

        with self._lock:
            db = self._db_for_persona(persona_id)
            unlink = _resolve_unlink(db)
            if unlink is None:
                raise RuntimeError(
                    "delete_edge requires mirror rebuild: TriviumDB adapter has no unlink API"
                )
            if db.get(source_id) is None or db.get(target_id) is None:
                return {}

            _call_unlink(unlink, source_id, target_id, label)
            db.flush()
        return {}

    def clear_namespace(
        self, persona_id: str, purge_embedding_cache: bool = False
    ) -> dict[str, Any]:
        self._close_db_handles([self._pop_persona_db(persona_id)])
        with self._lock:
            db_path = self._db_path_for_persona(persona_id)
            for path in db_path.parent.glob(db_path.name + "*"):
                if path.is_file():
                    path.unlink(missing_ok=True)
        if purge_embedding_cache:
            _clear_embedding_namespace_refs(self.embedding_provider, persona_id)
        return {}

    def configure_eval(self, request: dict[str, Any]) -> dict[str, Any]:
        trivium_dir = request.get("trivium_dir")
        embedding_cache_mode = request.get("embedding_cache_mode")
        embedding_cache_db_path = request.get("embedding_cache_db_path")
        searchable_text_version = request.get("searchable_text_version")
        text_normalization_version = request.get("text_normalization_version")
        with self._lock:
            self._close_db_handles(list(self._dbs.values()))
            self._dbs.clear()
            trivium = self.config.trivium
            embedding_cache = self.config.embedding_cache
            if isinstance(trivium_dir, str) and trivium_dir.strip():
                trivium = replace(trivium, dir=Path(trivium_dir).resolve())
                trivium.dir.mkdir(parents=True, exist_ok=True)
            if isinstance(embedding_cache_mode, str) and embedding_cache_mode.strip():
                normalized_mode = embedding_cache_mode.strip()
                if normalized_mode not in _VALID_EMBEDDING_CACHE_MODES:
                    raise ValueError(
                        "embedding_cache.mode must be one of off, read_write, read_only, refresh"
                    )
                embedding_cache = replace(
                    embedding_cache,
                    mode=normalized_mode,
                )
            if isinstance(embedding_cache_db_path, str) and embedding_cache_db_path.strip():
                embedding_cache = replace(
                    embedding_cache,
                    db_path=Path(embedding_cache_db_path).resolve(),
                )
                embedding_cache.db_path.parent.mkdir(parents=True, exist_ok=True)
            if isinstance(searchable_text_version, str) and searchable_text_version.strip():
                embedding_cache = replace(
                    embedding_cache,
                    searchable_text_version=searchable_text_version.strip(),
                )
            if isinstance(text_normalization_version, str) and text_normalization_version.strip():
                embedding_cache = replace(
                    embedding_cache,
                    text_normalization_version=text_normalization_version.strip(),
                )
            self.config = replace(
                self.config,
                trivium=trivium,
                embedding_cache=embedding_cache,
            )
            self.embedding_provider = self._wrap_embedding_provider(
                self._live_embedding_provider
            )
            mirror_stats = self._mirror_stats_unlocked()
            rerank_capability = _rerank_capability(self.config)
        return {
            "trivium_dir": str(self.config.trivium.dir.resolve()),
            "embedding_cache_mode": self.config.embedding_cache.mode,
            "embedding_cache_db_path": str(self.config.embedding_cache.db_path.resolve()),
            "embedding": _embedding_identity(self.config),
            "trivium_adapter_version": _TRIVIUM_ADAPTER_VERSION,
            "triviumdb_version": _triviumdb_version(self._triviumdb),
            **rerank_capability,
            **mirror_stats,
        }

    def find_candidates(self, request: dict[str, Any]) -> dict[str, Any]:
        _cleanup_expired_embedding_query_refs(self.embedding_provider)
        query_ref = None
        request_id = request.get("request_id")
        if isinstance(request_id, str) and request_id.strip():
            query_ref = {
                "kind": "query",
                "id": request_id.strip(),
                "persona_id": str(request["persona_id"]),
            }
        query_vector = _embed_with_optional_ref(
            self.embedding_provider,
            str(request["query_text"]),
            query_ref,
        )
        with self._lock:
            db = self._db_for_persona(str(request["persona_id"]))
            hits = list(
                db.search(
                    query_vector,
                    top_k=int(request["limit"]),
                    expand_depth=0,
                    min_score=0.0,
                    payload_filter=None,
                )
            )
        candidates = []
        for hit in hits:
            trivium_node_id = getattr(hit, "id", None)
            score = getattr(hit, "score", None)
            if not isinstance(trivium_node_id, int) or trivium_node_id <= 0:
                continue
            try:
                score_float = float(score)
            except (TypeError, ValueError):
                continue
            if not math.isfinite(score_float) or score_float <= 0:
                continue
            if score_float > 1.0:
                score_float = 1.0
            candidates.append(
                {
                    "trivium_node_id": trivium_node_id,
                    "score": score_float,
                    "source": "trivium_vector",
                }
            )
        result = {
            "candidates": candidates,
            "degraded": False,
        }
        stats = self._embedding_cache_stats()
        if stats is not None:
            result["embedding_cache_stats"] = stats
        return result

    def activate_graph(self, request: dict[str, Any]) -> dict[str, Any]:
        persona_id = str(request["persona_id"])
        with self._lock:
            db = self._db_for_persona(persona_id)
            neighbor_cache: dict[int, list[ActivationEdge]] = {}

            def neighbors(trivium_node_id: int) -> list[ActivationEdge]:
                if trivium_node_id in neighbor_cache:
                    return neighbor_cache[trivium_node_id]
                if db.get(trivium_node_id) is None:
                    neighbor_cache[trivium_node_id] = []
                    return []
                out: list[ActivationEdge] = []
                for edge in db.get_edges(trivium_node_id):
                    target_id = getattr(edge, "target_id", None)
                    if not isinstance(target_id, int) or target_id <= 0:
                        continue
                    if db.get(target_id) is None:
                        continue
                    out.append(
                        ActivationEdge(
                            target_id=target_id,
                            link_type=str(getattr(edge, "label", "related")),
                            weight=_finite_edge_weight(getattr(edge, "weight", 1.0)),
                        )
                    )
                neighbor_cache[trivium_node_id] = out
                return out

            def degree(trivium_node_id: int) -> int:
                return len(neighbors(trivium_node_id))

            run = activate_graph_with_diagnostics(
                list(request.get("seeds", [])),
                neighbors=neighbors,
                degree=degree,
                params=request.get("params", {}),
            )
        result = {"candidates": run.candidates, "degraded": run.degraded}
        if run.fallback_reason:
            result["fallback_reason"] = run.fallback_reason
        return result

    def rerank(self, request: dict[str, Any]) -> dict[str, Any]:
        provider = getattr(self, "rerank_provider", None)
        if provider is None:
            return {
                "results": [],
                "degraded": True,
                "fallback_reason": "rerank_not_configured",
            }
        try:
            return provider.rerank(
                str(request["query_text"]),
                list(request.get("candidates", [])),
            )
        except Exception:
            return {
                "results": [],
                "degraded": True,
                "fallback_reason": "rerank_provider_error",
            }

    def close(self) -> None:
        with self._lock:
            dbs = list(self._dbs.values())
            self._dbs.clear()
        self._close_db_handles(dbs)

    def _db_for_persona(self, persona_id: str) -> Any:
        with self._lock:
            db = self._dbs.get(persona_id)
            if db is not None:
                return db
            path = self._db_path_for_persona(persona_id)
            db = self._triviumdb.TriviumDB(
                str(path),
                dim=self.config.embedding.dimensions,
                dtype=self.config.trivium.dtype,
                sync_mode=self.config.trivium.sync_mode,
            )
            self._dbs[persona_id] = db
            return db

    def _db_path_for_persona(self, persona_id: str) -> Path:
        root = self.config.trivium.dir.resolve()
        path = root / f"{_safe_persona_id(persona_id)}.tdb"
        if path.resolve().parent != root:
            raise ValueError("persona_id resolves outside trivium dir")
        return path

    def _close_persona(self, persona_id: str) -> None:
        self._close_db_handles([self._pop_persona_db(persona_id)])

    def _pop_persona_db(self, persona_id: str) -> Any | None:
        with self._lock:
            return self._dbs.pop(persona_id, None)

    def _close_db_handles(self, dbs: list[Any | None]) -> None:
        first_error: Exception | None = None
        for db in dbs:
            if db is None:
                continue
            try:
                db.close()
            except Exception as exc:
                if first_error is None:
                    first_error = exc
        if first_error is not None:
            raise first_error

    def _wrap_embedding_provider(
        self, embedding_provider: EmbeddingProvider
    ) -> EmbeddingProvider:
        if self.config.embedding_cache.mode == "off":
            return embedding_provider
        return CachedEmbeddingProvider(
            embedding_provider,
            self.config.embedding,
            self.config.embedding_cache,
        )

    def _embedding_cache_stats(self) -> dict[str, int] | None:
        stats = getattr(self.embedding_provider, "stats", None)
        if not isinstance(stats, dict):
            return None
        return {
            "hits": int(stats.get("hits", 0)),
            "misses": int(stats.get("misses", 0)),
            "live_call_count": int(stats.get("live_call_count", 0)),
        }

    def _mirror_stats_unlocked(self) -> dict[str, Any]:
        try:
            node_count = 0
            edge_count = 0
            root = self.config.trivium.dir.resolve()
            for path in sorted(root.glob("*.tdb")):
                if not path.is_file():
                    continue
                stats = self._trivium_file_stats(path)
                node_count += stats["node_count"]
                edge_count += stats["edge_count"]
            return {
                "mirror_stats_available": True,
                "mirror_node_count": node_count,
                "mirror_edge_count": edge_count,
            }
        except Exception as exc:
            return {
                "mirror_stats_available": False,
                "mirror_stats_error": type(exc).__name__,
                "mirror_node_count": 0,
                "mirror_edge_count": 0,
            }

    def _trivium_file_stats(self, path: Path) -> dict[str, int]:
        db = self._triviumdb.TriviumDB(
            str(path),
            dim=self.config.embedding.dimensions,
            dtype=self.config.trivium.dtype,
            sync_mode=self.config.trivium.sync_mode,
        )
        try:
            node_ids = _all_node_ids(db)
            node_count = _node_count(db, node_ids)
            edge_count = 0
            for node_id in node_ids:
                edge_count += len(list(db.get_edges(node_id)))
            return {"node_count": node_count, "edge_count": edge_count}
        finally:
            db.close()


def _stable_node_id(persona_id: str, node_type: str, sqlite_node_id: str) -> int:
    digest = hashlib.sha256()
    for part in (persona_id, node_type, sqlite_node_id):
        digest.update(part.encode("utf-8"))
        digest.update(b"\x00")
    value = int.from_bytes(digest.digest()[:8], "big") & _MAX_TRIVIUM_ID
    return value or 1


def _embedding_identity(config: SidecarConfig) -> dict[str, str]:
    identity = {
        "provider_kind": config.embedding.provider,
        "base_url_hash": _sha256_hex(config.embedding.base_url.rstrip("/")),
        "model": config.embedding.model,
        "dimensions": str(config.embedding.dimensions),
        "encoding_format": config.embedding.encoding_format,
        "text_normalization_version": config.embedding_cache.text_normalization_version,
        "searchable_text_version": config.embedding_cache.searchable_text_version,
    }
    identity["fingerprint"] = _sha256_hex(
        json.dumps(identity, sort_keys=True, separators=(",", ":"))
    )
    return identity


def _rerank_capability(config: SidecarConfig) -> dict[str, Any]:
    if config.rerank.provider == "none":
        return {
            "rerank_provider_available": False,
            "rerank_provider_mode": "none",
            "rerank_capability_reason": "rerank_provider_none",
            "rerank_cache": False,
        }
    api_key = os.environ.get(config.rerank.api_key_env, "")
    if not api_key.strip():
        return {
            "rerank_provider_available": False,
            "rerank_provider_mode": "missing_api_key",
            "rerank_capability_reason": "missing_api_key",
            "rerank_cache": False,
        }
    return {
        "rerank_provider_available": True,
        "rerank_provider_mode": "live",
        "rerank_cache": False,
    }


def _all_node_ids(db: Any) -> list[int]:
    all_node_ids = getattr(db, "all_node_ids", None)
    if not callable(all_node_ids):
        raise RuntimeError("TriviumDB all_node_ids API is unavailable")
    node_ids = all_node_ids()
    if not isinstance(node_ids, list):
        raise RuntimeError("TriviumDB all_node_ids returned a non-list value")
    out: list[int] = []
    for node_id in node_ids:
        if not isinstance(node_id, int) or node_id <= 0:
            raise RuntimeError("TriviumDB all_node_ids returned an invalid node id")
        out.append(node_id)
    return out


def _node_count(db: Any, node_ids: list[int]) -> int:
    node_count = getattr(db, "node_count", None)
    if callable(node_count):
        value = node_count()
        if isinstance(value, int) and value >= 0:
            return value
        raise RuntimeError("TriviumDB node_count returned an invalid value")
    return len(node_ids)


def _triviumdb_version(module: Any) -> str:
    for package_name in ("triviumdb", "trivium-db"):
        try:
            return importlib.metadata.version(package_name)
        except importlib.metadata.PackageNotFoundError:
            continue
    version = getattr(module, "__version__", None)
    if isinstance(version, str) and version.strip():
        return version.strip()
    return "unknown"


def _sha256_hex(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()


def _safe_persona_id(persona_id: str) -> str:
    digest = hashlib.sha256(persona_id.encode("utf-8")).hexdigest()[:12]
    safe = re.sub(r"[^A-Za-z0-9._-]+", "_", persona_id).strip("._-")
    if not safe:
        safe = "persona"
    return f"{safe[:48]}-{digest}"


def _edge_endpoint_id(edge: dict[str, Any], persona_id: str, prefix: str) -> int | None:
    node_type = edge.get(f"{prefix}_node_type")
    node_id = edge.get(f"{prefix}_node_id")
    if not node_type or not node_id:
        return None
    return _stable_node_id(persona_id, str(node_type), str(node_id))


def _edge_label(edge: dict[str, Any]) -> str:
    label = edge.get("link_type")
    if isinstance(label, str) and label.strip():
        return label
    return "related"


def _edge_weight(edge: dict[str, Any]) -> float:
    try:
        weight = float(edge.get("weight", 1.0))
    except (TypeError, ValueError):
        raise ValueError("edge weight must be a finite float") from None
    if not math.isfinite(weight):
        raise ValueError("edge weight must be a finite float")
    return weight


def _finite_edge_weight(weight: Any) -> float:
    try:
        value = float(weight)
    except (TypeError, ValueError):
        return 1.0
    if not math.isfinite(value):
        return 1.0
    return value


def _resolve_unlink(db: Any) -> Any | None:
    for name in ("unlink", "delete_edge", "remove_edge"):
        method = getattr(db, name, None)
        if callable(method):
            return method
    return None


def _call_unlink(unlink: Any, source_id: int, target_id: int, label: str) -> None:
    attempts = (
        ((source_id, target_id), {"label": label}),
        ((source_id, target_id, label), {}),
        ((source_id, target_id), {}),
    )
    last_error: TypeError | None = None
    for args, kwargs in attempts:
        try:
            unlink(*args, **kwargs)
            return
        except TypeError as exc:
            last_error = exc
    if last_error is not None:
        raise last_error


def _embed_with_optional_ref(
    provider: EmbeddingProvider,
    text: str,
    ref: dict[str, str] | None,
) -> list[float]:
    if ref is None:
        return provider.embed(text)
    try:
        return provider.embed(text, ref=ref)  # type: ignore[call-arg]
    except TypeError as exc:
        if "unexpected keyword argument" not in str(exc):
            raise
        return provider.embed(text)


def _delete_embedding_node_ref(
    provider: EmbeddingProvider,
    persona_id: str,
    node_type: str,
    node_id: str,
) -> None:
    delete_node_ref = getattr(provider, "delete_node_ref", None)
    if callable(delete_node_ref):
        delete_node_ref(persona_id, node_type, node_id)


def _clear_embedding_namespace_refs(
    provider: EmbeddingProvider,
    persona_id: str,
) -> None:
    clear_namespace_refs = getattr(provider, "clear_namespace_refs", None)
    if callable(clear_namespace_refs):
        clear_namespace_refs(persona_id)


def _cleanup_expired_embedding_query_refs(provider: EmbeddingProvider) -> None:
    cleanup_expired_query_refs = getattr(provider, "cleanup_expired_query_refs", None)
    if callable(cleanup_expired_query_refs):
        cleanup_expired_query_refs()
