"""
API Tokens sub-client.

Endpoints covered
-----------------
Project-scoped:
  POST   /api/projects/:projectId/tokens             — create project token
  GET    /api/projects/:projectId/tokens             — list project tokens
  GET    /api/projects/:projectId/tokens/:tokenId    — get project token
  DELETE /api/projects/:projectId/tokens/:tokenId    — revoke project token

Account-scoped:
  POST   /api/tokens                                 — create account token
  GET    /api/tokens                                 — list account tokens
  GET    /api/tokens/:tokenId                        — get account token
  DELETE /api/tokens/:tokenId                        — revoke account token
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional
from urllib.parse import quote

from ._base import BaseClient


class APITokenClient(BaseClient):
    """Client for the API Tokens API (project-scoped and account-scoped)."""

    # ------------------------------------------------------------------
    # Project-scoped tokens
    # ------------------------------------------------------------------

    def _project_path(self, project_id: Optional[str] = None) -> str:
        pid = project_id or self._project_id
        if not pid:
            raise ValueError("project_id is required")
        return f"/api/projects/{quote(pid, safe='')}/tokens"

    def create_project_token(
        self, payload: Dict[str, Any], project_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """
        Create a project-scoped API token.

        POST /api/projects/:projectId/tokens

        Parameters
        ----------
        payload:
            Token creation data, e.g.::

                {
                    "name": "ci-token",
                    "scopes": ["read", "write"],
                }

        Returns
        -------
        dict
            Includes a one-time ``token`` field with the raw token value.
        """
        return self._post(self._project_path(project_id), json=payload)

    def list_project_tokens(
        self, project_id: Optional[str] = None
    ) -> List[Dict[str, Any]]:
        """
        List all tokens for a project.

        GET /api/projects/:projectId/tokens
        """
        data = self._get(self._project_path(project_id))
        if isinstance(data, list):
            return data
        return data.get("tokens", data.get("items", data))

    def get_project_token(
        self, token_id: str, project_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """
        Get a project token by ID.

        GET /api/projects/:projectId/tokens/:tokenId
        """
        return self._get(
            f"{self._project_path(project_id)}/{quote(token_id, safe='')}"
        )

    def revoke_project_token(
        self, token_id: str, project_id: Optional[str] = None
    ) -> None:
        """
        Revoke (delete) a project token.

        DELETE /api/projects/:projectId/tokens/:tokenId
        """
        self._delete(
            f"{self._project_path(project_id)}/{quote(token_id, safe='')}"
        )

    # ------------------------------------------------------------------
    # Account-scoped tokens
    # ------------------------------------------------------------------

    def create_account_token(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Create an account-scoped API token.

        POST /api/tokens

        Parameters
        ----------
        payload:
            Token creation data, e.g. ``{"name": "personal", "scopes": ["read"]}``.

        Returns
        -------
        dict
            Includes a one-time ``token`` field with the raw token value.
        """
        return self._post("/api/tokens", json=payload)

    def list_account_tokens(self) -> List[Dict[str, Any]]:
        """
        List all account-scoped tokens.

        GET /api/tokens
        """
        data = self._get("/api/tokens")
        if isinstance(data, list):
            return data
        return data.get("tokens", data.get("items", data))

    def get_account_token(self, token_id: str) -> Dict[str, Any]:
        """
        Get an account token by ID.

        GET /api/tokens/:tokenId
        """
        return self._get(f"/api/tokens/{quote(token_id, safe='')}")

    def revoke_account_token(self, token_id: str) -> None:
        """
        Revoke (delete) an account token.

        DELETE /api/tokens/:tokenId
        """
        self._delete(f"/api/tokens/{quote(token_id, safe='')}")
