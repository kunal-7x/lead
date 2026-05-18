import { expect, test } from '@playwright/test';

test.describe('AI Quality', () => {
  test('review queue shows evidence, correction controls, and promotion gate', async ({ page }) => {
    test.setTimeout(120_000);
    const response = await page.goto('/ai-quality');
    expect(response?.status()).not.toBe(500);

    await expect(page.getByRole('heading', { name: 'AI Quality' })).toBeVisible();
    await expect(page.getByText('Human Review Queue')).toBeVisible();
    await expect(page.getByText('review-001')).toBeVisible();
    await expect(page.getByText('KB chunks shown to LLM')).toBeVisible();
    await expect(page.getByText('claim_rewritten')).toBeVisible();
    await expect(page.getByLabel('Correct next action')).toHaveValue('handover');
    await expect(page.getByRole('table', { name: 'Prompt test runs' })).toBeVisible();
    await expect(
      page.getByRole('cell', { name: 'prompt_regression_5pct', exact: true }),
    ).toBeVisible();
  });
});
