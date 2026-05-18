import { expect, test } from '@playwright/test';

test.describe('WhatsApp messaging', () => {
  test('inbox shows threads and conversation pane', async ({ page }) => {
    await page.route('**/api/v1/whatsapp/threads?**', async (route) => {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          threads: [
            {
              id: 'wa-thread-demo-visit',
              tenant_id: 'tenant-demo',
              lead_id: 'lead-demo-002',
              phone: '+919988776655',
              service_window_until: '2026-05-19T06:00:00Z',
              updated_at: '2026-05-18T05:56:00Z',
            },
          ],
        }),
      });
    });

    await page.route('**/api/v1/whatsapp/threads/wa-thread-demo-visit/messages', async (route) => {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          messages: [
            {
              id: 'wa-msg-demo-visit-1',
              thread_id: 'wa-thread-demo-visit',
              direction: 'inbound',
              body: 'Can I visit on Saturday?',
              status: 'received',
              created_at: '2026-05-18T05:56:00Z',
            },
          ],
        }),
      });
    });

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
