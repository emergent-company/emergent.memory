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
from .api_tokens import APITokenClient
from .auth import AuthProvider, make_provider
from .branches import BranchesClient
from .chat import ChatClient
from .documents import DocumentsClient
from .graph import GraphClient
from .mcp import MCPClient
from .orgs import OrgsClient
from .projects import ProjectsClient
from .schemas import SchemasClient
from .search import SearchClient
from .skills import SkillsClient
from .tasks import TasksClient

_DEFAULT_TIMEOUT = httpx.Timeout(30.0)


# ---------------------------------------------------------------------------
# Config discovery helpers
# ---------------------------------------------------------------------------


def _walk_up_find(filename: str) -> Optional[str]:
    """Walk up from cwd to find filename. Returns path or None."""
    import pathlib

    d = pathlib.Path.cwd()
    while True:
        candidate = d / filename
        if candidate.is_file():
            return str(candidate)
        parent = d.parent
        if parent == d:
            return None
        d = parent


def _parse_dotenv(path: str) -> Dict[str, str]:
    """Parse a .env file into a dict. Handles KEY=VALUE and KEY="VALUE" lines."""
    result: Dict[str, str] = {}
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, _, val = line.partition("=")
                key = key.strip()
                val = val.strip().strip('"').strip("'")
                result[key] = val
    except OSError:
        pass
    return result


def _load_memory_yaml(path: str) -> Dict[str, str]:
    """Load ~/.memory/config.yaml. Returns relevant fields as strings."""
    try:
        import yaml  # type: ignore[import]

        with open(path) as f:
            data = yaml.safe_load(f) or {}
        return {k: str(v) for k, v in data.items() if v}
    except Exception:
        pass
    # Fallback: simple key: value parser (no PyYAML dependency)
    result: Dict[str, str] = {}
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or ":" not in line:
                    continue
                key, _, val = line.partition(":")
                key = key.strip()
                val = val.strip().strip('"').strip("'")
                if val:
                    result[key] = val
    except OSError:
        pass
    return result


def _load_env_config() -> Dict[str, str]:
    """
    Discover config from multiple sources in priority order (lowest first).

    Priority (highest wins):
      1. ~/.memory/config.yaml
      2. .env (walk up from cwd)
      3. .env.local (walk up from cwd)
      4. MEMORY_* environment variables
    """
    import pathlib

    cfg: Dict[str, str] = {}

    # 1. ~/.memory/config.yaml
    yaml_path = pathlib.Path.home() / ".memory" / "config.yaml"
    if yaml_path.is_file():
        raw = _load_memory_yaml(str(yaml_path))
        # Map YAML keys to our internal keys
        for yaml_key, cfg_key in [
            ("server_url", "server_url"),
            ("api_key", "api_key"),
            ("org_id", "org_id"),
            ("project_id", "project_id"),
            ("project_token", "project_token"),
        ]:
            if raw.get(yaml_key):
                cfg[cfg_key] = raw[yaml_key]

    # 2. .env (lower priority than .env.local)
    if p := _walk_up_find(".env"):
        for k, v in _parse_dotenv(p).items():
            _apply_env_key(cfg, k, v)

    # 3. .env.local (overrides .env)
    if p := _walk_up_find(".env.local"):
        for k, v in _parse_dotenv(p).items():
            _apply_env_key(cfg, k, v)

    # 4. Actual environment variables (highest priority)
    for k, v in os.environ.items():
        _apply_env_key(cfg, k, v)

    return cfg


def _apply_env_key(cfg: Dict[str, str], key: str, value: str) -> None:
    """Map a MEMORY_* env var name to our internal config key."""
    if not value:
        return
    mapping = {
        "MEMORY_SERVER_URL": "server_url",
        "MEMORY_API_URL": "server_url",  # alias, lower priority handled by order
        "MEMORY_API_KEY": "api_key",
        "MEMORY_ORG_ID": "org_id",
        "MEMORY_PROJECT_ID": "project_id",
        "MEMORY_PROJECT_TOKEN": "project_token",
    }
    if key in mapping:
        # MEMORY_SERVER_URL takes priority over MEMORY_API_URL
        # Since env vars are applied last and we iterate os.environ,
        # if both are set MEMORY_SERVER_URL should win.
        # Handle: only overwrite server_url from MEMORY_API_URL if not already set by MEMORY_SERVER_URL
        if (
            key == "MEMORY_API_URL"
            and cfg.get("server_url")
            and os.environ.get("MEMORY_SERVER_URL")
        ):
            return  # MEMORY_SERVER_URL already set it
        cfg[mapping[key]] = value


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
    * :attr:`documents`         — document management
    * :attr:`branches`          — graph branches
    * :attr:`api_tokens`        — project and account API tokens
    * :attr:`tasks`             — task management
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
        Build a Client by auto-discovering configuration.

        Resolution order (highest priority wins):

        1. ``~/.memory/config.yaml`` — CLI config file
        2. ``.env`` — dotenv file (walked up from current directory)
        3. ``.env.local`` — local overrides (walked up from current directory)
        4. ``MEMORY_*`` environment variables

        Recognised variables / YAML keys:

        * ``MEMORY_SERVER_URL`` / ``server_url`` — server URL (required)
        * ``MEMORY_API_KEY`` / ``api_key`` — API key or emt_* token
        * ``MEMORY_PROJECT_TOKEN`` / ``project_token`` — project-scoped emt_* token
        * ``MEMORY_ORG_ID`` / ``org_id`` — default organisation ID
        * ``MEMORY_PROJECT_ID`` / ``project_id`` — default project ID

        ``MEMORY_API_URL`` is accepted as an alias for ``MEMORY_SERVER_URL``.

        If ``MEMORY_PROJECT_TOKEN`` is set it is used as the credential (Bearer
        auth); otherwise ``MEMORY_API_KEY`` is used.

        Raises
        ------
        EnvironmentError
            If no server URL or API key can be found from any source.
        """
        discovered = _load_env_config()

        server_url = discovered.get("server_url", "")
        # project_token takes precedence over api_key as the credential
        api_key = discovered.get("project_token") or discovered.get("api_key", "")

        if not server_url:
            raise EnvironmentError(
                "No server URL found. Set MEMORY_SERVER_URL, add it to "
                "~/.memory/config.yaml, or create a .env.local file with "
                "MEMORY_SERVER_URL=http://localhost:3012"
            )
        if not api_key:
            raise EnvironmentError(
                "No API key found. Set MEMORY_API_KEY (or MEMORY_PROJECT_TOKEN), "
                "add api_key to ~/.memory/config.yaml, or create a .env.local file."
            )

        return cls(
            Config(
                server_url=server_url,
                api_key=api_key,
                mode="apikey",
                org_id=discovered.get("org_id", ""),
                project_id=discovered.get("project_id", ""),
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
        self.branches = BranchesClient(*args)
        self.api_tokens = APITokenClient(*args)
        self.tasks = TasksClient(*args)

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
            self.branches.set_context(org_id, project_id)
            self.api_tokens.set_context(org_id, project_id)
            self.tasks.set_context(org_id, project_id)
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
