import { Page, expect } from '@playwright/test';

export const workflowRepo = process.env.E2E_WORKFLOW_REPO ?? '';
export const workflowOrgId = process.env.E2E_WORKFLOW_ORG_ID ?? '';
export const workflowProjectId = process.env.E2E_WORKFLOW_PROJECT_ID ?? '';
export const workflowObjectId = process.env.E2E_WORKFLOW_BLOCK_OBJECT_ID ?? '';
export const workflowToken = process.env.E2E_WORKFLOW_GITHUB_TOKEN ?? '';

export function workflowConfigured(): boolean {
  return Boolean(workflowRepo && workflowOrgId && workflowProjectId && workflowToken);
}

export function workflowPath(subPath: string): string {
  return `/o/${workflowOrgId}/p/${workflowProjectId}${subPath}`;
}

export async function gotoWorkflow(page: Page, subPath: string): Promise<void> {
  await page.goto(workflowPath(subPath));
}

export async function github(method: string, path: string, body?: unknown): Promise<Response> {
  if (!workflowToken) throw new Error('E2E_WORKFLOW_GITHUB_TOKEN is not set');
  const res = await fetch(`https://api.github.com/repos/${workflowRepo}${path}`, {
    method,
    headers: {
      Accept: 'application/vnd.github+json',
      Authorization: `Bearer ${workflowToken}`,
      'X-GitHub-Api-Version': '2022-11-28',
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`GitHub ${method} ${path} -> ${res.status}: ${await res.text()}`);
  }
  return res;
}

export async function createExternalIssue(title: string, labels: string[] = []): Promise<{ number: number }> {
  const res = await github('POST', '/issues', { title, body: 'workflow e2e external mutation', labels });
  return (await res.json()) as { number: number };
}

export async function setIssueState(number: number, state: 'open' | 'closed'): Promise<void> {
  await github('PATCH', `/issues/${number}`, { state });
}

export async function expectCardTitle(page: Page, title: string, timeout = 30_000): Promise<void> {
  await page.reload();
  await expect(page.getByText(title, { exact: false })).toBeVisible({ timeout });
}
