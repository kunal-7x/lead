import { expect, test } from '@playwright/test';

test.describe('Site visits', () => {
  test('calendar renders high-volume week view', async ({ page }) => {
    test.setTimeout(120_000);
    const response = await page.goto('/site-visits');
    expect(response?.status()).not.toBe(500);
    await expect(page.getByRole('heading', { name: 'Site Visits' })).toBeVisible();
    await expect(page.getByText('May 18 - May 24, 2026')).toBeVisible();
    await expect(page.getByText('Asha Mehta').first()).toBeVisible();
    await expect(page.getByRole('link', { name: /no-shows/i })).toBeVisible();
  });

  test('detail and no-show worklist render recovery controls', async ({ page }) => {
    test.setTimeout(120_000);
    await page.goto('/site-visits/visit-11', { waitUntil: 'domcontentloaded' });
    await expect(page.getByRole('heading', { name: 'Site Visit Detail' })).toBeVisible();
    await expect(page.getByRole('button', { name: /reschedule/i })).toBeVisible();

    await page.goto('/site-visits/no-shows', { waitUntil: 'domcontentloaded' });
    await expect(page.getByRole('heading', { name: 'No-show Recovery' })).toBeVisible();
    await expect(page.getByRole('button', { name: /call/i }).first()).toBeVisible();
  });
});
