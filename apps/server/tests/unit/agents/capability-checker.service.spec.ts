import { describe, it, expect, beforeEach } from 'vitest';
import {
  CapabilityCheckerService,
  GraphOperationType,
} from '../../../src/modules/agents/capability-checker.service';
import { Agent, AgentCapabilities } from '../../../src/entities/agent.entity';

describe('CapabilityCheckerService', () => {
  let service: CapabilityCheckerService;

  // Mock factory
  const createMockCapabilities = (
    overrides: Partial<AgentCapabilities> = {}
  ): AgentCapabilities => ({
    canCreateObjects: true,
    canUpdateObjects: true,
    canDeleteObjects: false,
    canCreateRelationships: true,
    allowedObjectTypes: null,
    ...overrides,
  });

  const createMockAgent = (capabilities: AgentCapabilities | null): Agent =>
    ({
      id: 'agent-1',
      name: 'Test Agent',
      role: 'test-role',
      prompt: null,
      cronSchedule: '0 * * * *',
      enabled: true,
      triggerType: 'reaction',
      reactionConfig: null,
      executionMode: 'execute',
      capabilities,
      config: {},
      description: null,
      createdAt: new Date(),
      updatedAt: new Date(),
    } as Agent);

  beforeEach(() => {
    service = new CapabilityCheckerService();
  });

  describe('checkCapability', () => {
    describe('when capabilities are null (no restrictions)', () => {
      it('should allow all operations', () => {
        const agent = createMockAgent(null);

        expect(service.checkCapability(agent, 'create_object')).toEqual({
          allowed: true,
        });
        expect(service.checkCapability(agent, 'update_object')).toEqual({
          allowed: true,
        });
        expect(service.checkCapability(agent, 'delete_object')).toEqual({
          allowed: true,
        });
        expect(service.checkCapability(agent, 'create_relationship')).toEqual({
          allowed: true,
        });
      });
    });

    describe('operation-specific checks', () => {
      it('should deny create_object when canCreateObjects is false', () => {
        const agent = createMockAgent(
          createMockCapabilities({ canCreateObjects: false })
        );

        const result = service.checkCapability(agent, 'create_object');

        expect(result.allowed).toBe(false);
        expect(result.reason).toContain('create objects');
      });

      it('should allow create_object when canCreateObjects is true', () => {
        const agent = createMockAgent(
          createMockCapabilities({ canCreateObjects: true })
        );

        const result = service.checkCapability(agent, 'create_object');

        expect(result.allowed).toBe(true);
      });

      it('should deny update_object when canUpdateObjects is false', () => {
        const agent = createMockAgent(
          createMockCapabilities({ canUpdateObjects: false })
        );

        const result = service.checkCapability(agent, 'update_object');

        expect(result.allowed).toBe(false);
        expect(result.reason).toContain('update objects');
      });

      it('should allow update_object when canUpdateObjects is true', () => {
        const agent = createMockAgent(
          createMockCapabilities({ canUpdateObjects: true })
        );

        const result = service.checkCapability(agent, 'update_object');

        expect(result.allowed).toBe(true);
      });

      it('should deny delete_object when canDeleteObjects is false', () => {
        const agent = createMockAgent(
          createMockCapabilities({ canDeleteObjects: false })
        );

        const result = service.checkCapability(agent, 'delete_object');

        expect(result.allowed).toBe(false);
        expect(result.reason).toContain('delete objects');
      });

      it('should allow delete_object when canDeleteObjects is true', () => {
        const agent = createMockAgent(
          createMockCapabilities({ canDeleteObjects: true })
        );

        const result = service.checkCapability(agent, 'delete_object');

        expect(result.allowed).toBe(true);
      });

      it('should deny create_relationship when canCreateRelationships is false', () => {
        const agent = createMockAgent(
          createMockCapabilities({ canCreateRelationships: false })
        );

        const result = service.checkCapability(agent, 'create_relationship');

        expect(result.allowed).toBe(false);
        expect(result.reason).toContain('create relationships');
      });

      it('should allow create_relationship when canCreateRelationships is true', () => {
        const agent = createMockAgent(
          createMockCapabilities({ canCreateRelationships: true })
        );

        const result = service.checkCapability(agent, 'create_relationship');

        expect(result.allowed).toBe(true);
      });
    });

    describe('object type restrictions', () => {
      it('should allow all types when allowedObjectTypes is null', () => {
        const agent = createMockAgent(
          createMockCapabilities({ allowedObjectTypes: null })
        );

        const result = service.checkCapability(
          agent,
          'create_object',
          'AnyType'
        );

        expect(result.allowed).toBe(true);
      });

      it('should allow all types when allowedObjectTypes is empty array', () => {
        const agent = createMockAgent(
          createMockCapabilities({ allowedObjectTypes: [] })
        );

        const result = service.checkCapability(
          agent,
          'create_object',
          'AnyType'
        );

        expect(result.allowed).toBe(true);
      });

      it('should allow object type that is in the allowed list', () => {
        const agent = createMockAgent(
          createMockCapabilities({ allowedObjectTypes: ['Person', 'Company'] })
        );

        const result = service.checkCapability(
          agent,
          'create_object',
          'Person'
        );

        expect(result.allowed).toBe(true);
      });

      it('should deny object type that is not in the allowed list', () => {
        const agent = createMockAgent(
          createMockCapabilities({ allowedObjectTypes: ['Person', 'Company'] })
        );

        const result = service.checkCapability(
          agent,
          'create_object',
          'Project'
        );

        expect(result.allowed).toBe(false);
        expect(result.reason).toContain('Project');
        expect(result.reason).toContain('Person');
        expect(result.reason).toContain('Company');
      });

      it('should check operation before object type', () => {
        const agent = createMockAgent(
          createMockCapabilities({
            canCreateObjects: false,
            allowedObjectTypes: ['Person'],
          })
        );

        const result = service.checkCapability(
          agent,
          'create_object',
          'Person'
        );

        // Should fail on operation check, not object type
        expect(result.allowed).toBe(false);
        expect(result.reason).toContain('create objects');
      });
    });
  });

  describe('checkCapabilities', () => {
    it('should return success when all operations are allowed', () => {
      const agent = createMockAgent(
        createMockCapabilities({
          canCreateObjects: true,
          canUpdateObjects: true,
        })
      );

      const result = service.checkCapabilities(agent, [
        { operation: 'create_object', objectType: 'Person' },
        { operation: 'update_object', objectType: 'Person' },
      ]);

      expect(result.allowed).toBe(true);
    });

    it('should return first failure when an operation is denied', () => {
      const agent = createMockAgent(
        createMockCapabilities({
          canCreateObjects: true,
          canDeleteObjects: false,
        })
      );

      const result = service.checkCapabilities(agent, [
        { operation: 'create_object' },
        { operation: 'delete_object' },
        { operation: 'update_object' },
      ]);

      expect(result.allowed).toBe(false);
      expect(result.reason).toContain('delete objects');
    });

    it('should return success for empty operations array', () => {
      const agent = createMockAgent(
        createMockCapabilities({ canCreateObjects: false })
      );

      const result = service.checkCapabilities(agent, []);

      expect(result.allowed).toBe(true);
    });
  });

  describe('getCapabilitySummary', () => {
    it('should return "All capabilities" when no restrictions', () => {
      const agent = createMockAgent(null);

      const result = service.getCapabilitySummary(agent);

      expect(result).toBe('All capabilities (no restrictions)');
    });

    it('should list allowed and denied operations', () => {
      const agent = createMockAgent(
        createMockCapabilities({
          canCreateObjects: true,
          canUpdateObjects: true,
          canDeleteObjects: false,
          canCreateRelationships: false,
        })
      );

      const result = service.getCapabilitySummary(agent);

      expect(result).toContain('Allowed: create, update');
      expect(result).toContain('Denied: delete, relationships');
    });

    it('should include allowed object types when specified', () => {
      const agent = createMockAgent(
        createMockCapabilities({
          allowedObjectTypes: ['Person', 'Company'],
        })
      );

      const result = service.getCapabilitySummary(agent);

      expect(result).toContain('Types: Person, Company');
    });

    it('should not include Types when allowedObjectTypes is null', () => {
      const agent = createMockAgent(
        createMockCapabilities({
          allowedObjectTypes: null,
        })
      );

      const result = service.getCapabilitySummary(agent);

      expect(result).not.toContain('Types:');
    });

    it('should not include Types when allowedObjectTypes is empty', () => {
      const agent = createMockAgent(
        createMockCapabilities({
          allowedObjectTypes: [],
        })
      );

      const result = service.getCapabilitySummary(agent);

      expect(result).not.toContain('Types:');
    });
  });
});
