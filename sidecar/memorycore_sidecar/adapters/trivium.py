from __future__ import annotations

import hashlib
import math
import re
import threading
from pathlib import Path
from typing import Any

from memorycore_sidecar.config import SidecarConfig, load_config
from memorycore_sidecar.embedding import (
    EmbeddingProvider,
    build_embedding_provider,
)

from .base import MirrorAdapter

_MAX_TRIVIUM_ID = (1 << 63) - 1


class TriviumAdapter(MirrorAdapter):
    def __init__(
        self,
        config: SidecarConfig | None = None,
        embedding_provider: EmbeddingProvider | None = None,
    ) -> None:
        self.config = load_config() if config is None else config
        self.embedding_provider = (
            build_embedding_provider(self.config.embedding)
            if embedding_provider is None
            else embedding_provider
        )
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
        vector = self.embedding_provider.embed(searchable_text)
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
        trivium_node_id = _stable_node_id(
            persona_id,
            str(node["node_type"]),
            str(node["sqlite_node_id"]),
        )
        with self._lock:
            db = self._db_for_persona(persona_id)
            if db.get(trivium_node_id) is not None:
                db.delete(trivium_node_id)
                db.flush()
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
                return {}

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

    def clear_namespace(self, persona_id: str) -> dict[str, Any]:
        with self._lock:
            self._close_persona(persona_id)
            db_path = self._db_path_for_persona(persona_id)
            for path in db_path.parent.glob(db_path.name + "*"):
                if path.is_file():
                    path.unlink(missing_ok=True)
        return {}

    def find_candidates(self, request: dict[str, Any]) -> dict[str, Any]:
        query_vector = self.embedding_provider.embed(str(request["query_text"]))
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
        return {"candidates": candidates, "degraded": False}

    def close(self) -> None:
        with self._lock:
            for persona_id in list(self._dbs):
                self._close_persona(persona_id)

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
        with self._lock:
            db = self._dbs.pop(persona_id, None)
            if db is not None:
                db.close()


def _stable_node_id(persona_id: str, node_type: str, sqlite_node_id: str) -> int:
    digest = hashlib.sha256()
    for part in (persona_id, node_type, sqlite_node_id):
        digest.update(part.encode("utf-8"))
        digest.update(b"\x00")
    value = int.from_bytes(digest.digest()[:8], "big") & _MAX_TRIVIUM_ID
    return value or 1


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
