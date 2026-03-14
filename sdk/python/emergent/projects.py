"""
Projects sub-client.

Endpoints covered
-----------------
GET  /api/projects
POST /api/projects
GET  /api/projects/:id
"""
from __future__ import annotations

from typing import Any, Dict, List
from urllib.parse import quote

from ._base import BaseClient


class ProjectsClient(BaseClient):
    """Client for the Projects API."""

    def list(self) -> List[Dict[str, Any]]:
        """List all projects accessible to the current user. GET /api/projects"""
        data = self._get("/api/projects")
        if isinstance(data, list):
            return data
        return data.get("projects", data.get("items", data))

    def get(self, project_id: str) -> Dict[str, Any]:
        """Get a project by ID. GET /api/projects/:id"""
        return self._get(f"/api/projects/{quote(project_id, safe='')}")

    def create(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """Create a new project. POST /api/projects"""
        return self._post("/api/projects", json=payload)
