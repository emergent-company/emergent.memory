"""
Schemas sub-client.

Endpoints covered
-----------------
GET    /api/schemas/projects/:projectId/available         — list available packs
GET    /api/schemas/projects/:projectId/installed         — list installed packs
GET    /api/schemas/projects/:projectId/compiled-types    — get compiled types
POST   /api/schemas/projects/:projectId/assign            — assign a pack
PATCH  /api/schemas/projects/:projectId/assignments/:id   — update assignment
DELETE /api/schemas/projects/:projectId/assignments/:id   — delete assignment
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional
from urllib.parse import quote

from ._base import BaseClient


class SchemasClient(BaseClient):
    """Client for the Schemas API."""

    def _project_path(self, project_id: Optional[str] = None) -> str:
        pid = project_id or self._project_id
        if not pid:
            raise ValueError("project_id is required")
        return f"/api/schemas/projects/{quote(pid, safe='')}"

    def list_available(self, project_id: Optional[str] = None) -> List[Dict[str, Any]]:
        """GET /api/schemas/projects/:projectId/available"""
        data = self._get(f"{self._project_path(project_id)}/available")
        if isinstance(data, list):
            return data
        return data.get("available", data.get("items", data))

    def list_installed(self, project_id: Optional[str] = None) -> List[Dict[str, Any]]:
        """GET /api/schemas/projects/:projectId/installed"""
        data = self._get(f"{self._project_path(project_id)}/installed")
        if isinstance(data, list):
            return data
        return data.get("installed", data.get("items", data))

    def get_compiled_types(self, project_id: Optional[str] = None) -> Dict[str, Any]:
        """GET /api/schemas/projects/:projectId/compiled-types"""
        return self._get(f"{self._project_path(project_id)}/compiled-types")

    def assign(self, payload: Dict[str, Any], project_id: Optional[str] = None) -> Dict[str, Any]:
        """POST /api/schemas/projects/:projectId/assign"""
        return self._post(f"{self._project_path(project_id)}/assign", json=payload)

    def update_assignment(
        self, assignment_id: str, payload: Dict[str, Any], project_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """PATCH /api/schemas/projects/:projectId/assignments/:id"""
        return self._patch(
            f"{self._project_path(project_id)}/assignments/{quote(assignment_id, safe='')}",
            json=payload,
        )

    def delete_assignment(self, assignment_id: str, project_id: Optional[str] = None) -> None:
        """DELETE /api/schemas/projects/:projectId/assignments/:id"""
        self._delete(
            f"{self._project_path(project_id)}/assignments/{quote(assignment_id, safe='')}"
        )
