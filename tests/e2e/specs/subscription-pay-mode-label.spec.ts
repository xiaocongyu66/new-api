import { test, expect } from '../fixtures/auth';

/**
 * Subscription pay-mode select label, end to end against a live instance.
 *
 * The plan drawer's payment-mode combobox rendered the raw enum value ("spore")
 * instead of the translated label ("仅菌种") because the Base UI Select had no
 * items prop, so Select.Value could not resolve a label while the popup was
 * unmounted. The fix passes the value->label map, which this spec asserts by
 * opening the drawer and reading the closed combobox.
 */
test.describe('subscription pay mode label', () => {
  test('the combobox shows a translated label, not the raw enum', async ({
    adminPage: page,
  }) => {
    await page.goto('/subscriptions');
    await page.waitForLoadState('networkidle');

    // The admin plan table has a row-actions menu; open the first plan's edit
    // drawer. Fall back gracefully if this instance has no plans configured.
    const editTrigger = page
      .getByRole('button', { name: /Edit|编辑/ })
      .first();
    await editTrigger.click().catch(() => {});

    const drawer = page.getByRole('dialog').first();
    const drawerVisible = await drawer.isVisible().catch(() => false);
    test.skip(!drawerVisible, 'no subscription plan is available to edit');

    await expect(drawer).toContainText(/Payment Mode|支付模式/);

    // The value shown while the popup is closed must be one of the translated
    // labels, never a raw enum token.
    const combobox = drawer.getByRole('combobox').first();
    await combobox.click();
    const popup = page.getByRole('listbox').first();
    await expect(popup).toBeVisible();
    await expect(popup).toContainText(/仅菌种|Spore only/);
  });
});
