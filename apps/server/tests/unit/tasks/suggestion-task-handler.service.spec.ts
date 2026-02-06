import { describe, it, expect, beforeEach, vi } from 'vitest';
import { SuggestionTaskHandlerService } from '../../../src/modules/tasks/suggestion-task-handler.service';
import { Task } from '../../../src/entities/task.entity';
import {
  ObjectCreateSuggestionMetadata,
  ObjectUpdateSuggestionMetadata,
  ObjectDeleteSuggestionMetadata,
  RelationshipCreateSuggestionMetadata,
} from '../../../src/modules/agents/suggestion.service';

describe('SuggestionTaskHandlerService', () => {
  let service: SuggestionTaskHandlerService;
  let mockGraphService: {
    createObject: ReturnType<typeof vi.fn>;
    patchObject: ReturnType<typeof vi.fn>;
    deleteObject: ReturnType<typeof vi.fn>;
    getObjectById: ReturnType<typeof vi.fn>;
    createRelationship: ReturnType<typeof vi.fn>;
  };

  // Mock factory
  const createMockTask = (type: string, metadata: Record<string, any>): Task =>
    ({
      id: 'task-1',
      projectId: 'project-1',
      title: 'Test Task',
      description: 'Test description',
      type,
      status: 'pending',
      sourceType: 'agent',
      sourceId: 'agent-1',
      metadata,
      createdAt: new Date(),
      updatedAt: new Date(),
      resolvedAt: null,
      resolvedBy: null,
      resolutionNotes: null,
    } as Task);

  beforeEach(() => {
    // Create fresh mocks for each test
    mockGraphService = {
      createObject: vi.fn().mockResolvedValue({
        id: 'new-obj-1',
        version: 1,
      }),
      patchObject: vi.fn().mockResolvedValue({
        id: 'obj-1',
        version: 2,
      }),
      deleteObject: vi.fn().mockResolvedValue(undefined),
      getObjectById: vi.fn().mockResolvedValue({
        id: 'obj-1',
        version: 1,
      }),
      createRelationship: vi.fn().mockResolvedValue({
        id: 'rel-1',
      }),
    };

    // Direct instantiation with mock
    service = new SuggestionTaskHandlerService(mockGraphService);
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

    it('should return false for non-suggestion task types', () => {
      expect(service.isSuggestionTask('merge_suggestion')).toBe(false);
      expect(service.isSuggestionTask('review_task')).toBe(false);
    });
  });

  describe('applySuggestion', () => {
    describe('for non-suggestion tasks', () => {
      it('should return error for non-suggestion task type', async () => {
        const task = createMockTask('merge_suggestion', {});

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('not a suggestion task');
      });
    });

    describe('for object_create_suggestion', () => {
      const createMetadata: ObjectCreateSuggestionMetadata = {
        agentId: 'agent-1',
        agentName: 'Test Agent',
        objectType: 'Person',
        proposedData: { name: 'John Doe', email: 'john@example.com' },
      };

      it('should create object with user as actor', async () => {
        const task = createMockTask('object_create_suggestion', createMetadata);

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(true);
        expect(result.objectId).toBe('new-obj-1');
        expect(mockGraphService.createObject).toHaveBeenCalledWith(
          {
            type: 'Person',
            properties: { name: 'John Doe', email: 'john@example.com' },
            project_id: 'project-1',
            actor: { actorType: 'user', actorId: 'user-1' },
          },
          { projectId: 'project-1' }
        );
      });

      it('should return error if objectType is missing', async () => {
        const task = createMockTask('object_create_suggestion', {
          proposedData: {},
        });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('Missing objectType');
      });

      it('should return error if proposedData is missing', async () => {
        const task = createMockTask('object_create_suggestion', {
          objectType: 'Person',
        });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('Missing');
      });
    });

    describe('for object_update_suggestion', () => {
      const updateMetadata: ObjectUpdateSuggestionMetadata = {
        agentId: 'agent-1',
        agentName: 'Test Agent',
        targetObjectId: 'obj-1',
        objectType: 'Person',
        currentVersion: 1,
        proposedChanges: { email: 'newemail@example.com' },
      };

      it('should update object when version matches', async () => {
        const task = createMockTask('object_update_suggestion', updateMetadata);
        mockGraphService.getObjectById.mockResolvedValue({
          id: 'obj-1',
          version: 1,
        });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(true);
        expect(mockGraphService.patchObject).toHaveBeenCalledWith(
          'obj-1',
          {
            properties: { email: 'newemail@example.com' },
            actor: { actorType: 'user', actorId: 'user-1' },
          },
          { projectId: 'project-1' }
        );
      });

      it('should reject if object has been modified since suggestion', async () => {
        const task = createMockTask('object_update_suggestion', {
          ...updateMetadata,
          currentVersion: 1,
        });
        mockGraphService.getObjectById.mockResolvedValue({
          id: 'obj-1',
          version: 3, // Higher than suggestion's version
        });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('modified since suggestion');
        expect(mockGraphService.patchObject).not.toHaveBeenCalled();
      });

      it('should return error if target object not found', async () => {
        const task = createMockTask('object_update_suggestion', updateMetadata);
        mockGraphService.getObjectById.mockResolvedValue(null);

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('not found');
      });

      it('should return error if targetObjectId is missing', async () => {
        const task = createMockTask('object_update_suggestion', {
          proposedChanges: {},
        });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('Missing targetObjectId');
      });
    });

    describe('for object_delete_suggestion', () => {
      const deleteMetadata: ObjectDeleteSuggestionMetadata = {
        agentId: 'agent-1',
        agentName: 'Test Agent',
        targetObjectId: 'obj-1',
        objectType: 'Person',
      };

      it('should delete object when it exists', async () => {
        const task = createMockTask('object_delete_suggestion', deleteMetadata);
        mockGraphService.getObjectById.mockResolvedValue({
          id: 'obj-1',
          version: 1,
        });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(true);
        expect(result.objectId).toBe('obj-1');
        expect(mockGraphService.deleteObject).toHaveBeenCalledWith(
          'obj-1',
          { projectId: 'project-1' },
          { actorType: 'user', actorId: 'user-1' }
        );
      });

      it('should return error if target object not found', async () => {
        const task = createMockTask('object_delete_suggestion', deleteMetadata);
        mockGraphService.getObjectById.mockResolvedValue(null);

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('not found or already deleted');
      });

      it('should return error if targetObjectId is missing', async () => {
        const task = createMockTask('object_delete_suggestion', {
          objectType: 'Person',
        });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('Missing targetObjectId');
      });
    });

    describe('for relationship_create_suggestion', () => {
      const relationshipMetadata: RelationshipCreateSuggestionMetadata = {
        agentId: 'agent-1',
        agentName: 'Test Agent',
        sourceObjectId: 'obj-1',
        sourceObjectType: 'Person',
        targetObjectId: 'obj-2',
        targetObjectType: 'Company',
        relationshipType: 'WORKS_AT',
        relationshipProps: { role: 'Engineer' },
      };

      it('should create relationship when both objects exist', async () => {
        const task = createMockTask(
          'relationship_create_suggestion',
          relationshipMetadata
        );
        mockGraphService.getObjectById
          .mockResolvedValueOnce({ id: 'obj-1' })
          .mockResolvedValueOnce({ id: 'obj-2' });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(true);
        expect(result.relationshipId).toBe('rel-1');
        expect(mockGraphService.createRelationship).toHaveBeenCalledWith(
          {
            source_id: 'obj-1',
            target_id: 'obj-2',
            type: 'WORKS_AT',
            properties: { role: 'Engineer' },
            project_id: 'project-1',
          },
          { projectId: 'project-1' }
        );
      });

      it('should return error if source object not found', async () => {
        const task = createMockTask(
          'relationship_create_suggestion',
          relationshipMetadata
        );
        mockGraphService.getObjectById
          .mockResolvedValueOnce(null) // source not found
          .mockResolvedValueOnce({ id: 'obj-2' });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('Source object');
        expect(result.error).toContain('not found');
      });

      it('should return error if target object not found', async () => {
        const task = createMockTask(
          'relationship_create_suggestion',
          relationshipMetadata
        );
        mockGraphService.getObjectById
          .mockResolvedValueOnce({ id: 'obj-1' })
          .mockResolvedValueOnce(null); // target not found

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('Target object');
        expect(result.error).toContain('not found');
      });

      it('should return error if required metadata is missing', async () => {
        const task = createMockTask('relationship_create_suggestion', {
          sourceObjectId: 'obj-1',
          // missing targetObjectId and relationshipType
        });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toContain('Missing');
      });

      it('should handle missing relationshipProps', async () => {
        const task = createMockTask('relationship_create_suggestion', {
          ...relationshipMetadata,
          relationshipProps: undefined,
        });
        mockGraphService.getObjectById
          .mockResolvedValueOnce({ id: 'obj-1' })
          .mockResolvedValueOnce({ id: 'obj-2' });

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(true);
        expect(mockGraphService.createRelationship).toHaveBeenCalledWith(
          expect.objectContaining({
            properties: {},
          }),
          expect.anything()
        );
      });
    });

    describe('error handling', () => {
      it('should catch and return errors from graphService', async () => {
        const task = createMockTask('object_create_suggestion', {
          objectType: 'Person',
          proposedData: {},
        });
        mockGraphService.createObject.mockRejectedValue(
          new Error('Database error')
        );

        const result = await service.applySuggestion(task, 'user-1');

        expect(result.success).toBe(false);
        expect(result.error).toBe('Database error');
      });
    });
  });

  describe('getSuggestionSummary', () => {
    it('should return summary for object_create_suggestion', () => {
      const task = createMockTask('object_create_suggestion', {
        objectType: 'Person',
      });

      const result = service.getSuggestionSummary(task);

      expect(result).toBe('Create new Person');
    });

    it('should return summary for object_update_suggestion with field count', () => {
      const task = createMockTask('object_update_suggestion', {
        objectType: 'Person',
        proposedChanges: { name: 'New Name', email: 'new@email.com' },
      });

      const result = service.getSuggestionSummary(task);

      expect(result).toBe('Update Person (2 fields)');
    });

    it('should handle singular field in object_update_suggestion', () => {
      const task = createMockTask('object_update_suggestion', {
        objectType: 'Person',
        proposedChanges: { name: 'New Name' },
      });

      const result = service.getSuggestionSummary(task);

      expect(result).toBe('Update Person (1 field)');
    });

    it('should return summary for object_delete_suggestion', () => {
      const task = createMockTask('object_delete_suggestion', {
        objectType: 'Person',
      });

      const result = service.getSuggestionSummary(task);

      expect(result).toBe('Delete Person');
    });

    it('should return summary for relationship_create_suggestion', () => {
      const task = createMockTask('relationship_create_suggestion', {
        relationshipType: 'WORKS_AT',
        sourceObjectType: 'Person',
        targetObjectType: 'Company',
      });

      const result = service.getSuggestionSummary(task);

      expect(result).toBe(
        'Create WORKS_AT relationship between Person and Company'
      );
    });

    it('should return "Unknown task type" for non-suggestion tasks', () => {
      const task = createMockTask('merge_suggestion', {});

      const result = service.getSuggestionSummary(task);

      expect(result).toBe('Unknown task type');
    });
  });
});
