import {
  Entity,
  Column,
  PrimaryGeneratedColumn,
  CreateDateColumn,
  Index,
  ManyToOne,
  JoinColumn,
} from 'typeorm';
import { Agent } from './agent.entity';
import { GraphObject } from './graph-object.entity';

/**
 * Status of agent processing for a graph object
 */
export type AgentProcessingStatus =
  | 'pending'
  | 'processing'
  | 'completed'
  | 'failed'
  | 'abandoned'
  | 'skipped';

/**
 * Event type that triggered the processing
 */
export type ProcessingEventType = 'created' | 'updated' | 'deleted';

/**
 * AgentProcessingLog Entity
 *
 * Tracks which graph objects have been processed by reaction agents.
 * Used for:
 * - Avoiding duplicate processing of the same object version
 * - Concurrency control (skip vs parallel)
 * - Tracking processing status and errors
 * - Detecting stuck jobs for recovery
 */
@Entity({ schema: 'kb', name: 'agent_processing_log' })
@Index(['agentId', 'graphObjectId', 'objectVersion', 'eventType'])
@Index(['status', 'startedAt'])
@Index(['agentId', 'createdAt'])
@Index(['graphObjectId', 'createdAt'])
export class AgentProcessingLog {
  @PrimaryGeneratedColumn('uuid')
  id: string;

  @Column({ name: 'agent_id', type: 'uuid' })
  agentId: string;

  @Column({ name: 'graph_object_id', type: 'uuid' })
  graphObjectId: string;

  /**
   * Version of the graph object when processing was triggered
   */
  @Column({ name: 'object_version', type: 'integer' })
  objectVersion: number;

  /**
   * Event that triggered the processing
   */
  @Column({ name: 'event_type', type: 'text' })
  eventType: ProcessingEventType;

  /**
   * Current processing status
   */
  @Column({ type: 'text', default: 'pending' })
  status: AgentProcessingStatus;

  /**
   * When processing started (null if still pending)
   */
  @Column({ name: 'started_at', type: 'timestamptz', nullable: true })
  startedAt: Date | null;

  /**
   * When processing completed (null if not completed)
   */
  @Column({ name: 'completed_at', type: 'timestamptz', nullable: true })
  completedAt: Date | null;

  /**
   * Error message if status is 'failed'
   */
  @Column({ name: 'error_message', type: 'text', nullable: true })
  errorMessage: string | null;

  /**
   * Summary of processing results
   * e.g., { suggestionsCreated: 1, objectsUpdated: 0 }
   */
  @Column({ name: 'result_summary', type: 'jsonb', nullable: true })
  resultSummary: Record<string, any> | null;

  @CreateDateColumn({ name: 'created_at', type: 'timestamptz' })
  createdAt: Date;

  // Relations
  @ManyToOne(() => Agent, { onDelete: 'CASCADE' })
  @JoinColumn({ name: 'agent_id' })
  agent: Agent;

  @ManyToOne(() => GraphObject, { onDelete: 'CASCADE' })
  @JoinColumn({ name: 'graph_object_id' })
  graphObject: GraphObject;
}
