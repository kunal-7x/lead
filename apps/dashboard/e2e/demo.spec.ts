import { expect, test } from '@playwright/test';

test.describe('Demo sandbox', () => {
  test('demo tenant watermark is visible on protected pages', async ({ page }) => {
    test.setTimeout(120_000);
    for (const path of ['/dashboard', '/ai-quality', '/reports']) {
      const response = await page.goto(path);
      expect(response?.status()).not.toBe(500);
      await expect(page.getByLabel('Demo tenant watermark')).toContainText('DEMO TENANT');
    }
  });
});
