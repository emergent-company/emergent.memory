"""
MCP (Model Context Protocol) sub-client.

The Emergent server exposes a JSON-RPC 2.0 MCP endpoint at
``POST /api/mcp/rpc``.  This client wraps the protocol machinery
and exposes the most commonly needed methods.

Endpoints covered
-----------------
POST /api/mcp/rpc   (all methods via JSON-RPC 2.0)

Standard methods
----------------
initialize          — MCP protocol handshake
tools/list          — list available tools
tools/call          — call a tool by name
resources/list      — list available resources
resources/read      — read a resource by URI
prompts/list        — list available prompts
prompts/get         — get a prompt with arguments
"""
from __future__ import annotations

from typing import Any, Dict, Optional

from ._base import BaseClient
from .exceptions import APIError


class MCPClient(BaseClient):
    """
    Client for the MCP JSON-RPC endpoint.

    The project context is sent via ``X-Project-ID`` so that the server
    can scope tool calls to the right project.

    Example::

        client.set_context(org_id="org_1", project_id="proj_1")

        # List available tools
        tools = client.mcp.list_tools()
        for t in tools.get("tools", []):
            print(t["name"], "-", t["description"])

        # Call a tool
        result = client.mcp.call_tool(
            "graph_read_object",
            {"object_id": "abc123"}
        )
    """

    _rpc_id: int = 0

    def _next_id(self) -> int:
        self._rpc_id += 1
        return self._rpc_id

    def call_method(
        self,
        method: str,
        params: Optional[Any] = None,
    ) -> Any:
        """
        Call a JSON-RPC 2.0 method on the MCP endpoint.

        Returns the ``result`` field of the JSON-RPC response.
        Raises :class:`~emergent.exceptions.APIError` on HTTP errors and
        a plain :class:`RuntimeError` on JSON-RPC protocol errors.
        """
        body: Dict[str, Any] = {
            "jsonrpc": "2.0",
            "id": self._next_id(),
            "method": method,
        }
        if params is not None:
            body["params"] = params

        data = self._post("/api/mcp/rpc", json=body)

        if data and data.get("error"):
            err = data["error"]
            raise RuntimeError(
                f"JSON-RPC error {err.get('code')}: {err.get('message')}"
            )
        return data.get("result") if data else None

    # ------------------------------------------------------------------
    # Convenience wrappers
    # ------------------------------------------------------------------

    def initialize(self) -> Any:
        """Perform the MCP protocol handshake."""
        return self.call_method(
            "initialize",
            {
                "protocolVersion": "2025-11-25",
                "capabilities": {},
                "clientInfo": {"name": "emergent-python-sdk", "version": "0.1.0"},
            },
        )

    def list_tools(self) -> Any:
        """List available MCP tools. Returns the raw result dict."""
        return self.call_method("tools/list")

    def call_tool(self, name: str, arguments: Optional[Dict[str, Any]] = None) -> Any:
        """
        Call an MCP tool by name.

        Parameters
        ----------
        name:
            Tool name, e.g. ``"graph_read_object"``.
        arguments:
            Tool-specific argument dict.

        Returns the raw ``result`` payload from the server.
        """
        return self.call_method(
            "tools/call",
            {"name": name, "arguments": arguments or {}},
        )

    def list_resources(self) -> Any:
        """List available MCP resources."""
        return self.call_method("resources/list")

    def read_resource(self, uri: str) -> Any:
        """Read an MCP resource by URI."""
        return self.call_method("resources/read", {"uri": uri})

    def list_prompts(self) -> Any:
        """List available MCP prompts."""
        return self.call_method("prompts/list")

    def get_prompt(self, name: str, arguments: Optional[Dict[str, Any]] = None) -> Any:
        """Get an MCP prompt with arguments."""
        return self.call_method(
            "prompts/get",
            {"name": name, "arguments": arguments or {}},
        )
