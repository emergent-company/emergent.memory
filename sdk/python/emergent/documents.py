"""
Documents sub-client.

Endpoints covered
-----------------
GET    /api/documents               — list documents
POST   /api/documents               — create/ingest a document
GET    /api/documents/:id           — get a document by ID
DELETE /api/documents/:id           — delete a document by ID
GET    /api/documents/:id/content   — get raw document content
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional
from urllib.parse import quote

from ._base import BaseClient


class DocumentsClient(BaseClient):
    """Client for the Documents API."""

    def list(
        self,
        *,
        limit: int = 50,
        offset: int = 0,
        source_type: Optional[str] = None,
    ) -> List[Dict[str, Any]]:
        """
        List documents in the current project.

        GET /api/documents

        Parameters
        ----------
        limit:
            Maximum number of results (default 50).
        offset:
            Pagination offset (default 0).
        source_type:
            Filter by source type, e.g. ``"text"``, ``"url"``, ``"file"``.
        """
        params: Dict[str, Any] = {"limit": limit, "offset": offset}
        if source_type:
            params["sourceType"] = source_type
        data = self._get("/api/documents", params=params)
        if isinstance(data, list):
            return data
        return data.get("documents", data.get("items", data))

    def get(self, document_id: str) -> Dict[str, Any]:
        """
        Get a document by ID.

        GET /api/documents/:id
        """
        return self._get(f"/api/documents/{quote(document_id, safe='')}")

    def create(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        """
        Create / ingest a new document.

        POST /api/documents

        Parameters
        ----------
        payload:
            Document data, e.g.::

                {
                    "title": "My doc",
                    "content": "Hello world",
                    "sourceType": "text",
                }
        """
        return self._post("/api/documents", json=payload)

    def delete(self, document_id: str) -> None:
        """
        Delete a document by ID.

        DELETE /api/documents/:id
        """
        self._delete(f"/api/documents/{quote(document_id, safe='')}")

    def get_content(self, document_id: str) -> str:
        """
        Get the raw text content of a document.

        GET /api/documents/:id/content

        Returns
        -------
        str
            Plain-text content of the document.
        """
        resp = self._http.get(
            self._base + f"/api/documents/{quote(document_id, safe='')}/content",
            headers=self._auth_headers(),
        )
        self._raise_for_status(resp)
        return resp.text
