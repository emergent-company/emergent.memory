"""
Documents sub-client.

Endpoints covered
-----------------
GET    /api/documents                       — list documents
POST   /api/documents                       — create/ingest a document
GET    /api/documents/:id                   — get a document by ID
DELETE /api/documents/:id                   — delete a document by ID
GET    /api/documents/:id/content           — get raw document content
POST   /api/documents/upload                — upload a file
GET    /api/documents/:id/download          — get download URL
DELETE /api/documents                       — bulk delete documents
GET    /api/documents/:id/deletion-impact   — get deletion impact
POST   /api/documents/deletion-impact       — bulk deletion impact
GET    /api/documents/:id/extraction-summary — get extraction summary
GET    /api/documents/source-types          — list source types
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

    # ------------------------------------------------------------------
    # Upload / download
    # ------------------------------------------------------------------

    def upload(
        self,
        file_path: str,
        *,
        file_name: Optional[str] = None,
        auto_extract: bool = True,
    ) -> Dict[str, Any]:
        """
        Upload a file as a document.

        POST /api/documents/upload  (multipart/form-data)

        Parameters
        ----------
        file_path:
            Local path to the file.
        file_name:
            Override file name (defaults to the file's basename).
        auto_extract:
            Whether to auto-extract content from the file.

        Returns
        -------
        dict
            ``{"document": ..., "isDuplicate": bool, "existingDocumentId": str|None}``
        """
        import os

        name = file_name or os.path.basename(file_path)
        with open(file_path, "rb") as f:
            files = {"file": (name, f)}
            data = {"autoExtract": str(auto_extract).lower()}
            resp = self._http.post(
                self._base + "/api/documents/upload",
                files=files,
                data=data,
                headers=self._auth_headers(),
            )
        self._raise_for_status(resp)
        return resp.json()

    def download_url(self, document_id: str) -> str:
        """
        Get the download URL for a document.

        GET /api/documents/:id/download

        The server returns a 307 redirect to a signed URL.  This method
        returns the redirect target URL rather than following it.

        Returns
        -------
        str
            Signed download URL.
        """
        resp = self._http.get(
            self._base + f"/api/documents/{quote(document_id, safe='')}/download",
            headers=self._auth_headers(),
            follow_redirects=False,
        )
        if resp.status_code in (301, 302, 307, 308):
            return resp.headers.get("Location", "")
        self._raise_for_status(resp)
        # If the server returns a JSON body instead of a redirect
        data = resp.json()
        return data.get("url", "")

    # ------------------------------------------------------------------
    # Bulk operations
    # ------------------------------------------------------------------

    def bulk_delete(self, ids: List[str]) -> Dict[str, Any]:
        """
        Delete multiple documents at once.

        DELETE /api/documents  (with JSON body)

        Parameters
        ----------
        ids:
            List of document IDs to delete.

        Returns
        -------
        dict
            ``{"status": ..., "deleted": int, "notFound": int, "summary": ...}``
        """
        resp = self._http.request(
            "DELETE",
            self._base + "/api/documents",
            json={"ids": ids},
            headers=self._auth_headers(),
        )
        self._raise_for_status(resp)
        return resp.json()

    def get_deletion_impact(self, document_id: str) -> Dict[str, Any]:
        """
        Get the impact summary before deleting a document.

        GET /api/documents/:id/deletion-impact
        """
        return self._get(
            f"/api/documents/{quote(document_id, safe='')}/deletion-impact"
        )

    def bulk_deletion_impact(self, ids: List[str]) -> Dict[str, Any]:
        """
        Get the impact summary before deleting multiple documents.

        POST /api/documents/deletion-impact
        """
        return self._post("/api/documents/deletion-impact", json={"ids": ids})

    # ------------------------------------------------------------------
    # Metadata
    # ------------------------------------------------------------------

    def get_extraction_summary(self, document_id: str) -> Dict[str, Any]:
        """
        Get the extraction summary (stats) for a document.

        GET /api/documents/:id/extraction-summary
        """
        return self._get(
            f"/api/documents/{quote(document_id, safe='')}/extraction-summary"
        )

    def get_source_types(self) -> List[Dict[str, Any]]:
        """
        List all source types with counts.

        GET /api/documents/source-types

        Returns
        -------
        list
            ``[{"sourceType": str, "count": int}, ...]``
        """
        data = self._get("/api/documents/source-types")
        return data.get("sourceTypes", data)
