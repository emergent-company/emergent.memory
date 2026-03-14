"""
Internal base client shared by all sub-clients.

Every sub-client inherits :class:`BaseClient`, which provides:

* Authenticated ``httpx`` session with org/project context headers.
* ``_get``, ``_post``, ``_patch``, ``_put``, ``_delete`` helpers that
  handle auth, context headers, JSON encoding, error parsing, and response
  decoding in one place.
* ``_stream`` context manager for SSE streaming requests.
"""
from __future__ import annotations

import threading
from contextlib import contextmanager
from typing import Any, Dict, Generator, Iterator, Optional, Type, TypeVar

import httpx

from .auth import AuthProvider
from .exceptions import APIError

T = TypeVar("T")

_DEFAULT_TIMEOUT = httpx.Timeout(30.0)


class BaseClient:
    """
    Shared HTTP machinery for all Emergent sub-clients.

    Parameters
    ----------
    http:
        Shared :class:`httpx.Client` instance (connection-pooled).
    base_url:
        Server root URL, e.g. ``"https://api.emergent-company.ai"``.
    auth:
        Authentication provider.
    org_id:
        Default organisation ID injected via ``X-Org-ID`` header.
    project_id:
        Default project ID injected via ``X-Project-ID`` header.
    """

    def __init__(
        self,
        http: httpx.Client,
        base_url: str,
        auth: AuthProvider,
        org_id: str = "",
        project_id: str = "",
    ) -> None:
        self._http = http
        self._base = base_url.rstrip("/")
        self._auth = auth
        self._lock = threading.RLock()
        self._org_id = org_id
        self._project_id = project_id

    # ------------------------------------------------------------------
    # Context management
    # ------------------------------------------------------------------

    def set_context(self, org_id: str, project_id: str) -> None:
        """Update default org/project context (thread-safe)."""
        with self._lock:
            self._org_id = org_id
            self._project_id = project_id

    # ------------------------------------------------------------------
    # Header helpers
    # ------------------------------------------------------------------

    def _auth_headers(self) -> Dict[str, str]:
        with self._lock:
            headers = dict(self._auth.headers())
            if self._org_id:
                headers["X-Org-ID"] = self._org_id
            if self._project_id:
                headers["X-Project-ID"] = self._project_id
        return headers

    # ------------------------------------------------------------------
    # Error handling
    # ------------------------------------------------------------------

    @staticmethod
    def _raise_for_status(response: httpx.Response) -> None:
        if response.status_code >= 400:
            raise APIError.from_response(response.status_code, response.content)

    # ------------------------------------------------------------------
    # HTTP verbs
    # ------------------------------------------------------------------

    def _get(self, path: str, params: Optional[Dict[str, Any]] = None) -> Any:
        resp = self._http.get(
            self._base + path,
            params={k: v for k, v in (params or {}).items() if v is not None},
            headers=self._auth_headers(),
        )
        self._raise_for_status(resp)
        return resp.json()

    def _post(
        self,
        path: str,
        json: Optional[Any] = None,
        params: Optional[Dict[str, Any]] = None,
    ) -> Any:
        resp = self._http.post(
            self._base + path,
            json=json,
            params={k: v for k, v in (params or {}).items() if v is not None},
            headers=self._auth_headers(),
        )
        self._raise_for_status(resp)
        if resp.status_code == 204 or not resp.content:
            return None
        return resp.json()

    def _patch(self, path: str, json: Optional[Any] = None) -> Any:
        resp = self._http.patch(
            self._base + path,
            json=json,
            headers=self._auth_headers(),
        )
        self._raise_for_status(resp)
        if resp.status_code == 204 or not resp.content:
            return None
        return resp.json()

    def _put(self, path: str, json: Optional[Any] = None) -> Any:
        resp = self._http.put(
            self._base + path,
            json=json,
            headers=self._auth_headers(),
        )
        self._raise_for_status(resp)
        if resp.status_code == 204 or not resp.content:
            return None
        return resp.json()

    def _delete(self, path: str) -> None:
        resp = self._http.delete(
            self._base + path,
            headers=self._auth_headers(),
        )
        self._raise_for_status(resp)

    @contextmanager
    def _stream(
        self, path: str, json: Optional[Any] = None
    ) -> Generator[Iterator[bytes], None, None]:
        """
        Context manager that opens an SSE stream.

        Yields an iterator of raw bytes chunks.  The caller is responsible
        for parsing them (use :func:`emergent.sse.iter_sse_events`).

        Example::

            with client._stream("/api/projects/x/ask", json={"message": "hi"}) as chunks:
                for event in iter_sse_events(chunks):
                    ...
        """
        headers = {**self._auth_headers(), "Accept": "text/event-stream"}
        with self._http.stream(
            "POST",
            self._base + path,
            json=json,
            headers=headers,
        ) as response:
            self._raise_for_status(response)
            yield response.iter_bytes()
