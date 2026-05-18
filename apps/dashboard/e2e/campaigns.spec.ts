import { expect, test } from '@playwright/test';

test.describe('Campaigns', () => {
  test('campaign page renders controls and calls BFF campaign routes', async ({ page }) => {
    test.setTimeout(120_000);

    await page.route('**/api/v1/campaigns?**', async (route) => {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'campaign-demo-1',
            name: 'Demo 10-lead campaign',
            tenant_id: 'tenant-demo',
            project_id: 'project-skyline',
            kb_version_id: 'kb-demo-approved-1',
            script_version_id: 'script-demo-v1',
            prompt_version_id: 'prompt-demo-v1',
            status: 'draft',
          },
        ]),
      });
    });

    await page.route('**/api/v1/campaigns/campaign-demo-1/health', async (route) => {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          campaign_id: 'campaign-demo-1',
          connect_rate: 0.6,
          qualify_rate: 0.4,
          cost_burn_inr: 18.5,
          suppression_hit_rate: 0.1,
        }),
      });
    });

    const response = await page.goto('/campaigns');
    expect(response?.status()).not.toBe(500);
    await expect(page.getByRole('heading', { name: 'Campaigns' })).toBeVisible();
    await expect(page.getByText('Demo 10-lead campaign').first()).toBeVisible();
    await expect(page.getByRole('button', { name: 'Launch' })).toBeVisible();

    await page.getByRole('button', { name: 'Health' }).click();
    await expect(page.getByText('₹18.50')).toBeVisible();
  });
});
