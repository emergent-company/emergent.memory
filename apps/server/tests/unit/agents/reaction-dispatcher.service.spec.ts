import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ReactionDispatcherService } from '../../../src/modules/agents/reaction-dispatcher.service';
import { Agent, ReactionConfig } from '../../../src/entities/agent.entity';
import { GraphObject } from '../../../src/entities/graph-object.entity';
import { AgentProcessingLog } from '../../../src/entities/agent-processing-log.entity';
import { EventsService } from '../../../src/modules/events/events.service';
import { AgentProcessingLogService } from '../../../src/modules/agents/agent-processing-log.service';
import { AgentSchedulerService } from '../../../src/modules/agents/agent-scheduler.service';
import {
  EntityEvent,
  ActorContext,
} from '../../../src/modules/events/events.types';
import { ProcessingEventType } from '../../../src/entities/agent-processing-log.entity';
import { Repository, SelectQueryBuilder } from 'typeorm';

describe('ReactionDispatcherService', () => {
  let service: ReactionDispatcherService;
  let mockEventsService: {
    subscribeAll: ReturnType<typeof vi.fn>;
  };
  let mockAgentRepo: {
    find: ReturnType<typeof vi.fn>;
  };
  let mockGraphObjectRepo: {
    find: ReturnType<typeof vi.fn>;
    createQueryBuilder: ReturnType<typeof vi.fn>;
  };
  let mockProcessingLogRepo: {
    find: ReturnType<typeof vi.fn>;
  };
  let mockProcessingLogService: {
    createEntry: ReturnType<typeof vi.fn>;
    markProcessing: ReturnType<typeof vi.fn>;
    markCompleted: ReturnType<typeof vi.fn>;
    markFailed: ReturnType<typeof vi.fn>;
    markSkipped: ReturnType<typeof vi.fn>;
    isAgentProcessingObject: ReturnType<typeof vi.fn>;
    findPendingOrProcessing: ReturnType<typeof vi.fn>;
    markStuckJobsAsAbandoned: ReturnType<typeof vi.fn>;
  };
  let mockSchedulerService: {
    executeAgentWithReaction: ReturnType<typeof vi.fn>;
  };
  let mockQueryBuilder: {
    where: ReturnType<typeof vi.fn>;
    andWhere: ReturnType<typeof vi.fn>;
    orderBy: ReturnType<typeof vi.fn>;
    limit: ReturnType<typeof vi.fn>;
    getCount: ReturnType<typeof vi.fn>;
    getMany: ReturnType<typeof vi.fn>;
  };

  // Mock factories
  const createMockReactionConfig = (
    overrides: Partial<ReactionConfig> = {}
  ): ReactionConfig => ({
    events: ['created', 'updated'],
    objectTypes: [],
    concurrencyStrategy: 'skip',
    ignoreAgentTriggered: false,
    ignoreSelfTriggered: true,
    ...overrides,
  });

  const createMockAgent = (overrides: Partial<Agent> = {}): Agent =>
    ({
      id: 'agent-1',
      name: 'Test Agent',
      role: 'test-role',
      prompt: null,
      cronSchedule: '0 * * * *',
      enabled: true,
      triggerType: 'reaction',
      reactionConfig: createMockReactionConfig(),
      executionMode: 'execute',
      capabilities: null,
      config: {},
      description: null,
      projectId: 'project-1',
      createdAt: new Date(),
      updatedAt: new Date(),
      ...overrides,
    } as Agent);

  const createMockGraphObject = (
    overrides: Partial<GraphObject> = {}
  ): GraphObject =>
    ({
      id: 'obj-123',
      type: 'Person',
      key: 'person-1',
      version: 1,
      projectId: 'project-1',
      properties: { name: 'Test Person' },
      createdAt: new Date('2024-01-01T00:00:00Z'),
      updatedAt: new Date('2024-01-01T00:00:00Z'),
      ...overrides,
    } as GraphObject);

  const createMockEvent = (overrides: Partial<EntityEvent> = {}): EntityEvent =>
    ({
      entity: 'graph_object',
      type: 'entity.created',
      id: 'obj-123',
      projectId: 'project-1',
      orgId: 'org-1',
      objectType: 'Person',
      version: 1,
      timestamp: new Date(),
      ...overrides,
    } as EntityEvent);

  beforeEach(() => {
    // Create fresh mocks for each test
    mockEventsService = {
      subscribeAll: vi.fn().mockReturnValue(() => {}),
    };

    mockAgentRepo = {
      find: vi.fn().mockResolvedValue([]),
    };

    // Create chainable query builder mock
    mockQueryBuilder = {
      where: vi.fn().mockReturnThis(),
      andWhere: vi.fn().mockReturnThis(),
      orderBy: vi.fn().mockReturnThis(),
      limit: vi.fn().mockReturnThis(),
      getCount: vi.fn().mockResolvedValue(0),
      getMany: vi.fn().mockResolvedValue([]),
    };

    mockGraphObjectRepo = {
      find: vi.fn().mockResolvedValue([]),
      createQueryBuilder: vi.fn().mockReturnValue(mockQueryBuilder),
    };

    mockProcessingLogRepo = {
      find: vi.fn().mockResolvedValue([]),
    };

    mockProcessingLogService = {
      createEntry: vi.fn().mockResolvedValue({ id: 'log-1' }),
      markProcessing: vi.fn().mockResolvedValue({}),
      markCompleted: vi.fn().mockResolvedValue({}),
      markFailed: vi.fn().mockResolvedValue({}),
      markSkipped: vi.fn().mockResolvedValue({}),
      isAgentProcessingObject: vi.fn().mockResolvedValue(false),
      findPendingOrProcessing: vi.fn().mockResolvedValue(null),
      markStuckJobsAsAbandoned: vi.fn().mockResolvedValue(0),
    };

    mockSchedulerService = {
      executeAgentWithReaction: vi.fn().mockResolvedValue(undefined),
    };

    // Direct instantiation with mocks
    service = new ReactionDispatcherService(
      mockEventsService as unknown as EventsService,
      mockAgentRepo as unknown as Repository<Agent>,
      mockGraphObjectRepo as unknown as Repository<GraphObject>,
      mockProcessingLogRepo as unknown as Repository<AgentProcessingLog>,
      mockProcessingLogService as unknown as AgentProcessingLogService,
      mockSchedulerService as unknown as AgentSchedulerService
    );
  });

  describe('onModuleInit', () => {
    it('should subscribe to events on init', () => {
      service.onModuleInit();
      expect(mockEventsService.subscribeAll).toHaveBeenCalledTimes(1);
      expect(mockEventsService.subscribeAll).toHaveBeenCalledWith(
        expect.any(Function)
      );
    });
  });

  describe('onModuleDestroy', () => {
    it('should unsubscribe from events on destroy', () => {
      const unsubscribeFn = vi.fn();
      mockEventsService.subscribeAll.mockReturnValue(unsubscribeFn);

      service.onModuleInit();
      service.onModuleDestroy();

      expect(unsubscribeFn).toHaveBeenCalledTimes(1);
    });

    it('should not throw if not subscribed', () => {
      expect(() => service.onModuleDestroy()).not.toThrow();
    });
  });

  describe('findMatchingAgents', () => {
    it('should return agents matching the event type', async () => {
      const agent = createMockAgent();
      mockAgentRepo.find.mockResolvedValue([agent]);

      const result = await service.findMatchingAgents('created', 'Person');

      expect(result).toHaveLength(1);
      expect(result[0].id).toBe('agent-1');
    });

    it('should filter out agents not listening to the event type', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          events: ['deleted'],
        }),
      });
      mockAgentRepo.find.mockResolvedValue([agent]);

      const result = await service.findMatchingAgents('created', 'Person');

      expect(result).toHaveLength(0);
    });

    it('should match agents with empty objectTypes (all types)', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          objectTypes: [],
        }),
      });
      mockAgentRepo.find.mockResolvedValue([agent]);

      const result = await service.findMatchingAgents('created', 'Person');

      expect(result).toHaveLength(1);
    });

    it('should filter by specific object types', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          objectTypes: ['Company', 'Organization'],
        }),
      });
      mockAgentRepo.find.mockResolvedValue([agent]);

      const result = await service.findMatchingAgents('created', 'Person');

      expect(result).toHaveLength(0);
    });

    it('should match when object type is in allowed list', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          objectTypes: ['Person', 'Company'],
        }),
      });
      mockAgentRepo.find.mockResolvedValue([agent]);

      const result = await service.findMatchingAgents('created', 'Person');

      expect(result).toHaveLength(1);
    });

    it('should skip agents without reactionConfig', async () => {
      const agent = createMockAgent({
        reactionConfig: null as any,
      });
      mockAgentRepo.find.mockResolvedValue([agent]);

      const result = await service.findMatchingAgents('created', 'Person');

      expect(result).toHaveLength(0);
    });

    describe('loop prevention', () => {
      it('should skip agent-triggered events when ignoreAgentTriggered is true', async () => {
        const agent = createMockAgent({
          reactionConfig: createMockReactionConfig({
            ignoreAgentTriggered: true,
          }),
        });
        mockAgentRepo.find.mockResolvedValue([agent]);

        const actor: ActorContext = {
          actorType: 'agent',
          actorId: 'other-agent',
        };

        const result = await service.findMatchingAgents(
          'created',
          'Person',
          actor
        );

        expect(result).toHaveLength(0);
      });

      it('should allow agent-triggered events when ignoreAgentTriggered is false', async () => {
        const agent = createMockAgent({
          reactionConfig: createMockReactionConfig({
            ignoreAgentTriggered: false,
            ignoreSelfTriggered: false,
          }),
        });
        mockAgentRepo.find.mockResolvedValue([agent]);

        const actor: ActorContext = {
          actorType: 'agent',
          actorId: 'other-agent',
        };

        const result = await service.findMatchingAgents(
          'created',
          'Person',
          actor
        );

        expect(result).toHaveLength(1);
      });

      it('should skip self-triggered events when ignoreSelfTriggered is true', async () => {
        const agent = createMockAgent({
          id: 'agent-1',
          reactionConfig: createMockReactionConfig({
            ignoreAgentTriggered: false,
            ignoreSelfTriggered: true,
          }),
        });
        mockAgentRepo.find.mockResolvedValue([agent]);

        const actor: ActorContext = {
          actorType: 'agent',
          actorId: 'agent-1', // Same as the agent
        };

        const result = await service.findMatchingAgents(
          'created',
          'Person',
          actor
        );

        expect(result).toHaveLength(0);
      });

      it('should allow events from other agents when ignoreSelfTriggered is true', async () => {
        const agent = createMockAgent({
          id: 'agent-1',
          reactionConfig: createMockReactionConfig({
            ignoreAgentTriggered: false,
            ignoreSelfTriggered: true,
          }),
        });
        mockAgentRepo.find.mockResolvedValue([agent]);

        const actor: ActorContext = {
          actorType: 'agent',
          actorId: 'agent-2', // Different agent
        };

        const result = await service.findMatchingAgents(
          'created',
          'Person',
          actor
        );

        expect(result).toHaveLength(1);
      });

      it('should allow user-triggered events regardless of settings', async () => {
        const agent = createMockAgent({
          reactionConfig: createMockReactionConfig({
            ignoreAgentTriggered: true, // Only applies to agents
          }),
        });
        mockAgentRepo.find.mockResolvedValue([agent]);

        const actor: ActorContext = {
          actorType: 'user',
          actorId: 'user-1',
        };

        const result = await service.findMatchingAgents(
          'created',
          'Person',
          actor
        );

        expect(result).toHaveLength(1);
      });
    });
  });

  describe('shouldProcess', () => {
    it('should always return true for parallel strategy', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          concurrencyStrategy: 'parallel',
        }),
      });

      const result = await service.shouldProcess(agent, 'obj-1', 1, 'created');

      expect(result.shouldProcess).toBe(true);
    });

    it('should return false when already processing object with skip strategy', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          concurrencyStrategy: 'skip',
        }),
      });
      mockProcessingLogService.isAgentProcessingObject.mockResolvedValue(true);

      const result = await service.shouldProcess(agent, 'obj-1', 1, 'created');

      expect(result.shouldProcess).toBe(false);
      expect(result.reason).toContain('already processing');
    });

    it('should return false when same version+event is already pending/processing', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          concurrencyStrategy: 'skip',
        }),
      });
      mockProcessingLogService.isAgentProcessingObject.mockResolvedValue(false);
      mockProcessingLogService.findPendingOrProcessing.mockResolvedValue({
        id: 'existing-log',
      });

      const result = await service.shouldProcess(agent, 'obj-1', 1, 'created');

      expect(result.shouldProcess).toBe(false);
      expect(result.reason).toContain('Already processing');
    });

    it('should return true when no existing processing for skip strategy', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          concurrencyStrategy: 'skip',
        }),
      });
      mockProcessingLogService.isAgentProcessingObject.mockResolvedValue(false);
      mockProcessingLogService.findPendingOrProcessing.mockResolvedValue(null);

      const result = await service.shouldProcess(agent, 'obj-1', 1, 'created');

      expect(result.shouldProcess).toBe(true);
    });
  });

  describe('cleanupStuckJobs', () => {
    it('should call markStuckJobsAsAbandoned', async () => {
      mockProcessingLogService.markStuckJobsAsAbandoned.mockResolvedValue(3);

      const count = await service.cleanupStuckJobs();

      expect(
        mockProcessingLogService.markStuckJobsAsAbandoned
      ).toHaveBeenCalled();
      expect(count).toBe(3);
    });
  });

  describe('event type mapping', () => {
    // Access private method via any cast for testing
    const mapEventType = (service: any, type: string) =>
      service.mapEventType(type);

    it('should map entity.created to created', () => {
      expect(mapEventType(service, 'entity.created')).toBe('created');
    });

    it('should map entity.updated to updated', () => {
      expect(mapEventType(service, 'entity.updated')).toBe('updated');
    });

    it('should map entity.deleted to deleted', () => {
      expect(mapEventType(service, 'entity.deleted')).toBe('deleted');
    });

    it('should return null for unknown event types', () => {
      expect(mapEventType(service, 'entity.batch')).toBeNull();
      expect(mapEventType(service, 'unknown')).toBeNull();
    });
  });

  describe('getPendingEvents', () => {
    it('should return empty result for agent without reactionConfig', async () => {
      const agent = createMockAgent({ reactionConfig: null as any });

      const result = await service.getPendingEvents(agent);

      expect(result.totalCount).toBe(0);
      expect(result.objects).toHaveLength(0);
      expect(result.reactionConfig.objectTypes).toEqual([]);
      expect(result.reactionConfig.events).toEqual([]);
    });

    it('should query objects filtered by projectId', async () => {
      const agent = createMockAgent({ projectId: 'project-1' });
      mockQueryBuilder.getCount.mockResolvedValue(0);
      mockQueryBuilder.getMany.mockResolvedValue([]);

      await service.getPendingEvents(agent);

      expect(mockGraphObjectRepo.createQueryBuilder).toHaveBeenCalledWith('go');
      expect(mockQueryBuilder.where).toHaveBeenCalledWith(
        'go.projectId = :projectId',
        { projectId: 'project-1' }
      );
    });

    it('should filter by objectTypes when specified in config', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          objectTypes: ['Person', 'Company'],
        }),
      });
      mockQueryBuilder.getCount.mockResolvedValue(0);
      mockQueryBuilder.getMany.mockResolvedValue([]);

      await service.getPendingEvents(agent);

      expect(mockQueryBuilder.andWhere).toHaveBeenCalledWith(
        'go.type IN (:...objectTypes)',
        { objectTypes: ['Person', 'Company'] }
      );
    });

    it('should NOT filter by objectTypes when empty array', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          objectTypes: [],
        }),
      });
      mockQueryBuilder.getCount.mockResolvedValue(0);
      mockQueryBuilder.getMany.mockResolvedValue([]);

      await service.getPendingEvents(agent);

      // Should only have the NOT EXISTS subquery, not the objectTypes filter
      expect(mockQueryBuilder.andWhere).toHaveBeenCalledTimes(1);
      expect(mockQueryBuilder.andWhere).toHaveBeenCalledWith(
        expect.stringContaining('NOT EXISTS'),
        expect.any(Object)
      );
    });

    it('should exclude objects that have been completed by this agent', async () => {
      const agent = createMockAgent({ id: 'agent-1' });
      mockQueryBuilder.getCount.mockResolvedValue(0);
      mockQueryBuilder.getMany.mockResolvedValue([]);

      await service.getPendingEvents(agent);

      expect(mockQueryBuilder.andWhere).toHaveBeenCalledWith(
        expect.stringContaining('NOT EXISTS'),
        { agentId: 'agent-1' }
      );
      expect(mockQueryBuilder.andWhere).toHaveBeenCalledWith(
        expect.stringContaining("status = 'completed'"),
        expect.any(Object)
      );
    });

    it('should return totalCount and limited objects', async () => {
      const agent = createMockAgent();
      const mockObjects = [
        createMockGraphObject({ id: 'obj-1', type: 'Person', key: 'p-1' }),
        createMockGraphObject({ id: 'obj-2', type: 'Person', key: 'p-2' }),
      ];
      mockQueryBuilder.getCount.mockResolvedValue(5);
      mockQueryBuilder.getMany.mockResolvedValue(mockObjects);

      const result = await service.getPendingEvents(agent, 2);

      expect(result.totalCount).toBe(5);
      expect(result.objects).toHaveLength(2);
      expect(result.objects[0].id).toBe('obj-1');
      expect(result.objects[0].type).toBe('Person');
      expect(result.objects[0].key).toBe('p-1');
      expect(mockQueryBuilder.limit).toHaveBeenCalledWith(2);
    });

    it('should order by createdAt descending', async () => {
      const agent = createMockAgent();
      mockQueryBuilder.getCount.mockResolvedValue(0);
      mockQueryBuilder.getMany.mockResolvedValue([]);

      await service.getPendingEvents(agent);

      expect(mockQueryBuilder.orderBy).toHaveBeenCalledWith(
        'go.createdAt',
        'DESC'
      );
    });

    it('should include reactionConfig in response', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          objectTypes: ['Person'],
          events: ['created', 'updated'],
        }),
      });
      mockQueryBuilder.getCount.mockResolvedValue(0);
      mockQueryBuilder.getMany.mockResolvedValue([]);

      const result = await service.getPendingEvents(agent);

      expect(result.reactionConfig.objectTypes).toEqual(['Person']);
      expect(result.reactionConfig.events).toEqual(['created', 'updated']);
    });

    it('should use default limit of 100', async () => {
      const agent = createMockAgent();
      mockQueryBuilder.getCount.mockResolvedValue(0);
      mockQueryBuilder.getMany.mockResolvedValue([]);

      await service.getPendingEvents(agent);

      expect(mockQueryBuilder.limit).toHaveBeenCalledWith(100);
    });
  });

  describe('batchTrigger', () => {
    it('should return all skipped when agent has no reactionConfig', async () => {
      const agent = createMockAgent({ reactionConfig: null as any });
      const objectIds = ['obj-1', 'obj-2'];

      const result = await service.batchTrigger(agent, objectIds);

      expect(result.queued).toBe(0);
      expect(result.skipped).toBe(2);
      expect(result.skippedDetails).toHaveLength(2);
      expect(result.skippedDetails[0].reason).toContain('no reaction config');
    });

    it('should skip objects not found in project', async () => {
      const agent = createMockAgent({ projectId: 'project-1' });
      mockGraphObjectRepo.find.mockResolvedValue([]);

      const result = await service.batchTrigger(agent, ['obj-not-found']);

      expect(result.queued).toBe(0);
      expect(result.skipped).toBe(1);
      expect(result.skippedDetails[0].objectId).toBe('obj-not-found');
      expect(result.skippedDetails[0].reason).toContain('not found');
    });

    it('should skip objects with wrong type', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          objectTypes: ['Company'], // Only Company, not Person
        }),
      });
      const obj = createMockGraphObject({ id: 'obj-1', type: 'Person' });
      mockGraphObjectRepo.find.mockResolvedValue([obj]);

      const result = await service.batchTrigger(agent, ['obj-1']);

      expect(result.queued).toBe(0);
      expect(result.skipped).toBe(1);
      expect(result.skippedDetails[0].reason).toContain("type 'Person'");
      expect(result.skippedDetails[0].reason).toContain('Company');
    });

    it('should skip objects already being processed', async () => {
      const agent = createMockAgent();
      const obj = createMockGraphObject({ id: 'obj-1', type: 'Person' });
      mockGraphObjectRepo.find.mockResolvedValue([obj]);
      mockProcessingLogService.isAgentProcessingObject.mockResolvedValue(true);

      const result = await service.batchTrigger(agent, ['obj-1']);

      expect(result.queued).toBe(0);
      expect(result.skipped).toBe(1);
      expect(result.skippedDetails[0].reason).toContain('already processing');
    });

    it('should queue objects matching criteria', async () => {
      const agent = createMockAgent({
        id: 'agent-1',
        projectId: 'project-1',
        reactionConfig: createMockReactionConfig({
          objectTypes: [],
        }),
      });
      const obj = createMockGraphObject({ id: 'obj-1', type: 'Person' });
      mockGraphObjectRepo.find.mockResolvedValue([obj]);
      mockProcessingLogService.isAgentProcessingObject.mockResolvedValue(false);
      mockProcessingLogService.findPendingOrProcessing.mockResolvedValue(null);
      mockProcessingLogService.createEntry.mockResolvedValue({ id: 'log-1' });

      const result = await service.batchTrigger(agent, ['obj-1']);

      expect(result.queued).toBe(1);
      expect(result.skipped).toBe(0);
      expect(mockProcessingLogService.createEntry).toHaveBeenCalledWith({
        agentId: 'agent-1',
        graphObjectId: 'obj-1',
        objectVersion: 1,
        eventType: 'created',
      });
    });

    it('should queue multiple objects', async () => {
      const agent = createMockAgent({ projectId: 'project-1' });
      const objects = [
        createMockGraphObject({ id: 'obj-1', type: 'Person' }),
        createMockGraphObject({ id: 'obj-2', type: 'Person' }),
        createMockGraphObject({ id: 'obj-3', type: 'Person' }),
      ];
      mockGraphObjectRepo.find.mockResolvedValue(objects);
      mockProcessingLogService.isAgentProcessingObject.mockResolvedValue(false);
      mockProcessingLogService.findPendingOrProcessing.mockResolvedValue(null);
      mockProcessingLogService.createEntry.mockResolvedValue({ id: 'log-1' });

      const result = await service.batchTrigger(agent, [
        'obj-1',
        'obj-2',
        'obj-3',
      ]);

      expect(result.queued).toBe(3);
      expect(result.skipped).toBe(0);
      expect(mockProcessingLogService.createEntry).toHaveBeenCalledTimes(3);
    });

    it('should handle mixed results (some queued, some skipped)', async () => {
      const agent = createMockAgent({
        reactionConfig: createMockReactionConfig({
          objectTypes: ['Person'], // Only Person type
        }),
      });
      const objects = [
        createMockGraphObject({ id: 'obj-1', type: 'Person' }),
        createMockGraphObject({ id: 'obj-2', type: 'Company' }), // Wrong type
      ];
      mockGraphObjectRepo.find.mockResolvedValue(objects);
      mockProcessingLogService.isAgentProcessingObject.mockResolvedValue(false);
      mockProcessingLogService.findPendingOrProcessing.mockResolvedValue(null);
      mockProcessingLogService.createEntry.mockResolvedValue({ id: 'log-1' });

      const result = await service.batchTrigger(agent, [
        'obj-1',
        'obj-2',
        'obj-not-found',
      ]);

      expect(result.queued).toBe(1);
      expect(result.skipped).toBe(2);
      expect(result.skippedDetails).toContainEqual(
        expect.objectContaining({ objectId: 'obj-2' })
      );
      expect(result.skippedDetails).toContainEqual(
        expect.objectContaining({ objectId: 'obj-not-found' })
      );
    });

    it('should use created as default event type', async () => {
      const agent = createMockAgent({ projectId: 'project-1' });
      const obj = createMockGraphObject({ id: 'obj-1' });
      mockGraphObjectRepo.find.mockResolvedValue([obj]);
      mockProcessingLogService.isAgentProcessingObject.mockResolvedValue(false);
      mockProcessingLogService.findPendingOrProcessing.mockResolvedValue(null);
      mockProcessingLogService.createEntry.mockResolvedValue({ id: 'log-1' });

      await service.batchTrigger(agent, ['obj-1']);

      expect(mockProcessingLogService.createEntry).toHaveBeenCalledWith(
        expect.objectContaining({ eventType: 'created' })
      );
    });

    it('should query objects with correct projectId filter', async () => {
      const agent = createMockAgent({ projectId: 'project-1' });
      mockGraphObjectRepo.find.mockResolvedValue([]);

      await service.batchTrigger(agent, ['obj-1']);

      expect(mockGraphObjectRepo.find).toHaveBeenCalledWith({
        where: {
          id: expect.anything(),
          projectId: 'project-1',
        },
      });
    });
  });
});
