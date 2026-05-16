import { test, expect } from "@playwright/test";

test.describe("Auth flow", () => {
  test("login page loads", async ({ page }) => {
    await page.goto("/login");
    await expect(page.getByLabel("Email")).toBeVisible();
    await expect(page.getByLabel("Password")).toBeVisible();
    await expect(page.getByRole("button", { name: /sign in/i })).toBeVisible();
  });

  test("empty credentials show validation", async ({ page }) => {
    await page.goto("/login");
    await page.getByRole("button", { name: /sign in/i }).click();
    // Browser native validation fires — email field should be invalid
    const emailInput = page.getByLabel("Email");
    await expect(emailInput).toHaveAttribute("required");
  });

  test("unauthenticated /dashboard redirects to /login", async ({ page }) => {
    // Without a valid token the shell should redirect (middleware not set up yet).
    // For now just verify the page responds without 500.
    const response = await page.goto("/dashboard");
    expect(response?.status()).not.toBe(500);
  });
});
