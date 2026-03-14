"""
Chat sub-client — conversations, messages, and SSE streaming.

Endpoints covered
-----------------
GET    /api/chat/conversations
POST   /api/chat/conversations
GET    /api/chat/:id
PATCH  /api/chat/:id
DELETE /api/chat/:id
POST   /api/chat/:id/messages
POST   /api/chat/stream           (SSE)
POST   /api/projects/:id/ask      (SSE)
POST   /api/projects/:id/query    (SSE)
"""
from __future__ import annotations

from typing import Any, Dict, Generator, Iterator, List, Optional
from urllib.parse import quote

from ._base import BaseClient
from .sse import SSEEvent, iter_sse_events


class Conversation:
    """Lightweight wrapper around a conversation dict."""

    def __init__(self, data: Dict[str, Any]) -> None:
        self._data = data

    def __getattr__(self, name: str) -> Any:
        try:
            return self._data[name]
        except KeyError:
            raise AttributeError(name)

    @property
    def id(self) -> str:
        return self._data["id"]

    def __repr__(self) -> str:
        return f"<Conversation id={self.id!r} title={self._data.get('title', '')!r}>"


class Message:
    """Lightweight wrapper around a message dict."""

    def __init__(self, data: Dict[str, Any]) -> None:
        self._data = data

    def __getattr__(self, name: str) -> Any:
        try:
            return self._data[name]
        except KeyError:
            raise AttributeError(name)

    @property
    def id(self) -> str:
        return self._data["id"]

    def __repr__(self) -> str:
        role = self._data.get("role", "")
        content = self._data.get("content", "")[:60]
        return f"<Message id={self.id!r} role={role!r} content={content!r}>"


class ChatClient(BaseClient):
    """
    Client for the Chat API.

    Typical usage::

        # Simple streaming ask
        with client.chat.ask(project_id="proj_123", message="What is gravity?") as events:
            for event in events:
                if event.type == "token":
                    print(event.token, end="", flush=True)

        # Conversation CRUD
        conv = client.chat.create_conversation(title="My chat", message="Hello!")
        msgs = client.chat.get_conversation(conv.id)
    """

    # ------------------------------------------------------------------
    # Conversations
    # ------------------------------------------------------------------

    def list_conversations(
        self, limit: int = 50, offset: int = 0
    ) -> Dict[str, Any]:
        """
        List conversations for the current project context.

        GET /api/chat/conversations
        """
        return self._get(
            "/api/chat/conversations",
            params={"limit": limit, "offset": offset},
        )

    def create_conversation(
        self,
        title: str,
        message: str,
        canonical_id: Optional[str] = None,
    ) -> Conversation:
        """
        Create a new conversation with an initial message.

        POST /api/chat/conversations
        """
        body: Dict[str, Any] = {"title": title, "message": message}
        if canonical_id is not None:
            body["canonicalId"] = canonical_id
        data = self._post("/api/chat/conversations", json=body)
        return Conversation(data)

    def get_conversation(self, conversation_id: str) -> Dict[str, Any]:
        """
        Get a conversation with all its messages.

        GET /api/chat/:id
        """
        return self._get(f"/api/chat/{quote(conversation_id, safe='')}")

    def update_conversation(
        self,
        conversation_id: str,
        title: Optional[str] = None,
        draft_text: Optional[str] = None,
    ) -> Conversation:
        """
        Update conversation properties.

        PATCH /api/chat/:id
        """
        body: Dict[str, Any] = {}
        if title is not None:
            body["title"] = title
        if draft_text is not None:
            body["draftText"] = draft_text
        data = self._patch(f"/api/chat/{quote(conversation_id, safe='')}", json=body)
        return Conversation(data)

    def delete_conversation(self, conversation_id: str) -> None:
        """
        Delete a conversation and all its messages.

        DELETE /api/chat/:id
        """
        self._delete(f"/api/chat/{quote(conversation_id, safe='')}")

    # ------------------------------------------------------------------
    # Messages
    # ------------------------------------------------------------------

    def add_message(
        self,
        conversation_id: str,
        role: str,
        content: str,
        retrieval_context: Optional[Any] = None,
    ) -> Message:
        """
        Add a message to an existing conversation.

        POST /api/chat/:id/messages
        """
        body: Dict[str, Any] = {"role": role, "content": content}
        if retrieval_context is not None:
            body["retrievalContext"] = retrieval_context
        data = self._post(
            f"/api/chat/{quote(conversation_id, safe='')}/messages", json=body
        )
        return Message(data)

    # ------------------------------------------------------------------
    # Streaming
    # ------------------------------------------------------------------

    def stream(
        self,
        message: str,
        conversation_id: Optional[str] = None,
        canonical_id: Optional[str] = None,
        agent_definition_id: Optional[str] = None,
    ) -> Generator[SSEEvent, None, None]:
        """
        Start a streaming chat session via SSE.

        POST /api/chat/stream

        Yields typed :class:`~emergent.sse.SSEEvent` objects until the stream
        ends with a :class:`~emergent.sse.DoneEvent` or
        :class:`~emergent.sse.ErrorEvent`.

        Example::

            for event in client.chat.stream("What is a knowledge graph?"):
                if event.type == "token":
                    print(event.token, end="", flush=True)
                elif event.type == "done":
                    break
        """
        body: Dict[str, Any] = {"message": message}
        if conversation_id is not None:
            body["conversationId"] = conversation_id
        if canonical_id is not None:
            body["canonicalId"] = canonical_id
        if agent_definition_id is not None:
            body["agentDefinitionId"] = agent_definition_id

        with self._stream("/api/chat/stream", json=body) as chunks:
            yield from iter_sse_events(chunks)

    def ask(
        self,
        message: str,
        project_id: Optional[str] = None,
    ) -> Generator[SSEEvent, None, None]:
        """
        Ask a question and stream the answer via SSE.

        Uses ``POST /api/projects/:projectId/ask`` when *project_id* is given
        (or the client's default project context), otherwise falls back to
        ``POST /api/ask``.

        Example::

            for event in client.chat.ask("Summarise project goals"):
                if event.type == "token":
                    print(event.token, end="", flush=True)
        """
        pid = project_id or self._project_id
        if pid:
            path = f"/api/projects/{quote(pid, safe='')}/ask"
        else:
            path = "/api/ask"

        with self._stream(path, json={"message": message}) as chunks:
            yield from iter_sse_events(chunks)

    def query(
        self,
        message: str,
        project_id: Optional[str] = None,
    ) -> Generator[SSEEvent, None, None]:
        """
        Run a graph-query-agent stream.

        POST /api/projects/:projectId/query
        """
        pid = project_id or self._project_id
        if not pid:
            raise ValueError("project_id is required for query()")
        path = f"/api/projects/{quote(pid, safe='')}/query"
        with self._stream(path, json={"message": message}) as chunks:
            yield from iter_sse_events(chunks)

    def ask_collect(
        self,
        message: str,
        project_id: Optional[str] = None,
    ) -> str:
        """
        Convenience wrapper: stream ``ask()`` and return the full text.

        All ``token`` events are accumulated and the final string is returned.
        Tool calls and metadata events are silently ignored.
        """
        parts: List[str] = []
        for event in self.ask(message, project_id=project_id):
            if event.type == "token":
                parts.append(event.token)  # type: ignore[union-attr]
        return "".join(parts)
