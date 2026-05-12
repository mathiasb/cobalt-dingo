import { test, expect } from '@playwright/test';

// Requires server running with FORTNOX_MODE + LLM configured.
// Run: BASE_URL=http://localhost:8080 task e2e -- tests/chat.spec.ts

test.describe('chat', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/chat');
    const title = await page.title();
    if (!title.includes('Chat')) {
      test.skip(true, 'chat route not enabled (requires FORTNOX_MODE + LLM config)');
    }
  });

  test('banner shows mode and model', async ({ page }) => {
    // Mode banner contains "Mode:" label and a model name
    const banner = page.locator('#active-model');
    await expect(banner).toBeVisible();
    const model = await banner.textContent();
    expect(model?.length).toBeGreaterThan(0);
  });

  test('escalate button toggles model', async ({ page }) => {
    const escBtn = page.locator('#escalate-btn');
    // Skip if escalation not configured
    if (!(await escBtn.isVisible())) return;

    const modelSpan = page.locator('#active-model');
    const defaultModel = await modelSpan.textContent();

    await escBtn.click();
    const escalatedModel = await modelSpan.textContent();
    expect(escalatedModel).not.toBe(defaultModel);
    expect(await escBtn.textContent()).toBe('Default');

    // Toggle back
    await escBtn.click();
    expect(await modelSpan.textContent()).toBe(defaultModel);
    expect(await escBtn.textContent()).toBe('Escalate');
  });

  test('sending a message gets a response', async ({ page }) => {
    await page.fill('#chat-input', 'what is 1+1?');
    await page.click('#chat-send');

    // User bubble appears
    await expect(page.locator('.chat-bubble-user')).toContainText('what is 1+1?');

    // Thinking indicator appears then disappears
    await page.locator('.chat-thinking').waitFor({ state: 'visible', timeout: 5_000 });
    await page.locator('.chat-thinking').waitFor({ state: 'detached', timeout: 30_000 });

    // Assistant response bubble appears with content
    const bubble = page.locator('.chat-bubble-assistant').last();
    await bubble.waitFor({ state: 'visible', timeout: 30_000 });

    // Poll until content stabilises (stream done)
    let prev = '';
    let stable = 0;
    while (stable < 3) {
      const cur = await bubble.textContent() ?? '';
      if (cur === prev && cur.length > 0) stable++;
      else { stable = 0; prev = cur; }
      await page.waitForTimeout(300);
    }
    expect(prev.length).toBeGreaterThan(0);
  });

  test('send disabled during streaming', async ({ page }) => {
    await page.fill('#chat-input', 'hello');
    await page.click('#chat-send');

    // Button should be disabled while thinking
    await page.locator('.chat-thinking').waitFor({ state: 'visible', timeout: 5_000 });
    await expect(page.locator('#chat-send')).toBeDisabled();

    // Re-enabled after response
    await page.locator('.chat-thinking').waitFor({ state: 'detached', timeout: 30_000 });
    await expect(page.locator('#chat-send')).toBeEnabled({ timeout: 5_000 });
  });
});
