/**
 * Agents API Client
 *
 * TypeScript client for agent management endpoints
 */

/**
 * Trigger type for agent execution
 * - 'schedule': Agent runs automatically on cron schedule
 * - 'manual': Agent only runs when manually triggered
 * - 'reaction': Agent runs in response to graph object events
 */
export type AgentTriggerType = 'schedule' | 'manual' | 'reaction';

/**
 * Execution mode for reaction agents
 * - 'suggest': Agent creates tasks for human review
 * - 'execute': Agent directly modifies the graph
 * - 'hybrid': Agent can do both based on confidence/operation type
 */
export type AgentExecutionMode = 'suggest' | 'execute' | 'hybrid';

/**
 * Events that can trigger a reaction agent
 */
export type ReactionEventType = 'created' | 'updated' | 'deleted';

/**
 * Concurrency strategy for reaction agents
 * - 'skip': Skip if already processing the same object
 * - 'parallel': Allow concurrent processing of the same object
 */
export type ConcurrencyStrategy = 'skip' | 'parallel';

/**
 * Configuration for reaction triggers
 */
export interface ReactionConfig {
  /** Object types to react to (empty array = all types) */
  objectTypes: string[];
  /** Events to react to */
  events: ReactionEventType[];
  /** How to handle concurrent events for the same object */
  concurrencyStrategy: ConcurrencyStrategy;
  /** Ignore events triggered by any agent */
  ignoreAgentTriggered: boolean;
  /** Ignore events triggered by this agent (default: true) */
  ignoreSelfTriggered: boolean;
}

/**
 * Capability restrictions for agents
 */
export interface AgentCapabilities {
  /** Can the agent create new objects */
  canCreateObjects?: boolean;
  /** Can the agent update existing objects */
  canUpdateObjects?: boolean;
  /** Can the agent delete objects */
  canDeleteObjects?: boolean;
  /** Can the agent create relationships */
  canCreateRelationships?: boolean;
  /** Restrict to specific object types (null = all types allowed) */
  allowedObjectTypes?: string[] | null;
}

/**
 * Agent configuration
 */
export interface Agent {
  id: string;
  /** Project this agent belongs to */
  projectId: string;
  name: string;
  /** Strategy type identifier - determines which strategy implementation to use */
  strategyType: string;
  prompt: string | null;
  cronSchedule: string;
  enabled: boolean;
  triggerType: AgentTriggerType;
  /** Configuration for reaction triggers (only when triggerType is 'reaction') */
  reactionConfig: ReactionConfig | null;
  /** Execution mode for the agent */
  executionMode: AgentExecutionMode;
  /** Capability restrictions for the agent */
  capabilities: AgentCapabilities | null;
  config: Record<string, any>;
  description: string | null;
  createdAt: string;
  updatedAt: string;
}

/**
 * Agent run history entry
 */
export interface AgentRun {
  id: string;
  agentId: string;
  status:
    | 'running'
    | 'success'
    | 'completed'
    | 'error'
    | 'failed'
    | 'paused'
    | 'cancelled'
    | 'skipped';
  startedAt: string;
  completedAt: string | null;
  durationMs: number | null;
  summary: Record<string, any> | null;
  errorMessage: string | null;
  skipReason: string | null;

  // Execution metrics
  stepCount: number;
  maxSteps: number | null;

  // Multi-agent coordination
  parentRunId: string | null;
  resumedFrom: string | null;
}

/**
 * Agent run message (conversation history)
 */
export interface AgentRunMessage {
  id: string;
  runId: string;
  role: 'system' | 'user' | 'assistant' | 'tool_result';
  content: Record<string, any>;
  stepNumber: number;
  createdAt: string;
}

/**
 * Agent run tool call (tool invocation record)
 */
export interface AgentRunToolCall {
  id: string;
  runId: string;
  messageId: string | null;
  toolName: string;
  input: Record<string, any>;
  output: Record<string, any>;
  status: 'completed' | 'error';
  durationMs: number;
  stepNumber: number;
  createdAt: string;
}

/**
 * Model configuration for agents
 */
export interface ModelConfig {
  name: string;
  temperature?: number;
  maxTokens?: number;
}

/**
 * Agent definition (configuration/manifest)
 */
export interface AgentDefinition {
  id: string;
  productId?: string;
  projectId: string;
  name: string;
  description?: string;
  systemPrompt?: string;
  model?: ModelConfig;
  tools: string[];
  flowType: string;
  isDefault: boolean;
  maxSteps?: number;
  defaultTimeout?: number;
  visibility: string;
  createdAt: string;
  updatedAt: string;
}

/**
 * Create agent definition payload
 */
export interface CreateAgentDefinitionPayload {
  name: string;
  description?: string;
  systemPrompt?: string;
  model?: ModelConfig;
  tools?: string[];
  flowType?: string;
  isDefault?: boolean;
  maxSteps?: number;
  defaultTimeout?: number;
  visibility?: string;
}

/**
 * Update agent definition payload
 */
export interface UpdateAgentDefinitionPayload {
  name?: string;
  description?: string;
  systemPrompt?: string;
  model?: ModelConfig;
  tools?: string[];
  flowType?: string;
  isDefault?: boolean;
  maxSteps?: number;
  defaultTimeout?: number;
  visibility?: string;
}

/**
 * Paginated response wrapper
 */
export interface PaginatedResponse<T> {
  items: T[];
  totalCount: number;
  limit: number;
  offset: number;
}

/**
 * Create agent payload
 */
export interface CreateAgentPayload {
  projectId: string;
  name: string;
  strategyType: string;
  prompt?: string;
  cronSchedule: string;
  enabled?: boolean;
  triggerType?: AgentTriggerType;
  reactionConfig?: ReactionConfig | null;
  executionMode?: AgentExecutionMode;
  capabilities?: AgentCapabilities | null;
  config?: Record<string, any>;
  description?: string;
}

/**
 * Update agent payload
 */
export interface UpdateAgentPayload {
  name?: string;
  prompt?: string;
  enabled?: boolean;
  cronSchedule?: string;
  triggerType?: AgentTriggerType;
  reactionConfig?: ReactionConfig | null;
  executionMode?: AgentExecutionMode;
  capabilities?: AgentCapabilities | null;
  config?: Record<string, any>;
  description?: string;
}

/**
 * API response wrapper
 */
export interface AgentApiResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
  message?: string;
}

/**
 * Pending event object (unprocessed graph object)
 */
export interface PendingEventObject {
  id: string;
  type: string;
  key: string;
  version: number;
  createdAt: string;
  updatedAt: string;
}

/**
 * Response for pending events query
 */
export interface PendingEventsResponse {
  /** Total count of objects matching the agent's filters */
  totalCount: number;
  /** Sample of unprocessed objects (limited to 100) */
  objects: PendingEventObject[];
  /** Agent's reaction config for reference */
  reactionConfig: {
    objectTypes: string[];
    events: string[];
  };
}

/**
 * Response for batch trigger
 */
export interface BatchTriggerResponse {
  /** Number of objects queued for processing */
  queued: number;
  /** Number of objects skipped (already processed or processing) */
  skipped: number;
  /** Details about skipped objects */
  skippedDetails: {
    objectId: string;
    reason: string;
  }[];
}

/**
 * API client interface
 */
export interface AgentsClient {
  /**
   * List all agents
   */
  listAgents(): Promise<Agent[]>;

  /**
   * Get agent by ID
   */
  getAgent(id: string): Promise<Agent | null>;

  /**
   * Get recent runs for an agent
   */
  getAgentRuns(id: string): Promise<AgentRun[]>;

  /**
   * Create a new agent
   */
  createAgent(payload: CreateAgentPayload): Promise<Agent>;

  /**
   * Update an agent
   */
  updateAgent(id: string, payload: UpdateAgentPayload): Promise<Agent>;

  /**
   * Delete an agent
   */
  deleteAgent(id: string): Promise<void>;

  /**
   * Trigger an immediate run
   */
  triggerAgent(
    id: string
  ): Promise<{ success: boolean; message?: string; error?: string }>;

  /**
   * Get pending events (unprocessed graph objects) for a reaction agent
   */
  getPendingEvents(id: string, limit?: number): Promise<PendingEventsResponse>;

  /**
   * Batch trigger a reaction agent for multiple objects
   */
  batchTrigger(id: string, objectIds: string[]): Promise<BatchTriggerResponse>;

  /**
   * Get messages (conversation history) for a run
   */
  getRunMessages(projectId: string, runId: string): Promise<AgentRunMessage[]>;

  /**
   * Get tool calls for a run
   */
  getRunToolCalls(
    projectId: string,
    runId: string
  ): Promise<AgentRunToolCall[]>;

  /**
   * Cancel a running agent run
   */
  cancelRun(agentId: string, runId: string): Promise<void>;

  /**
   * List agent definitions
   */
  listDefinitions(projectId: string): Promise<AgentDefinition[]>;

  /**
   * Get agent definition by ID
   */
  getDefinition(id: string): Promise<AgentDefinition | null>;

  /**
   * Create agent definition
   */
  createDefinition(
    projectId: string,
    payload: CreateAgentDefinitionPayload
  ): Promise<AgentDefinition>;

  /**
   * Update agent definition
   */
  updateDefinition(
    id: string,
    payload: UpdateAgentDefinitionPayload
  ): Promise<AgentDefinition>;

  /**
   * Delete agent definition
   */
  deleteDefinition(id: string): Promise<void>;

  /**
   * List all runs for a project with filtering
   */
  listProjectRuns(
    projectId: string,
    options?: {
      limit?: number;
      offset?: number;
      agentId?: string;
      status?: AgentRun['status'];
    }
  ): Promise<PaginatedResponse<AgentRun>>;
}

/**
 * Create agents API client
 *
 * @param apiBase - Base API URL from useApi hook
 * @param fetchJson - Fetch function from useApi hook
 * @returns Agents client
 */
export function createAgentsClient(
  apiBase: string,
  fetchJson: <T>(url: string, init?: any) => Promise<T>
): AgentsClient {
  const baseUrl = `${apiBase}/api/admin/agents`;

  return {
    async listAgents() {
      const response = await fetchJson<AgentApiResponse<Agent[]>>(baseUrl);
      return response.data || [];
    },

    async getAgent(id: string) {
      const response = await fetchJson<AgentApiResponse<Agent>>(
        `${baseUrl}/${id}`
      );
      return response.data || null;
    },

    async getAgentRuns(id: string) {
      const response = await fetchJson<AgentApiResponse<AgentRun[]>>(
        `${baseUrl}/${id}/runs`
      );
      return response.data || [];
    },

    async createAgent(payload: CreateAgentPayload) {
      const response = await fetchJson<AgentApiResponse<Agent>>(baseUrl, {
        method: 'POST',
        body: payload,
      });
      if (!response.success || !response.data) {
        throw new Error(response.error || 'Failed to create agent');
      }
      return response.data;
    },

    async updateAgent(id: string, payload: UpdateAgentPayload) {
      const response = await fetchJson<AgentApiResponse<Agent>>(
        `${baseUrl}/${id}`,
        {
          method: 'PATCH',
          body: payload,
        }
      );
      if (!response.success || !response.data) {
        throw new Error(response.error || 'Failed to update agent');
      }
      return response.data;
    },

    async deleteAgent(id: string) {
      const response = await fetchJson<AgentApiResponse<void>>(
        `${baseUrl}/${id}`,
        {
          method: 'DELETE',
        }
      );
      if (!response.success) {
        throw new Error(response.error || 'Failed to delete agent');
      }
    },

    async triggerAgent(id: string) {
      return fetchJson<{ success: boolean; message?: string; error?: string }>(
        `${baseUrl}/${id}/trigger`,
        {
          method: 'POST',
        }
      );
    },

    async getPendingEvents(id: string, limit = 100) {
      const response = await fetchJson<AgentApiResponse<PendingEventsResponse>>(
        `${baseUrl}/${id}/pending-events?limit=${limit}`
      );
      if (!response.success || !response.data) {
        throw new Error(response.error || 'Failed to get pending events');
      }
      return response.data;
    },

    async batchTrigger(id: string, objectIds: string[]) {
      const response = await fetchJson<AgentApiResponse<BatchTriggerResponse>>(
        `${baseUrl}/${id}/batch-trigger`,
        {
          method: 'POST',
          body: { objectIds },
        }
      );
      if (!response.success || !response.data) {
        throw new Error(response.error || 'Failed to batch trigger agent');
      }
      return response.data;
    },

    async getRunMessages(projectId: string, runId: string) {
      const response = await fetchJson<AgentApiResponse<AgentRunMessage[]>>(
        `${apiBase}/api/projects/${projectId}/agent-runs/${runId}/messages`
      );
      return response.data || [];
    },

    async getRunToolCalls(projectId: string, runId: string) {
      const response = await fetchJson<AgentApiResponse<AgentRunToolCall[]>>(
        `${apiBase}/api/projects/${projectId}/agent-runs/${runId}/tool-calls`
      );
      return response.data || [];
    },

    async cancelRun(agentId: string, runId: string) {
      const response = await fetchJson<AgentApiResponse<void>>(
        `${baseUrl}/${agentId}/runs/${runId}/cancel`,
        {
          method: 'POST',
        }
      );
      if (!response.success) {
        throw new Error(response.error || 'Failed to cancel run');
      }
    },

    async listDefinitions(projectId: string) {
      const response = await fetchJson<AgentApiResponse<AgentDefinition[]>>(
        `${apiBase}/api/admin/agent-definitions?projectId=${projectId}`
      );
      return response.data || [];
    },

    async getDefinition(id: string) {
      const response = await fetchJson<AgentApiResponse<AgentDefinition>>(
        `${apiBase}/api/admin/agent-definitions/${id}`
      );
      return response.data || null;
    },

    async createDefinition(
      projectId: string,
      payload: CreateAgentDefinitionPayload
    ) {
      const response = await fetchJson<AgentApiResponse<AgentDefinition>>(
        `${apiBase}/api/admin/agent-definitions`,
        {
          method: 'POST',
          body: { ...payload, projectId },
        }
      );
      if (!response.success || !response.data) {
        throw new Error(response.error || 'Failed to create agent definition');
      }
      return response.data;
    },

    async updateDefinition(id: string, payload: UpdateAgentDefinitionPayload) {
      const response = await fetchJson<AgentApiResponse<AgentDefinition>>(
        `${apiBase}/api/admin/agent-definitions/${id}`,
        {
          method: 'PATCH',
          body: payload,
        }
      );
      if (!response.success || !response.data) {
        throw new Error(response.error || 'Failed to update agent definition');
      }
      return response.data;
    },

    async deleteDefinition(id: string) {
      const response = await fetchJson<AgentApiResponse<void>>(
        `${apiBase}/api/admin/agent-definitions/${id}`,
        {
          method: 'DELETE',
        }
      );
      if (!response.success) {
        throw new Error(response.error || 'Failed to delete agent definition');
      }
    },

    async listProjectRuns(
      projectId: string,
      options?: {
        limit?: number;
        offset?: number;
        agentId?: string;
        status?: AgentRun['status'];
      }
    ) {
      const params = new URLSearchParams();
      if (options?.limit) params.set('limit', options.limit.toString());
      if (options?.offset) params.set('offset', options.offset.toString());
      if (options?.agentId) params.set('agentId', options.agentId);
      if (options?.status) params.set('status', options.status);

      const queryString = params.toString();
      const url = `${apiBase}/api/projects/${projectId}/agent-runs${
        queryString ? `?${queryString}` : ''
      }`;

      const response = await fetchJson<
        AgentApiResponse<PaginatedResponse<AgentRun>>
      >(url);
      if (!response.success || !response.data) {
        throw new Error(response.error || 'Failed to list project runs');
      }
      return response.data;
    },
  };
}
