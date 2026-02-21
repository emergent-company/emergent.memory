/**
 * AgentChatTransport — Custom ChatTransport implementation that bridges the
 * Go backend's SSE chat protocol with the Vercel AI SDK's UIMessageChunk stream.
 *
 * The Go backend (POST /api/chat/stream) emits SSE events in this format:
 *   data: {"type":"meta","conversationId":"..."}
 *   data: {"type":"token","token":"Hello"}
 *   data: {"type":"mcp_tool","tool":"search_entities","status":"started"}
 *   data: {"type":"mcp_tool","tool":"search_entities","status":"completed","result":{...}}
 *   data: {"type":"error","error":"..."}
 *   data: {"type":"done"}
 *
 * This transport translates those into AI SDK UIMessageChunk events:
 *   { type: 'start' } + { type: 'start-step' }
 *   { type: 'text-start', id } / { type: 'text-delta', id, delta } / { type: 'text-end', id }
 *   { type: 'tool-input-start', toolCallId, toolName } / { type: 'tool-input-available', ... }
 *   { type: 'tool-output-available', toolCallId, output }
 *   { type: 'finish-step' } + { type: 'finish', finishReason: 'stop' }
 */

import type { UIMessage } from '@ai-sdk/react';

// UIMessageChunk type — matches the discriminated union from the AI SDK.
// We define it here to avoid importing internal types.
type UIMessageChunk =
  | { type: 'start'; messageId?: string }
  | {
      type: 'finish';
      finishReason?:
        | 'stop'
        | 'length'
        | 'tool-calls'
        | 'error'
        | 'other'
        | 'unknown';
    }
  | { type: 'start-step' }
  | { type: 'finish-step' }
  | { type: 'text-start'; id: string }
  | { type: 'text-delta'; id: string; delta: string }
  | { type: 'text-end'; id: string }
  | { type: 'tool-input-start'; toolCallId: string; toolName: string }
  | {
      type: 'tool-input-available';
      toolCallId: string;
      toolName: string;
      input: unknown;
    }
  | {
      type: 'tool-output-available';
      toolCallId: string;
      output: unknown;
    }
  | { type: 'tool-output-error'; toolCallId: string; errorText: string }
  | { type: 'error'; errorText: string };

/** Go backend SSE event shapes */
interface GoSSEEvent {
  type: 'meta' | 'token' | 'mcp_tool' | 'error' | 'done';
  // meta
  conversationId?: string;
  // token
  token?: string;
  // mcp_tool
  tool?: string;
  status?: 'started' | 'completed' | 'error';
  result?: unknown;
  args?: unknown;
  error?: string;
}

export interface AgentChatTransportOptions {
  /** The Go backend SSE endpoint. Defaults to '/api/chat/stream'. */
  api?: string;
  /** Function that returns auth headers (from useApi). */
  buildHeaders: (opts: { json: boolean }) => Record<string, string>;
  /** Active project ID — sent as X-Project-ID header. */
  projectId: string;
  /** Existing conversation ID (for multi-turn). Omit for new conversations. */
  conversationId?: string;
  /** Agent definition ID to use for new conversations. */
  agentDefinitionId: string;
  /**
   * Callback fired when the backend creates a new conversation.
   * Receives the conversation ID from the `meta` SSE event.
   */
  onConversationCreated?: (conversationId: string) => void;
}

/**
 * ChatTransport<UIMessage> implementation for agent-backed conversations.
 * Sends messages to the Go backend's /api/chat/stream SSE endpoint and
 * translates the response into AI SDK UIMessageChunk streams.
 */
export class AgentChatTransport {
  private api: string;
  private buildHeaders: AgentChatTransportOptions['buildHeaders'];
  private projectId: string;
  private conversationId?: string;
  private agentDefinitionId: string;
  private onConversationCreated?: (conversationId: string) => void;

  constructor(options: AgentChatTransportOptions) {
    this.api = options.api ?? '/api/chat/stream';
    this.buildHeaders = options.buildHeaders;
    this.projectId = options.projectId;
    this.conversationId = options.conversationId;
    this.agentDefinitionId = options.agentDefinitionId;
    this.onConversationCreated = options.onConversationCreated;
  }

  async sendMessages(options: {
    trigger: 'submit-message' | 'regenerate-message';
    chatId: string;
    messageId: string | undefined;
    messages: UIMessage[];
    abortSignal: AbortSignal | undefined;
  }): Promise<ReadableStream<UIMessageChunk>> {
    // Extract the latest user message text from the messages array
    const lastMessage = options.messages[options.messages.length - 1];
    const userText = lastMessage?.parts
      ?.filter((p): p is { type: 'text'; text: string } => p.type === 'text')
      .map((p) => p.text)
      .join('');

    if (!userText) {
      throw new Error('No user message text found');
    }

    // Build request body matching Go backend's StreamRequest
    const body: Record<string, unknown> = {
      message: userText,
    };
    if (this.conversationId) {
      body.conversationId = this.conversationId;
    } else {
      // New conversation — include agentDefinitionId
      body.agentDefinitionId = this.agentDefinitionId;
    }

    const headers = this.buildHeaders({ json: true });

    // POST to the Go backend
    const response = await fetch(this.api, {
      method: 'POST',
      headers: {
        ...headers,
        'X-Project-ID': this.projectId,
      },
      body: JSON.stringify(body),
      signal: options.abortSignal,
    });

    if (!response.ok) {
      let errorText = `HTTP ${response.status}`;
      try {
        const errorBody = await response.text();
        errorText = errorBody || errorText;
      } catch {
        // ignore
      }
      throw new Error(`Agent chat request failed: ${errorText}`);
    }

    if (!response.body) {
      throw new Error('No response body from agent chat endpoint');
    }

    // Parse the SSE stream and translate to UIMessageChunk
    return this.parseSSEStream(response.body);
  }

  async reconnectToStream(_options: {
    chatId: string;
  }): Promise<ReadableStream<UIMessageChunk> | null> {
    // No reconnection support
    return null;
  }

  /**
   * Parses an SSE byte stream from the Go backend and yields UIMessageChunk events.
   */
  private parseSSEStream(
    body: ReadableStream<Uint8Array>
  ): ReadableStream<UIMessageChunk> {
    const onConversationCreated = this.onConversationCreated;

    // State for tracking active text/tool parts
    let textPartCounter = 0;
    let toolCallCounter = 0;
    let currentTextPartId: string | null = null;
    let hasEmittedStart = false;
    // Map from tool name to the tool call ID for matching started→completed
    const activeToolCalls = new Map<string, string>();

    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    return new ReadableStream<UIMessageChunk>({
      async pull(controller) {
        // Helper to enqueue a chunk
        const emit = (chunk: UIMessageChunk) => controller.enqueue(chunk);

        // Helper to close any active text part
        const closeTextPart = () => {
          if (currentTextPartId) {
            emit({ type: 'text-end', id: currentTextPartId });
            currentTextPartId = null;
          }
        };

        try {
          while (true) {
            const { done, value } = await reader.read();

            if (done) {
              // Stream ended — clean up
              closeTextPart();
              if (hasEmittedStart) {
                emit({ type: 'finish-step' });
                emit({ type: 'finish', finishReason: 'stop' });
              }
              controller.close();
              return;
            }

            buffer += decoder.decode(value, { stream: true });

            // Process complete SSE events from the buffer.
            // SSE events are separated by double newlines.
            let eventBoundary: number;
            while ((eventBoundary = buffer.indexOf('\n\n')) !== -1) {
              const eventBlock = buffer.slice(0, eventBoundary);
              buffer = buffer.slice(eventBoundary + 2);

              // Extract data lines from the event block
              const dataLines: string[] = [];
              for (const line of eventBlock.split('\n')) {
                if (line.startsWith('data: ')) {
                  dataLines.push(line.slice(6));
                } else if (line.startsWith('data:')) {
                  dataLines.push(line.slice(5));
                }
                // Skip event:, id:, comment lines
              }

              if (dataLines.length === 0) continue;

              const dataStr = dataLines.join('\n');
              let event: GoSSEEvent;
              try {
                event = JSON.parse(dataStr);
              } catch {
                console.warn(
                  '[AgentChatTransport] Failed to parse SSE data:',
                  dataStr
                );
                continue;
              }

              // Translate Go SSE event → UIMessageChunk(s)
              switch (event.type) {
                case 'meta': {
                  if (!hasEmittedStart) {
                    emit({ type: 'start' });
                    emit({ type: 'start-step' });
                    hasEmittedStart = true;
                  }
                  // Notify caller of the conversation ID
                  if (event.conversationId && onConversationCreated) {
                    onConversationCreated(event.conversationId);
                  }
                  break;
                }

                case 'token': {
                  if (!hasEmittedStart) {
                    emit({ type: 'start' });
                    emit({ type: 'start-step' });
                    hasEmittedStart = true;
                  }
                  const text = event.token ?? '';
                  if (text) {
                    // Start a new text part if needed
                    if (!currentTextPartId) {
                      currentTextPartId = `text-${textPartCounter++}`;
                      emit({
                        type: 'text-start',
                        id: currentTextPartId,
                      });
                    }
                    emit({
                      type: 'text-delta',
                      id: currentTextPartId,
                      delta: text,
                    });
                  }
                  break;
                }

                case 'mcp_tool': {
                  if (!hasEmittedStart) {
                    emit({ type: 'start' });
                    emit({ type: 'start-step' });
                    hasEmittedStart = true;
                  }

                  const toolName = event.tool ?? 'unknown_tool';

                  if (event.status === 'started') {
                    // Close any active text part before tool call
                    closeTextPart();

                    const toolCallId = `tool-${toolCallCounter++}`;
                    activeToolCalls.set(toolName, toolCallId);

                    emit({
                      type: 'tool-input-start',
                      toolCallId,
                      toolName,
                    });
                    emit({
                      type: 'tool-input-available',
                      toolCallId,
                      toolName,
                      input: event.args ?? {},
                    });
                  } else if (event.status === 'completed') {
                    const toolCallId =
                      activeToolCalls.get(toolName) ??
                      `tool-${toolCallCounter++}`;
                    activeToolCalls.delete(toolName);

                    emit({
                      type: 'tool-output-available',
                      toolCallId,
                      output: event.result ?? '',
                    });
                  } else if (event.status === 'error') {
                    const toolCallId =
                      activeToolCalls.get(toolName) ??
                      `tool-${toolCallCounter++}`;
                    activeToolCalls.delete(toolName);

                    emit({
                      type: 'tool-output-error',
                      toolCallId,
                      errorText: event.error ?? 'Tool execution failed',
                    });
                  }
                  break;
                }

                case 'error': {
                  emit({
                    type: 'error',
                    errorText: event.error ?? 'Unknown error',
                  });
                  break;
                }

                case 'done': {
                  closeTextPart();
                  if (hasEmittedStart) {
                    emit({ type: 'finish-step' });
                    emit({ type: 'finish', finishReason: 'stop' });
                  }
                  controller.close();
                  return;
                }
              }
            }
          }
        } catch (err) {
          if (err instanceof DOMException && err.name === 'AbortError') {
            closeTextPart();
            if (hasEmittedStart) {
              emit({ type: 'finish-step' });
              emit({ type: 'finish', finishReason: 'stop' });
            }
            controller.close();
          } else {
            controller.error(err);
          }
        }
      },

      cancel() {
        reader.cancel();
      },
    });
  }
}
