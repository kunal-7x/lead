import { expect, test } from '@playwright/test';

test('model switcher exposes super admin controls', async ({ page }) => {
  await page.goto('/admin/models');

  await expect(page.getByRole('heading', { name: 'Model Switcher' })).toBeVisible();
  await expect(page.getByLabel('Global LLM')).toHaveValue('groq_llama');
  await expect(page.getByLabel('Global STT')).toHaveValue('sarvam');
  await expect(page.getByLabel('Global TTS')).toHaveValue('sarvam_bulbul');
  await expect(page.getByRole('table', { name: 'Tenant model overrides' })).toBeVisible();
  await expect(page.getByText('GPU endpoint missing').first()).toBeVisible();
});

test('internal admin modules render operational controls', async ({ page }) => {
  await page.goto('/admin');
  await expect(page.getByRole('heading', { name: 'Internal Admin' })).toBeVisible();

  await page.goto('/tenants');
  await expect(page.getByRole('table', { name: 'Tenant operations' })).toBeVisible();
  await expect(page.getByRole('button', { name: /Impersonate/ }).first()).toBeVisible();

  await page.goto('/providers');
  await expect(page.getByRole('table', { name: 'Provider health table' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Emergency controls' })).toBeVisible();

  await page.goto('/jobs');
  await expect(page.getByRole('table', { name: 'Failed jobs table' })).toBeVisible();

  await page.goto('/support');
  await expect(page.getByLabel('Lead phone')).toHaveValue('+919876543210');
  await expect(page.getByText('site_visit.booked')).toBeVisible();

  await page.goto('/audit');
  await expect(page.getByRole('table', { name: 'Audit log table' })).toBeVisible();

  await page.goto('/flags');
  await expect(page.getByRole('heading', { name: 'Feature Flags' })).toBeVisible();

  await page.goto('/quality');
  await expect(page.getByRole('heading', { name: 'AI Quality Queue' })).toBeVisible();
});
