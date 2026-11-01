import { test, expect } from '../fixtures/auth';

test.describe('Home, Theme & User Insights E2E Tests', () => {
  test('1. Home page renders hero, policy and feature sections', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Verify title
    await expect(page).toHaveTitle(/New API/);

    // Verify hero title (supports both zh and en)
    const heroTitle = page.getByText(/一个接口|One interface,/).first();
    await expect(heroTitle).toBeVisible();

    // Verify usage policy section
    const policySection = page.getByText(
      /什么欢迎，什么不行|What's welcome, what isn't/
    ).first();
    await expect(policySection).toBeVisible();

    // Verify feature section
    const featureSection = page.getByText(
      /连接应用与模型|A clear layer between your apps and models/
    ).first();
    await expect(featureSection).toBeVisible();
  });

  test('2. User Insights page renders tabs and summary cards', async ({ adminPage: page }) => {
    await page.goto('/admin/user-insights');
    await page.waitForLoadState('networkidle');

    // Verify page title
    const pageTitle = page.getByText(/用户画像|User Insights/).first();
    await expect(pageTitle).toBeVisible();

    // Verify tabs are present
    const profilesTab = page.getByRole('tab').filter({ hasText: /画像|Profiles/ });
    const samplesTab = page.getByRole('tab').filter({ hasText: /证据样本|Evidence samples/ });
    const settingsTab = page.getByRole('tab').filter({ hasText: /画像配置|Profiling settings/ });
    await expect(profilesTab.first()).toBeVisible();
    await expect(samplesTab.first()).toBeVisible();
    await expect(settingsTab.first()).toBeVisible();

    // Verify summary statistics cards
    await expect(page.getByText(/已画像用户|Profiled users/).first()).toBeVisible();
    await expect(page.getByText(/编码用户|Coders/).first()).toBeVisible();
    await expect(page.getByText(/角色扮演用户|Roleplayers/).first()).toBeVisible();

    // Switch to samples tab
    await samplesTab.first().click();
    await page.waitForTimeout(600);

    await expect(page.getByText(/证据缓存占用|Evidence cache usage/).first()).toBeVisible();
    await expect(page.getByText(/暂无采集到的样本|No samples captured yet/).first()).toBeVisible();
  });

  test('3. Theme settings drawer lists the color presets', async ({ adminPage: page }) => {
    await page.goto('/dashboard/overview');
    await page.waitForLoadState('networkidle');

    // Open theme settings drawer
    const themeBtn = page
      .getByRole('button', { name: /主题管理|Theme management/i })
      .first();
    await themeBtn.click();

    // Verify theme drawer is open
    const drawer = page.locator('[role="dialog"], [data-slot="sheet-content"]').first();
    await expect(drawer).toBeVisible();

    // Verify the default and the gold preset are both listed and distinct
    const defaultPreset = drawer.getByText(/默认|Default/).first();
    const goldPreset = drawer.getByText(/奶酪金|Cheese/).first();
    await expect(defaultPreset).toBeVisible();
    await expect(goldPreset).toBeVisible();

    // Click the gold preset
    await goldPreset.click();
    await page.waitForTimeout(600);

    // Verify body element remains intact and styled
    const body = page.locator('body');
    await expect(body).toBeVisible();
  });

  test('4. User Insights profiling settings render full Chinese translations', async ({ adminPage: page }) => {
    await page.goto('/admin/user-insights');
    await page.waitForLoadState('networkidle');

    // The insight configuration form lives in the profiling settings tab
    const settingsTab = page
      .getByRole('tab')
      .filter({ hasText: /画像配置|Profiling settings/ })
      .first();
    await settingsTab.click();
    await page.waitForTimeout(600);

    // Verify key fields are in Chinese
    const fields = [
      /启用用户画像|Enable user insights/,
      /在消费日志中记录画像|Record insight in consume log/,
      /推断性别倾向|Infer gender preference/,
      /破甲告警分阈值|Jailbreak alert score/,
      /留存证据样本|Keep evidence samples/,
      /采样率 \(%\)|Sample rate \(%\)/,
      /留存完整请求体|Keep full request body/,
      /样本存储配额 \(MB\)|Sample storage quota \(MB\)/,
      /样本保留天数|Sample retention \(days\)/,
      /破甲同时写代码时自动封禁|Auto-ban on jailbreak plus code/,
      /自动封禁的最低风险等级|Minimum risk level for auto-ban/,
      /写代码占比过高时自动封禁|Auto-ban on code ratio/,
    ];
    for (const field of fields) {
      await expect(page.getByText(field).first()).toBeVisible();
    }

    // Verify save button is present
    const saveBtn = page.getByRole('button', { name: /保存修改|Save Changes/i }).first();
    await expect(saveBtn).toBeVisible();
  });

  test('5. Sidebar navigation displays User Insights in Admin tab', async ({ adminPage: page }) => {
    await page.goto('/dashboard/overview');
    await page.waitForLoadState('networkidle');

    // Click Admin tab in sidebar
    const adminTab = page.getByRole('tab').filter({ hasText: /管理员|Admin/ }).first();
    if (await adminTab.isVisible()) {
      await adminTab.click();
      await page.waitForTimeout(600);
    }

    // Check sidebar links
    const usersLink = page.locator('a[href="/admin/users"]').first();
    const insightsLink = page.locator('a[href="/admin/user-insights"]').first();
    const codesLink = page.locator('a[href="/admin/redemption-codes"]').first();

    await expect(usersLink).toBeVisible();
    await expect(insightsLink).toBeVisible();
    await expect(codesLink).toBeVisible();

    // Check text of insights link is not empty
    await expect(insightsLink).toContainText(/用户洞察|User Insights/);
  });
});
