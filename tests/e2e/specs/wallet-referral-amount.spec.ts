import { test, expect } from '../fixtures/auth';

/**
 * Referral card amount alignment, end to end against a live instance.
 *
 * The wallet referral card used to fall through to a generic description in
 * quota mode (showing no number at all) and never showed the invitee reward,
 * because /api/status did not emit invitee_reward_display. Now the card reads
 * both rewards from /api/status and renders them in the configured currency.
 *
 * Requires a root account; BASE_URL points at the web dev server proxying to
 * its API. Env overrides match the shared fixture.
 */
test.describe('wallet referral amounts', () => {
  test('the referral card shows the inviter and invitee rewards', async ({
    adminPage: page,
  }) => {
    // /api/status is the single source for both reward numbers; read it first
    // so the assertion is anchored on what the backend actually emits rather
    // than a hardcoded figure.
    const status = await page.request.get('/api/status');
    expect(status.ok()).toBeTruthy();
    const statusJson = await status.json();
    const data = statusJson.data ?? {};

    const inviterDisplay = Number(data.inviter_reward_display ?? 0);
    const inviteeDisplay = Number(data.invitee_reward_display ?? 0);
    const currency = data.inviter_reward_currency ?? 'quota';
    const sporeReward = Number(data.spore_inviter_reward ?? 0);

    // The card only shows a number line when at least one reward is configured;
    // a disabled referral program keeps the generic description.
    test.skip(
      inviterDisplay <= 0 && inviteeDisplay <= 0 && sporeReward <= 0,
      'no invite reward is configured on this instance'
    );

    await page.goto('/wallet');
    await page.waitForLoadState('networkidle');

    const card = page
      .locator('div')
      .filter({ hasText: /Referral Program|推荐计划/ })
      .first();
    await expect(card).toBeVisible();

    // The invitee reward is quota-only; whenever it is configured the card must
    // surface it, which is the field the backend used to omit entirely.
    if (inviteeDisplay > 0) {
      await expect(card).toContainText(String(inviteeDisplay));
    }

    // In a spore-paying mode the inviter reward must show the voucher amount,
    // not the quota figure the old description silently dropped.
    if (currency === 'spore' || currency === 'both') {
      if (sporeReward > 0) {
        await expect(card).toContainText(String(sporeReward));
      }
    } else if (inviterDisplay > 0) {
      await expect(card).toContainText(String(inviterDisplay));
    }
  });
});
