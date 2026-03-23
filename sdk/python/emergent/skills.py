"""
Skills sub-client.

Endpoints covered
-----------------
GET    /api/projects/:projectId/skills
POST   /api/projects/:projectId/skills
PATCH  /api/projects/:projectId/skills/:id
DELETE /api/projects/:projectId/skills/:id
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional
from urllib.parse import quote

from ._base import BaseClient


class SkillsClient(BaseClient):
    """Client for the Skills API."""

    def _project_path(self, project_id: Optional[str] = None) -> str:
        pid = project_id or self._project_id
        if not pid:
            raise ValueError("project_id is required")
        return f"/api/projects/{quote(pid, safe='')}"

    def list(self, project_id: Optional[str] = None) -> List[Dict[str, Any]]:
        """GET /api/projects/:id/skills"""
        data = self._get(f"{self._project_path(project_id)}/skills")
        if isinstance(data, list):
            return data
        return data.get("skills", data.get("items", data))

    def create(
        self, payload: Dict[str, Any], project_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """POST /api/projects/:id/skills"""
        return self._post(f"{self._project_path(project_id)}/skills", json=payload)

    def update(
        self,
        skill_id: str,
        payload: Dict[str, Any],
        project_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """PATCH /api/projects/:id/skills/:skillId"""
        return self._patch(
            f"{self._project_path(project_id)}/skills/{quote(skill_id, safe='')}",
            json=payload,
        )

    def delete(self, skill_id: str, project_id: Optional[str] = None) -> None:
        """DELETE /api/projects/:id/skills/:skillId"""
        self._delete(
            f"{self._project_path(project_id)}/skills/{quote(skill_id, safe='')}"
        )
