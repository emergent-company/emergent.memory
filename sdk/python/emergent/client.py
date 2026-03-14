"""
Root Emergent API client.

This module provides the top-level :class:`Client` that wires together all
sub-clients and exposes the public SDK surface.  It mirrors the Go SDK at
``apps/server/pkg/sdk/sdk.go``.

Typical usage
-------------
.. code-block:: python

    from emergent import Client

    # API key (standalone / self-hosted)
    client = Client.from_api_key("http://localhost:3012", "my-server-key")

    # Project API token  (emt_* prefix auto-detected)
    client = Client.from_api_key("https://api.emergent-company.ai", "emt_abc123...")

    # Full config
    from emergent import Client, Config
    client = Client(Config(
        server_url="https://api.emergent-company.ai",
        api_key="emt_abc123",
        org_id="org_1",
        project_id="proj_1",
    ))

    # Set / switch context later
    client.set_context(org_id="org_2", project_id="proj_2")

    # Use sub-clients
    projects = client.projects.list()
    for event in client.chat.stream(conversation_id="conv_1", message="Hello"):
        if event.type == "token":
            print(event.token, end="", flush=True)

    client.close()
"""
from __future__ import annotations

import os
import threading
from dataclasses import dataclass, field
from typing import Any, Dict, Optional

import httpx

from .agent_definitions import AgentDefinitionsClient
from .agents import AgentsClient
from .auth import AuthProvider, make_provider
from .chat import ChatClient
from .documents import DocumentsClient
from .graph import GraphClient
from .mcp import MCPClient
from .orgs import OrgsClient
from .projects import ProjectsClient
from .schemas import SchemasClient
from .search import SearchClient
from .skills import SkillsClient

_DEFAULT_TIMEOUT = httpx.Timeout(30.0)


# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------


@dataclass
class Config:
    """
    Configuration for the :class:`Client`.

    Parameters
    ----------
    server_url:
        Root URL of the Emergent server, e.g. ``"https://api.emergent-company.ai"``.
    api_key:
        API key or project API token.  An ``emt_`` prefix is auto-detected and
        switches to ``APITokenProvider`` (Bearer auth) transparently.
    mode:
        Auth mode: ``"apikey"`` (default), ``"apitoken"``, or ``"oauth"``.
    access_token:
        OAuth 2.0 Bearer token (only for ``mode="oauth"``).
    org_id:
        Default organisation ID — injected as ``X-Org-ID`` on every request.
    project_id:
        Default project ID — injected as ``X-Project-ID`` on every request.
    http_client:
        Custom :class:`httpx.Client`.  If ``None`` a default client with a
        30 s timeout is created and owned by this :class:`Client`.
    """

    server_url: str
    api_key: str = ""
    mode: str = "apikey"
    access_token: str = ""
    org_id: str = ""
    project_id: str = ""
    http_client: Optional[httpx.Client] = field(default=None, repr=False)


# ---------------------------------------------------------------------------
# Root Client
# ---------------------------------------------------------------------------


class Client:
    """
    Main SDK client for the Emergent API.

    All sub-clients are exposed as attributes:

    * :attr:`chat`              — conversations and streaming ask/query
    * :attr:`agents`            — agents, runs, questions, webhooks
    * :attr:`agent_definitions` — agent definition templates
    * :attr:`mcp`               — MCP JSON-RPC 2.0 endpoint
    * :attr:`graph`             — knowledge-graph objects and relationships
    * :attr:`search`            — full-text and semantic search
    * :attr:`projects`          — project management
    * :attr:`orgs`              — organisation management
    * :attr:`schemas`           — entity schemas
    * :attr:`skills`            — skills library
    """

    def __init__(self, config: Config) -> None:
        if not config.server_url:
            raise ValueError("Config.server_url is required")

        self._auth: AuthProvider = make_provider(
            mode=config.mode,
            api_key=config.api_key,
            access_token=config.access_token,
        )

        # Shared HTTP client — we own it if none was supplied.
        self._owns_http = config.http_client is None
        self._http: httpx.Client = config.http_client or httpx.Client(
            timeout=_DEFAULT_TIMEOUT
        )

        self._base = config.server_url.rstrip("/")
        self._mu = threading.RLock()
        self._org_id = config.org_id
        self._project_id = config.project_id

        self._init_clients()

    # ------------------------------------------------------------------
    # Class-method constructors (convenience shorthands)
    # ------------------------------------------------------------------

    @classmethod
    def from_env(cls) -> "Client":
        """
        Build a Client from environment variables.

        Reads the following environment variables:

        * ``MEMORY_API_URL``   — server URL (required)
        * ``MEMORY_API_KEY``   — API key or emt_* token (required)
        * ``MEMORY_ORG_ID``    — default organisation ID (optional)
        * ``MEMORY_PROJECT_ID`` — default project ID (optional)

        This is the recommended pattern inside sandbox containers::

            from emergent import Client
            client = Client.from_env()

        Raises
        ------
        EnvironmentError
            If ``MEMORY_API_URL`` or ``MEMORY_API_KEY`` is not set.
        """
        server_url = os.environ.get("MEMORY_API_URL", "")
        api_key = os.environ.get("MEMORY_API_KEY", "")
        if not server_url:
            raise EnvironmentError(
                "MEMORY_API_URL environment variable is not set. "
                "Set it to the Emergent server URL, e.g. http://localhost:3012"
            )
        if not api_key:
            raise EnvironmentError(
                "MEMORY_API_KEY environment variable is not set. "
                "Set it to your API key or emt_* token."
            )
        return cls(
            Config(
                server_url=server_url,
                api_key=api_key,
                mode="apikey",
                org_id=os.environ.get("MEMORY_ORG_ID", ""),
                project_id=os.environ.get("MEMORY_PROJECT_ID", ""),
            )
        )

    @classmethod
    def from_api_key(
        cls,
        server_url: str,
        api_key: str,
        *,
        org_id: str = "",
        project_id: str = "",
    ) -> "Client":
        """
        Shorthand constructor for API-key / API-token auth.

        If *api_key* starts with ``emt_`` the client automatically uses
        Bearer token auth (same behaviour as the Go SDK).

        Example::

            client = Client.from_api_key("http://localhost:3012", "my-key")
        """
        return cls(
            Config(
                server_url=server_url,
                api_key=api_key,
                mode="apikey",
                org_id=org_id,
                project_id=project_id,
            )
        )

    @classmethod
    def from_oauth_token(
        cls,
        server_url: str,
        access_token: str,
        *,
        org_id: str = "",
        project_id: str = "",
    ) -> "Client":
        """
        Shorthand constructor for OAuth Bearer token auth.

        The caller is responsible for obtaining and refreshing *access_token*.

        Example::

            client = Client.from_oauth_token(
                "https://api.emergent-company.ai",
                access_token=my_token_manager.get_token(),
            )
        """
        return cls(
            Config(
                server_url=server_url,
                mode="oauth",
                access_token=access_token,
                org_id=org_id,
                project_id=project_id,
            )
        )

    # ------------------------------------------------------------------
    # Internal initialisation
    # ------------------------------------------------------------------

    def _init_clients(self) -> None:
        """Instantiate all sub-clients (called once from __init__)."""
        args = (self._http, self._base, self._auth, self._org_id, self._project_id)

        # Context-scoped sub-clients
        self.chat = ChatClient(*args)
        self.agents = AgentsClient(*args)
        self.agent_definitions = AgentDefinitionsClient(*args)
        self.mcp = MCPClient(*args)
        self.graph = GraphClient(*args)
        self.search = SearchClient(*args)
        self.schemas = SchemasClient(*args)
        self.skills = SkillsClient(*args)
        self.documents = DocumentsClient(*args)

        # Non-context sub-clients (no org/project needed for their API paths)
        self.projects = ProjectsClient(self._http, self._base, self._auth)
        self.orgs = OrgsClient(self._http, self._base, self._auth)

    # ------------------------------------------------------------------
    # Context management
    # ------------------------------------------------------------------

    def set_context(self, org_id: str, project_id: str) -> None:
        """
        Set the default organisation and project context.

        The update is applied atomically to all sub-clients so that no
        concurrent API call sees a partially-updated state.

        Parameters
        ----------
        org_id:
            Organisation ID to inject as ``X-Org-ID``.
        project_id:
            Project ID to inject as ``X-Project-ID``.

        Example::

            client.set_context(org_id="org_abc", project_id="proj_xyz")
        """
        with self._mu:
            self._org_id = org_id
            self._project_id = project_id

            # Propagate to every context-scoped sub-client
            self.chat.set_context(org_id, project_id)
            self.agents.set_context(org_id, project_id)
            self.agent_definitions.set_context(org_id, project_id)
            self.mcp.set_context(org_id, project_id)
            self.graph.set_context(org_id, project_id)
            self.search.set_context(org_id, project_id)
            self.schemas.set_context(org_id, project_id)
            self.skills.set_context(org_id, project_id)
            self.documents.set_context(org_id, project_id)
            # projects / orgs have no org/project context headers

    # ------------------------------------------------------------------
    # Manual auth helper (mirrors Go AuthenticateRequest)
    # ------------------------------------------------------------------

    def authenticate_request(self, headers: Dict[str, str]) -> Dict[str, str]:
        """
        Inject authentication and context headers into a plain dict.

        Useful when you manage the HTTP transport yourself (e.g. for a custom
        streaming setup).  Returns the *same* dict with headers added in-place
        and also as the return value for convenience.

        Parameters
        ----------
        headers:
            A ``dict`` that will receive the auth + context headers.

        Returns
        -------
        dict
            The same ``headers`` dict, now containing ``Authorization`` /
            ``X-API-Key`` and, where set, ``X-Org-ID`` / ``X-Project-ID``.

        Example::

            hdrs = client.authenticate_request({})
            # hdrs == {"Authorization": "Bearer emt_...", "X-Org-ID": "...", ...}
        """
        headers.update(self._auth.headers())
        with self._mu:
            if self._org_id:
                headers["X-Org-ID"] = self._org_id
            if self._project_id:
                headers["X-Project-ID"] = self._project_id
        return headers

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    def close(self) -> None:
        """
        Release resources held by the client.

        Closes the underlying :class:`httpx.Client` connection pool if it was
        created by this :class:`Client` (i.e. no custom ``http_client`` was
        supplied).  After calling this method the client **must not** be used.
        """
        if self._owns_http:
            self._http.close()

    def __enter__(self) -> "Client":
        return self

    def __exit__(self, *_: Any) -> None:
        self.close()

    def __repr__(self) -> str:
        return (
            f"Client(server_url={self._base!r}, "
            f"org_id={self._org_id!r}, project_id={self._project_id!r})"
        )
