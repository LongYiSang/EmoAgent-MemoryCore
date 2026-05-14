from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Any


class MirrorAdapter(ABC):
    def handle_operation(self, operation: str, payload: dict[str, Any]) -> dict[str, Any]:
        if operation == "upsert_node":
            return self.upsert_node(payload)
        if operation == "delete_node":
            return self.delete_node(payload)
        if operation == "upsert_edge":
            return self.upsert_edge(payload)
        if operation == "delete_edge":
            return self.delete_edge(payload)
        raise ValueError(f"unsupported operation: {operation}")

    @abstractmethod
    def upsert_node(self, node: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError

    @abstractmethod
    def delete_node(self, node: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError

    @abstractmethod
    def upsert_edge(self, edge: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError

    @abstractmethod
    def delete_edge(self, edge: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError

    @abstractmethod
    def clear_namespace(self, persona_id: str) -> dict[str, Any]:
        raise NotImplementedError

    @abstractmethod
    def find_candidates(self, request: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError

    @abstractmethod
    def activate_graph(self, request: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError

    @abstractmethod
    def rerank(self, request: dict[str, Any]) -> dict[str, Any]:
        raise NotImplementedError
