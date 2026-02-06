import { beforeAll, afterAll, describe, it, expect } from 'vitest';
import { createE2EContext, E2EContext } from './e2e-context';
import request from 'supertest';
import { Pool } from 'pg';
import { getTestDbConfig } from '../test-db-config';

describe('Agents Batch Trigger E2E', () => {
  let ctx: E2EContext;
  let pool: Pool;
  let reactionAgentId: string;
  let scheduledAgentId: string;
  let disabledAgentId: string;
  let graphObjectIds: string[] = [];

  beforeAll(async () => {
    ctx = await createE2EContext();

    // Create a dedicated pool for test data setup
    const dbConfig = getTestDbConfig();
    pool = new Pool({
      host: dbConfig.host,
      port: dbConfig.port,
      user: dbConfig.user,
      password: dbConfig.password,
      database: dbConfig.database,
    });

    // Create test agents and graph objects
    await setupTestData();
  });

  afterAll(async () => {
    // Clean up test data
    await cleanupTestData();

    if (pool) {
      await pool.end();
    }
    if (ctx?.app) {
      await ctx.close();
    }
  });

  async function setupTestData() {
    // Create a reaction agent (enabled)
    // Note: cron_schedule and strategy_type are NOT NULL in the DB schema
    const reactionAgentResult = await pool.query<{ id: string }>(
      `INSERT INTO kb.agents(name, trigger_type, enabled, reaction_config, project_id, strategy_type, prompt, cron_schedule)
       VALUES($1, $2, $3, $4, $5, $6, $7, $8)
       RETURNING id`,
      [
        `E2E Reaction Agent ${Date.now()}`,
        'reaction',
        true,
        JSON.stringify({
          events: ['created', 'updated'],
          objectTypes: ['Person', 'Company'],
          concurrencyStrategy: 'skip',
          ignoreAgentTriggered: false,
          ignoreSelfTriggered: true,
        }),
        ctx.projectId,
        'reaction-handler',
        'Test prompt for reaction handler',
        '', // cron_schedule not used for reaction agents but required
      ]
    );
    reactionAgentId = reactionAgentResult.rows[0].id;

    // Create a scheduled agent (not reaction)
    const scheduledAgentResult = await pool.query<{ id: string }>(
      `INSERT INTO kb.agents(name, trigger_type, enabled, cron_schedule, project_id, strategy_type)
       VALUES($1, $2, $3, $4, $5, $6)
       RETURNING id`,
      [
        `E2E Scheduled Agent ${Date.now()}`,
        'schedule',
        true,
        '0 * * * *',
        ctx.projectId,
        'summarization', // Default strategy for scheduled agents
      ]
    );
    scheduledAgentId = scheduledAgentResult.rows[0].id;

    // Create a disabled reaction agent
    const disabledAgentResult = await pool.query<{ id: string }>(
      `INSERT INTO kb.agents(name, trigger_type, enabled, reaction_config, project_id, strategy_type, prompt, cron_schedule)
       VALUES($1, $2, $3, $4, $5, $6, $7, $8)
       RETURNING id`,
      [
        `E2E Disabled Agent ${Date.now()}`,
        'reaction',
        false, // Disabled
        JSON.stringify({
          events: ['created'],
          objectTypes: [],
          concurrencyStrategy: 'skip',
          ignoreAgentTriggered: false,
          ignoreSelfTriggered: true,
        }),
        ctx.projectId,
        'reaction-handler',
        'Test prompt for disabled agent',
        '', // cron_schedule not used for reaction agents but required
      ]
    );
    disabledAgentId = disabledAgentResult.rows[0].id;

    // Create test graph objects
    for (let i = 0; i < 5; i++) {
      const objectId = crypto.randomUUID();
      const canonicalId = crypto.randomUUID();
      const objectResult = await pool.query<{ id: string }>(
        `INSERT INTO kb.graph_objects(id, canonical_id, type, key, properties, project_id, version)
         VALUES($1, $2, $3, $4, $5, $6, $7)
         RETURNING id`,
        [
          objectId,
          canonicalId,
          i < 3 ? 'Person' : 'Company', // 3 Person, 2 Company
          `test-object-${Date.now()}-${i}`,
          JSON.stringify({ name: `Test Object ${i}` }),
          ctx.projectId,
          1,
        ]
      );
      graphObjectIds.push(objectResult.rows[0].id);
    }
  }

  async function cleanupTestData() {
    try {
      // Clean up processing logs
      if (reactionAgentId) {
        await pool.query(
          'DELETE FROM kb.agent_processing_log WHERE agent_id = $1',
          [reactionAgentId]
        );
      }
      if (disabledAgentId) {
        await pool.query(
          'DELETE FROM kb.agent_processing_log WHERE agent_id = $1',
          [disabledAgentId]
        );
      }

      // Clean up graph objects
      for (const id of graphObjectIds) {
        await pool.query('DELETE FROM kb.graph_objects WHERE id = $1', [id]);
      }

      // Clean up agents
      if (reactionAgentId) {
        await pool.query('DELETE FROM kb.agents WHERE id = $1', [
          reactionAgentId,
        ]);
      }
      if (scheduledAgentId) {
        await pool.query('DELETE FROM kb.agents WHERE id = $1', [
          scheduledAgentId,
        ]);
      }
      if (disabledAgentId) {
        await pool.query('DELETE FROM kb.agents WHERE id = $1', [
          disabledAgentId,
        ]);
      }
    } catch (error) {
      // Ignore cleanup errors
      console.error('Cleanup error:', error);
    }
  }

  describe('GET /admin/agents/:id/pending-events', () => {
    it('returns 401 when no auth token provided', async () => {
      const response = await request(ctx.app.getHttpServer())
        .get(`/admin/agents/${reactionAgentId}/pending-events`)
        .expect(401);

      expect(response.body.error.code).toBe('unauthorized');
    });

    it('returns 404 for non-existent agent', async () => {
      const fakeId = '00000000-0000-0000-0000-000000000000';
      const response = await request(ctx.app.getHttpServer())
        .get(`/admin/agents/${fakeId}/pending-events`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .expect(404);

      expect(response.body.error.code).toBe('not-found');
    });

    it('returns 400 for scheduled (non-reaction) agent', async () => {
      const response = await request(ctx.app.getHttpServer())
        .get(`/admin/agents/${scheduledAgentId}/pending-events`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .expect(400);

      expect(response.body.error.message).toContain('reaction');
    });

    it('returns pending events for reaction agent', async () => {
      const response = await request(ctx.app.getHttpServer())
        .get(`/admin/agents/${reactionAgentId}/pending-events`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .expect(200);

      expect(response.body.success).toBe(true);
      expect(response.body.data).toHaveProperty('totalCount');
      expect(response.body.data).toHaveProperty('objects');
      expect(response.body.data).toHaveProperty('reactionConfig');
      expect(Array.isArray(response.body.data.objects)).toBe(true);
      // Should include our test objects (5 total, all matching type filter)
      expect(response.body.data.totalCount).toBeGreaterThanOrEqual(5);
    });

    it('respects limit query parameter', async () => {
      const response = await request(ctx.app.getHttpServer())
        .get(`/admin/agents/${reactionAgentId}/pending-events?limit=2`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .expect(200);

      expect(response.body.success).toBe(true);
      expect(response.body.data.objects.length).toBeLessThanOrEqual(2);
    });

    it('returns correct object structure', async () => {
      const response = await request(ctx.app.getHttpServer())
        .get(`/admin/agents/${reactionAgentId}/pending-events?limit=1`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .expect(200);

      if (response.body.data.objects.length > 0) {
        const obj = response.body.data.objects[0];
        expect(obj).toHaveProperty('id');
        expect(obj).toHaveProperty('type');
        expect(obj).toHaveProperty('key');
        expect(obj).toHaveProperty('version');
        expect(obj).toHaveProperty('createdAt');
        expect(obj).toHaveProperty('updatedAt');
      }
    });

    it('returns reaction config in response', async () => {
      const response = await request(ctx.app.getHttpServer())
        .get(`/admin/agents/${reactionAgentId}/pending-events`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .expect(200);

      expect(response.body.data.reactionConfig).toHaveProperty('objectTypes');
      expect(response.body.data.reactionConfig).toHaveProperty('events');
      expect(response.body.data.reactionConfig.objectTypes).toContain('Person');
      expect(response.body.data.reactionConfig.objectTypes).toContain(
        'Company'
      );
      expect(response.body.data.reactionConfig.events).toContain('created');
    });
  });

  describe('POST /admin/agents/:id/batch-trigger', () => {
    it('returns 401 when no auth token provided', async () => {
      const response = await request(ctx.app.getHttpServer())
        .post(`/admin/agents/${reactionAgentId}/batch-trigger`)
        .send({ objectIds: [graphObjectIds[0]] })
        .expect(401);

      expect(response.body.error.code).toBe('unauthorized');
    });

    it('returns 404 for non-existent agent', async () => {
      const fakeId = '00000000-0000-0000-0000-000000000000';
      const response = await request(ctx.app.getHttpServer())
        .post(`/admin/agents/${fakeId}/batch-trigger`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .send({ objectIds: [graphObjectIds[0]] })
        .expect(404);

      expect(response.body.error.code).toBe('not-found');
    });

    it('returns 400 for scheduled (non-reaction) agent', async () => {
      const response = await request(ctx.app.getHttpServer())
        .post(`/admin/agents/${scheduledAgentId}/batch-trigger`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .send({ objectIds: [graphObjectIds[0]] })
        .expect(400);

      expect(response.body.error.message).toContain('reaction');
    });

    it('returns 400 for disabled agent', async () => {
      const response = await request(ctx.app.getHttpServer())
        .post(`/admin/agents/${disabledAgentId}/batch-trigger`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .send({ objectIds: [graphObjectIds[0]] })
        .expect(400);

      expect(response.body.error.message).toContain('disabled');
    });

    it('returns 400 when objectIds is empty', async () => {
      const response = await request(ctx.app.getHttpServer())
        .post(`/admin/agents/${reactionAgentId}/batch-trigger`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .send({ objectIds: [] })
        .expect(400);

      expect(response.body.error.message).toBeDefined();
    });

    it('returns 400 when objectIds contains invalid UUIDs', async () => {
      const response = await request(ctx.app.getHttpServer())
        .post(`/admin/agents/${reactionAgentId}/batch-trigger`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .send({ objectIds: ['not-a-uuid'] })
        .expect(400);

      expect(response.body.error.message).toBeDefined();
    });

    it('successfully triggers batch processing', async () => {
      const response = await request(ctx.app.getHttpServer())
        .post(`/admin/agents/${reactionAgentId}/batch-trigger`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .send({ objectIds: graphObjectIds.slice(0, 2) })
        .expect(200);

      expect(response.body.success).toBe(true);
      expect(response.body.data).toHaveProperty('queued');
      expect(response.body.data).toHaveProperty('skipped');
      expect(response.body.data).toHaveProperty('skippedDetails');
      expect(response.body.data.queued).toBeGreaterThanOrEqual(0);
      expect(Array.isArray(response.body.data.skippedDetails)).toBe(true);
    });

    it('skips objects not found', async () => {
      // Use crypto.randomUUID() to generate a valid v4 UUID that doesn't exist in the database
      const fakeObjectId = crypto.randomUUID();
      const response = await request(ctx.app.getHttpServer())
        .post(`/admin/agents/${reactionAgentId}/batch-trigger`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .send({ objectIds: [fakeObjectId] })
        .expect(200);

      expect(response.body.success).toBe(true);
      expect(response.body.data.queued).toBe(0);
      expect(response.body.data.skipped).toBe(1);
      expect(response.body.data.skippedDetails).toContainEqual(
        expect.objectContaining({
          objectId: fakeObjectId,
          reason: expect.stringContaining('not found'),
        })
      );
    });

    it('handles multiple objects with mixed results', async () => {
      // Use crypto.randomUUID() to generate a valid v4 UUID that doesn't exist in the database
      const fakeObjectId = crypto.randomUUID();
      const response = await request(ctx.app.getHttpServer())
        .post(`/admin/agents/${reactionAgentId}/batch-trigger`)
        .set('Authorization', 'Bearer e2e-all')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .send({ objectIds: [...graphObjectIds.slice(2, 4), fakeObjectId] })
        .expect(200);

      expect(response.body.success).toBe(true);
      // At least one should be skipped (the fake one)
      expect(response.body.data.skipped).toBeGreaterThanOrEqual(1);
    });
  });

  describe('Scopes', () => {
    it('requires admin:read scope for pending-events', async () => {
      const response = await request(ctx.app.getHttpServer())
        .get(`/admin/agents/${reactionAgentId}/pending-events`)
        .set('Authorization', 'Bearer no-scope')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .expect(403);

      expect(response.body.error.code).toBe('forbidden');
    });

    it('requires admin:write scope for batch-trigger', async () => {
      const response = await request(ctx.app.getHttpServer())
        .post(`/admin/agents/${reactionAgentId}/batch-trigger`)
        .set('Authorization', 'Bearer no-scope')
        .set('x-project-id', ctx.projectId)
        .set('x-org-id', ctx.orgId)
        .send({ objectIds: graphObjectIds.slice(0, 1) })
        .expect(403);

      expect(response.body.error.code).toBe('forbidden');
    });
  });
});
