import { test, expect } from '@playwright/test';
import { createWebSource, deleteDriveSource } from '../../helpers/datasources';

// Sync trigger + job progress for the Web provider (gateway /data-sources/:id +
// /sync). The mock keys the job outcome off the start URL list: a URL containing
// `slow` stays processing (progress + cancel), `fail` fails, anything else
// completes.

test.describe('Web data source sync', () => {
  test('triggers a sync, shows an in-progress job, and cancels it', async ({ page }) => {
    const src = await createWebSource(page, {
      name: `e2e-web-${Date.now()}`,
      startUrls: ['https://slow.example.com/docs'],
    });
    try {
      await page.getByTestId('ds-trigger-sync').click();
      await page.waitForURL(/synced=1/);

      const job = page.getByTestId('ds-sync-job-row');
      await expect(job).toBeVisible();
      await expect(job.getByText(/processing/i)).toBeVisible();
      await expect(job.getByTestId('ds-sync-counts')).toContainText('3 / 10 processed');
      await expect(job.getByTestId('ds-sync-phase')).toContainText('Crawling');

      await expect(job.getByTestId('ds-cancel-sync')).toBeVisible();
      await job.getByTestId('ds-cancel-sync').click();
      await page.waitForURL(/cancelled=1/);

      const cancelled = page.getByTestId('ds-sync-job-row');
      await expect(cancelled.getByText('cancelled', { exact: true })).toBeVisible();
      await expect(cancelled.getByTestId('ds-cancel-sync')).toHaveCount(0);
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });

  test('surfaces a failed sync with the error message', async ({ page }) => {
    const src = await createWebSource(page, {
      name: `e2e-web-${Date.now()}`,
      startUrls: ['https://fail.example.com/docs'],
    });
    try {
      await page.getByTestId('ds-trigger-sync').click();
      await page.waitForURL(/synced=1/);

      const job = page.getByTestId('ds-sync-job-row');
      await expect(job.getByText('failed', { exact: true })).toBeVisible();
      await expect(job).toContainText('Start URL is not reachable.');
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });

  test('completes a sync with counts and then reports last-synced in the list', async ({ page }) => {
    const src = await createWebSource(page, {
      name: `e2e-web-${Date.now()}`,
      startUrls: ['https://docs.example.com'],
    });
    try {
      await page.getByTestId('ds-trigger-sync').click();
      await page.waitForURL(/synced=1/);

      const job = page.getByTestId('ds-sync-job-row');
      await expect(job.getByText(/completed/i)).toBeVisible();
      await expect(job.getByTestId('ds-sync-counts')).toContainText('3 / 3 processed');

      await page.goto('/data-sources');
      const row = page.getByTestId('ds-source-row').filter({ hasText: src.name });
      await expect(row.getByText(/Last synced/)).toBeVisible();
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });

  test('tests an existing source connection', async ({ page }) => {
    const src = await createWebSource(page, {
      name: `e2e-web-${Date.now()}`,
      startUrls: ['https://docs.example.com'],
    });
    try {
      await page.getByTestId('ds-test-connection').click();
      await expect(page.getByTestId('ds-test-result')).toContainText('Connection OK.');
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });
});
