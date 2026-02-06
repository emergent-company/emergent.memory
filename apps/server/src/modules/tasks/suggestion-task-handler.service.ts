import { Injectable, Logger, Inject, forwardRef } from '@nestjs/common';
import { Task } from '../../entities/task.entity';
import {
  SuggestionTaskType,
  ObjectCreateSuggestionMetadata,
  ObjectUpdateSuggestionMetadata,
  ObjectDeleteSuggestionMetadata,
  RelationshipCreateSuggestionMetadata,
} from '../agents/suggestion.service';

/**
 * Result of applying a suggestion
 */
export interface SuggestionApplyResult {
  success: boolean;
  /** The ID of the created/updated object (if applicable) */
  objectId?: string;
  /** The ID of the created relationship (if applicable) */
  relationshipId?: string;
  /** Error message if failed */
  error?: string;
  /** Additional details about the operation */
  details?: Record<string, any>;
}

/**
 * Suggestion task types
 */
const SUGGESTION_TASK_TYPES: SuggestionTaskType[] = [
  'object_create_suggestion',
  'object_update_suggestion',
  'object_delete_suggestion',
  'relationship_create_suggestion',
];

/**
 * SuggestionTaskHandlerService
 *
 * Handles approval and rejection of suggestion tasks created by reaction agents.
 * When a suggestion is approved, this service applies the suggested changes
 * to the graph using the GraphService.
 */
@Injectable()
export class SuggestionTaskHandlerService {
  private readonly logger = new Logger(SuggestionTaskHandlerService.name);

  constructor(
    @Inject(forwardRef(() => require('../graph/graph.service').GraphService))
    private readonly graphService: /* GraphService */ any
  ) {}

  /**
   * Check if a task type is a suggestion task
   */
  isSuggestionTask(taskType: string): taskType is SuggestionTaskType {
    return SUGGESTION_TASK_TYPES.includes(taskType as SuggestionTaskType);
  }

  /**
   * Apply a suggestion task when it is approved
   *
   * @param task The suggestion task to apply
   * @param userId The user who approved the task
   * @returns Result of applying the suggestion
   */
  async applySuggestion(
    task: Task,
    userId: string
  ): Promise<SuggestionApplyResult> {
    if (!this.isSuggestionTask(task.type)) {
      return {
        success: false,
        error: `Task type '${task.type}' is not a suggestion task`,
      };
    }

    this.logger.log(
      `Applying suggestion task ${task.id} (type: ${task.type}) by user ${userId}`
    );

    try {
      switch (task.type as SuggestionTaskType) {
        case 'object_create_suggestion':
          return await this.applyObjectCreateSuggestion(task, userId);
        case 'object_update_suggestion':
          return await this.applyObjectUpdateSuggestion(task, userId);
        case 'object_delete_suggestion':
          return await this.applyObjectDeleteSuggestion(task, userId);
        case 'relationship_create_suggestion':
          return await this.applyRelationshipCreateSuggestion(task, userId);
        default:
          return {
            success: false,
            error: `Unknown suggestion task type: ${task.type}`,
          };
      }
    } catch (error) {
      const err = error as Error;
      this.logger.error(
        `Failed to apply suggestion task ${task.id}: ${err.message}`,
        err.stack
      );
      return {
        success: false,
        error: err.message,
      };
    }
  }

  /**
   * Apply an object creation suggestion
   */
  private async applyObjectCreateSuggestion(
    task: Task,
    userId: string
  ): Promise<SuggestionApplyResult> {
    const metadata = task.metadata as ObjectCreateSuggestionMetadata;

    if (!metadata.objectType || !metadata.proposedData) {
      return {
        success: false,
        error: 'Missing objectType or proposedData in task metadata',
      };
    }

    // Create the object with user as actor (not the original agent)
    const created = await this.graphService.createObject(
      {
        type: metadata.objectType,
        properties: metadata.proposedData,
        project_id: task.projectId,
        actor: { actorType: 'user', actorId: userId },
      },
      { projectId: task.projectId }
    );

    this.logger.log(
      `Created object ${created.id} (type: ${metadata.objectType}) from suggestion task ${task.id}`
    );

    return {
      success: true,
      objectId: created.id,
      details: {
        objectType: metadata.objectType,
        version: created.version,
      },
    };
  }

  /**
   * Apply an object update suggestion
   */
  private async applyObjectUpdateSuggestion(
    task: Task,
    userId: string
  ): Promise<SuggestionApplyResult> {
    const metadata = task.metadata as ObjectUpdateSuggestionMetadata;

    if (!metadata.targetObjectId || !metadata.proposedChanges) {
      return {
        success: false,
        error: 'Missing targetObjectId or proposedChanges in task metadata',
      };
    }

    // Check if the object version is still current
    // This prevents applying outdated suggestions
    const currentObject = await this.graphService.getObjectById(
      metadata.targetObjectId
    );

    if (!currentObject) {
      return {
        success: false,
        error: `Target object ${metadata.targetObjectId} not found`,
      };
    }

    if (
      metadata.currentVersion !== undefined &&
      currentObject.version > metadata.currentVersion
    ) {
      return {
        success: false,
        error: `Object has been modified since suggestion was created (current: v${currentObject.version}, suggestion: v${metadata.currentVersion})`,
        details: {
          currentVersion: currentObject.version,
          suggestionVersion: metadata.currentVersion,
        },
      };
    }

    // Update the object with user as actor
    const updated = await this.graphService.patchObject(
      metadata.targetObjectId,
      {
        properties: metadata.proposedChanges,
        actor: { actorType: 'user', actorId: userId },
      },
      { projectId: task.projectId }
    );

    this.logger.log(
      `Updated object ${metadata.targetObjectId} from suggestion task ${task.id}`
    );

    return {
      success: true,
      objectId: updated.id,
      details: {
        objectType: metadata.objectType,
        previousVersion: metadata.currentVersion,
        newVersion: updated.version,
      },
    };
  }

  /**
   * Apply an object deletion suggestion
   */
  private async applyObjectDeleteSuggestion(
    task: Task,
    userId: string
  ): Promise<SuggestionApplyResult> {
    const metadata = task.metadata as ObjectDeleteSuggestionMetadata;

    if (!metadata.targetObjectId) {
      return {
        success: false,
        error: 'Missing targetObjectId in task metadata',
      };
    }

    // Check if object still exists
    const currentObject = await this.graphService.getObjectById(
      metadata.targetObjectId
    );

    if (!currentObject) {
      return {
        success: false,
        error: `Target object ${metadata.targetObjectId} not found or already deleted`,
      };
    }

    // Delete the object with user as actor
    await this.graphService.deleteObject(
      metadata.targetObjectId,
      { projectId: task.projectId },
      { actorType: 'user', actorId: userId }
    );

    this.logger.log(
      `Deleted object ${metadata.targetObjectId} from suggestion task ${task.id}`
    );

    return {
      success: true,
      objectId: metadata.targetObjectId,
      details: {
        objectType: metadata.objectType,
        deleted: true,
      },
    };
  }

  /**
   * Apply a relationship creation suggestion
   */
  private async applyRelationshipCreateSuggestion(
    task: Task,
    userId: string
  ): Promise<SuggestionApplyResult> {
    const metadata = task.metadata as RelationshipCreateSuggestionMetadata;

    if (
      !metadata.sourceObjectId ||
      !metadata.targetObjectId ||
      !metadata.relationshipType
    ) {
      return {
        success: false,
        error:
          'Missing sourceObjectId, targetObjectId, or relationshipType in task metadata',
      };
    }

    // Verify both objects exist
    const [sourceObject, targetObject] = await Promise.all([
      this.graphService.getObjectById(metadata.sourceObjectId),
      this.graphService.getObjectById(metadata.targetObjectId),
    ]);

    if (!sourceObject) {
      return {
        success: false,
        error: `Source object ${metadata.sourceObjectId} not found`,
      };
    }

    if (!targetObject) {
      return {
        success: false,
        error: `Target object ${metadata.targetObjectId} not found`,
      };
    }

    // Create the relationship
    const relationship = await this.graphService.createRelationship(
      {
        source_id: metadata.sourceObjectId,
        target_id: metadata.targetObjectId,
        type: metadata.relationshipType,
        properties: metadata.relationshipProps || {},
        project_id: task.projectId,
      },
      { projectId: task.projectId }
    );

    this.logger.log(
      `Created relationship ${relationship.id} (${metadata.relationshipType}) from suggestion task ${task.id}`
    );

    return {
      success: true,
      relationshipId: relationship.id,
      details: {
        relationshipType: metadata.relationshipType,
        sourceObjectId: metadata.sourceObjectId,
        targetObjectId: metadata.targetObjectId,
      },
    };
  }

  /**
   * Get a human-readable summary of what this suggestion would do
   */
  getSuggestionSummary(task: Task): string {
    if (!this.isSuggestionTask(task.type)) {
      return 'Unknown task type';
    }

    const metadata = task.metadata;

    switch (task.type as SuggestionTaskType) {
      case 'object_create_suggestion': {
        const m = metadata as ObjectCreateSuggestionMetadata;
        return `Create new ${m.objectType}`;
      }
      case 'object_update_suggestion': {
        const m = metadata as ObjectUpdateSuggestionMetadata;
        const changeCount = Object.keys(m.proposedChanges || {}).length;
        return `Update ${m.objectType} (${changeCount} field${
          changeCount !== 1 ? 's' : ''
        })`;
      }
      case 'object_delete_suggestion': {
        const m = metadata as ObjectDeleteSuggestionMetadata;
        return `Delete ${m.objectType}`;
      }
      case 'relationship_create_suggestion': {
        const m = metadata as RelationshipCreateSuggestionMetadata;
        return `Create ${m.relationshipType} relationship between ${m.sourceObjectType} and ${m.targetObjectType}`;
      }
      default:
        return 'Unknown suggestion type';
    }
  }
}
