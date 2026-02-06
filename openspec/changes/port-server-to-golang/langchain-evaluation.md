# LangChain/LangGraph Go Evaluation

## Executive Summary

**Recommendation: Google ADK-Go for Extraction Pipeline** (Updated Jan 2026)

~~LangChainGo provides sufficient coverage for basic chat and tool calling but lacks the advanced StateGraph/LangGraph features needed for the complex extraction pipeline.~~

**UPDATE**: Google's Agent Development Kit for Go ([google/adk-go](https://github.com/google/adk-go), 6657 stars) provides a viable native Go solution:

1. **Use Native Vertex AI SDK** for chat completion (already implemented)
2. **Use Google ADK-Go** for extraction pipeline with:
   - `SequentialAgent` for multi-step pipelines
   - `LoopAgent` for retry logic
   - `OutputSchema` for structured JSON output
   - `OutputKey` for state passing between agents
3. **No Python sidecar needed** - full Go implementation possible

---

## Current NestJS LangChain Usage Analysis

### Packages Used

| Package                                    | Version | Usage                          |
| ------------------------------------------ | ------- | ------------------------------ |
| `@langchain/core`                          | ^1.1.0  | Tools, messages, callbacks     |
| `@langchain/google-vertexai`               | ^1.0.4  | ChatVertexAI for Gemini        |
| `@langchain/google-genai`                  | ^2.0.0  | ChatGoogleGenerativeAI         |
| `@langchain/langgraph`                     | ^1.0.2  | StateGraph, createReactAgent   |
| `@langchain/langgraph-checkpoint-postgres` | ^1.0.0  | PostgresSaver for persistence  |
| `@langchain/textsplitters`                 | ^1.0.1  | RecursiveCharacterTextSplitter |

### Feature Usage Matrix

| Feature                   | Used In             | Complexity | Go Equivalent                |
| ------------------------- | ------------------- | ---------- | ---------------------------- |
| **Chat Completion**       | Chat module         | Low        | Native Vertex AI client      |
| **Streaming**             | Chat module         | Low        | Native SSE implementation    |
| **Tool Calling**          | Chat SDK tools      | Medium     | LangChainGo `tools` package  |
| **DynamicStructuredTool** | 10+ custom tools    | Medium     | Go struct-based tools        |
| **ReAct Agent**           | Chat, Reactions     | Medium     | LangChainGo `agents` package |
| **StateGraph**            | Extraction pipeline | High       | **NOT AVAILABLE in Go**      |
| **Conditional Edges**     | Extraction pipeline | High       | **NOT AVAILABLE in Go**      |
| **Annotation/Reducers**   | Extraction state    | High       | **NOT AVAILABLE in Go**      |
| **PostgresSaver**         | Checkpointing       | Medium     | Custom implementation needed |
| **Text Splitters**        | Document chunking   | Low        | LangChainGo `textsplitter`   |

---

## LangChainGo Feature Coverage

### Available Features (v0.1.14)

| Category           | Features                                              | Status    |
| ------------------ | ----------------------------------------------------- | --------- |
| **LLMs**           | OpenAI, Anthropic, Gemini, Vertex AI, Ollama, Bedrock | Supported |
| **Embeddings**     | OpenAI, Vertex AI, Hugging Face, Jina, Voyage AI      | Supported |
| **Vector Stores**  | pgvector, Chroma, Pinecone, Qdrant, Weaviate, Milvus  | Supported |
| **Text Splitters** | RecursiveCharacterTextSplitter, TokenTextSplitter     | Supported |
| **Tools**          | DuckDuckGo, SerpAPI, Wikipedia, SQL Database, Scraper | Supported |
| **Agents**         | MRKL Agent (ReAct-style)                              | Supported |
| **Chains**         | LLM Chain, Retrieval QA, Summarization, SQL           | Supported |
| **Memory**         | ConversationBuffer, SQLite3, MongoDB, Zep             | Supported |
| **Callbacks**      | Custom handlers, streaming                            | Supported |

### Missing Features (Critical for Extraction)

| Feature               | LangGraph JS/Python        | LangChainGo       | Impact                          |
| --------------------- | -------------------------- | ----------------- | ------------------------------- |
| **StateGraph**        | Full support               | **Not available** | Cannot port extraction pipeline |
| **Annotation.Root()** | Typed state with reducers  | **Not available** | Cannot manage complex state     |
| **Conditional Edges** | Dynamic routing            | **Not available** | Cannot implement retry logic    |
| **createReactAgent**  | Prebuilt agent             | Partial (MRKL)    | Different API                   |
| **PostgresSaver**     | Conversation checkpointing | **Not available** | Need custom implementation      |

---

## Go LangGraph Alternatives Evaluated

### LangGraph Go Ports (Immature)

| Project                                                                       | Stars | Maturity | Assessment                                  |
| ----------------------------------------------------------------------------- | ----- | -------- | ------------------------------------------- |
| [piotrlaczkowski/GoLangGraph](https://github.com/piotrlaczkowski/GoLangGraph) | 11    | Early    | Too immature, limited features              |
| [futurxlab/golanggraph](https://github.com/futurxlab/golanggraph)             | 6     | Early    | Actively developed but not production-ready |
| [KhanhD1nh/langgraph-sdk-go](https://github.com/KhanhD1nh/langgraph-sdk-go)   | 1     | Early    | SDK client, not full implementation         |
| [tmc/langgraphgo](https://github.com/tmc/langgraphgo)                         | 232   | Early    | Linear graph only, no conditional edges     |

**Conclusion**: No mature LangGraph Go implementation exists.

---

## Google Agent Development Kit (ADK) - NEW OPTION

### Overview

**[google/adk-go](https://github.com/google/adk-go)** (6657 stars, Apache 2.0, updated Jan 2026)

Google's official Agent Development Kit for Go. "An open-source, code-first Go toolkit for building, evaluating, and deploying sophisticated AI agents with flexibility and control."

### Key Features

| Feature                    | Available | Notes                                                        |
| -------------------------- | --------- | ------------------------------------------------------------ |
| **LLM Agent (llmagent)**   | ✅        | Full LLM agent with tools, callbacks, structured output      |
| **Sequential Agent**       | ✅        | Run sub-agents in sequence (like pipeline)                   |
| **Loop Agent**             | ✅        | Repeat sub-agents N times or until escalation                |
| **Parallel Agent**         | ✅        | Run sub-agents concurrently                                  |
| **Custom Agents**          | ✅        | `agent.New()` for custom logic                               |
| **OutputSchema (JSON)**    | ✅        | `genai.Schema` for structured JSON output                    |
| **OutputKey (State)**      | ✅        | Store agent output in session state for chaining             |
| **Instruction Templates**  | ✅        | `{variable}` placeholders resolved from state                |
| **Tools**                  | ✅        | Function tools, MCP toolset, Gemini tools                    |
| **Before/After Callbacks** | ✅        | Agent, model, and tool callbacks                             |
| **Gemini Integration**     | ✅        | Native `gemini.NewModel()` with genai SDK                    |
| **Vertex AI Integration**  | ✅        | Via genai ClientConfig                                       |
| **State Management**       | ✅        | Session state across agents                                  |
| **Conditional Edges**      | ⚠️        | Not LangGraph-style, but via `Escalate` action in loop agent |

### Workflow Agent Patterns

```go
// Sequential Pipeline (like our extraction)
codePipelineAgent, _ := sequentialagent.New(sequentialagent.Config{
    AgentConfig: agent.Config{
        Name: "ExtractionPipeline",
        SubAgents: []agent.Agent{
            entityExtractorAgent,      // Step 1: Extract entities
            relationshipBuilderAgent,  // Step 2: Build relationships
            verificationAgent,         // Step 3: Verify results
        },
    },
})

// Loop with conditional exit (via Escalate)
loopAgent, _ := loopagent.New(loopagent.Config{
    MaxIterations: 3,  // Max retries
    AgentConfig: agent.Config{
        Name: "RetryableExtraction",
        SubAgents: []agent.Agent{extractorWithRetry},
    },
})

// LLM Agent with structured output
extractorAgent, _ := llmagent.New(llmagent.Config{
    Name:  "EntityExtractor",
    Model: geminiModel,
    Instruction: `Extract entities from the document.

Document: {document_content}`,
    OutputSchema: &genai.Schema{
        Type: "array",
        Items: &genai.Schema{
            Type: "object",
            Properties: map[string]*genai.Schema{
                "name": {Type: "string"},
                "type": {Type: "string"},
            },
        },
    },
    OutputKey: "extracted_entities",  // Stored in session state
})
```

### Assessment for ObjectExtractionWorker

| Requirement                 | ADK-Go Support     | Notes                                |
| --------------------------- | ------------------ | ------------------------------------ |
| Multi-step pipeline         | ✅ Sequential      | 4 agents in sequence                 |
| Retry on failure            | ✅ Loop + Escalate | Loop until success or max iterations |
| State between steps         | ✅ OutputKey       | Pass entities → relationships        |
| JSON structured output      | ✅ OutputSchema    | For entity/relationship extraction   |
| Gemini/Vertex AI            | ✅ Native          | genai SDK integration                |
| Parallel verification       | ✅ Parallel        | Verify entities in parallel          |
| Complex conditional routing | ⚠️ Limited         | Not as flexible as LangGraph edges   |
| Annotation/Reducers         | ❌ Not available   | Would need custom state management   |

### Comparison: ADK-Go vs LangGraph

| Capability           | LangGraph (Python/JS) | ADK-Go               | Verdict                 |
| -------------------- | --------------------- | -------------------- | ----------------------- |
| Graph-based workflow | ✅ StateGraph         | ⚠️ Agent composition | LangGraph more flexible |
| Conditional edges    | ✅ Full support       | ⚠️ Via Escalate only | LangGraph more flexible |
| State annotations    | ✅ Reducers           | ⚠️ OutputKey only    | LangGraph more powerful |
| Sequential execution | ✅ Yes                | ✅ SequentialAgent   | Equal                   |
| Retry/loop           | ✅ Yes                | ✅ LoopAgent         | Equal                   |
| Structured output    | ✅ with_structured    | ✅ OutputSchema      | Equal                   |
| Go native            | ❌ No                 | ✅ Yes               | ADK-Go wins             |
| Maturity             | ✅ Production         | ⚠️ New (v0.x)        | LangGraph more mature   |

### Recommendation Update

**ADK-Go is viable for a simplified ObjectExtractionWorker** that:

1. Uses SequentialAgent for the 4-step pipeline
2. Uses LoopAgent for retry logic (with `Escalate` to break)
3. Uses OutputKey/state to pass data between agents
4. Uses OutputSchema for structured JSON extraction

**Tradeoffs**:

- ✅ Native Go, no Python sidecar needed
- ✅ Google-supported, actively maintained
- ⚠️ Less flexible than LangGraph conditional edges
- ⚠️ May need to simplify some complex routing logic

### ADK-Go Installation

```go
// go.mod
require google.golang.org/adk latest

// Requires Go 1.24+ (as of Jan 2026)
```

### Dependencies

```go
google.golang.org/genai v1.40.0+  // Gemini SDK
google.golang.org/adk             // Agent Development Kit
```

---

## Recommended Approach

### Option Analysis (Updated Jan 2026)

| Option                      | Pros                                | Cons                                   | Effort    |
| --------------------------- | ----------------------------------- | -------------------------------------- | --------- |
| **A. Full LangChainGo**     | Single language, no sidecar         | Missing StateGraph, rebuild extraction | High      |
| **B. Python Sidecar**       | Reuse existing code, full LangGraph | Additional service, network overhead   | Medium    |
| **C. Custom Go StateGraph** | Native Go, no dependencies          | Significant development effort         | Very High |
| **D. Hybrid Approach**      | Best of both worlds                 | Some complexity                        | Medium    |
| **E. Google ADK-Go** ⭐ NEW | Native Go, Google-supported, active | May need to simplify some routing      | Medium    |

### Updated Recommendation: Option E - Google ADK-Go

**NEW**: With the discovery of Google's Agent Development Kit for Go (ADK-Go), we can implement the extraction pipeline natively in Go without a Python sidecar.

```
┌─────────────────────────────────────────────────────────────────┐
│                         Go Server                                │
├─────────────────────────────────────────────────────────────────┤
│  Chat Module (Native Vertex AI SDK - already implemented)       │
├─────────────────────────────────────────────────────────────────┤
│  Extraction Pipeline (Google ADK-Go)                             │
│  ├── SequentialAgent: 4-step extraction pipeline                │
│  │   ├── EntityExtractorAgent (LLMAgent + OutputSchema)         │
│  │   ├── RelationshipBuilderAgent (LLMAgent + OutputSchema)     │
│  │   ├── EntityVerificationAgent (LLMAgent)                     │
│  │   └── RelationshipVerificationAgent (LLMAgent)               │
│  ├── LoopAgent: Retry with max iterations                       │
│  └── State: Pass data via OutputKey between agents              │
└─────────────────────────────────────────────────────────────────┘
```

### Fallback: Option D - Hybrid Approach (if ADK-Go insufficient)

```
┌─────────────────────────────────────────────────────────────────┐
│                         Go Server                                │
├─────────────────────────────────────────────────────────────────┤
│  Chat Module (Native Go)                                        │
│  ├── Vertex AI Client (already implemented)                    │
│  ├── SSE Streaming (already implemented)                       │
│  ├── Tool Calling (simple tools only)                          │
│  └── Conversation CRUD                                          │
├─────────────────────────────────────────────────────────────────┤
│  Extraction Jobs (gRPC/HTTP to Python)                          │
│  └── Enqueue job → Python Worker processes → Callback to Go     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ gRPC/HTTP
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Python Sidecar                              │
├─────────────────────────────────────────────────────────────────┤
│  LangGraph Extraction Pipeline                                   │
│  ├── StateGraph with conditional edges                          │
│  ├── Entity extraction with retries                             │
│  ├── Relationship building                                       │
│  └── Verification nodes                                          │
└─────────────────────────────────────────────────────────────────┘
```

### Implementation Plan (Updated for ADK-Go)

#### Phase 1: Chat (Already Done)

- [x] Native Vertex AI client for Gemini
- [x] SSE streaming implementation
- [x] Conversation CRUD
- [x] E2E tests for chat

#### Phase 2: Extraction Job Services (Done)

- [x] `ObjectExtractionJobsService` (27 tests passing)
- [x] `DocumentParsingJobsService` (20 tests passing)
- [x] `ChunkEmbeddingJobsService`
- [x] `GraphEmbeddingJobsService` (18 tests passing)

#### Phase 3: ADK-Go Integration (NEW)

- [ ] Add `google.golang.org/adk` dependency
- [ ] Create `pkg/adk/` package for ADK integration
- [ ] Configure Gemini model with genai SDK
- [ ] Implement extraction schema types

#### Phase 4: ObjectExtractionWorker with ADK-Go (NEW)

- [ ] Create EntityExtractorAgent (LLMAgent with OutputSchema)
- [ ] Create RelationshipBuilderAgent (LLMAgent with OutputSchema)
- [ ] Create VerificationAgent (optional, simplified)
- [ ] Compose with SequentialAgent for pipeline
- [ ] Add LoopAgent for retry logic
- [ ] Integrate with ObjectExtractionJobsService
- [ ] Add E2E tests

#### Phase 5: Fallback (If Needed)

If ADK-Go proves insufficient for complex routing:

- [ ] Create Python sidecar with FastAPI/gRPC
- [ ] Port existing LangGraph extraction code
- [ ] Define job queue interface

---

## Chat Module: What We Already Have

The Go server already implements chat without LangChain:

```go
// pkg/llm/vertex/client.go
type Client struct {
    model *genai.GenerativeModel
}

func (c *Client) GenerateContentStream(ctx context.Context, messages []Message) (<-chan StreamChunk, error) {
    // Direct Vertex AI SDK usage
}
```

This approach is:

- **Simpler**: No LangChain abstraction layer
- **Faster**: Direct SDK calls, no overhead
- **Maintainable**: Less dependencies to update

For chat, LangChainGo adds no significant value since we're already using the native Vertex AI SDK directly.

---

## MCP Integration Consideration

For MCP (Model Context Protocol) tool calling, we have options:

1. **Native Go Implementation**: Implement MCP protocol directly
2. **Use mark3labs/mcp-go**: Existing Go MCP SDK

Recommendation: Evaluate `mark3labs/mcp-go` for MCP integration as it's more mature than rolling our own.

---

## Conclusion (Updated Jan 2026)

| Workflow                | Approach                      | Rationale                                      |
| ----------------------- | ----------------------------- | ---------------------------------------------- |
| **Chat + Streaming**    | Native Go (Vertex AI SDK)     | Already implemented, simpler                   |
| **Simple Tools**        | Native Go                     | Easy to implement, no LangChain needed         |
| **Complex Agents**      | Google ADK-Go ⭐ NEW          | Native Go, Google-supported, sequential agents |
| **Extraction Pipeline** | Google ADK-Go ⭐ NEW          | SequentialAgent + LoopAgent + OutputSchema     |
| **MCP Integration**     | ADK-Go `mcptoolset` or mcp-go | ADK-Go has built-in MCP support                |

**Key Update**: The discovery of Google's Agent Development Kit (ADK-Go) changes our recommendation. We can now implement the extraction pipeline natively in Go using:

- `SequentialAgent` for the 4-step extraction pipeline
- `LoopAgent` for retry logic with max iterations
- `OutputSchema` for structured JSON extraction
- `OutputKey` for state passing between agents

This eliminates the need for a Python sidecar, simplifying deployment and reducing operational complexity.
