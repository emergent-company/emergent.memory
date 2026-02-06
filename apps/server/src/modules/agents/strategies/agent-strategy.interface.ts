import { Agent } from '../../../entities/agent.entity';
import { EntityEvent } from '../../events/events.types';
import { ProcessingEventType } from '../../../entities/agent-processing-log.entity';

/**
 * Result returned from an agent strategy execution
 */
export interface AgentRunResult {
  /** Whether the run was successful */
  success: boolean;
  /** Summary data to store in agent_runs.summary */
  summary: Record<string, any>;
  /** Optional error message if success is false */
  errorMessage?: string;
  /** Optional skip reason if the agent decided to skip this run */
  skipReason?: string;
}

/**
 * Context passed to agent strategies during execution
 */
export interface AgentExecutionContext {
  /** The agent configuration */
  agent: Agent;
  /** Timestamp when the run started */
  startedAt: Date;
  /** Run ID for logging purposes */
  runId: string;
  /** Langfuse trace ID for observability (optional) */
  traceId?: string;
  /** Reaction context - only present for reaction-triggered agents */
  reaction?: ReactionContext;
}

/**
 * Context for reaction-triggered agent execution
 * Contains information about the triggering event
 */
export interface ReactionContext {
  /** The graph object ID that triggered the reaction */
  graphObjectId: string;
  /** The version of the graph object */
  objectVersion: number;
  /** The type of the graph object (e.g., 'Person', 'Company') */
  objectType: string;
  /** The event type that triggered the reaction */
  eventType: ProcessingEventType;
  /** The project ID */
  projectId: string;
  /** The processing log entry ID */
  processingLogId: string;
  /** The full event payload (for advanced use cases) */
  event: EntityEvent;
}

/**
 * Interface for agent strategies
 *
 * Each strategy type (e.g., 'merge-suggestion') has a corresponding strategy
 * that implements the actual logic for that agent type.
 */
export interface AgentStrategy {
  /**
   * The strategy type this strategy handles (must match Agent.strategyType)
   */
  readonly role: string;

  /**
   * Execute the agent's logic
   *
   * @param context - Execution context with agent config and run info
   * @returns Result of the execution
   */
  execute(context: AgentExecutionContext): Promise<AgentRunResult>;

  /**
   * Optional: Check if the agent should skip this run
   * Called before execute() to allow early exit without running full logic
   *
   * @param context - Execution context
   * @returns Skip reason if should skip, undefined to continue
   */
  shouldSkip?(context: AgentExecutionContext): Promise<string | undefined>;
}
