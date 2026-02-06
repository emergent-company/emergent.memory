import { Injectable, Logger } from '@nestjs/common';
import { Agent, AgentCapabilities } from '../../entities/agent.entity';

/**
 * Types of graph operations that can be performed
 */
export type GraphOperationType =
  | 'create_object'
  | 'update_object'
  | 'delete_object'
  | 'create_relationship';

/**
 * Result of a capability check
 */
export interface CapabilityCheckResult {
  /** Whether the operation is allowed */
  allowed: boolean;
  /** Reason why the operation was denied (if not allowed) */
  reason?: string;
}

/**
 * CapabilityCheckerService
 *
 * Validates whether an agent has the capabilities to perform specific
 * graph operations. Used to enforce restrictions on reaction agents.
 *
 * Capabilities are defined in the Agent entity:
 * - canCreateObjects: Can create new graph objects
 * - canUpdateObjects: Can update existing graph objects
 * - canDeleteObjects: Can delete graph objects
 * - canCreateRelationships: Can create relationships between objects
 * - allowedObjectTypes: Restrict to specific object types (null = all)
 */
@Injectable()
export class CapabilityCheckerService {
  private readonly logger = new Logger(CapabilityCheckerService.name);

  /**
   * Check if an agent can perform a specific operation
   *
   * @param agent The agent to check
   * @param operation The type of operation
   * @param objectType The object type involved (optional, for type-based restrictions)
   * @returns Result indicating if the operation is allowed
   */
  checkCapability(
    agent: Agent,
    operation: GraphOperationType,
    objectType?: string
  ): CapabilityCheckResult {
    const capabilities = agent.capabilities;

    // If no capabilities defined, default to allowing all operations
    // This maintains backward compatibility with existing agents
    if (!capabilities) {
      return { allowed: true };
    }

    // Check operation-specific capability
    const operationAllowed = this.checkOperationCapability(
      capabilities,
      operation
    );
    if (!operationAllowed.allowed) {
      return operationAllowed;
    }

    // Check object type restriction (if applicable)
    if (objectType) {
      const typeAllowed = this.checkObjectTypeCapability(
        capabilities,
        objectType
      );
      if (!typeAllowed.allowed) {
        return typeAllowed;
      }
    }

    return { allowed: true };
  }

  /**
   * Check if the operation type is allowed by capabilities
   */
  private checkOperationCapability(
    capabilities: AgentCapabilities,
    operation: GraphOperationType
  ): CapabilityCheckResult {
    switch (operation) {
      case 'create_object':
        if (capabilities.canCreateObjects === false) {
          return {
            allowed: false,
            reason: 'Agent does not have permission to create objects',
          };
        }
        break;

      case 'update_object':
        if (capabilities.canUpdateObjects === false) {
          return {
            allowed: false,
            reason: 'Agent does not have permission to update objects',
          };
        }
        break;

      case 'delete_object':
        if (capabilities.canDeleteObjects === false) {
          return {
            allowed: false,
            reason: 'Agent does not have permission to delete objects',
          };
        }
        break;

      case 'create_relationship':
        if (capabilities.canCreateRelationships === false) {
          return {
            allowed: false,
            reason: 'Agent does not have permission to create relationships',
          };
        }
        break;
    }

    return { allowed: true };
  }

  /**
   * Check if the object type is allowed by capabilities
   */
  private checkObjectTypeCapability(
    capabilities: AgentCapabilities,
    objectType: string
  ): CapabilityCheckResult {
    const allowedTypes = capabilities.allowedObjectTypes;

    // If no type restrictions, allow all types
    if (!allowedTypes || allowedTypes.length === 0) {
      return { allowed: true };
    }

    // Check if the object type is in the allowed list
    if (!allowedTypes.includes(objectType)) {
      return {
        allowed: false,
        reason: `Agent is not allowed to operate on object type '${objectType}'. Allowed types: ${allowedTypes.join(
          ', '
        )}`,
      };
    }

    return { allowed: true };
  }

  /**
   * Check multiple operations at once
   * Returns the first failure, or success if all are allowed
   */
  checkCapabilities(
    agent: Agent,
    operations: Array<{ operation: GraphOperationType; objectType?: string }>
  ): CapabilityCheckResult {
    for (const { operation, objectType } of operations) {
      const result = this.checkCapability(agent, operation, objectType);
      if (!result.allowed) {
        return result;
      }
    }
    return { allowed: true };
  }

  /**
   * Get a summary of an agent's capabilities for logging/display
   */
  getCapabilitySummary(agent: Agent): string {
    const caps = agent.capabilities;
    if (!caps) {
      return 'All capabilities (no restrictions)';
    }

    const allowed: string[] = [];
    const denied: string[] = [];

    if (caps.canCreateObjects !== false) allowed.push('create');
    else denied.push('create');

    if (caps.canUpdateObjects !== false) allowed.push('update');
    else denied.push('update');

    if (caps.canDeleteObjects !== false) allowed.push('delete');
    else denied.push('delete');

    if (caps.canCreateRelationships !== false) allowed.push('relationships');
    else denied.push('relationships');

    let summary = `Allowed: ${allowed.join(', ') || 'none'}`;
    if (denied.length > 0) {
      summary += ` | Denied: ${denied.join(', ')}`;
    }

    if (caps.allowedObjectTypes && caps.allowedObjectTypes.length > 0) {
      summary += ` | Types: ${caps.allowedObjectTypes.join(', ')}`;
    }

    return summary;
  }
}
