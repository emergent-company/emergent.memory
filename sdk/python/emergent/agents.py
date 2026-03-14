"""
Agents sub-client — agent CRUD, runs, questions, and webhook hooks.

Endpoints covered
-----------------
GET    /api/projects/:projectId/agents
POST   /api/projects/:projectId/agents
GET    /api/projects/:projectId/agents/:id
PATCH  /api/projects/:projectId/agents/:id
DELETE /api/projects/:projectId/agents/:id
POST   /api/projects/:projectId/agents/:id/trigger
POST   /api/projects/:projectId/agents/:id/batch-trigger
GET    /api/projects/:projectId/agents/:id/runs
POST   /api/projects/:projectId/agents/:id/runs/:runId/cancel
GET    /api/projects/:projectId/agent-runs
GET    /api/projects/:projectId/agent-runs/:runId
GET    /api/projects/:projectId/agent-runs/:runId/messages
GET    /api/projects/:projectId/agent-runs/:runId/tool-calls
GET    /api/projects/:projectId/agent-runs/:runId/questions
GET    /api/projects/:projectId/agent-questions
POST   /api/projects/:projectId/agent-questions/:questionId/respond
GET    /api/projects/:projectId/agents/:id/hooks
POST   /api/projects/:projectId/agents/:id/hooks
DELETE /api/projects/:projectId/agents/:id/hooks/:hookId
GET    /api/projects/:projectId/adk-sessions
GET    /api/projects/:projectId/adk-sessions/:sessionId
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional
from urllib.parse import quote

from ._base import BaseClient


class AgentsClient(BaseClient):
    """
    Client for the Agents API.

    Typical usage::

        agents = client.agents.list()
        run_resp = client.agents.trigger(agent_id="abc123")
        runs = client.agents.list_project_runs(limit=20)
    """

    def _project_path(self, project_id: Optional[str] = None) -> str:
        pid = project_id or self._project_id
        if not pid:
            raise ValueError("project_id is required (set via set_context or pass explicitly)")
        return f"/api/projects/{quote(pid, safe='')}"

    # ------------------------------------------------------------------
    # Agent CRUD
    # ------------------------------------------------------------------

    def list(self, project_id: Optional[str] = None) -> List[Dict[str, Any]]:
        """List all agents for the project. GET /api/projects/:id/agents"""
        data = self._get(f"{self._project_path(project_id)}/agents")
        return data.get("data", data)

    def get(self, agent_id: str, project_id: Optional[str] = None) -> Dict[str, Any]:
        """Get a single agent by ID."""
        data = self._get(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}"
        )
        return data.get("data", data)

    def create(
        self, payload: Dict[str, Any], project_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """Create a new agent. POST /api/projects/:id/agents"""
        data = self._post(f"{self._project_path(project_id)}/agents", json=payload)
        return data.get("data", data)

    def update(
        self,
        agent_id: str,
        payload: Dict[str, Any],
        project_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """Partial update an agent. PATCH /api/projects/:id/agents/:agentId"""
        data = self._patch(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}",
            json=payload,
        )
        return data.get("data", data)

    def delete(self, agent_id: str, project_id: Optional[str] = None) -> None:
        """Delete an agent."""
        self._delete(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}"
        )

    # ------------------------------------------------------------------
    # Triggers
    # ------------------------------------------------------------------

    def trigger(
        self, agent_id: str, project_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """
        Trigger an immediate run of an agent.

        POST /api/projects/:projectId/agents/:id/trigger
        Returns ``{"success": bool, "runId": str | None, ...}``.
        """
        return self._post(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}/trigger"
        )

    def batch_trigger(
        self,
        agent_id: str,
        object_ids: List[str],
        project_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Trigger a reaction agent for multiple graph objects (max 100).

        POST /api/projects/:projectId/agents/:id/batch-trigger
        """
        return self._post(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}/batch-trigger",
            json={"objectIds": object_ids},
        )

    # ------------------------------------------------------------------
    # Runs
    # ------------------------------------------------------------------

    def get_runs(
        self,
        agent_id: str,
        limit: int = 20,
        project_id: Optional[str] = None,
    ) -> List[Dict[str, Any]]:
        """Get recent runs for an agent."""
        data = self._get(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}/runs",
            params={"limit": limit},
        )
        return data.get("data", data)

    def list_project_runs(
        self,
        project_id: Optional[str] = None,
        agent_id: Optional[str] = None,
        status: Optional[str] = None,
        limit: int = 50,
        offset: int = 0,
    ) -> Dict[str, Any]:
        """
        List agent runs for a project with filtering and pagination.

        GET /api/projects/:projectId/agent-runs
        Returns ``{"items": [...], "totalCount": int, ...}``.
        """
        return self._get(
            f"{self._project_path(project_id)}/agent-runs",
            params={
                "limit": limit,
                "offset": offset,
                "agentId": agent_id,
                "status": status,
            },
        )

    def get_project_run(
        self, run_id: str, project_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """Get a specific run by ID (includes token usage)."""
        data = self._get(
            f"{self._project_path(project_id)}/agent-runs/{quote(run_id, safe='')}"
        )
        return data.get("data", data)

    def cancel_run(
        self, agent_id: str, run_id: str, project_id: Optional[str] = None
    ) -> None:
        """Cancel a running agent run."""
        self._post(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}"
            f"/runs/{quote(run_id, safe='')}/cancel"
        )

    def get_run_messages(
        self, run_id: str, project_id: Optional[str] = None
    ) -> List[Dict[str, Any]]:
        """Get conversation messages for a run."""
        data = self._get(
            f"{self._project_path(project_id)}/agent-runs/{quote(run_id, safe='')}/messages"
        )
        return data.get("data", data)

    def get_run_tool_calls(
        self, run_id: str, project_id: Optional[str] = None
    ) -> List[Dict[str, Any]]:
        """Get tool invocations for a run."""
        data = self._get(
            f"{self._project_path(project_id)}/agent-runs/{quote(run_id, safe='')}/tool-calls"
        )
        return data.get("data", data)

    # ------------------------------------------------------------------
    # Questions
    # ------------------------------------------------------------------

    def get_run_questions(
        self, run_id: str, project_id: Optional[str] = None
    ) -> List[Dict[str, Any]]:
        """Get questions posed by the agent during a run."""
        data = self._get(
            f"{self._project_path(project_id)}/agent-runs/{quote(run_id, safe='')}/questions"
        )
        return data.get("data", data)

    def list_project_questions(
        self,
        status: Optional[str] = None,
        project_id: Optional[str] = None,
    ) -> List[Dict[str, Any]]:
        """List agent questions for a project, optionally filtered by status."""
        data = self._get(
            f"{self._project_path(project_id)}/agent-questions",
            params={"status": status},
        )
        return data.get("data", data)

    def respond_to_question(
        self,
        question_id: str,
        response: str,
        project_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Respond to a pending agent question and resume the paused run.

        POST /api/projects/:projectId/agent-questions/:questionId/respond
        """
        data = self._post(
            f"{self._project_path(project_id)}/agent-questions/"
            f"{quote(question_id, safe='')}/respond",
            json={"response": response},
        )
        return data.get("data", data) if data else {}

    # ------------------------------------------------------------------
    # Pending events (reaction agents)
    # ------------------------------------------------------------------

    def get_pending_events(
        self,
        agent_id: str,
        limit: int = 20,
        project_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """Get pending events for a reaction agent."""
        data = self._get(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}/pending-events",
            params={"limit": limit},
        )
        return data.get("data", data)

    # ------------------------------------------------------------------
    # Webhook hooks
    # ------------------------------------------------------------------

    def list_webhook_hooks(
        self, agent_id: str, project_id: Optional[str] = None
    ) -> List[Dict[str, Any]]:
        """List webhook hooks for an agent."""
        data = self._get(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}/hooks"
        )
        return data.get("data", data)

    def create_webhook_hook(
        self,
        agent_id: str,
        label: str,
        rate_limit_config: Optional[Dict[str, Any]] = None,
        project_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Create a webhook hook for an agent.  The plaintext token is only
        returned once in the response.
        """
        body: Dict[str, Any] = {"label": label}
        if rate_limit_config is not None:
            body["rateLimitConfig"] = rate_limit_config
        data = self._post(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}/hooks",
            json=body,
        )
        return data.get("data", data)

    def delete_webhook_hook(
        self, agent_id: str, hook_id: str, project_id: Optional[str] = None
    ) -> None:
        """Delete a webhook hook."""
        self._delete(
            f"{self._project_path(project_id)}/agents/{quote(agent_id, safe='')}"
            f"/hooks/{quote(hook_id, safe='')}"
        )

    # ------------------------------------------------------------------
    # ADK sessions
    # ------------------------------------------------------------------

    def list_adk_sessions(self, project_id: Optional[str] = None) -> List[Dict[str, Any]]:
        """List ADK sessions for the project."""
        data = self._get(f"{self._project_path(project_id)}/adk-sessions")
        return data.get("items", data)

    def get_adk_session(
        self, session_id: str, project_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """Get a specific ADK session and its events."""
        data = self._get(
            f"{self._project_path(project_id)}/adk-sessions/{quote(session_id, safe='')}"
        )
        return data.get("data", data)
