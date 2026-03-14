"""
Search sub-client — unified hybrid search across graph objects and text chunks.

Endpoints covered
-----------------
POST /api/search/unified
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional

from ._base import BaseClient


class SearchClient(BaseClient):
    """
    Client for the unified Search API.

    Combines full-text search, vector similarity, and relationship search
    into a single ranked result list.

    Example::

        results = client.search.search(
            query="vector database",
            strategy="hybrid",
            result_types="graph",
            limit=10,
        )
        for item in results["results"]:
            print(item["score"], item.get("key"), item.get("object_type"))
    """

    def search(
        self,
        query: str,
        limit: int = 20,
        strategy: Optional[str] = None,
        result_types: Optional[str] = None,
        fusion_strategy: Optional[str] = None,
        include_debug: bool = False,
    ) -> Dict[str, Any]:
        """
        Unified search across the knowledge graph.

        POST /api/search/unified

        Parameters
        ----------
        query:
            Natural-language search query.
        limit:
            Maximum number of results to return.
        strategy:
            ``"hybrid"`` (default), ``"semantic"``, or ``"keyword"``.
        result_types:
            ``"graph"``, ``"text"``, or ``"both"``.
        fusion_strategy:
            ``"weighted"``, ``"rrf"``, ``"interleave"``,
            ``"graph_first"``, or ``"text_first"``.
        include_debug:
            If ``True``, include per-channel scoring details.

        Returns
        -------
        dict
            ``{"results": [...], "metadata": {...}}``.
        """
        body: Dict[str, Any] = {"query": query, "limit": limit}
        if strategy:
            body["strategy"] = strategy
        if result_types:
            body["resultTypes"] = result_types
        if fusion_strategy:
            body["fusionStrategy"] = fusion_strategy
        if include_debug:
            body["includeDebug"] = True
        return self._post("/api/search/unified", json=body)

    def graph_search(
        self,
        query: str,
        limit: int = 20,
        **kwargs: Any,
    ) -> List[Dict[str, Any]]:
        """
        Convenience: search graph objects only and return the result list.
        """
        resp = self.search(query, limit=limit, result_types="graph", **kwargs)
        return resp.get("results", [])

    def text_search(
        self,
        query: str,
        limit: int = 20,
        **kwargs: Any,
    ) -> List[Dict[str, Any]]:
        """
        Convenience: search text chunks only and return the result list.
        """
        resp = self.search(query, limit=limit, result_types="text", **kwargs)
        return resp.get("results", [])
