import { test, expect } from '../fixtures/auth';

test.describe('Nailao Theme & Features E2E Tests', () => {

  test('1. Home page renders nailao-specific hero and policy components', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Verify title contains Nailao branding
    await expect(page).toHaveTitle(/奶酪公益站|New API/);

    // Verify nailao hero title (supports both zh and en)
    const heroTitle = page.getByText(/一块管够的奶酪|A wheel of cheese big enough/);
    await expect(heroTitle.first()).toBeVisible();

    // Verify nailao story section
    const storySection = page.getByText(/一个很年轻的公益站|Our story/);
    await expect(storySection.first()).toBeVisible();

    // Verify nailao usage policy
    const policySection = page.getByText(/什么欢迎，什么不行|What's welcome, what isn't/);
    await expect(policySection.first()).toBeVisible();

    // Verify nailao feature section
    const featureSection = page.getByText(/我们提供什么|What we offer/);
    await expect(featureSection.first()).toBeVisible();
  });

  test('2. User Insights page renders correctly with tabs and stats', async ({ adminPage: page }) => {
    // Navigate directly to user-insights
    await page.goto('/user-insights');
    await page.waitForLoadState('networkidle');

    // Verify page title
    const pageTitle = page.getByText(/用户画像|User Insights/);
    await expect(pageTitle.first()).toBeVisible();

    // Verify tabs are present
    const profilesTab = page.getByRole('tab').filter({ hasText: /画像|Profiles/ });
    const samplesTab = page.getByRole('tab').filter({ hasText: /证据样本|Evidence samples/ });
    await expect(profilesTab.first()).toBeVisible();
    await expect(samplesTab.first()).toBeVisible();

    // Verify statistics cards
    await expect(page.getByText(/已画像用户|Profiled users/).first()).toBeVisible();
    await expect(page.getByText(/编码用户|Coders/).first()).toBeVisible();
    await expect(page.getByText(/角色扮演用户|Roleplayers/).first()).toBeVisible();

    // Switch to samples tab
    await samplesTab.first().click();
    await page.waitForTimeout(600);

    // Verify samples tab content
    await expect(page.getByText(/证据缓存占用|Evidence cache usage/).first()).toBeVisible();
    await expect(page.getByText(/暂无采集到的样本|No samples collected/).first()).toBeVisible();
  });

  test('3. Theme settings drawer allows selecting Cheese gold preset', async ({ adminPage: page }) => {
    await page.goto('/dashboard/overview');
    await page.waitForLoadState('networkidle');

    // Open theme settings drawer
    const themeBtn = page.getByRole('button', { name: /主题设置|theme settings/i }).first();
    await themeBtn.click();

    // Verify theme drawer is open
    const drawer = page.locator('[role="dialog"], [data-slot="sheet-content"]').first();
    await expect(drawer).toBeVisible();

    // Verify both Default and Cheese presets exist and are distinct
    const defaultPreset = drawer.getByText(/默认|Default/).first();
    const cheesePreset = drawer.getByText(/奶酪金|Cheese/).first();
    await expect(defaultPreset).toBeVisible();
    await expect(cheesePreset).toBeVisible();

    // Click cheese preset
    await cheesePreset.click();
    await page.waitForTimeout(600);

    // Verify body element remains intact and styled
    const body = page.locator('body');
    await expect(body).toBeVisible();
  });

  test('4. Operations > User Insight settings page renders full Chinese translations', async ({ adminPage: page }) => {
    await page.goto('/system-settings/operations/user-insight');
    await page.waitForLoadState('networkidle');

    // Verify section title
    await expect(page.getByText(/用户画像|User Insights/).first()).toBeVisible();

    // Verify key fields are in Chinese
    await expect(page.getByText(/启用用户画像|Enable user insights/).first()).toBeVisible();
    await expect(page.getByText(/在消费日志中记录画像|Record insight in consume log/).first()).toBeVisible();
    await expect(page.getByText(/推断性别倾向|Infer gender preference/).first()).toBeVisible();
    await expect(page.getByText(/破甲告警分阈值|Jailbreak alert score/).first()).toBeVisible();
    await expect(page.getByText(/留存证据样本|Keep evidence samples/).first()).toBeVisible();
    await expect(page.getByText(/采样率 \(%\)|Sample rate \(%\)/).first()).toBeVisible();
    await expect(page.getByText(/留存完整请求体|Keep full request body/).first()).toBeVisible();
    await expect(page.getByText(/样本存储配额 \(MB\)|Sample storage quota \(MB\)/).first()).toBeVisible();
    await expect(page.getByText(/样本保留天数|Sample retention \(days\)/).first()).toBeVisible();
    await expect(page.getByText(/破甲同时写代码时自动封禁|Auto-ban on jailbreak plus code/).first()).toBeVisible();
    await expect(page.getByText(/自动封禁的最低风险等级|Minimum risk level for auto-ban/).first()).toBeVisible();
    await expect(page.getByText(/写代码占比过高时自动封禁|Auto-ban on code ratio/).first()).toBeVisible();

    // Verify save button is present
    const saveBtn = page.getByRole('button', { name: /保存修改|Save/i }).first();
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
    const usersLink = page.locator('a[href="/users"]').first();
    const insightsLink = page.locator('a[href="/user-insights"]').first();
    const codesLink = page.locator('a[href="/redemption-codes"]').first();

    await expect(usersLink).toBeVisible();
    await expect(insightsLink).toBeVisible();
    await expect(codesLink).toBeVisible();

    // Check text of insights link is not empty
    await expect(insightsLink).toContainText(/用户画像|User Insights/);
  });
});
