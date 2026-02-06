import { Injectable, Logger } from '@nestjs/common';
import { TasksService } from '../tasks/tasks.service';
import { TaskSourceType } from '../tasks/dto/task.dto';
import { Agent } from '../../entities/agent.entity';
import { Task } from '../../entities/task.entity';

/**
 * Suggestion task types for reaction agent suggestions
 */
export type SuggestionTaskType =
  | 'object_create_suggestion'
  | 'object_update_suggestion'
  | 'object_delete_suggestion'
  | 'relationship_create_suggestion';

/**
 * Base metadata for suggestion tasks
 */
export interface SuggestionMetadata {
  /** The agent that created this suggestion */
  agentId: string;
  /** Agent name for display */
  agentName: string;
  /** Processing log entry ID */
  processingLogId?: string;
  /** Confidence score (0-1) if available */
  confidence?: number;
}

/**
 * Metadata for object creation suggestions
 */
export interface ObjectCreateSuggestionMetadata extends SuggestionMetadata {
  /** The object type to create */
  objectType: string;
  /** The proposed object data */
  proposedData: Record<string, any>;
  /** Reason for the suggestion */
  reason?: string;
}

/**
 * Metadata for object update suggestions
 */
export interface ObjectUpdateSuggestionMetadata extends SuggestionMetadata {
  /** The target object ID */
  targetObjectId: string;
  /** The object type */
  objectType: string;
  /** Current version of the object */
  currentVersion: number;
  /** The proposed changes */
  proposedChanges: Record<string, any>;
  /** Reason for the suggestion */
  reason?: string;
}

/**
 * Metadata for object deletion suggestions
 */
export interface ObjectDeleteSuggestionMetadata extends SuggestionMetadata {
  /** The target object ID */
  targetObjectId: string;
  /** The object type */
  objectType: string;
  /** Current object data (snapshot for review) */
  currentData?: Record<string, any>;
  /** Reason for the suggestion */
  reason?: string;
}

/**
 * Metadata for relationship creation suggestions
 */
export interface RelationshipCreateSuggestionMetadata
  extends SuggestionMetadata {
  /** Source object ID */
  sourceObjectId: string;
  /** Source object type */
  sourceObjectType: string;
  /** Target object ID */
  targetObjectId: string;
  /** Target object type */
  targetObjectType: string;
  /** Relationship type */
  relationshipType: string;
  /** Relationship properties */
  relationshipProps?: Record<string, any>;
  /** Reason for the suggestion */
  reason?: string;
}

/**
 * Input for creating an object creation suggestion
 */
export interface CreateObjectSuggestionInput {
  projectId: string;
  agent: Agent;
  objectType: string;
  proposedData: Record<string, any>;
  reason?: string;
  confidence?: number;
  processingLogId?: string;
}

/**
 * Input for creating an object update suggestion
 */
export interface UpdateObjectSuggestionInput {
  projectId: string;
  agent: Agent;
  targetObjectId: string;
  objectType: string;
  currentVersion: number;
  proposedChanges: Record<string, any>;
  reason?: string;
  confidence?: number;
  processingLogId?: string;
}

/**
 * Input for creating an object deletion suggestion
 */
export interface DeleteObjectSuggestionInput {
  projectId: string;
  agent: Agent;
  targetObjectId: string;
  objectType: string;
  currentData?: Record<string, any>;
  reason?: string;
  confidence?: number;
  processingLogId?: string;
}

/**
 * Input for creating a relationship creation suggestion
 */
export interface CreateRelationshipSuggestionInput {
  projectId: string;
  agent: Agent;
  sourceObjectId: string;
  sourceObjectType: string;
  targetObjectId: string;
  targetObjectType: string;
  relationshipType: string;
  relationshipProps?: Record<string, any>;
  reason?: string;
  confidence?: number;
  processingLogId?: string;
}

/**
 * SuggestionService
 *
 * Creates suggestion tasks when reaction agents operate in 'suggest' mode.
 * These tasks require human approval before the suggested changes are applied.
 *
 * Suggestion task types:
 * - object_create_suggestion: Agent suggests creating a new object
 * - object_update_suggestion: Agent suggests updating an existing object
 * - object_delete_suggestion: Agent suggests deleting an object
 * - relationship_create_suggestion: Agent suggests creating a relationship
 */
@Injectable()
export class SuggestionService {
  private readonly logger = new Logger(SuggestionService.name);

  constructor(private readonly tasksService: TasksService) {}

  /**
   * Create a suggestion task for creating a new object
   */
  async createObjectSuggestion(
    input: CreateObjectSuggestionInput
  ): Promise<Task> {
    const {
      projectId,
      agent,
      objectType,
      proposedData,
      reason,
      confidence,
      processingLogId,
    } = input;

    const metadata: ObjectCreateSuggestionMetadata = {
      agentId: agent.id,
      agentName: agent.name,
      processingLogId,
      confidence,
      objectType,
      proposedData,
      reason,
    };

    const title = `${agent.name} suggests creating a new ${objectType}`;
    const description = reason || `Review the suggested ${objectType} creation`;

    const task = await this.tasksService.create({
      projectId,
      title,
      description,
      type: 'object_create_suggestion' as SuggestionTaskType,
      sourceType: TaskSourceType.AGENT,
      sourceId: agent.id,
      metadata,
    });

    this.logger.log(
      `Created object creation suggestion task ${task.id} for ${objectType} from agent ${agent.name}`
    );

    return task;
  }

  /**
   * Create a suggestion task for updating an existing object
   */
  async createUpdateSuggestion(
    input: UpdateObjectSuggestionInput
  ): Promise<Task> {
    const {
      projectId,
      agent,
      targetObjectId,
      objectType,
      currentVersion,
      proposedChanges,
      reason,
      confidence,
      processingLogId,
    } = input;

    const metadata: ObjectUpdateSuggestionMetadata = {
      agentId: agent.id,
      agentName: agent.name,
      processingLogId,
      confidence,
      targetObjectId,
      objectType,
      currentVersion,
      proposedChanges,
      reason,
    };

    const title = `${agent.name} suggests updating ${objectType}`;
    const description =
      reason ||
      `Review the suggested changes to ${objectType} ${targetObjectId}`;

    const task = await this.tasksService.create({
      projectId,
      title,
      description,
      type: 'object_update_suggestion' as SuggestionTaskType,
      sourceType: TaskSourceType.AGENT,
      sourceId: agent.id,
      metadata,
    });

    this.logger.log(
      `Created object update suggestion task ${task.id} for ${objectType} ${targetObjectId} from agent ${agent.name}`
    );

    return task;
  }

  /**
   * Create a suggestion task for deleting an object
   */
  async createDeleteSuggestion(
    input: DeleteObjectSuggestionInput
  ): Promise<Task> {
    const {
      projectId,
      agent,
      targetObjectId,
      objectType,
      currentData,
      reason,
      confidence,
      processingLogId,
    } = input;

    const metadata: ObjectDeleteSuggestionMetadata = {
      agentId: agent.id,
      agentName: agent.name,
      processingLogId,
      confidence,
      targetObjectId,
      objectType,
      currentData,
      reason,
    };

    const title = `${agent.name} suggests deleting ${objectType}`;
    const description =
      reason ||
      `Review the suggested deletion of ${objectType} ${targetObjectId}`;

    const task = await this.tasksService.create({
      projectId,
      title,
      description,
      type: 'object_delete_suggestion' as SuggestionTaskType,
      sourceType: TaskSourceType.AGENT,
      sourceId: agent.id,
      metadata,
    });

    this.logger.log(
      `Created object deletion suggestion task ${task.id} for ${objectType} ${targetObjectId} from agent ${agent.name}`
    );

    return task;
  }

  /**
   * Create a suggestion task for creating a relationship
   */
  async createRelationshipSuggestion(
    input: CreateRelationshipSuggestionInput
  ): Promise<Task> {
    const {
      projectId,
      agent,
      sourceObjectId,
      sourceObjectType,
      targetObjectId,
      targetObjectType,
      relationshipType,
      relationshipProps,
      reason,
      confidence,
      processingLogId,
    } = input;

    const metadata: RelationshipCreateSuggestionMetadata = {
      agentId: agent.id,
      agentName: agent.name,
      processingLogId,
      confidence,
      sourceObjectId,
      sourceObjectType,
      targetObjectId,
      targetObjectType,
      relationshipType,
      relationshipProps,
      reason,
    };

    const title = `${agent.name} suggests creating ${relationshipType} relationship`;
    const description =
      reason ||
      `Review the suggested ${relationshipType} relationship between ${sourceObjectType} and ${targetObjectType}`;

    const task = await this.tasksService.create({
      projectId,
      title,
      description,
      type: 'relationship_create_suggestion' as SuggestionTaskType,
      sourceType: TaskSourceType.AGENT,
      sourceId: agent.id,
      metadata,
    });

    this.logger.log(
      `Created relationship suggestion task ${task.id} for ${relationshipType} from agent ${agent.name}`
    );

    return task;
  }

  /**
   * Check if a task is a suggestion task
   */
  isSuggestionTask(taskType: string): taskType is SuggestionTaskType {
    return [
      'object_create_suggestion',
      'object_update_suggestion',
      'object_delete_suggestion',
      'relationship_create_suggestion',
    ].includes(taskType);
  }
}
