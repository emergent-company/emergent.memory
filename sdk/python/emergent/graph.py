"""
Graph sub-client — objects, relationships, search, traversal, and analytics.

Endpoints covered
-----------------
Object CRUD:
  POST   /api/graph/objects
  PUT    /api/graph/objects/upsert
  GET    /api/graph/objects/:id
  PATCH  /api/graph/objects/:id
  DELETE /api/graph/objects/:id
  POST   /api/graph/objects/:id/restore
  GET    /api/graph/objects/:id/history
  GET    /api/graph/objects/:id/edges
  GET    /api/graph/objects/:id/similar
  POST   /api/graph/objects/:id/move

Object search:
  GET    /api/graph/objects/search
  GET    /api/graph/objects/count
  GET    /api/graph/objects/fts
  POST   /api/graph/objects/vector-search
  GET    /api/graph/objects/tags

Bulk:
  POST   /api/graph/objects/bulk
  POST   /api/graph/objects/bulk-update-status

Relationships:
  POST   /api/graph/relationships
  GET    /api/graph/relationships/:id
  PATCH  /api/graph/relationships/:id
  DELETE /api/graph/relationships/:id
  GET    /api/graph/relationships/search
  POST   /api/graph/relationships/bulk
  POST   /api/graph/relationships/:id/restore
  GET    /api/graph/relationships/:id/history
  GET    /api/graph/relationships/count

Graph algorithms:
  POST   /api/graph/search
  POST   /api/graph/search-with-neighbors
  POST   /api/graph/expand
  POST   /api/graph/traverse

Analytics:
  GET    /api/graph/analytics/most-accessed
  GET    /api/graph/analytics/unused
"""
from __future__ import annotations

import json
from typing import Any, Dict, List, Optional
from urllib.parse import quote

from ._base import BaseClient


class GraphClient(BaseClient):
    """
    Client for the Graph API.

    The dual-ID model
    -----------------
    Every graph object has two identifiers:

    * **version_id** (``id`` in older responses) — changes on every update.
    * **entity_id** (``canonical_id`` in older responses) — stable across
      all versions.

    Store ``entity_id`` in references; use ``version_id`` only for the
    current operation.

    Example::

        # Create an object
        obj = client.graph.create_object({"type": "Person", "properties": {"name": "Alice"}})
        print(obj["entity_id"])  # stable ID

        # Hybrid search
        results = client.graph.hybrid_search({
            "query": "knowledge graph platform",
            "limit": 10,
        })
        for item in results["data"]:
            print(item["object"]["entity_id"], item["score"])
    """

    # ------------------------------------------------------------------
    # Object CRUD
    # ------------------------------------------------------------------

    def create_object(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Create a new graph object.

        POST /api/graph/objects

        Required key: ``type`` (string).
        Optional keys: ``key``, ``status``, ``properties``, ``labels``, ``branch_id``.
        """
        return self._post("/api/graph/objects", json=payload)

    def upsert_object(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Create or update an object by (type, key).

        PUT /api/graph/objects/upsert
        """
        return self._put("/api/graph/objects/upsert", json=payload)

    def get_object(self, object_id: str) -> Dict[str, Any]:
        """Get an object by its version ID or entity ID."""
        return self._get(f"/api/graph/objects/{quote(object_id, safe='')}")

    def update_object(self, object_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Patch an object — creates a new version.

        PATCH /api/graph/objects/:id

        WARNING: The returned object has a NEW ``id``/``version_id``.
        Do not reuse the old ID after calling this method.
        """
        return self._patch(f"/api/graph/objects/{quote(object_id, safe='')}", json=payload)

    def delete_object(self, object_id: str) -> None:
        """Soft-delete an object."""
        self._delete(f"/api/graph/objects/{quote(object_id, safe='')}")

    def restore_object(self, object_id: str) -> Dict[str, Any]:
        """Restore a soft-deleted object."""
        return self._post(f"/api/graph/objects/{quote(object_id, safe='')}/restore")

    def get_object_history(self, object_id: str) -> Dict[str, Any]:
        """Get the version history of an object."""
        return self._get(f"/api/graph/objects/{quote(object_id, safe='')}/history")

    def get_object_edges(
        self,
        object_id: str,
        type: Optional[str] = None,
        types: Optional[List[str]] = None,
        direction: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Get incoming and outgoing relationships for an object.

        GET /api/graph/objects/:id/edges

        Parameters
        ----------
        direction:
            ``"incoming"``, ``"outgoing"``, or ``None`` for both.
        """
        params: Dict[str, Any] = {}
        if type:
            params["type"] = type
        if types:
            params["types"] = ",".join(types)
        if direction:
            params["direction"] = direction
        return self._get(f"/api/graph/objects/{quote(object_id, safe='')}/edges", params=params)

    def find_similar(
        self,
        object_id: str,
        limit: int = 10,
        max_distance: Optional[float] = None,
        min_score: Optional[float] = None,
        type: Optional[str] = None,
        branch_id: Optional[str] = None,
        key_prefix: Optional[str] = None,
        labels_all: Optional[List[str]] = None,
        labels_any: Optional[List[str]] = None,
    ) -> List[Dict[str, Any]]:
        """
        Find objects similar to the given object by vector distance.

        GET /api/graph/objects/:id/similar
        """
        params: Dict[str, Any] = {"limit": limit}
        if max_distance is not None:
            params["maxDistance"] = max_distance
        if min_score is not None:
            params["minScore"] = min_score
        if type:
            params["type"] = type
        if branch_id:
            params["branchId"] = branch_id
        if key_prefix:
            params["keyPrefix"] = key_prefix
        if labels_all:
            params["labelsAll"] = ",".join(labels_all)
        if labels_any:
            params["labelsAny"] = ",".join(labels_any)
        return self._get(f"/api/graph/objects/{quote(object_id, safe='')}/similar", params=params)

    def move_object(
        self, object_id: str, target_branch_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """
        Move an object (and its relationships) to another branch.

        POST /api/graph/objects/:id/move

        Parameters
        ----------
        target_branch_id:
            Target branch ID.  Pass ``None`` to move to the main branch.

        Returns
        -------
        dict
            ``{"object": ..., "moved_relationships": [...]}``
        """
        return self._post(
            f"/api/graph/objects/{quote(object_id, safe='')}/move",
            json={"target_branch_id": target_branch_id},
        )

    # ------------------------------------------------------------------
    # Object search / list
    # ------------------------------------------------------------------

    def list_objects(
        self,
        type: Optional[str] = None,
        types: Optional[List[str]] = None,
        label: Optional[str] = None,
        labels: Optional[List[str]] = None,
        status: Optional[str] = None,
        key: Optional[str] = None,
        branch_id: Optional[str] = None,
        include_deleted: bool = False,
        limit: int = 50,
        cursor: Optional[str] = None,
        order: Optional[str] = None,
        related_to_id: Optional[str] = None,
        ids: Optional[List[str]] = None,
        extraction_job_id: Optional[str] = None,
        property_filters: Optional[List[Dict[str, Any]]] = None,
    ) -> Dict[str, Any]:
        """
        List / search graph objects.

        GET /api/graph/objects/search
        """
        params: Dict[str, Any] = {"limit": limit}
        if type:
            params["type"] = type
        if types:
            params["types"] = ",".join(types)
        if label:
            params["label"] = label
        if labels:
            params["labels"] = ",".join(labels)
        if status:
            params["status"] = status
        if key:
            params["key"] = key
        if branch_id:
            params["branch_id"] = branch_id
        if include_deleted:
            params["include_deleted"] = "true"
        if cursor:
            params["cursor"] = cursor
        if order:
            params["order"] = order
        if related_to_id:
            params["related_to_id"] = related_to_id
        if ids:
            params["ids"] = ",".join(ids)
        if extraction_job_id:
            params["extraction_job_id"] = extraction_job_id
        if property_filters:
            params["property_filters"] = json.dumps(property_filters)
        return self._get("/api/graph/objects/search", params=params)

    def count_objects(
        self,
        type: Optional[str] = None,
        types: Optional[List[str]] = None,
        label: Optional[str] = None,
        labels: Optional[List[str]] = None,
        status: Optional[str] = None,
        key: Optional[str] = None,
        branch_id: Optional[str] = None,
        include_deleted: bool = False,
        ids: Optional[List[str]] = None,
        property_filters: Optional[List[Dict[str, Any]]] = None,
    ) -> int:
        """Count objects matching the given filters. GET /api/graph/objects/count"""
        params: Dict[str, Any] = {}
        if type:
            params["type"] = type
        if types:
            params["types"] = ",".join(types)
        if label:
            params["label"] = label
        if labels:
            params["labels"] = ",".join(labels)
        if status:
            params["status"] = status
        if key:
            params["key"] = key
        if branch_id:
            params["branch_id"] = branch_id
        if include_deleted:
            params["include_deleted"] = "true"
        if ids:
            params["ids"] = ",".join(ids)
        if property_filters:
            params["property_filters"] = json.dumps(property_filters)
        data = self._get("/api/graph/objects/count", params=params)
        return data.get("count", 0)

    def fts_search(
        self,
        query: str,
        types: Optional[List[str]] = None,
        labels: Optional[List[str]] = None,
        status: Optional[str] = None,
        branch_id: Optional[str] = None,
        include_deleted: bool = False,
        limit: int = 20,
        offset: int = 0,
    ) -> Dict[str, Any]:
        """Full-text search on graph objects. GET /api/graph/objects/fts"""
        params: Dict[str, Any] = {"q": query, "limit": limit, "offset": offset}
        if types:
            params["types"] = ",".join(types)
        if labels:
            params["labels"] = ",".join(labels)
        if status:
            params["status"] = status
        if branch_id:
            params["branch_id"] = branch_id
        if include_deleted:
            params["include_deleted"] = "true"
        return self._get("/api/graph/objects/fts", params=params)

    def vector_search(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Vector similarity search.

        POST /api/graph/objects/vector-search

        Required key: ``vector`` (list of floats).
        """
        return self._post("/api/graph/objects/vector-search", json=payload)

    def list_tags(
        self,
        type: Optional[str] = None,
        prefix: Optional[str] = None,
        limit: Optional[int] = None,
    ) -> List[str]:
        """Get all labels/tags used across objects. GET /api/graph/objects/tags"""
        params: Dict[str, Any] = {}
        if type:
            params["type"] = type
        if prefix:
            params["prefix"] = prefix
        if limit:
            params["limit"] = limit
        data = self._get("/api/graph/objects/tags", params=params)
        return data.get("tags", [])

    # ------------------------------------------------------------------
    # Bulk object operations
    # ------------------------------------------------------------------

    def bulk_create_objects(self, items: List[Dict[str, Any]]) -> Dict[str, Any]:
        """
        Create multiple objects in one request (max 100).

        POST /api/graph/objects/bulk
        """
        return self._post("/api/graph/objects/bulk", json={"items": items})

    def bulk_update_status(
        self, ids: List[str], status: str
    ) -> Dict[str, Any]:
        """
        Update the status of multiple objects at once.

        POST /api/graph/objects/bulk-update-status
        """
        return self._post(
            "/api/graph/objects/bulk-update-status",
            json={"ids": ids, "status": status},
        )

    # ------------------------------------------------------------------
    # Relationships
    # ------------------------------------------------------------------

    def create_relationship(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Create a relationship between two objects.

        POST /api/graph/relationships

        Required keys: ``type``, ``src_id``, ``dst_id``.
        Optional keys: ``properties``, ``weight``, ``branch_id``.
        """
        return self._post("/api/graph/relationships", json=payload)

    def get_relationship(self, relationship_id: str) -> Dict[str, Any]:
        """Get a relationship by ID."""
        return self._get(f"/api/graph/relationships/{quote(relationship_id, safe='')}")

    def update_relationship(
        self, relationship_id: str, payload: Dict[str, Any]
    ) -> Dict[str, Any]:
        """Patch a relationship."""
        return self._patch(
            f"/api/graph/relationships/{quote(relationship_id, safe='')}", json=payload
        )

    def delete_relationship(self, relationship_id: str) -> None:
        """Delete a relationship."""
        self._delete(f"/api/graph/relationships/{quote(relationship_id, safe='')}")

    def list_relationships(
        self,
        type: Optional[str] = None,
        types: Optional[List[str]] = None,
        src_id: Optional[str] = None,
        dst_id: Optional[str] = None,
        object_id: Optional[str] = None,
        branch_id: Optional[str] = None,
        include_deleted: bool = False,
        limit: int = 50,
        cursor: Optional[str] = None,
    ) -> Dict[str, Any]:
        """List relationships. GET /api/graph/relationships/search"""
        params: Dict[str, Any] = {"limit": limit}
        if type:
            params["type"] = type
        if types:
            params["types"] = ",".join(types)
        if src_id:
            params["src_id"] = src_id
        if dst_id:
            params["dst_id"] = dst_id
        if object_id:
            params["object_id"] = object_id
        if branch_id:
            params["branch_id"] = branch_id
        if include_deleted:
            params["include_deleted"] = "true"
        if cursor:
            params["cursor"] = cursor
        return self._get("/api/graph/relationships/search", params=params)

    def bulk_create_relationships(
        self, items: List[Dict[str, Any]]
    ) -> Dict[str, Any]:
        """Bulk-create relationships (max 100). POST /api/graph/relationships/bulk"""
        return self._post("/api/graph/relationships/bulk", json={"items": items})

    def restore_relationship(self, relationship_id: str) -> Dict[str, Any]:
        """
        Restore a soft-deleted relationship.

        POST /api/graph/relationships/:id/restore
        """
        return self._post(
            f"/api/graph/relationships/{quote(relationship_id, safe='')}/restore"
        )

    def get_relationship_history(self, relationship_id: str) -> List[Dict[str, Any]]:
        """
        Get the version history of a relationship.

        GET /api/graph/relationships/:id/history
        """
        return self._get(
            f"/api/graph/relationships/{quote(relationship_id, safe='')}/history"
        )

    def count_relationships(
        self,
        type: Optional[str] = None,
        src_id: Optional[str] = None,
        dst_id: Optional[str] = None,
        branch_id: Optional[str] = None,
        include_deleted: bool = False,
    ) -> int:
        """
        Count relationships matching the given filters.

        GET /api/graph/relationships/count
        """
        params: Dict[str, Any] = {}
        if type:
            params["type"] = type
        if src_id:
            params["src_id"] = src_id
        if dst_id:
            params["dst_id"] = dst_id
        if branch_id:
            params["branch_id"] = branch_id
        if include_deleted:
            params["include_deleted"] = "true"
        data = self._get("/api/graph/relationships/count", params=params)
        return data.get("count", 0)

    # ------------------------------------------------------------------
    # Graph algorithms
    # ------------------------------------------------------------------

    def hybrid_search(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Hybrid FTS + vector search.

        POST /api/graph/search

        Required key: ``query``.
        Optional keys: ``vector``, ``types``, ``labels``, ``limit``,
        ``offset``, ``lexicalWeight``, ``vectorWeight``, ``includeDebug``.
        """
        return self._post("/api/graph/search", json=payload)

    def search_with_neighbors(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Search and return results with their graph neighbors.

        POST /api/graph/search-with-neighbors
        """
        return self._post("/api/graph/search-with-neighbors", json=payload)

    def expand(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Expand the graph from root nodes.

        POST /api/graph/expand

        Required key: ``root_ids`` (list of IDs).
        Optional keys: ``direction``, ``max_depth``, ``max_nodes``,
        ``max_edges``, ``relationship_types``, ``object_types``, ``labels``.
        """
        return self._post("/api/graph/expand", json=payload)

    def traverse(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Paginated graph traversal.

        POST /api/graph/traverse
        """
        return self._post("/api/graph/traverse", json=payload)

    def bulk_update(self, items: List[Dict[str, Any]]) -> Dict[str, Any]:
        """
        Bulk-update multiple objects (partial updates).

        POST /api/graph/objects/bulk-update

        Parameters
        ----------
        items:
            List of ``{"id": str, <fields to update>}`` dicts (max 100).
        """
        return self._post("/api/graph/objects/bulk-update", json={"items": items})

    def subgraph(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Extract a subgraph around a set of objects.

        POST /api/graph/subgraph

        Parameters
        ----------
        payload:
            Query payload, e.g.::

                {
                    "object_ids": ["id1", "id2"],
                    "depth": 2,
                    "branch_id": "branch_1",   # optional
                }
        """
        return self._post("/api/graph/subgraph", json=payload)

    def upsert_relationship(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Upsert a relationship (create or update by natural key).

        PUT /api/graph/relationships/upsert

        Parameters
        ----------
        payload:
            Relationship data with ``src_id``, ``dst_id``, ``type``, and
            optional properties.
        """
        return self._put("/api/graph/relationships/upsert", json=payload)

    # ------------------------------------------------------------------
    # Branch merge
    # ------------------------------------------------------------------

    def merge_branch(
        self, target_branch_id: str, payload: Dict[str, Any]
    ) -> Dict[str, Any]:
        """
        Preview or execute a branch merge.

        POST /api/graph/branches/:targetBranchId/merge

        Required keys: ``sourceBranchId``.
        Optional keys: ``execute`` (bool, default false), ``limit``.
        """
        return self._post(
            f"/api/graph/branches/{quote(target_branch_id, safe='')}/merge",
            json=payload,
        )

    # ------------------------------------------------------------------
    # Analytics
    # ------------------------------------------------------------------

    def most_accessed(
        self,
        limit: int = 20,
        types: Optional[List[str]] = None,
        labels: Optional[List[str]] = None,
        branch_id: Optional[str] = None,
        order: Optional[str] = None,
    ) -> Dict[str, Any]:
        """Get the most-accessed graph objects. GET /api/graph/analytics/most-accessed"""
        params: Dict[str, Any] = {"limit": limit}
        if types:
            params["types"] = ",".join(types)
        if labels:
            params["labels"] = ",".join(labels)
        if branch_id:
            params["branch_id"] = branch_id
        if order:
            params["order"] = order
        return self._get("/api/graph/analytics/most-accessed", params=params)

    def unused_objects(
        self,
        limit: int = 20,
        types: Optional[List[str]] = None,
        labels: Optional[List[str]] = None,
        branch_id: Optional[str] = None,
        days_idle: Optional[int] = None,
    ) -> Dict[str, Any]:
        """Get objects that haven't been accessed recently. GET /api/graph/analytics/unused"""
        params: Dict[str, Any] = {"limit": limit}
        if types:
            params["types"] = ",".join(types)
        if labels:
            params["labels"] = ",".join(labels)
        if branch_id:
            params["branch_id"] = branch_id
        if days_idle is not None:
            params["daysIdle"] = days_idle
        return self._get("/api/graph/analytics/unused", params=params)
