import { test as base, Page, expect } from '@playwright/test';

type AuthFixtures = {
  adminPage: Page;
};

export const test = base.extend<AuthFixtures>({
  adminPage: async ({ page }, use) => {
    // Navigate to sign-in page
    await page.goto('/sign-in');
    await page.waitForLoadState('networkidle');

    const userField = page.locator('input[placeholder*="username" i], input[placeholder*="用户名" i], input[name="username"], input:not([type="password"])').first();
    const passField = page.locator('input[type="password"]').first();
    const submitBtn = page.locator('button[type="submit"]').first();

    await userField.fill('nailaoadmin');
    await passField.fill('NailaoAdmin123!');
    await submitBtn.click();

    // Wait for redirect to dashboard
    await page.waitForURL((url) => !url.pathname.includes('/sign-in'), { timeout: 15000 });
    await page.waitForLoadState('networkidle');

    // Switch to Chinese locale if not already
    const langBtn = page.getByRole('button', { name: /Change language|更改语言/i }).first();
    if (await langBtn.isVisible()) {
      await langBtn.click();
      await page.waitForTimeout(300);
      const zhOption = page.locator('[role="menuitem"]:has-text("简体中文"), button:has-text("简体中文")').first();
      if (await zhOption.isVisible()) {
        await zhOption.click();
        await page.waitForTimeout(600);
      }
    }

    await use(page);
  },
});

export { expect };
