import { test, expect } from '@playwright/test';

test.describe('smoke', () => {
  test('healthz returns 200', async ({ request }) => {
    const res = await request.get('/healthz');
    expect(res.status()).toBe(200);
  });

  test('home page loads with nav', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveTitle(/cobalt-dingo/);
    await expect(page.locator('.cd-nav-brand')).toContainText('cobalt-dingo');
    await expect(page.locator('a[href="/invoices"]')).toBeVisible();
    await expect(page.locator('a[href="/chat"]')).toBeVisible();
  });

  test('invoices page loads with table', async ({ page }) => {
    await page.goto('/invoices');
    await expect(page).toHaveTitle(/cobalt-dingo/);
    await expect(page.locator('.cd-nav')).toBeVisible();
    await expect(page.locator('.cd-main')).toBeVisible();
    // When Fortnox is configured, invoice rows appear
    const rows = page.locator('table tbody tr, [data-invoice]');
    const hasRows = await rows.count() > 0;
    if (hasRows) {
      await expect(rows.first()).toBeVisible();
    }
  });

  test('chat page loads with input and send button', async ({ page }) => {
    await page.goto('/chat');
    // chat route only registered when LLM + Fortnox enabled.
    // GET / is a catch-all, so unregistered /chat still returns 200 (invoices page).
    // Detect by title: chat page title contains "Chat".
    const title = await page.title();
    if (!title.includes('Chat')) {
      test.skip(true, 'chat route not enabled (requires FORTNOX_MODE + LLM config)');
      return;
    }
    await expect(page.locator('#chat-input')).toBeVisible();
    await expect(page.locator('#chat-send')).toBeVisible();
    await expect(page.locator('#chat-thread')).toBeVisible();
  });
});
