"""
Tasks sub-client.

Endpoints covered
-----------------
GET    /api/tasks                  — list tasks (project-scoped)
GET    /api/tasks/counts           — task counts (project-scoped)
GET    /api/tasks/all              — list tasks (cross-project)
GET    /api/tasks/all/counts       — task counts (cross-project)
GET    /api/tasks/:id              — get a task by ID
POST   /api/tasks/:id/resolve     — resolve a task (accept/reject)
POST   /api/tasks/:id/cancel      — cancel a task
"""
from __future__ import annotations

from typing import Any, Dict, Optional
from urllib.parse import quote

from ._base import BaseClient


class TasksClient(BaseClient):
    """Client for the Tasks API."""

    # ------------------------------------------------------------------
    # Project-scoped
    # ------------------------------------------------------------------

    def list(
        self,
        *,
        project_id: Optional[str] = None,
        status: Optional[str] = None,
        type: Optional[str] = None,
        limit: int = 50,
        offset: int = 0,
    ) -> Dict[str, Any]:
        """
        List tasks for the current project.

        GET /api/tasks

        Parameters
        ----------
        project_id:
            Explicit project ID (overrides X-Project-ID header).
        status:
            Filter by status, e.g. ``"pending"``, ``"accepted"``.
        type:
            Filter by task type.
        limit:
            Maximum number of results (default 50).
        offset:
            Pagination offset (default 0).

        Returns
        -------
        dict
            ``{"data": [...], "total": int}``
        """
        params: Dict[str, Any] = {"limit": limit, "offset": offset}
        if project_id:
            params["project_id"] = project_id
        if status:
            params["status"] = status
        if type:
            params["type"] = type
        return self._get("/api/tasks", params=params)

    def counts(self, project_id: Optional[str] = None) -> Dict[str, Any]:
        """
        Get task counts for the current project.

        GET /api/tasks/counts

        Returns
        -------
        dict
            ``{"pending": int, "accepted": int, "rejected": int, "cancelled": int}``
        """
        params: Dict[str, Any] = {}
        if project_id:
            params["project_id"] = project_id
        return self._get("/api/tasks/counts", params=params)

    # ------------------------------------------------------------------
    # Cross-project
    # ------------------------------------------------------------------

    def list_all(
        self,
        *,
        status: Optional[str] = None,
        type: Optional[str] = None,
        limit: int = 50,
        offset: int = 0,
    ) -> Dict[str, Any]:
        """
        List tasks across all projects.

        GET /api/tasks/all

        Returns
        -------
        dict
            ``{"data": [...], "total": int}``
        """
        params: Dict[str, Any] = {"limit": limit, "offset": offset}
        if status:
            params["status"] = status
        if type:
            params["type"] = type
        return self._get("/api/tasks/all", params=params)

    def counts_all(self) -> Dict[str, Any]:
        """
        Get task counts across all projects.

        GET /api/tasks/all/counts

        Returns
        -------
        dict
            ``{"pending": int, "accepted": int, "rejected": int, "cancelled": int}``
        """
        return self._get("/api/tasks/all/counts")

    # ------------------------------------------------------------------
    # Single task operations
    # ------------------------------------------------------------------

    def get(self, task_id: str, project_id: Optional[str] = None) -> Dict[str, Any]:
        """
        Get a task by ID.

        GET /api/tasks/:id

        Returns
        -------
        dict
            ``{"data": <Task>}``
        """
        params: Dict[str, Any] = {}
        if project_id:
            params["project_id"] = project_id
        return self._get(f"/api/tasks/{quote(task_id, safe='')}", params=params)

    def resolve(
        self,
        task_id: str,
        resolution: str,
        *,
        resolution_notes: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Resolve a task.

        POST /api/tasks/:id/resolve

        Parameters
        ----------
        resolution:
            ``"accepted"`` or ``"rejected"``.
        resolution_notes:
            Optional notes for the resolution.

        Returns
        -------
        dict
            ``{"status": "resolved"}``
        """
        payload: Dict[str, Any] = {"resolution": resolution}
        if resolution_notes is not None:
            payload["resolutionNotes"] = resolution_notes
        return self._post(f"/api/tasks/{quote(task_id, safe='')}/resolve", json=payload)

    def cancel(self, task_id: str) -> Dict[str, Any]:
        """
        Cancel a task.

        POST /api/tasks/:id/cancel

        Returns
        -------
        dict
            ``{"status": "cancelled"}``
        """
        return self._post(f"/api/tasks/{quote(task_id, safe='')}/cancel")
