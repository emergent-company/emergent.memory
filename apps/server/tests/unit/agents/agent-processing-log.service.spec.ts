import { Test, TestingModule } from '@nestjs/testing';
import { getRepositoryToken } from '@nestjs/typeorm';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { In, LessThan } from 'typeorm';
import { AgentProcessingLogService } from '../../../src/modules/agents/agent-processing-log.service';
import {
  AgentProcessingLog,
  ProcessingEventType,
} from '../../../src/entities/agent-processing-log.entity';

describe('AgentProcessingLogService', () => {
  let service: AgentProcessingLogService;

  // Mock factory
  const createMockEntry = (
    overrides: Partial<AgentProcessingLog> = {}
  ): AgentProcessingLog =>
    ({
      id: 'log-1',
      agentId: 'agent-1',
      graphObjectId: 'obj-1',
      objectVersion: 1,
      eventType: 'created' as ProcessingEventType,
      status: 'pending',
      createdAt: new Date(),
      startedAt: null,
      completedAt: null,
      resultSummary: null,
      errorMessage: null,
      ...overrides,
    } as AgentProcessingLog);

  // Mock QueryBuilder
  const createMockQueryBuilder = (overrides: Record<string, unknown> = {}) => ({
    select: vi.fn().mockReturnThis(),
    where: vi.fn().mockReturnThis(),
    andWhere: vi.fn().mockReturnThis(),
    delete: vi.fn().mockReturnThis(),
    execute: vi.fn().mockResolvedValue({ affected: 0 }),
    getRawOne: vi.fn().mockResolvedValue({
      total: '10',
      pending: '2',
      processing: '1',
      completed: '5',
      failed: '1',
      abandoned: '0',
      skipped: '1',
    }),
    ...overrides,
  });

  const mockRepo = {
    create: vi.fn((data) => ({ id: 'new-log-id', ...data })),
    save: vi.fn((entity) =>
      Promise.resolve(Array.isArray(entity) ? entity : entity)
    ),
    findOne: vi.fn(),
    find: vi.fn(),
    count: vi.fn(),
    createQueryBuilder: vi.fn(() => createMockQueryBuilder()),
  };

  beforeEach(async () => {
    vi.resetAllMocks();

    // Reset mock implementations
    mockRepo.create.mockImplementation((data) => ({
      id: 'new-log-id',
      ...data,
    }));
    mockRepo.save.mockImplementation((entity) =>
      Promise.resolve(Array.isArray(entity) ? entity : entity)
    );
    mockRepo.findOne.mockResolvedValue(null);
    mockRepo.find.mockResolvedValue([]);
    mockRepo.count.mockResolvedValue(0);

    const module: TestingModule = await Test.createTestingModule({
      providers: [
        AgentProcessingLogService,
        {
          provide: getRepositoryToken(AgentProcessingLog),
          useValue: mockRepo,
        },
      ],
    }).compile();

    service = module.get<AgentProcessingLogService>(AgentProcessingLogService);
  });

  describe('createEntry', () => {
    it('should create a new entry with pending status', async () => {
      const input = {
        agentId: 'agent-1',
        graphObjectId: 'obj-1',
        objectVersion: 1,
        eventType: 'created' as ProcessingEventType,
      };

      const result = await service.createEntry(input);

      expect(mockRepo.create).toHaveBeenCalledWith({
        ...input,
        status: 'pending',
      });
      expect(mockRepo.save).toHaveBeenCalled();
      expect(result.status).toBe('pending');
    });
  });

  describe('markProcessing', () => {
    it('should update status to processing and set startedAt', async () => {
      const entry = createMockEntry({ id: 'log-1', status: 'pending' });
      mockRepo.findOne.mockResolvedValue(entry);

      const result = await service.markProcessing('log-1');

      expect(mockRepo.findOne).toHaveBeenCalledWith({
        where: { id: 'log-1' },
      });
      expect(result?.status).toBe('processing');
      expect(result?.startedAt).toBeInstanceOf(Date);
      expect(mockRepo.save).toHaveBeenCalled();
    });

    it('should return null if entry not found', async () => {
      mockRepo.findOne.mockResolvedValue(null);

      const result = await service.markProcessing('non-existent');

      expect(result).toBeNull();
      expect(mockRepo.save).not.toHaveBeenCalled();
    });
  });

  describe('markCompleted', () => {
    it('should update status to completed and set completedAt', async () => {
      const entry = createMockEntry({ id: 'log-1', status: 'processing' });
      mockRepo.findOne.mockResolvedValue(entry);

      const result = await service.markCompleted('log-1');

      expect(result?.status).toBe('completed');
      expect(result?.completedAt).toBeInstanceOf(Date);
    });

    it('should store result summary if provided', async () => {
      const entry = createMockEntry({ id: 'log-1', status: 'processing' });
      mockRepo.findOne.mockResolvedValue(entry);
      const summary = { objectsCreated: 2, suggestionsCreated: 1 };

      const result = await service.markCompleted('log-1', summary);

      expect(result?.resultSummary).toEqual(summary);
    });

    it('should return null if entry not found', async () => {
      mockRepo.findOne.mockResolvedValue(null);

      const result = await service.markCompleted('non-existent');

      expect(result).toBeNull();
    });
  });

  describe('markFailed', () => {
    it('should update status to failed and set error message', async () => {
      const entry = createMockEntry({ id: 'log-1', status: 'processing' });
      mockRepo.findOne.mockResolvedValue(entry);

      const result = await service.markFailed('log-1', 'Something went wrong');

      expect(result?.status).toBe('failed');
      expect(result?.completedAt).toBeInstanceOf(Date);
      expect(result?.errorMessage).toBe('Something went wrong');
    });

    it('should return null if entry not found', async () => {
      mockRepo.findOne.mockResolvedValue(null);

      const result = await service.markFailed('non-existent', 'error');

      expect(result).toBeNull();
    });
  });

  describe('markSkipped', () => {
    it('should update status to skipped', async () => {
      const entry = createMockEntry({ id: 'log-1', status: 'pending' });
      mockRepo.findOne.mockResolvedValue(entry);

      const result = await service.markSkipped('log-1');

      expect(result?.status).toBe('skipped');
      expect(result?.completedAt).toBeInstanceOf(Date);
    });

    it('should store reason as errorMessage if provided', async () => {
      const entry = createMockEntry({ id: 'log-1', status: 'pending' });
      mockRepo.findOne.mockResolvedValue(entry);

      const result = await service.markSkipped('log-1', 'Concurrency skip');

      expect(result?.errorMessage).toBe('Concurrency skip');
    });

    it('should return null if entry not found', async () => {
      mockRepo.findOne.mockResolvedValue(null);

      const result = await service.markSkipped('non-existent');

      expect(result).toBeNull();
    });
  });

  describe('findPendingOrProcessing', () => {
    it('should find entry with pending or processing status', async () => {
      const entry = createMockEntry({ status: 'pending' });
      mockRepo.findOne.mockResolvedValue(entry);

      const result = await service.findPendingOrProcessing(
        'agent-1',
        'obj-1',
        1,
        'created'
      );

      expect(mockRepo.findOne).toHaveBeenCalledWith({
        where: {
          agentId: 'agent-1',
          graphObjectId: 'obj-1',
          objectVersion: 1,
          eventType: 'created',
          status: In(['pending', 'processing']),
        },
      });
      expect(result).toEqual(entry);
    });

    it('should return null if no matching entry exists', async () => {
      mockRepo.findOne.mockResolvedValue(null);

      const result = await service.findPendingOrProcessing(
        'agent-1',
        'obj-1',
        1,
        'created'
      );

      expect(result).toBeNull();
    });
  });

  describe('isAgentProcessingObject', () => {
    it('should return true if there are pending/processing entries', async () => {
      mockRepo.count.mockResolvedValue(1);

      const result = await service.isAgentProcessingObject('agent-1', 'obj-1');

      expect(mockRepo.count).toHaveBeenCalledWith({
        where: {
          agentId: 'agent-1',
          graphObjectId: 'obj-1',
          status: In(['pending', 'processing']),
        },
      });
      expect(result).toBe(true);
    });

    it('should return false if no pending/processing entries', async () => {
      mockRepo.count.mockResolvedValue(0);

      const result = await service.isAgentProcessingObject('agent-1', 'obj-1');

      expect(result).toBe(false);
    });
  });

  describe('markStuckJobsAsAbandoned', () => {
    it('should mark stuck jobs as abandoned', async () => {
      const stuckJob = createMockEntry({
        id: 'stuck-1',
        status: 'processing',
        startedAt: new Date(Date.now() - 10 * 60 * 1000), // 10 minutes ago
      });
      mockRepo.find.mockResolvedValue([stuckJob]);

      const count = await service.markStuckJobsAsAbandoned();

      expect(count).toBe(1);
      expect(stuckJob.status).toBe('abandoned');
      expect(stuckJob.completedAt).toBeInstanceOf(Date);
      expect(stuckJob.errorMessage).toContain('stuck');
      expect(mockRepo.save).toHaveBeenCalledWith([stuckJob]);
    });

    it('should return 0 if no stuck jobs', async () => {
      mockRepo.find.mockResolvedValue([]);

      const count = await service.markStuckJobsAsAbandoned();

      expect(count).toBe(0);
      expect(mockRepo.save).not.toHaveBeenCalled();
    });

    it('should find jobs in processing status older than 5 minutes', async () => {
      mockRepo.find.mockResolvedValue([]);

      await service.markStuckJobsAsAbandoned();

      expect(mockRepo.find).toHaveBeenCalledWith({
        where: {
          status: 'processing',
          startedAt: expect.any(Object), // LessThan comparison
        },
      });
    });
  });

  describe('getAgentHistory', () => {
    it('should return entries ordered by createdAt descending', async () => {
      const entries = [
        createMockEntry({ id: 'log-2' }),
        createMockEntry({ id: 'log-1' }),
      ];
      mockRepo.find.mockResolvedValue(entries);

      const result = await service.getAgentHistory('agent-1');

      expect(mockRepo.find).toHaveBeenCalledWith({
        where: { agentId: 'agent-1' },
        order: { createdAt: 'DESC' },
        take: 50,
      });
      expect(result).toEqual(entries);
    });

    it('should respect custom limit', async () => {
      mockRepo.find.mockResolvedValue([]);

      await service.getAgentHistory('agent-1', 10);

      expect(mockRepo.find).toHaveBeenCalledWith({
        where: { agentId: 'agent-1' },
        order: { createdAt: 'DESC' },
        take: 10,
      });
    });
  });

  describe('getObjectHistory', () => {
    it('should return entries for a graph object', async () => {
      const entries = [createMockEntry({ id: 'log-1' })];
      mockRepo.find.mockResolvedValue(entries);

      const result = await service.getObjectHistory('obj-1');

      expect(mockRepo.find).toHaveBeenCalledWith({
        where: { graphObjectId: 'obj-1' },
        order: { createdAt: 'DESC' },
        take: 50,
      });
      expect(result).toEqual(entries);
    });
  });

  describe('getAgentStats', () => {
    it('should return statistics for an agent', async () => {
      const result = await service.getAgentStats('agent-1');

      expect(result).toEqual({
        totalEntries: 10,
        pendingCount: 2,
        processingCount: 1,
        completedCount: 5,
        failedCount: 1,
        abandonedCount: 0,
        skippedCount: 1,
      });
    });
  });

  describe('cleanupOldEntries', () => {
    it('should delete old completed/failed entries', async () => {
      const mockQb = createMockQueryBuilder({
        execute: vi.fn().mockResolvedValue({ affected: 5 }),
      });
      mockRepo.createQueryBuilder.mockReturnValue(mockQb);

      const count = await service.cleanupOldEntries(30);

      expect(count).toBe(5);
      expect(mockQb.delete).toHaveBeenCalled();
      expect(mockQb.where).toHaveBeenCalledWith('status IN (:...statuses)', {
        statuses: ['completed', 'failed', 'abandoned', 'skipped'],
      });
    });

    it('should use custom retention period', async () => {
      const mockQb = createMockQueryBuilder({
        execute: vi.fn().mockResolvedValue({ affected: 0 }),
      });
      mockRepo.createQueryBuilder.mockReturnValue(mockQb);

      await service.cleanupOldEntries(7);

      expect(mockQb.andWhere).toHaveBeenCalledWith(
        'created_at < :cutoffDate',
        expect.objectContaining({
          cutoffDate: expect.any(Date),
        })
      );
    });
  });
});
