# Chat

Chat lets you ask natural language questions about your knowledge graph and documents. The AI retrieves relevant context from your project and generates a grounded response with citations.

## Starting a Conversation

=== "Admin UI"
    Click **Chat** in the sidebar and start typing.

=== "API"
    ```http
    POST /api/chat/conversations
    Content-Type: application/json

    {
      "title": "Database architecture questions",
      "projectId": "proj_xyz789"
    }
    ```

---

## Sending a Message

=== "API (streaming — recommended)"
    Returns a server-sent event (SSE) stream with partial tokens as they are generated:

    ```http
    POST /api/chat/stream
    Content-Type: application/json

    {
      "conversationId": "<uuid>",
      "message": "What decisions did we make about the database?"
    }
    ```

    SSE events:
    ```
    data: {"type":"token","content":"We decided "}
    data: {"type":"token","content":"to use PostgreSQL "}
    data: {"type":"citations","citations":[{"objectId":"...","title":"Use PostgreSQL"}]}
    data: {"type":"done"}
    ```

=== "API (non-streaming)"
    ```http
    POST /api/chat/{conversationId}/messages
    Content-Type: application/json

    {
      "role": "user",
      "content": "What decisions did we make about the database?"
    }
    ```

---

## Conversations

### List conversations

```http
GET /api/chat/conversations
```

### Get a conversation with messages

```http
GET /api/chat/{id}
```

### Update a conversation

```http
PATCH /api/chat/{id}
Content-Type: application/json

{
  "title": "Renamed conversation",
  "draftText": "Work in progress..."
}
```

### Delete a conversation

```http
DELETE /api/chat/{id}
```

---

## Conversation fields

| Field | Description |
|---|---|
| `title` | Display name for the conversation |
| `isPrivate` | If true, only visible to you (default: `true`) |
| `enabledTools` | List of tool names the AI can use in this conversation |
| `agentDefinitionId` | Optional: link to a specific agent definition to use as the AI persona |
| `objectId` / `canonicalId` | Optional: pin the conversation to a specific graph object for focused Q&A |

---

## Citations

Every assistant response includes citations — references to the graph objects or document chunks used to generate the answer. Citations appear in the `citations` field of the message:

```json
{
  "role": "assistant",
  "content": "We decided to use PostgreSQL because of team expertise.",
  "citations": [
    {
      "type": "graph_object",
      "objectId": "<canonical_id>",
      "title": "Use PostgreSQL",
      "snippet": "Team expertise and existing infrastructure"
    }
  ]
}
```

---

## Tools

The chat AI can call tools to answer questions. The available tools depend on what is enabled for the conversation:

| Tool | What it does |
|---|---|
| `graph_query` | Searches and retrieves graph objects |
| `search` | Runs hybrid search across documents and objects |
| `graph_create_object` | Creates a new graph object (write-enabled conversations) |

Control which tools are available via `enabledTools` when creating or updating a conversation.

---

## Tips

- **Be specific about type**: "What `Decision` objects relate to authentication?" gives more precise results than "What do we know about auth?".
- **Reference objects by name**: The AI can look up objects by their title or key property.
- **Long conversations**: The platform maintains a context summary as conversations grow to keep responses relevant.
