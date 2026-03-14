"""
Exceptions raised by the Emergent SDK.
"""
from __future__ import annotations

from typing import Any, Optional


class EmergentError(Exception):
    """Base class for all Emergent SDK errors."""


class APIError(EmergentError):
    """
    Raised when the server returns a 4xx or 5xx HTTP status code.

    Attributes
    ----------
    status_code : int
        HTTP status code returned by the server.
    code : str | None
        Machine-readable error code from the server (may be empty).
    message : str
        Human-readable error message.
    details : dict
        Extra structured detail payload from the server.
    """

    def __init__(
        self,
        status_code: int,
        message: str,
        code: Optional[str] = None,
        details: Optional[dict[str, Any]] = None,
    ) -> None:
        self.status_code = status_code
        self.code = code or ""
        self.message = message
        self.details = details or {}
        if self.code:
            super().__init__(f"[{status_code}] {self.code}: {message}")
        else:
            super().__init__(f"[{status_code}] {message}")

    @property
    def is_not_found(self) -> bool:
        return self.status_code == 404

    @property
    def is_forbidden(self) -> bool:
        return self.status_code == 403

    @property
    def is_unauthorized(self) -> bool:
        return self.status_code == 401

    @property
    def is_bad_request(self) -> bool:
        return self.status_code == 400

    @classmethod
    def from_response(cls, status_code: int, body: bytes) -> "APIError":
        """Parse an HTTP error body into an APIError."""
        import json

        try:
            data = json.loads(body)
            err = data.get("error", {})
            if isinstance(err, dict) and err.get("message"):
                return cls(
                    status_code=status_code,
                    message=err["message"],
                    code=err.get("code"),
                    details=err.get("details"),
                )
        except Exception:
            pass
        return cls(status_code=status_code, message=body.decode(errors="replace"))


class AuthError(EmergentError):
    """Raised when authentication fails or configuration is invalid."""


class StreamError(EmergentError):
    """Raised when an error is received over the SSE stream."""
