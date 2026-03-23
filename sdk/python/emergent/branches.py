"""
Branches sub-client.

Endpoints covered
-----------------
GET    /api/graph/branches           — list branches
GET    /api/graph/branches/:id       — get a branch by ID
POST   /api/graph/branches           — create a branch
PATCH  /api/graph/branches/:id       — update a branch
POST   /api/graph/branches/:id/fork  — fork a branch
DELETE /api/graph/branches/:id       — delete a branch
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional
from urllib.parse import quote

from ._base import BaseClient


class BranchesClient(BaseClient):
    """Client for the Branches API."""

    def list(self, project_id: Optional[str] = None) -> List[Dict[str, Any]]:
        """
        List branches.

        GET /api/graph/branches

        Parameters
        ----------
        project_id:
            Optional project ID filter.  Falls back to the context project.
        """
        params: Dict[str, Any] = {}
        if project_id:
            params["project_id"] = project_id
        data = self._get("/api/graph/branches", params=params)
        if isinstance(data, list):
            return data
        return data.get("branches", data.get("items", data))

    def get(self, branch_id: str) -> Dict[str, Any]:
        """
        Get a branch by ID.

        GET /api/graph/branches/:id
        """
        return self._get(f"/api/graph/branches/{quote(branch_id, safe='')}")

    def create(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Create a new branch.

        POST /api/graph/branches

        Parameters
        ----------
        payload:
            Branch data, e.g.::

                {
                    "name": "experiment-1",
                    "description": "Testing new ontology",
                    "project_id": "proj_1",          # optional if context set
                    "parent_branch_id": "branch_1",   # optional
                }
        """
        return self._post("/api/graph/branches", json=payload)

    def update(self, branch_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Update a branch.

        PATCH /api/graph/branches/:id

        Parameters
        ----------
        payload:
            Fields to update, e.g. ``{"name": "new-name", "description": "..."}``.
        """
        return self._patch(
            f"/api/graph/branches/{quote(branch_id, safe='')}", json=payload
        )

    def delete(self, branch_id: str) -> None:
        """
        Delete a branch.

        DELETE /api/graph/branches/:id
        """
        self._delete(f"/api/graph/branches/{quote(branch_id, safe='')}")

    def fork(self, branch_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Fork a branch (create a copy).

        POST /api/graph/branches/:id/fork

        Parameters
        ----------
        branch_id:
            ID of the branch to fork.
        payload:
            Fork options, e.g. ``{"name": "fork-name", "description": "..."}``.
        """
        return self._post(
            f"/api/graph/branches/{quote(branch_id, safe='')}/fork",
            json=payload,
        )
