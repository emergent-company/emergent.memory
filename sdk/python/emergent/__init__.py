"""
emergent — Python SDK for the Emergent Memory API.

Quick start
-----------
.. code-block:: python

    from emergent import Client

    # API key (standalone / self-hosted server)
    client = Client.from_api_key("http://localhost:3012", "my-server-api-key")

    # Project API token (emt_* prefix auto-detected, uses Bearer auth)
    client = Client.from_api_key("https://api.emergent-company.ai", "emt_abc123...")

    # OAuth Bearer token
    client = Client.from_oauth_token("https://api.emergent-company.ai", access_token)

    # Set org / project context
    client.set_context(org_id="org_1", project_id="proj_1")

    # List projects
    projects = client.projects.list()

    # Streaming ask
    for event in client.chat.stream(conversation_id="conv_1", message="Hello"):
        if event.type == "token":
            print(event.token, end="", flush=True)

    # Always close when done (or use as a context manager)
    client.close()

    # --- Context manager ---
    with Client.from_api_key("http://localhost:3012", "my-key") as c:
        c.set_context("org_1", "proj_1")
        result = c.chat.ask_collect(conversation_id="conv_1", message="Summarise")
        print(result["text"])
"""

from .api_tokens import APITokenClient
from .auth import APIKeyProvider, APITokenProvider, AuthProvider, OAuthProvider, make_provider
from .branches import BranchesClient
from .client import Client, Config
from .documents import DocumentsClient
from .exceptions import APIError, AuthError, EmergentError, StreamError
from .sse import (
    DoneEvent,
    ErrorEvent,
    MCPToolEvent,
    MetaEvent,
    TokenEvent,
    UnknownEvent,
    iter_sse_events,
)
from .tasks import TasksClient

__all__ = [
    # Root client
    "Client",
    "Config",
    # Sub-clients (exported for type hints)
    "APITokenClient",
    "BranchesClient",
    "DocumentsClient",
    "TasksClient",
    # Auth
    "AuthProvider",
    "APIKeyProvider",
    "APITokenProvider",
    "OAuthProvider",
    "make_provider",
    # Exceptions
    "EmergentError",
    "APIError",
    "AuthError",
    "StreamError",
    # SSE event types
    "iter_sse_events",
    "MetaEvent",
    "TokenEvent",
    "MCPToolEvent",
    "ErrorEvent",
    "DoneEvent",
    "UnknownEvent",
]
