import { Page } from '@playwright/test';
import { readBootstrap } from './bootstrap';
import { memoryAuthHeaders } from './objects';
import { MEMORY_API_URL } from './tokens';

/**
 * A board-enabled object type seeded through the memory API: the schema pack
 * (created via POST /api/schemas), its project assignment, and the type name.
 */
export interface BoardSchemaHandle {
  schemaId: string;
  assignmentId: string;
  type: string;
}

/** The six canonical board statuses (gateway/board.go boardStatusOrder). */
const BOARD_STATUSES = ['ready', 'in_progress', 'review', 'revision', 'blocked', 'done'];

/**
 * Create a board-enabled object type and assign it to the active project.
 *
 * The gateway exposes no create-UI for board-enabled object types, so this
 * seeds the pack and its assignment directly through the memory API using the
 * signed-in session token (helpers/objects.ts `memoryAuthHeaders`) plus the
 * bootstrap project id. `type` must be unique per run (callers pass a
 * `Date.now()`-stamped name) so the pack never collides with a prior run's rows.
 *
 * Returns the ids the cleanup path needs: the pack id, the assignment id, and
 * the type name. Throws on any non-2xx so a missing route/field fails loudly
 * rather than leaving a half-seeded board behind.
 */
export async function createBoardEnabledSchema(page: Page, type: string): Promise<BoardSchemaHandle> {
  const projectId = readBootstrap()?.projectId;
  if (!projectId) {
    throw new Error('createBoardEnabledSchema: bootstrap state has no projectId');
  }
  const headers = await memoryAuthHeaders(page);
  const stamp = Date.now();

  const packResp = await page.request.post(`${MEMORY_API_URL}/api/schemas`, {
    headers,
    data: {
      name: `e2e-board-${stamp}`,
      version: '1.0.0',
      object_type_schemas: [
        {
          name: type,
          label: 'E2E Board Task',
          boardEnabled: true,
          allowedStatuses: BOARD_STATUSES,
          skipEmbeddings: true,
          skipExtraction: true,
          excludeFromSearch: true,
          properties: {
            title: { type: 'string', required: true },
            description: { type: 'string' },
          },
        },
      ],
      relationship_type_schemas: [],
    },
    failOnStatusCode: false,
  });
  if (!packResp.ok()) {
    throw new Error(
      `createBoardEnabledSchema: POST /api/schemas failed (HTTP ${packResp.status()}): ${await packResp.text()}`,
    );
  }
  const pack = (await packResp.json()) as { id?: string };
  if (!pack.id) {
    throw new Error(
      `createBoardEnabledSchema: CreatePack response has no id: ${JSON.stringify(pack)}`,
    );
  }

  const assignResp = await page.request.post(
    `${MEMORY_API_URL}/api/schemas/projects/${encodeURIComponent(projectId)}/assign`,
    {
      headers,
      data: { schema_id: pack.id, merge: true },
      failOnStatusCode: false,
    },
  );
  if (!assignResp.ok()) {
    throw new Error(
      `createBoardEnabledSchema: POST /api/schemas/projects/${projectId}/assign failed ` +
        `(HTTP ${assignResp.status()}): ${await assignResp.text()}`,
    );
  }
  const assign = (await assignResp.json()) as { assignment_id?: string };
  if (!assign.assignment_id) {
    throw new Error(
      `createBoardEnabledSchema: AssignPack response has no assignment_id: ${JSON.stringify(assign)}`,
    );
  }

  return { schemaId: pack.id, assignmentId: assign.assignment_id, type };
}

/**
 * Best-effort cleanup for a board-enabled schema: delete the assignment, then
 * the pack. Never throws, so it is safe to call from `finally`/`afterAll`
 * without masking the spec's real failure.
 */
export async function cleanupBoardSchema(page: Page, handle: BoardSchemaHandle | null): Promise<void> {
  if (!handle) return;
  try {
    const projectId = readBootstrap()?.projectId;
    const headers = await memoryAuthHeaders(page);
    if (projectId && handle.assignmentId) {
      await page.request
        .delete(
          `${MEMORY_API_URL}/api/schemas/projects/${encodeURIComponent(projectId)}/assignments/${encodeURIComponent(handle.assignmentId)}`,
          { headers, failOnStatusCode: false },
        )
        .catch(() => undefined);
    }
    if (handle.schemaId) {
      await page.request
        .delete(`${MEMORY_API_URL}/api/schemas/${encodeURIComponent(handle.schemaId)}`, {
          headers,
          failOnStatusCode: false,
        })
        .catch(() => undefined);
    }
  } catch {
    // Best-effort only — never mask the spec's real failure.
  }
}
