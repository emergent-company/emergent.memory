"""
AgentDefinitions sub-client.

Endpoints covered
-----------------
GET    /api/projects/:projectId/agent-definitions
POST   /api/projects/:projectId/agent-definitions
GET    /api/projects/:projectId/agent-definitions/:id
PATCH  /api/projects/:projectId/agent-definitions/:id
DELETE /api/projects/:projectId/agent-definitions/:id
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional
from urllib.parse import quote

from ._base import BaseClient


class AgentDefinitionsClient(BaseClient):
    """
    Client for the Agent Definitions API.

    Agent definitions store agent configurations (system prompts, tools,
    model config, flow type, visibility) independently from runtime state.

    Example::

        defs = client.agent_definitions.list()
        new_def = client.agent_definitions.create({
            "name": "Summariser",
            "systemPrompt": "You summarise knowledge graph objects.",
            "tools": ["graph_read_object"],
            "flowType": "single",
            "visibility": "project",
        })
    """

    def _project_path(self, project_id: Optional[str] = None) -> str:
        pid = project_id or self._project_id
        if not pid:
            raise ValueError("project_id is required")
        return f"/api/projects/{quote(pid, safe='')}"

    def list(self, project_id: Optional[str] = None) -> List[Dict[str, Any]]:
        """
        List agent definition summaries for the project.

        GET /api/projects/:projectId/agent-definitions
        """
        data = self._get(f"{self._project_path(project_id)}/agent-definitions")
        return data.get("data", data)

    def get(self, definition_id: str, project_id: Optional[str] = None) -> Dict[str, Any]:
        """
        Get a full agent definition by ID.

        GET /api/projects/:projectId/agent-definitions/:id
        """
        data = self._get(
            f"{self._project_path(project_id)}/agent-definitions/{quote(definition_id, safe='')}"
        )
        return data.get("data", data)

    def create(
        self, payload: Dict[str, Any], project_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """
        Create a new agent definition.

        POST /api/projects/:projectId/agent-definitions

        Required keys: ``name``.
        Optional keys: ``description``, ``systemPrompt``, ``model``
        (``{name, temperature, maxTokens}``), ``tools``, ``flowType``
        (``single`` | ``sequential`` | ``loop``), ``visibility``
        (``external`` | ``project`` | ``internal``), ``maxSteps``,
        ``defaultTimeout``, ``dispatchMode``, ``acpConfig``, ``config``.
        """
        data = self._post(f"{self._project_path(project_id)}/agent-definitions", json=payload)
        return data.get("data", data)

    def update(
        self,
        definition_id: str,
        payload: Dict[str, Any],
        project_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Partial update an agent definition.

        PATCH /api/projects/:projectId/agent-definitions/:id
        """
        data = self._patch(
            f"{self._project_path(project_id)}/agent-definitions/{quote(definition_id, safe='')}",
            json=payload,
        )
        return data.get("data", data)

    def delete(self, definition_id: str, project_id: Optional[str] = None) -> None:
        """
        Delete an agent definition.

        DELETE /api/projects/:projectId/agent-definitions/:id
        """
        self._delete(
            f"{self._project_path(project_id)}/agent-definitions/{quote(definition_id, safe='')}"
        )
