import { expect, test } from '@playwright/test';

test.describe('Reports', () => {
  test('reports module renders filters and drilldown rows', async ({ page }) => {
    test.setTimeout(120_000);
    const response = await page.goto('/reports');
    expect(response?.status()).not.toBe(500);
    await expect(page.getByRole('heading', { name: 'Reports' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'campaign' })).toBeVisible();
    await expect(page.getByText('tenant scoped - freshness 8s')).toBeVisible();
    await expect(page.getByText('Skyline Residency')).toBeVisible();
  });
});
