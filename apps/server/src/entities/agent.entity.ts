import {
  Entity,
  Column,
  PrimaryGeneratedColumn,
  CreateDateColumn,
  UpdateDateColumn,
  Index,
  ManyToOne,
  JoinColumn,
} from 'typeorm';
import { Project } from './project.entity';

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
 * Agent Entity
 *
 * Represents a configurable background agent that runs periodically.
 * Agents can be scheduled via cron expressions and have admin-tunable prompts.
 */
@Entity({ schema: 'kb', name: 'agents' })
@Index(['strategyType'])
@Index(['enabled'])
@Index(['projectId'])
export class Agent {
  @PrimaryGeneratedColumn('uuid')
  id: string;

  /**
   * The project this agent belongs to
   */
  @Column({ name: 'project_id', type: 'uuid' })
  projectId: string;

  @ManyToOne(() => Project, { onDelete: 'CASCADE' })
  @JoinColumn({ name: 'project_id' })
  project?: Project;

  /**
   * Human-readable name for the agent
   */
  @Column({ type: 'text' })
  name: string;

  /**
   * Strategy type identifier used by the strategy registry.
   * Multiple agents can share the same strategy type.
   * e.g., 'merge-suggestion', 'cleanup', 'summary'
   */
  @Column({ name: 'strategy_type', type: 'text' })
  strategyType: string;

  /**
   * Admin-tunable prompt/configuration for the agent
   * Stored as text to allow for multi-line prompts
   */
  @Column({ type: 'text', nullable: true })
  prompt: string | null;

  /**
   * Cron expression for scheduling
   * Example: every 3 minutes = "* /3 * * * *" (without space)
   */
  @Column({ name: 'cron_schedule', type: 'text' })
  cronSchedule: string;

  /**
   * Whether the agent is enabled and should run on schedule
   */
  @Column({ type: 'boolean', default: true })
  enabled: boolean;

  /**
   * How the agent is triggered
   * - 'schedule': Runs automatically on cron schedule
   * - 'manual': Only runs when manually triggered via admin UI
   * - 'reaction': Runs in response to graph object events
   */
  @Column({ name: 'trigger_type', type: 'text', default: 'schedule' })
  triggerType: AgentTriggerType;

  /**
   * Configuration for reaction triggers
   * Only applicable when triggerType is 'reaction'
   */
  @Column({ name: 'reaction_config', type: 'jsonb', nullable: true })
  reactionConfig: ReactionConfig | null;

  /**
   * How the agent executes its actions
   * - 'suggest': Creates tasks for human review
   * - 'execute': Directly modifies the graph
   * - 'hybrid': Can do both based on confidence/operation type
   */
  @Column({ name: 'execution_mode', type: 'text', default: 'execute' })
  executionMode: AgentExecutionMode;

  /**
   * Capability restrictions for the agent
   * Defines what operations the agent is allowed to perform
   */
  @Column({ type: 'jsonb', nullable: true })
  capabilities: AgentCapabilities | null;

  /**
   * Agent-specific configuration as JSON
   * e.g., { similarityThreshold: 0.10, maxPendingNotifications: 5 }
   */
  @Column({ type: 'jsonb', default: {} })
  config: Record<string, any>;

  /**
   * Optional description of what the agent does
   */
  @Column({ type: 'text', nullable: true })
  description: string | null;

  /**
   * Timestamp of the last successful run
   */
  @Column({ name: 'last_run_at', type: 'timestamptz', nullable: true })
  lastRunAt: Date | null;

  /**
   * Status of the last run: 'success', 'skipped', 'error'
   */
  @Column({ name: 'last_run_status', type: 'text', nullable: true })
  lastRunStatus: string | null;

  @CreateDateColumn({ name: 'created_at', type: 'timestamptz' })
  createdAt: Date;

  @UpdateDateColumn({ name: 'updated_at', type: 'timestamptz' })
  updatedAt: Date;
}
