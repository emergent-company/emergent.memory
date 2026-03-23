"""
Orgs sub-client.

Endpoints covered
-----------------
GET    /api/orgs          — list organisations
GET    /api/orgs/:id      — get an organisation by ID
POST   /api/orgs          — create an organisation
DELETE /api/orgs/:id      — delete an organisation
"""
from __future__ import annotations

from typing import Any, Dict, List
from urllib.parse import quote

from ._base import BaseClient


class OrgsClient(BaseClient):
    """Client for the Organisations API."""

    def list(self) -> List[Dict[str, Any]]:
        """GET /api/orgs"""
        data = self._get("/api/orgs")
        if isinstance(data, list):
            return data
        return data.get("orgs", data.get("items", data))

    def get(self, org_id: str) -> Dict[str, Any]:
        """GET /api/orgs/:id"""
        return self._get(f"/api/orgs/{quote(org_id, safe='')}")

    def create(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Create a new organisation.

        POST /api/orgs
        """
        return self._post("/api/orgs", json=payload)

    def delete(self, org_id: str) -> None:
        """
        Delete an organisation.

        DELETE /api/orgs/:id
        """
        self._delete(f"/api/orgs/{quote(org_id, safe='')}")
