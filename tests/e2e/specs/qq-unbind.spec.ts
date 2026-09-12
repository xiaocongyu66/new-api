import { test, expect } from '../fixtures/auth';

/**
 * QQ unbind, end to end against a live instance.
 *
 * Requires a root account whose QQ is already bound and QQ check-in enabled:
 *   E2E_ADMIN_USERNAME / E2E_ADMIN_PASSWORD point at that account,
 *   BASE_URL points at the web dev server proxying to its API.
 *
 * These two paths are the ones that shipped without any way to undo a binding:
 * the profile card had no unbind control, and the admin binding dialog listed
 * no QQ row at all.
 */
test.describe('QQ unbind', () => {
  test('a user unbinds their own QQ from the profile bind-code card', async ({
    adminPage: page,
  }) => {
    await page.goto('/profile');
    await page.waitForLoadState('networkidle');

    const card = page
      .locator('div')
      .filter({ hasText: /QQ 绑定验证码|QQ bind code/ })
      .last();
    await expect(card.first()).toBeVisible();
    await expect(
      page.getByText(/已绑定 QQ 账号|Bound QQ account/).first()
    ).toBeVisible();

    const unbindButton = page
      .getByRole('button')
      .filter({ hasText: /^(解绑|Unbind)$/ })
      .first();
    await expect(unbindButton).toBeVisible();

    const unbindResponse = page.waitForResponse(
      (response) =>
        response.url().includes('/api/user/qq/bind') &&
        response.request().method() === 'DELETE'
    );
    await unbindButton.click();

    const confirmDialog = page.getByRole('alertdialog');
    await expect(confirmDialog).toBeVisible();
    await confirmDialog
      .getByRole('button')
      .filter({ hasText: /确认解绑|Confirm Unbind/ })
      .first()
      .click();

    expect((await unbindResponse).status()).toBe(200);

    // 解绑后卡片必须换回验证码生成入口，而不是停在已绑定面板上。
    await expect(
      page
        .getByRole('button')
        .filter({ hasText: /生成验证码|Generate code/ })
        .first()
    ).toBeVisible();
    await expect(
      page.getByText(/已绑定 QQ 账号|Bound QQ account/)
    ).toHaveCount(0);
  });

  test('an admin clears another user QQ binding from the binding dialog', async ({
    adminPage: page,
  }) => {
    await page.goto('/users');
    await page.waitForLoadState('networkidle');

    // 表格数据是 networkidle 之后才落地的，直接等这一行出现而不是等网络空闲。
    const targetRow = page.locator('tr', { hasText: 'qquser' }).first();
    await expect(targetRow).toBeVisible({ timeout: 20000 });
    await targetRow.getByRole('button').last().click();

    await page
      .getByRole('menuitem')
      .filter({ hasText: /管理绑定|Manage Bindings/ })
      .first()
      .click();

    const dialog = page.getByRole('dialog').first();
    await expect(dialog).toBeVisible();

    // QQ 行只有在 qq_open_id 真的回传时才会出现在"仅显示已绑定"视图里；
    // 这里同时断言它展示的是真实 open_id，而不是"未绑定"占位。
    const qqRow = dialog
      .locator('div.rounded-md.border')
      .filter({ hasText: 'DEMO_OPENID_QQUSER' })
      .first();
    await expect(qqRow).toBeVisible();

    const clearResponse = page.waitForResponse(
      (response) =>
        /\/api\/user\/\d+\/bindings\/qq$/.test(response.url()) &&
        response.request().method() === 'DELETE'
    );
    await qqRow.getByRole('button').last().click();

    const confirmDialog = page.getByRole('alertdialog');
    await expect(confirmDialog).toBeVisible();
    await confirmDialog
      .getByRole('button')
      .filter({ hasText: /确认解绑|Confirm Unbind/ })
      .first()
      .click();

    const response = await clearResponse;
    expect(response.status()).toBe(200);
    // 路径参数必须是 provider 名 qq；发列名会拿到 200 + success:false 的静默失败。
    expect(await response.json()).toMatchObject({ success: true });
  });
});
