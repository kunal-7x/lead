import { expect, test } from '@playwright/test';

test.describe('WhatsApp messaging', () => {
  test('inbox shows threads and conversation pane', async ({ page }) => {
    const response = await page.goto('/messages');
    expect(response?.status()).not.toBe(500);
    await expect(page.getByRole('heading', { name: 'WhatsApp Inbox' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Rohan Shah' })).toBeVisible();
    await expect(page.getByLabel('Quick reply')).toBeVisible();
    await expect(page.getByRole('link', { name: /templates/i })).toBeVisible();
  });

  test('template builder exposes Meta sync controls', async ({ page }) => {
    const response = await page.goto('/messages/templates');
    expect(response?.status()).not.toBe(500);
    await expect(page.getByRole('heading', { name: 'WhatsApp Templates' })).toBeVisible();
    await expect(page.getByLabel('Template name')).toBeVisible();
    await expect(page.getByLabel('Category')).toBeVisible();
    await expect(page.getByRole('button', { name: /sync meta/i })).toBeVisible();
  });
});
