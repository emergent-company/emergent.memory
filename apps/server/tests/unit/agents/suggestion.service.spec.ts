import { describe, it, expect, beforeEach, vi } from 'vitest';
import { SuggestionService } from '../../../src/modules/agents/suggestion.service';
import { TasksService } from '../../../src/modules/tasks/tasks.service';
import { Agent } from '../../../src/entities/agent.entity';
import { Task } from '../../../src/entities/task.entity';
import { TaskSourceType } from '../../../src/modules/tasks/dto/task.dto';

describe('SuggestionService', () => {
  let service: SuggestionService;
  let mockTasksService: {
    create: ReturnType<typeof vi.fn>;
  };

  // Mock factory
  const createMockAgent = (overrides: Partial<Agent> = {}): Agent =>
    ({
      id: 'agent-1',
      name: 'Test Agent',
      role: 'test-role',
      prompt: null,
      cronSchedule: '0 * * * *',
      enabled: true,
      triggerType: 'reaction',
      reactionConfig: null,
      executionMode: 'suggest',
      capabilities: null,
      config: {},
      description: null,
      createdAt: new Date(),
      updatedAt: new Date(),
      ...overrides,
    } as Agent);

  const createMockTask = (overrides: Partial<Task> = {}): Task =>
    ({
      id: 'task-1',
      projectId: 'project-1',
      title: 'Test Task',
      description: 'Test description',
      type: 'object_create_suggestion',
      status: 'pending',
      sourceType: 'agent',
      sourceId: 'agent-1',
      metadata: {},
      createdAt: new Date(),
      updatedAt: new Date(),
      resolvedAt: null,
      resolvedBy: null,
      resolutionNotes: null,
      ...overrides,
    } as Task);

  beforeEach(() => {
    // Create fresh mock for each test
    mockTasksService = {
      create: vi.fn().mockResolvedValue(createMockTask()),
    };

    // Direct instantiation with mock
    service = new SuggestionService(
      mockTasksService as unknown as TasksService
    );
  });

  describe('createObjectSuggestion', () => {
    it('should create an object creation suggestion task', async () => {
      const agent = createMockAgent();
      const input = {
        projectId: 'project-1',
        agent,
        objectType: 'Person',
        proposedData: { name: 'John Doe', email: 'john@example.com' },
        reason: 'Found in email',
        confidence: 0.85,
        processingLogId: 'log-1',
      };

      await service.createObjectSuggestion(input);

      expect(mockTasksService.create).toHaveBeenCalledWith({
        projectId: 'project-1',
        title: 'Test Agent suggests creating a new Person',
        description: 'Found in email',
        type: 'object_create_suggestion',
        sourceType: TaskSourceType.AGENT,
        sourceId: 'agent-1',
        metadata: {
          agentId: 'agent-1',
          agentName: 'Test Agent',
          processingLogId: 'log-1',
          confidence: 0.85,
          objectType: 'Person',
          proposedData: { name: 'John Doe', email: 'john@example.com' },
          reason: 'Found in email',
        },
      });
    });

    it('should use default description when reason is not provided', async () => {
      const agent = createMockAgent();
      const input = {
        projectId: 'project-1',
        agent,
        objectType: 'Company',
        proposedData: { name: 'Acme Corp' },
      };

      await service.createObjectSuggestion(input);

      expect(mockTasksService.create).toHaveBeenCalledWith(
        expect.objectContaining({
          description: 'Review the suggested Company creation',
        })
      );
    });

    it('should return the created task', async () => {
      const expectedTask = createMockTask({ id: 'new-task-id' });
      mockTasksService.create.mockResolvedValue(expectedTask);

      const result = await service.createObjectSuggestion({
        projectId: 'project-1',
        agent: createMockAgent(),
        objectType: 'Person',
        proposedData: {},
      });

      expect(result).toEqual(expectedTask);
    });
  });

  describe('createUpdateSuggestion', () => {
    it('should create an object update suggestion task', async () => {
      const agent = createMockAgent();
      const input = {
        projectId: 'project-1',
        agent,
        targetObjectId: 'obj-123',
        objectType: 'Person',
        currentVersion: 3,
        proposedChanges: { email: 'updated@example.com' },
        reason: 'Email address changed',
        confidence: 0.9,
      };

      await service.createUpdateSuggestion(input);

      expect(mockTasksService.create).toHaveBeenCalledWith({
        projectId: 'project-1',
        title: 'Test Agent suggests updating Person',
        description: 'Email address changed',
        type: 'object_update_suggestion',
        sourceType: TaskSourceType.AGENT,
        sourceId: 'agent-1',
        metadata: expect.objectContaining({
          targetObjectId: 'obj-123',
          objectType: 'Person',
          currentVersion: 3,
          proposedChanges: { email: 'updated@example.com' },
        }),
      });
    });

    it('should include currentVersion in metadata', async () => {
      await service.createUpdateSuggestion({
        projectId: 'project-1',
        agent: createMockAgent(),
        targetObjectId: 'obj-123',
        objectType: 'Person',
        currentVersion: 5,
        proposedChanges: {},
      });

      expect(mockTasksService.create).toHaveBeenCalledWith(
        expect.objectContaining({
          metadata: expect.objectContaining({
            currentVersion: 5,
          }),
        })
      );
    });
  });

  describe('createDeleteSuggestion', () => {
    it('should create an object deletion suggestion task', async () => {
      const agent = createMockAgent();
      const input = {
        projectId: 'project-1',
        agent,
        targetObjectId: 'obj-123',
        objectType: 'Person',
        currentData: { name: 'John Doe' },
        reason: 'Duplicate entry',
      };

      await service.createDeleteSuggestion(input);

      expect(mockTasksService.create).toHaveBeenCalledWith({
        projectId: 'project-1',
        title: 'Test Agent suggests deleting Person',
        description: 'Duplicate entry',
        type: 'object_delete_suggestion',
        sourceType: TaskSourceType.AGENT,
        sourceId: 'agent-1',
        metadata: expect.objectContaining({
          targetObjectId: 'obj-123',
          objectType: 'Person',
          currentData: { name: 'John Doe' },
        }),
      });
    });
  });

  describe('createRelationshipSuggestion', () => {
    it('should create a relationship suggestion task', async () => {
      const agent = createMockAgent();
      const input = {
        projectId: 'project-1',
        agent,
        sourceObjectId: 'obj-1',
        sourceObjectType: 'Person',
        targetObjectId: 'obj-2',
        targetObjectType: 'Company',
        relationshipType: 'WORKS_AT',
        relationshipProps: { role: 'Engineer' },
        reason: 'Mentioned in document',
      };

      await service.createRelationshipSuggestion(input);

      expect(mockTasksService.create).toHaveBeenCalledWith({
        projectId: 'project-1',
        title: 'Test Agent suggests creating WORKS_AT relationship',
        description: 'Mentioned in document',
        type: 'relationship_create_suggestion',
        sourceType: TaskSourceType.AGENT,
        sourceId: 'agent-1',
        metadata: expect.objectContaining({
          sourceObjectId: 'obj-1',
          sourceObjectType: 'Person',
          targetObjectId: 'obj-2',
          targetObjectType: 'Company',
          relationshipType: 'WORKS_AT',
          relationshipProps: { role: 'Engineer' },
        }),
      });
    });

    it('should use default description when reason is not provided', async () => {
      await service.createRelationshipSuggestion({
        projectId: 'project-1',
        agent: createMockAgent(),
        sourceObjectId: 'obj-1',
        sourceObjectType: 'Person',
        targetObjectId: 'obj-2',
        targetObjectType: 'Company',
        relationshipType: 'EMPLOYED_BY',
      });

      expect(mockTasksService.create).toHaveBeenCalledWith(
        expect.objectContaining({
          description:
            'Review the suggested EMPLOYED_BY relationship between Person and Company',
        })
      );
    });
  });

  describe('isSuggestionTask', () => {
    it('should return true for object_create_suggestion', () => {
      expect(service.isSuggestionTask('object_create_suggestion')).toBe(true);
    });

    it('should return true for object_update_suggestion', () => {
      expect(service.isSuggestionTask('object_update_suggestion')).toBe(true);
    });

    it('should return true for object_delete_suggestion', () => {
      expect(service.isSuggestionTask('object_delete_suggestion')).toBe(true);
    });

    it('should return true for relationship_create_suggestion', () => {
      expect(service.isSuggestionTask('relationship_create_suggestion')).toBe(
        true
      );
    });

    it('should return false for merge_suggestion', () => {
      expect(service.isSuggestionTask('merge_suggestion')).toBe(false);
    });

    it('should return false for other task types', () => {
      expect(service.isSuggestionTask('review_task')).toBe(false);
      expect(service.isSuggestionTask('custom_task')).toBe(false);
      expect(service.isSuggestionTask('')).toBe(false);
    });
  });
});
