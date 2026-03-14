"""
Schemas sub-client.

Endpoints covered
-----------------
GET    /api/projects/:projectId/schemas
POST   /api/projects/:projectId/schemas
GET    /api/projects/:projectId/schemas/:id
PATCH  /api/projects/:projectId/schemas/:id
DELETE /api/projects/:projectId/schemas/:id
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
        return f"/api/projects/{quote(pid, safe='')}"

    def list(self, project_id: Optional[str] = None) -> List[Dict[str, Any]]:
        """GET /api/projects/:id/schemas"""
        data = self._get(f"{self._project_path(project_id)}/schemas")
        if isinstance(data, list):
            return data
        return data.get("schemas", data.get("items", data))

    def get(self, schema_id: str, project_id: Optional[str] = None) -> Dict[str, Any]:
        """GET /api/projects/:id/schemas/:schemaId"""
        return self._get(
            f"{self._project_path(project_id)}/schemas/{quote(schema_id, safe='')}"
        )

    def create(
        self, payload: Dict[str, Any], project_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """POST /api/projects/:id/schemas"""
        return self._post(f"{self._project_path(project_id)}/schemas", json=payload)

    def update(
        self,
        schema_id: str,
        payload: Dict[str, Any],
        project_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """PATCH /api/projects/:id/schemas/:schemaId"""
        return self._patch(
            f"{self._project_path(project_id)}/schemas/{quote(schema_id, safe='')}",
            json=payload,
        )

    def delete(self, schema_id: str, project_id: Optional[str] = None) -> None:
        """DELETE /api/projects/:id/schemas/:schemaId"""
        self._delete(
            f"{self._project_path(project_id)}/schemas/{quote(schema_id, safe='')}"
        )
