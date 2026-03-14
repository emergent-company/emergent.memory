"""
Authentication providers for the Emergent SDK.

Three modes are supported:

* ``apikey``   — standalone server API key, sent as ``X-API-Key`` header.
* ``apitoken`` — project-scoped token (``emt_*`` prefix), sent as
                  ``Authorization: Bearer <token>``.
* ``oauth``    — OAuth 2.0 Bearer token, sent as ``Authorization: Bearer``.
                  Callers are responsible for obtaining and refreshing the
                  access token (device-flow helper not yet implemented).
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Dict


class AuthProvider(ABC):
    """Abstract base for all authentication providers."""

    @abstractmethod
    def headers(self) -> Dict[str, str]:
        """Return HTTP headers that carry the credentials."""

    def refresh(self) -> None:  # noqa: B027
        """
        Refresh credentials if they are expiring.
        The default implementation is a no-op (used by static-key providers).
        """


# ---------------------------------------------------------------------------
# Concrete providers
# ---------------------------------------------------------------------------


def _is_api_token(key: str) -> bool:
    """Return True if *key* looks like a project API token (``emt_`` prefix)."""
    return key.startswith("emt_")


class APIKeyProvider(AuthProvider):
    """
    Standalone server API key — sent as ``X-API-Key: <key>``.

    This is the authentication mode used by self-hosted / standalone
    deployments that do not have Zitadel configured.
    """

    def __init__(self, api_key: str) -> None:
        if not api_key:
            raise ValueError("api_key must not be empty")
        self._api_key = api_key

    def headers(self) -> Dict[str, str]:
        return {"X-API-Key": self._api_key}


class APITokenProvider(AuthProvider):
    """
    Project-scoped API token — sent as ``Authorization: Bearer <token>``.

    Tokens are created via the admin UI or the ``/api/api-tokens`` endpoint
    and always start with the ``emt_`` prefix.
    """

    def __init__(self, token: str) -> None:
        if not token:
            raise ValueError("token must not be empty")
        self._token = token

    def headers(self) -> Dict[str, str]:
        return {"Authorization": f"Bearer {self._token}"}


class OAuthProvider(AuthProvider):
    """
    Generic OAuth 2.0 Bearer token provider.

    The caller is responsible for obtaining and refreshing the access token.
    Pass the raw Bearer token string; the SDK will include it in the
    ``Authorization`` header on every request.

    Example::

        provider = OAuthProvider(access_token="eyJhb...")
        client = Client(server_url="https://api.example.com", auth=provider)
    """

    def __init__(self, access_token: str) -> None:
        if not access_token:
            raise ValueError("access_token must not be empty")
        self._token = access_token

    def update_token(self, access_token: str) -> None:
        """Replace the stored access token (call from your refresh logic)."""
        self._token = access_token

    def headers(self) -> Dict[str, str]:
        return {"Authorization": f"Bearer {self._token}"}


# ---------------------------------------------------------------------------
# Factory helper (mirrors Go sdk.New() auth-mode detection)
# ---------------------------------------------------------------------------


def make_provider(mode: str, api_key: str = "", access_token: str = "") -> AuthProvider:
    """
    Create an :class:`AuthProvider` from an explicit *mode* string.

    Parameters
    ----------
    mode:
        ``"apikey"``    — use :class:`APIKeyProvider` (auto-detects ``emt_`` prefix
                          and upgrades to :class:`APITokenProvider`).
        ``"apitoken"``  — use :class:`APITokenProvider` (requires ``api_key``).
        ``"oauth"``     — use :class:`OAuthProvider` (requires ``access_token``).
    api_key:
        API key string for ``apikey`` / ``apitoken`` modes.
    access_token:
        Bearer token string for ``oauth`` mode.
    """
    if mode == "apikey":
        if not api_key:
            raise ValueError("api_key is required for 'apikey' mode")
        if _is_api_token(api_key):
            return APITokenProvider(api_key)
        return APIKeyProvider(api_key)
    elif mode == "apitoken":
        if not api_key:
            raise ValueError("api_key is required for 'apitoken' mode")
        return APITokenProvider(api_key)
    elif mode == "oauth":
        if not access_token:
            raise ValueError("access_token is required for 'oauth' mode")
        return OAuthProvider(access_token)
    else:
        raise ValueError(
            f"Invalid auth mode: {mode!r}. Must be 'apikey', 'apitoken', or 'oauth'."
        )
