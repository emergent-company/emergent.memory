import { DataSource, Repository } from 'typeorm';
import { z } from 'zod';
import { GraphObject } from '../../../entities/graph-object.entity';
import { AgentCapabilities } from '../../../entities/agent.entity';

// Lazy-loaded langchain tool function
// Use 'any' type to avoid TS2589 "Type instantiation is excessively deep" errors
// caused by @langchain/core/tools complex generic types
// eslint-disable-next-line @typescript-eslint/no-explicit-any
let toolFn: any = null;

async function loadToolFunction(): Promise<any> {
  if (toolFn) return toolFn;
  const { tool } = await import('@langchain/core/tools');
  toolFn = tool;
  return toolFn;
}

/**
 * Actor context for tracking who made changes
 */
interface ActorContext {
  actorType: 'agent' | 'user' | 'system';
  actorId?: string;
  source?: string;
}

/**
 * Options for creating reaction agent tools
 */
interface CreateToolsOptions {
  dataSource: DataSource;
  graphObjectRepo: Repository<GraphObject>;
  capabilities: AgentCapabilities;
  actorContext: ActorContext;
  projectId: string;
}

/**
 * Create LangChain tools for the reaction agent based on capabilities
 * Note: This function is now async due to lazy loading of @langchain/core/tools
 */
export async function createReactionAgentTools(options: CreateToolsOptions) {
  const { dataSource, graphObjectRepo, capabilities, actorContext, projectId } =
    options;

  // Load the tool function (lazy-loaded to avoid blocking server startup)
  const tool = await loadToolFunction();

  const tools: any[] = [];

  // Always available: read-only tools
  tools.push(createSearchObjectsTool(tool, dataSource, projectId));
  tools.push(createGetObjectTool(tool, graphObjectRepo, projectId));
  tools.push(createGetRelatedObjectsTool(tool, dataSource, projectId));

  // Capability-gated write tools
  if (capabilities.canCreateObjects) {
    tools.push(
      createCreateObjectTool(tool, dataSource, actorContext, projectId)
    );
  }

  if (capabilities.canUpdateObjects) {
    tools.push(
      createUpdateObjectTool(tool, dataSource, actorContext, projectId)
    );
  }

  if (capabilities.canDeleteObjects) {
    tools.push(
      createDeleteObjectTool(tool, dataSource, actorContext, projectId)
    );
  }

  if (capabilities.canCreateRelationships) {
    tools.push(
      createCreateRelationshipTool(tool, dataSource, actorContext, projectId)
    );
  }

  return tools;
}

/**
 * Search for graph objects by type and/or key pattern
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function createSearchObjectsTool(
  tool: any,
  dataSource: DataSource,
  projectId: string
) {
  return tool(
    async ({
      type,
      keyPattern,
      limit,
    }: {
      type?: string;
      keyPattern?: string;
      limit?: number;
    }) => {
      const maxLimit = Math.min(limit || 20, 100);

      let sql = `
        WITH heads AS (
          SELECT DISTINCT ON (canonical_id) *
          FROM kb.graph_objects
          WHERE project_id = $1
          ORDER BY canonical_id, version DESC
        )
        SELECT id, canonical_id, type, key, properties, version, created_at, updated_at
        FROM heads
        WHERE deleted_at IS NULL
      `;

      const params: any[] = [projectId];
      let paramIndex = 2;

      if (type) {
        sql += ` AND type = $${paramIndex}`;
        params.push(type);
        paramIndex++;
      }

      if (keyPattern) {
        sql += ` AND key ILIKE $${paramIndex}`;
        params.push(`%${keyPattern}%`);
        paramIndex++;
      }

      sql += ` ORDER BY updated_at DESC LIMIT $${paramIndex}`;
      params.push(maxLimit);

      const result = await dataSource.query(sql, params);

      return JSON.stringify({
        count: result.length,
        objects: result.map((r: any) => ({
          id: r.id,
          canonicalId: r.canonical_id,
          type: r.type,
          key: r.key,
          properties: r.properties,
          version: r.version,
        })),
      });
    },
    {
      name: 'search_objects',
      description:
        'Search for graph objects in the knowledge base. You can filter by type and/or key pattern. Returns up to 100 objects.',
      schema: z.object({
        type: z
          .string()
          .optional()
          .describe('Object type to filter by (e.g., "Person", "Company")'),
        keyPattern: z
          .string()
          .optional()
          .describe('Partial key to search for (case-insensitive)'),
        limit: z
          .number()
          .optional()
          .default(20)
          .describe('Maximum number of results to return (max 100)'),
      }),
    }
  );
}

/**
 * Get a specific graph object by ID
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function createGetObjectTool(
  tool: any,
  graphObjectRepo: Repository<GraphObject>,
  projectId: string
) {
  return tool(
    async ({ objectId }: { objectId: string }) => {
      const object = await graphObjectRepo.findOne({
        where: { id: objectId, projectId },
      });

      if (!object) {
        return JSON.stringify({ error: `Object not found: ${objectId}` });
      }

      return JSON.stringify({
        id: object.id,
        canonicalId: object.canonicalId,
        type: object.type,
        key: object.key,
        properties: object.properties,
        version: object.version,
        createdAt: object.createdAt,
        updatedAt: object.updatedAt,
        deletedAt: object.deletedAt,
      });
    },
    {
      name: 'get_object',
      description:
        'Get detailed information about a specific graph object by its ID.',
      schema: z.object({
        objectId: z
          .string()
          .uuid()
          .describe('The UUID of the object to retrieve'),
      }),
    }
  );
}

/**
 * Get objects related to a specific object
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function createGetRelatedObjectsTool(
  tool: any,
  dataSource: DataSource,
  projectId: string
) {
  return tool(
    async ({
      objectId,
      relationshipType,
      direction,
      limit,
    }: {
      objectId: string;
      relationshipType?: string;
      direction?: 'outgoing' | 'incoming' | 'both';
      limit?: number;
    }) => {
      const maxLimit = Math.min(limit || 20, 100);
      const dir = direction || 'both';

      // Query for relationships and related objects
      let sql = '';
      const params: any[] = [projectId, objectId, maxLimit];

      if (dir === 'outgoing' || dir === 'both') {
        sql += `
          SELECT 
            r.id as relationship_id,
            r.type as relationship_type,
            r.source_id,
            r.target_id,
            'outgoing' as direction,
            o.id, o.canonical_id, o.type as object_type, o.key, o.properties
          FROM kb.graph_relationships r
          JOIN kb.graph_objects o ON o.canonical_id = r.target_id
          WHERE r.project_id = $1
            AND r.source_id = (SELECT canonical_id FROM kb.graph_objects WHERE id = $2 AND project_id = $1)
            AND r.deleted_at IS NULL
            AND o.deleted_at IS NULL
            AND o.version = (SELECT MAX(version) FROM kb.graph_objects WHERE canonical_id = o.canonical_id)
        `;

        if (relationshipType) {
          sql += ` AND r.type = $4`;
          params.push(relationshipType);
        }
      }

      if (dir === 'both') {
        sql += ' UNION ALL ';
      }

      if (dir === 'incoming' || dir === 'both') {
        const typeParamIdx = relationshipType ? (dir === 'both' ? 4 : 4) : null;

        sql += `
          SELECT 
            r.id as relationship_id,
            r.type as relationship_type,
            r.source_id,
            r.target_id,
            'incoming' as direction,
            o.id, o.canonical_id, o.type as object_type, o.key, o.properties
          FROM kb.graph_relationships r
          JOIN kb.graph_objects o ON o.canonical_id = r.source_id
          WHERE r.project_id = $1
            AND r.target_id = (SELECT canonical_id FROM kb.graph_objects WHERE id = $2 AND project_id = $1)
            AND r.deleted_at IS NULL
            AND o.deleted_at IS NULL
            AND o.version = (SELECT MAX(version) FROM kb.graph_objects WHERE canonical_id = o.canonical_id)
        `;

        if (relationshipType && typeParamIdx) {
          sql += ` AND r.type = $${typeParamIdx}`;
        }
      }

      sql += ` LIMIT $3`;

      const result = await dataSource.query(sql, params);

      return JSON.stringify({
        count: result.length,
        relationships: result.map((r: any) => ({
          relationshipId: r.relationship_id,
          relationshipType: r.relationship_type,
          direction: r.direction,
          relatedObject: {
            id: r.id,
            canonicalId: r.canonical_id,
            type: r.object_type,
            key: r.key,
            properties: r.properties,
          },
        })),
      });
    },
    {
      name: 'get_related_objects',
      description:
        'Get objects that are related to a specific object through relationships. You can filter by relationship type and direction.',
      schema: z.object({
        objectId: z
          .string()
          .uuid()
          .describe('The UUID of the object to find relationships for'),
        relationshipType: z
          .string()
          .optional()
          .describe('Filter by relationship type (e.g., "WORKS_AT", "KNOWS")'),
        direction: z
          .enum(['outgoing', 'incoming', 'both'])
          .optional()
          .default('both')
          .describe('Direction of relationships to include'),
        limit: z
          .number()
          .optional()
          .default(20)
          .describe('Maximum number of results to return (max 100)'),
      }),
    }
  );
}

/**
 * Create a new graph object
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function createCreateObjectTool(
  tool: any,
  dataSource: DataSource,
  actorContext: ActorContext,
  projectId: string
) {
  return tool(
    async ({
      type,
      key,
      properties,
    }: {
      type: string;
      key?: string;
      properties: Record<string, any>;
    }) => {
      const id = crypto.randomUUID();
      const canonicalId = crypto.randomUUID();

      const result = await dataSource.query(
        `
        INSERT INTO kb.graph_objects (
          id, canonical_id, project_id, type, key, properties, version,
          actor_type, actor_id, change_summary,
          created_at, updated_at
        ) VALUES (
          $1, $2, $3, $4, $5, $6, 1,
          $7, $8, $9,
          NOW(), NOW()
        )
        RETURNING id, canonical_id, type, key, properties, version
        `,
        [
          id,
          canonicalId,
          projectId,
          type,
          key || null,
          JSON.stringify(properties),
          actorContext.actorType,
          actorContext.actorId || null,
          JSON.stringify({
            source: actorContext.source,
            operation: 'create',
          }),
        ]
      );

      return JSON.stringify({
        success: true,
        object: {
          id: result[0].id,
          canonicalId: result[0].canonical_id,
          type: result[0].type,
          key: result[0].key,
          properties: result[0].properties,
          version: result[0].version,
        },
      });
    },
    {
      name: 'create_object',
      description:
        'Create a new graph object in the knowledge base. Requires canCreateObjects capability.',
      schema: z.object({
        type: z
          .string()
          .describe('The type of object to create (e.g., "Person", "Company")'),
        key: z
          .string()
          .optional()
          .describe('Optional unique key/name for the object'),
        properties: z
          .record(z.any())
          .describe('Properties of the object as key-value pairs'),
      }),
    }
  );
}

/**
 * Update an existing graph object
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function createUpdateObjectTool(
  tool: any,
  dataSource: DataSource,
  actorContext: ActorContext,
  projectId: string
) {
  return tool(
    async ({
      objectId,
      properties,
      key,
    }: {
      objectId: string;
      properties?: Record<string, any>;
      key?: string;
    }) => {
      // Get the current HEAD version
      const current = await dataSource.query(
        `
        SELECT id, canonical_id, type, key, properties, version
        FROM kb.graph_objects
        WHERE id = $1 AND project_id = $2 AND deleted_at IS NULL
        `,
        [objectId, projectId]
      );

      if (!current.length) {
        return JSON.stringify({
          success: false,
          error: `Object not found: ${objectId}`,
        });
      }

      const obj = current[0];
      const newVersion = obj.version + 1;
      const newId = crypto.randomUUID();

      // Merge properties
      const mergedProperties = {
        ...(obj.properties || {}),
        ...(properties || {}),
      };

      const newKey = key !== undefined ? key : obj.key;

      // Create new version
      await dataSource.query(
        `
        INSERT INTO kb.graph_objects (
          id, canonical_id, project_id, type, key, properties, version,
          actor_type, actor_id, change_summary,
          created_at, updated_at
        ) VALUES (
          $1, $2, $3, $4, $5, $6, $7,
          $8, $9, $10,
          NOW(), NOW()
        )
        `,
        [
          newId,
          obj.canonical_id,
          projectId,
          obj.type,
          newKey,
          JSON.stringify(mergedProperties),
          newVersion,
          actorContext.actorType,
          actorContext.actorId || null,
          JSON.stringify({
            source: actorContext.source,
            operation: 'update',
            previousVersion: obj.version,
          }),
        ]
      );

      return JSON.stringify({
        success: true,
        object: {
          id: newId,
          canonicalId: obj.canonical_id,
          type: obj.type,
          key: newKey,
          properties: mergedProperties,
          version: newVersion,
        },
      });
    },
    {
      name: 'update_object',
      description:
        'Update an existing graph object. Creates a new version with merged properties. Requires canUpdateObjects capability.',
      schema: z.object({
        objectId: z
          .string()
          .uuid()
          .describe('The UUID of the object to update'),
        properties: z
          .record(z.any())
          .optional()
          .describe('Properties to update/add (will be merged with existing)'),
        key: z.string().optional().describe('New key/name for the object'),
      }),
    }
  );
}

/**
 * Soft-delete a graph object
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function createDeleteObjectTool(
  tool: any,
  dataSource: DataSource,
  actorContext: ActorContext,
  projectId: string
) {
  return tool(
    async ({ objectId, reason }: { objectId: string; reason?: string }) => {
      // Verify object exists
      const current = await dataSource.query(
        `
        SELECT id, canonical_id, type, key, version
        FROM kb.graph_objects
        WHERE id = $1 AND project_id = $2 AND deleted_at IS NULL
        `,
        [objectId, projectId]
      );

      if (!current.length) {
        return JSON.stringify({
          success: false,
          error: `Object not found: ${objectId}`,
        });
      }

      const obj = current[0];

      // Soft delete by setting deleted_at
      await dataSource.query(
        `
        UPDATE kb.graph_objects
        SET deleted_at = NOW(),
            actor_type = $3,
            actor_id = $4,
            change_summary = $5
        WHERE canonical_id = $2 AND project_id = $1
        `,
        [
          projectId,
          obj.canonical_id,
          actorContext.actorType,
          actorContext.actorId || null,
          JSON.stringify({
            source: actorContext.source,
            operation: 'delete',
            reason: reason || 'Deleted by agent',
          }),
        ]
      );

      return JSON.stringify({
        success: true,
        deleted: {
          id: objectId,
          canonicalId: obj.canonical_id,
          type: obj.type,
          key: obj.key,
        },
      });
    },
    {
      name: 'delete_object',
      description:
        'Soft-delete a graph object (marks it as deleted, does not permanently remove). Requires canDeleteObjects capability.',
      schema: z.object({
        objectId: z
          .string()
          .uuid()
          .describe('The UUID of the object to delete'),
        reason: z.string().optional().describe('Reason for deletion'),
      }),
    }
  );
}

/**
 * Create a relationship between two objects
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function createCreateRelationshipTool(
  tool: any,
  dataSource: DataSource,
  actorContext: ActorContext,
  projectId: string
) {
  return tool(
    async ({
      sourceObjectId,
      targetObjectId,
      type,
      properties,
    }: {
      sourceObjectId: string;
      targetObjectId: string;
      type: string;
      properties?: Record<string, any>;
    }) => {
      // Get canonical IDs for source and target
      const objects = await dataSource.query(
        `
        SELECT id, canonical_id
        FROM kb.graph_objects
        WHERE id IN ($1, $2) AND project_id = $3 AND deleted_at IS NULL
        `,
        [sourceObjectId, targetObjectId, projectId]
      );

      if (objects.length !== 2) {
        return JSON.stringify({
          success: false,
          error: 'One or both objects not found',
        });
      }

      const sourceObj = objects.find((o: any) => o.id === sourceObjectId);
      const targetObj = objects.find((o: any) => o.id === targetObjectId);

      if (!sourceObj || !targetObj) {
        return JSON.stringify({
          success: false,
          error: 'One or both objects not found',
        });
      }

      const relationshipId = crypto.randomUUID();

      await dataSource.query(
        `
        INSERT INTO kb.graph_relationships (
          id, project_id, source_id, target_id, type, properties, version,
          created_at, updated_at
        ) VALUES (
          $1, $2, $3, $4, $5, $6, 1,
          NOW(), NOW()
        )
        `,
        [
          relationshipId,
          projectId,
          sourceObj.canonical_id,
          targetObj.canonical_id,
          type,
          JSON.stringify(properties || {}),
        ]
      );

      return JSON.stringify({
        success: true,
        relationship: {
          id: relationshipId,
          sourceId: sourceObj.canonical_id,
          targetId: targetObj.canonical_id,
          type,
          properties: properties || {},
        },
      });
    },
    {
      name: 'create_relationship',
      description:
        'Create a relationship between two graph objects. Requires canCreateRelationships capability.',
      schema: z.object({
        sourceObjectId: z
          .string()
          .uuid()
          .describe('The UUID of the source object'),
        targetObjectId: z
          .string()
          .uuid()
          .describe('The UUID of the target object'),
        type: z
          .string()
          .describe('The type of relationship (e.g., "WORKS_AT", "KNOWS")'),
        properties: z
          .record(z.any())
          .optional()
          .describe('Optional properties for the relationship'),
      }),
    }
  );
}
