import { Injectable, Logger } from '@nestjs/common';
import { EventEmitter2 } from '@nestjs/event-emitter';
import { ActorContext, EntityEvent, EntityType } from './events.types';

/**
 * Options for emitting entity events with extended metadata
 */
export interface EmitOptions {
  /** Partial update payload */
  data?: Record<string, any>;
  /** Actor context for reaction agent loop prevention */
  actor?: ActorContext;
  /** Object version (for graph objects) */
  version?: number;
  /** Object type (for graph objects) */
  objectType?: string;
}

/**
 * Service for publishing entity events to the event bus.
 * Events are project-scoped and delivered to all SSE connections for that project.
 */
@Injectable()
export class EventsService {
  private readonly logger = new Logger(EventsService.name);

  constructor(private readonly eventEmitter: EventEmitter2) {}

  /**
   * Emit an entity event to all subscribers for the given project
   */
  emit(event: EntityEvent): void {
    const channel = `events.${event.projectId}`;
    this.logger.debug(
      `Emitting ${event.type} for ${event.entity}:${event.id} on channel ${channel}`
    );
    this.eventEmitter.emit(channel, event);
  }

  /**
   * Emit an entity.created event
   * @param entity Entity type (e.g., 'graph_object', 'document')
   * @param id Entity ID
   * @param projectId Project ID (events are always project-scoped)
   * @param options Optional: data payload, actor context, version, objectType
   */
  emitCreated(
    entity: EntityType,
    id: string,
    projectId: string,
    options?: EmitOptions | Record<string, any>
  ): void {
    // Support both old signature (data object) and new signature (EmitOptions)
    const opts = this.normalizeOptions(options);
    this.emit({
      type: 'entity.created',
      entity,
      id,
      projectId,
      data: opts.data,
      actor: opts.actor,
      version: opts.version,
      objectType: opts.objectType,
      timestamp: new Date().toISOString(),
    });
  }

  /**
   * Emit an entity.updated event
   * @param entity Entity type (e.g., 'graph_object', 'document')
   * @param id Entity ID
   * @param projectId Project ID (events are always project-scoped)
   * @param options Optional: data payload, actor context, version, objectType
   */
  emitUpdated(
    entity: EntityType,
    id: string,
    projectId: string,
    options?: EmitOptions | Record<string, any>
  ): void {
    // Support both old signature (data object) and new signature (EmitOptions)
    const opts = this.normalizeOptions(options);
    this.emit({
      type: 'entity.updated',
      entity,
      id,
      projectId,
      data: opts.data,
      actor: opts.actor,
      version: opts.version,
      objectType: opts.objectType,
      timestamp: new Date().toISOString(),
    });
  }

  /**
   * Emit an entity.deleted event
   * @param entity Entity type (e.g., 'graph_object', 'document')
   * @param id Entity ID
   * @param projectId Project ID (events are always project-scoped)
   * @param options Optional: actor context, version, objectType
   */
  emitDeleted(
    entity: EntityType,
    id: string,
    projectId: string,
    options?: Omit<EmitOptions, 'data'>
  ): void {
    this.emit({
      type: 'entity.deleted',
      entity,
      id,
      projectId,
      actor: options?.actor,
      version: options?.version,
      objectType: options?.objectType,
      timestamp: new Date().toISOString(),
    });
  }

  /**
   * Normalize options to handle both old data-only signature and new EmitOptions signature
   */
  private normalizeOptions(
    options?: EmitOptions | Record<string, any>
  ): EmitOptions {
    if (!options) return {};

    // Check if this looks like EmitOptions (has actor, version, or objectType at top level)
    if ('actor' in options || 'version' in options || 'objectType' in options) {
      return options as EmitOptions;
    }

    // Treat as legacy data-only format
    return { data: options };
  }

  /**
   * Emit an entity.batch event for multiple entities
   */
  emitBatch(
    entity: EntityType,
    ids: string[],
    projectId: string,
    data?: Record<string, any>
  ): void {
    this.emit({
      type: 'entity.batch',
      entity,
      id: null,
      ids,
      projectId,
      data,
      timestamp: new Date().toISOString(),
    });
  }

  /**
   * Subscribe to events for a specific project
   * Returns an unsubscribe function
   */
  subscribe(
    projectId: string,
    callback: (event: EntityEvent) => void
  ): () => void {
    const channel = `events.${projectId}`;
    this.eventEmitter.on(channel, callback);
    return () => {
      this.eventEmitter.off(channel, callback);
    };
  }

  /**
   * Subscribe to all events (for debugging)
   */
  subscribeAll(callback: (event: EntityEvent) => void): () => void {
    const handler = (event: EntityEvent) => callback(event);
    this.eventEmitter.on('events.*', handler);
    return () => {
      this.eventEmitter.off('events.*', handler);
    };
  }
}
