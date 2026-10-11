import { Page, expect } from '@playwright/test';

export interface DriveSource {
  id: string;
  name: string;
}

/**
 * Fill and submit the connect form, returning the new source's id and name.
 * The gateway validates `folder_id` + `refresh_token`, creates the source via
 * the mock, and redirects to its detail page.
 */
export async function createDriveSource(
  page: Page,
  opts: { name: string; folder: string; token?: string },
): Promise<DriveSource> {
  await page.goto('/data-sources/new');
  await expect(page.getByTestId('ds-connect-form')).toBeVisible();
  await page.getByTestId('ds-field-name').fill(opts.name);
  await page.getByTestId('ds-field-folder_id').fill(opts.folder);
  await page.getByTestId('ds-field-refresh_token').fill(opts.token ?? 'e2e-refresh-token');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.waitForURL(/\/data-sources\/ds-\d+/);
  const id = page.url().match(/\/data-sources\/(ds-\d+)/)?.[1] ?? '';
  expect(id, 'connect should redirect to the new source detail page').not.toBe('');
  return { id, name: opts.name };
}

/**
 * Delete a source through the gateway form endpoint. Cleanup is best-effort so
 * a failed assertion never masks the original failure.
 */
export async function deleteDriveSource(page: Page, id: string): Promise<void> {
  if (!id) return;
  try {
    await page.request.post(`/data-sources/${id}/delete`);
  } catch {
    /* cleanup is best-effort */
  }
}
