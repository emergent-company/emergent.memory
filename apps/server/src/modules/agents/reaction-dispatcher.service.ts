import {
  Injectable,
  Logger,
  OnModuleInit,
  OnModuleDestroy,
} from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository, In, Not } from 'typeorm';
import { EventsService } from '../events/events.service';
import { EntityEvent, ActorContext } from '../events/events.types';
import { Agent, ReactionConfig } from '../../entities/agent.entity';
import { GraphObject } from '../../entities/graph-object.entity';
import { AgentProcessingLog } from '../../entities/agent-processing-log.entity';
import { AgentProcessingLogService } from './agent-processing-log.service';
import { AgentSchedulerService } from './agent-scheduler.service';
import { ProcessingEventType } from '../../entities/agent-processing-log.entity';
import { ReactionContext } from './strategies';
import {
  PendingEventsResponse,
  BatchTriggerResponse,
} from './dto/batch-trigger.dto';

/**
 * ReactionDispatcherService
 *
 * Subscribes to graph object events and dispatches them to matching reaction agents.
 * Handles:
 * - Event subscription via EventsService
 * - Finding agents with matching reaction configs
 * - Loop prevention (ignoring self-triggered and agent-triggered events)
 * - Concurrency control (skip vs parallel)
 * - Creating processing log entries
 * - Dispatching execution to AgentSchedulerService
 */
@Injectable()
export class ReactionDispatcherService
  implements OnModuleInit, OnModuleDestroy
{
  private readonly logger = new Logger(ReactionDispatcherService.name);
  private unsubscribe: (() => void) | null = null;

  constructor(
    private readonly eventsService: EventsService,
    @InjectRepository(Agent)
    private readonly agentRepo: Repository<Agent>,
    @InjectRepository(GraphObject)
    private readonly graphObjectRepo: Repository<GraphObject>,
    @InjectRepository(AgentProcessingLog)
    private readonly processingLogRepo: Repository<AgentProcessingLog>,
    private readonly processingLogService: AgentProcessingLogService,
    private readonly schedulerService: AgentSchedulerService
  ) {}

  /**
   * Subscribe to all graph object events on module init
   */
  onModuleInit(): void {
    this.logger.log('Initializing reaction dispatcher...');
    this.unsubscribe = this.eventsService.subscribeAll((event) => {
      // Only handle graph_object events
      if (event.entity === 'graph_object') {
        this.handleGraphObjectEvent(event).catch((err) => {
          this.logger.error(
            `Error handling graph object event: ${err.message}`,
            err.stack
          );
        });
      }
    });
    this.logger.log('Reaction dispatcher subscribed to graph object events');
  }

  /**
   * Unsubscribe from events on module destroy
   */
  onModuleDestroy(): void {
    if (this.unsubscribe) {
      this.unsubscribe();
      this.unsubscribe = null;
      this.logger.log('Reaction dispatcher unsubscribed from events');
    }
  }

  /**
   * Handle a graph object event by finding and dispatching to matching agents
   */
  private async handleGraphObjectEvent(event: EntityEvent): Promise<void> {
    // Skip batch events - we only handle individual entity events
    if (event.type === 'entity.batch' || !event.id) {
      return;
    }

    // Map event type to processing event type
    const eventType = this.mapEventType(event.type);
    if (!eventType) {
      return;
    }

    this.logger.debug(
      `Handling ${eventType} event for graph object ${event.id} (type: ${
        event.objectType || 'unknown'
      })`
    );

    // Find all matching reaction agents
    const matchingAgents = await this.findMatchingAgents(
      eventType,
      event.objectType,
      event.actor
    );

    if (matchingAgents.length === 0) {
      this.logger.debug(`No matching reaction agents for event`);
      return;
    }

    this.logger.debug(
      `Found ${matchingAgents.length} matching agent(s) for ${eventType} event on ${event.objectType}`
    );

    // Dispatch to each matching agent
    for (const agent of matchingAgents) {
      await this.dispatchToAgent(agent, event, eventType);
    }
  }

  /**
   * Map EntityEventType to ProcessingEventType
   */
  private mapEventType(eventType: string): ProcessingEventType | null {
    switch (eventType) {
      case 'entity.created':
        return 'created';
      case 'entity.updated':
        return 'updated';
      case 'entity.deleted':
        return 'deleted';
      default:
        return null;
    }
  }

  /**
   * Find all enabled reaction agents that match the event criteria
   *
   * @param eventType The type of event (created/updated/deleted)
   * @param objectType The type of the graph object
   * @param actor The actor context (for loop prevention)
   * @returns Matching agents that should process this event
   */
  async findMatchingAgents(
    eventType: ProcessingEventType,
    objectType: string | undefined,
    actor?: ActorContext
  ): Promise<Agent[]> {
    // Find all enabled reaction agents
    const reactionAgents = await this.agentRepo.find({
      where: {
        enabled: true,
        triggerType: 'reaction',
      },
    });

    // Filter agents based on their reaction config
    return reactionAgents.filter((agent) => {
      const config = agent.reactionConfig;
      if (!config) {
        this.logger.warn(
          `Reaction agent ${agent.name} has no reactionConfig, skipping`
        );
        return false;
      }

      // Check if agent listens to this event type
      if (!config.events.includes(eventType)) {
        return false;
      }

      // Check if agent listens to this object type (empty array = all types)
      if (
        config.objectTypes.length > 0 &&
        objectType &&
        !config.objectTypes.includes(objectType)
      ) {
        return false;
      }

      // Loop prevention: check actor context
      if (actor) {
        // If ignoreAgentTriggered is true, skip all agent-triggered events
        if (config.ignoreAgentTriggered && actor.actorType === 'agent') {
          this.logger.debug(
            `Agent ${agent.name} ignores agent-triggered events`
          );
          return false;
        }

        // If ignoreSelfTriggered is true (default), skip events triggered by this agent
        if (
          config.ignoreSelfTriggered !== false &&
          actor.actorType === 'agent' &&
          actor.actorId === agent.id
        ) {
          this.logger.debug(
            `Agent ${agent.name} ignores self-triggered events`
          );
          return false;
        }
      }

      return true;
    });
  }

  /**
   * Check if the agent should process this event based on concurrency strategy
   *
   * @param agent The agent to check
   * @param graphObjectId The graph object ID
   * @param objectVersion The object version
   * @param eventType The event type
   * @returns true if the agent should process, false if it should skip
   */
  async shouldProcess(
    agent: Agent,
    graphObjectId: string,
    objectVersion: number,
    eventType: ProcessingEventType
  ): Promise<{ shouldProcess: boolean; reason?: string }> {
    const config = agent.reactionConfig as ReactionConfig;

    // For 'parallel' strategy, always process
    if (config.concurrencyStrategy === 'parallel') {
      return { shouldProcess: true };
    }

    // For 'skip' strategy, check if already processing this object
    const isProcessing =
      await this.processingLogService.isAgentProcessingObject(
        agent.id,
        graphObjectId
      );

    if (isProcessing) {
      return {
        shouldProcess: false,
        reason: `Agent is already processing object ${graphObjectId}`,
      };
    }

    // Also check if this exact version+event was already processed
    const existingEntry =
      await this.processingLogService.findPendingOrProcessing(
        agent.id,
        graphObjectId,
        objectVersion,
        eventType
      );

    if (existingEntry) {
      return {
        shouldProcess: false,
        reason: `Already processing ${eventType} event for object ${graphObjectId} v${objectVersion}`,
      };
    }

    return { shouldProcess: true };
  }

  /**
   * Dispatch an event to a specific agent for processing
   *
   * @param agent The agent to dispatch to
   * @param event The triggering event
   * @param eventType The processing event type
   */
  private async dispatchToAgent(
    agent: Agent,
    event: EntityEvent,
    eventType: ProcessingEventType
  ): Promise<void> {
    const graphObjectId = event.id!;
    const objectVersion = event.version ?? 1;

    // Check concurrency
    const { shouldProcess, reason } = await this.shouldProcess(
      agent,
      graphObjectId,
      objectVersion,
      eventType
    );

    if (!shouldProcess) {
      this.logger.debug(`Skipping agent ${agent.name}: ${reason}`);
      return;
    }

    // Create processing log entry
    const logEntry = await this.processingLogService.createEntry({
      agentId: agent.id,
      graphObjectId,
      objectVersion,
      eventType,
    });

    this.logger.log(
      `Dispatching ${eventType} event for ${
        event.objectType || 'object'
      } ${graphObjectId} to agent ${agent.name}`
    );

    // Execute asynchronously (fire-and-forget)
    this.executeReactionAgent(agent, event, logEntry.id, eventType).catch(
      (err) => {
        this.logger.error(
          `Error executing reaction agent ${agent.name}: ${err.message}`,
          err.stack
        );
      }
    );
  }

  /**
   * Execute a reaction agent for a specific event
   *
   * @param agent The agent to execute
   * @param event The triggering event
   * @param processingLogId The processing log entry ID
   * @param eventType The event type
   */
  private async executeReactionAgent(
    agent: Agent,
    event: EntityEvent,
    processingLogId: string,
    eventType: ProcessingEventType
  ): Promise<void> {
    // Mark as processing
    await this.processingLogService.markProcessing(processingLogId);

    try {
      // Build reaction context to pass to the agent strategy
      const reactionContext: ReactionContext = {
        graphObjectId: event.id!,
        objectVersion: event.version ?? 1,
        objectType: event.objectType || 'unknown',
        eventType,
        projectId: event.projectId,
        processingLogId,
        event,
      };

      // Execute the agent with reaction context
      await this.schedulerService.executeAgentWithReaction(
        agent,
        reactionContext
      );

      // Mark as completed
      await this.processingLogService.markCompleted(processingLogId, {
        eventType,
        graphObjectId: event.id,
        objectType: event.objectType,
        objectVersion: event.version,
      });

      this.logger.debug(
        `Reaction agent ${agent.name} completed processing ${eventType} for ${event.id}`
      );
    } catch (error) {
      const err = error as Error;
      await this.processingLogService.markFailed(processingLogId, err.message);
      this.logger.error(
        `Reaction agent ${agent.name} failed: ${err.message}`,
        err.stack
      );
    }
  }

  /**
   * Manually trigger stuck job cleanup
   * Called periodically or on-demand
   */
  async cleanupStuckJobs(): Promise<number> {
    const count = await this.processingLogService.markStuckJobsAsAbandoned();
    if (count > 0) {
      this.logger.log(`Cleaned up ${count} stuck processing jobs`);
    }
    return count;
  }

  /**
   * Get pending events (unprocessed graph objects) for a reaction agent
   *
   * Returns graph objects that:
   * 1. Match the agent's objectTypes filter (or all types if empty)
   * 2. Are in the agent's project
   * 3. Have NOT been successfully processed by this agent yet
   *
   * @param agent The reaction agent
   * @param limit Maximum number of objects to return (default 100)
   * @returns Pending events response with count and sample objects
   */
  async getPendingEvents(
    agent: Agent,
    limit = 100
  ): Promise<PendingEventsResponse> {
    const config = agent.reactionConfig as ReactionConfig;
    if (!config) {
      return {
        totalCount: 0,
        objects: [],
        reactionConfig: { objectTypes: [], events: [] },
      };
    }

    // Build the query to find unprocessed objects
    const queryBuilder = this.graphObjectRepo
      .createQueryBuilder('go')
      .where('go.projectId = :projectId', { projectId: agent.projectId });

    // Filter by object types if specified
    if (config.objectTypes && config.objectTypes.length > 0) {
      queryBuilder.andWhere('go.type IN (:...objectTypes)', {
        objectTypes: config.objectTypes,
      });
    }

    // Exclude objects that have been successfully processed by this agent
    // We look for entries with status 'completed' in the processing log
    queryBuilder.andWhere(
      `NOT EXISTS (
        SELECT 1 FROM kb.agent_processing_log apl
        WHERE apl.agent_id = :agentId
        AND apl.graph_object_id = go.id
        AND apl.status = 'completed'
      )`,
      { agentId: agent.id }
    );

    // Get total count
    const totalCount = await queryBuilder.getCount();

    // Get sample objects with limit
    const objects = await queryBuilder
      .orderBy('go.createdAt', 'DESC')
      .limit(limit)
      .getMany();

    return {
      totalCount,
      objects: objects.map((obj) => ({
        id: obj.id,
        type: obj.type,
        key: obj.key,
        version: obj.version,
        createdAt: obj.createdAt.toISOString(),
        updatedAt: obj.updatedAt.toISOString(),
      })),
      reactionConfig: {
        objectTypes: config.objectTypes || [],
        events: config.events || [],
      },
    };
  }

  /**
   * Batch trigger a reaction agent for multiple graph objects
   *
   * @param agent The reaction agent to trigger
   * @param objectIds Array of graph object IDs to process
   * @returns Batch trigger response with queued/skipped counts
   */
  async batchTrigger(
    agent: Agent,
    objectIds: string[]
  ): Promise<BatchTriggerResponse> {
    const config = agent.reactionConfig as ReactionConfig;
    if (!config) {
      return {
        queued: 0,
        skipped: objectIds.length,
        skippedDetails: objectIds.map((id) => ({
          objectId: id,
          reason: 'Agent has no reaction config',
        })),
      };
    }

    // Fetch the graph objects
    const graphObjects = await this.graphObjectRepo.find({
      where: {
        id: In(objectIds),
        projectId: agent.projectId,
      },
    });

    const foundIds = new Set(graphObjects.map((obj) => obj.id));
    const response: BatchTriggerResponse = {
      queued: 0,
      skipped: 0,
      skippedDetails: [],
    };

    // Check for not found objects
    for (const id of objectIds) {
      if (!foundIds.has(id)) {
        response.skipped++;
        response.skippedDetails.push({
          objectId: id,
          reason: 'Object not found or not in project',
        });
      }
    }

    // Use 'created' as the default event type for manual triggers
    const eventType: ProcessingEventType = 'created';

    // Process each found object
    for (const obj of graphObjects) {
      // Check if object type matches filter
      if (
        config.objectTypes.length > 0 &&
        !config.objectTypes.includes(obj.type)
      ) {
        response.skipped++;
        response.skippedDetails.push({
          objectId: obj.id,
          reason: `Object type '${
            obj.type
          }' not in allowed types: ${config.objectTypes.join(', ')}`,
        });
        continue;
      }

      // Check concurrency/already processed
      const { shouldProcess, reason } = await this.shouldProcess(
        agent,
        obj.id,
        obj.version,
        eventType
      );

      if (!shouldProcess) {
        response.skipped++;
        response.skippedDetails.push({
          objectId: obj.id,
          reason: reason || 'Already processed or processing',
        });
        continue;
      }

      // Create processing log entry
      const logEntry = await this.processingLogService.createEntry({
        agentId: agent.id,
        graphObjectId: obj.id,
        objectVersion: obj.version,
        eventType,
      });

      // Build a synthetic event for the dispatcher
      const event: EntityEvent = {
        entity: 'graph_object',
        type: 'entity.created',
        id: obj.id,
        version: obj.version,
        objectType: obj.type,
        projectId: agent.projectId,
        timestamp: new Date().toISOString(),
      };

      // Execute asynchronously (fire-and-forget)
      this.executeReactionAgent(agent, event, logEntry.id, eventType).catch(
        (err) => {
          this.logger.error(
            `Error executing batch reaction for ${obj.id}: ${err.message}`,
            err.stack
          );
        }
      );

      response.queued++;
    }

    this.logger.log(
      `Batch trigger for agent ${agent.name}: queued=${response.queued}, skipped=${response.skipped}`
    );

    return response;
  }
}
