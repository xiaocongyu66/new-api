/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { afterAll, afterEach, describe, mock, test } from 'bun:test'
import assert from 'node:assert/strict'

import {
  installDomGlobals,
  resetSharedDomWindow,
} from '@/test-utils/happy-dom-env'
import type { AuthUser } from '@/stores/auth-store'

// `bun test` 全进程共用一个 happy-dom window：先钉住 DOM 全局，再加载 React 与组件。
installDomGlobals()

// 页签内容区的重组件与页头排布无关，换成空壳，单测只锁页头契约。
// 相对路径与各组件内的 import 形式一致，mock.module 才能命中。
mock.module('../../../system-settings/models/model-pricing-section', () => ({
  ModelPricingSection: () => null,
}))
mock.module('../channel-list-tab', () => ({ ChannelListTab: () => null }))
mock.module('../channel-create-tab', () => ({ ChannelCreateTab: () => null }))
mock.module('../channels-dialogs', () => ({ ChannelsDialogs: () => null }))

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { ROLE } = await import('@/lib/roles')
const { useAuthStore } = await import('@/stores/auth-store')
const { Channels } = await import('@/features/channels')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'Channel Management': 'Channel Management',
        Create: 'Create',
        'Create Channel': 'Create Channel',
        Channels: 'Channels',
        'Batch Operations': 'Batch Operations',
        'Tag Mode': 'Tag Mode',
        'Sort by ID': 'Sort by ID',
        'Test All Channels': 'Test All Channels',
        'Update All Balances': 'Update All Balances',
        'Detect All Upstream Updates': 'Detect All Upstream Updates',
        'Apply All Upstream Updates': 'Apply All Upstream Updates',
        'Repair Channel Consistency': 'Repair Channel Consistency',
        'Delete All Disabled': 'Delete All Disabled',
        Pricing: 'Pricing',
        'Tool prices': 'Tool prices',
        'Upstream sync': 'Upstream sync',
      },
    },
  },
})

type RenderedPage = {
  host: HTMLDivElement
  queryClient: InstanceType<typeof QueryClient>
  root: ReturnType<typeof createRoot>
}

let rendered: RenderedPage | null = null

async function renderChannelsPage(
  user: Pick<AuthUser, 'id' | 'username' | 'role' | 'permissions'>
): Promise<HTMLDivElement> {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  rendered = { host, queryClient, root }

  await act(async () => {
    useAuthStore.getState().auth.setUser(user)
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <Channels />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })

  return host
}

function pageTabLabels(host: HTMLElement): string[] {
  const tabBar = host.querySelector('[data-slot="tabs-list"]')
  assert.ok(tabBar, 'expected the page tab bar to render')
  return [
    ...tabBar.querySelectorAll<HTMLButtonElement>('[data-slot="tabs-trigger"]'),
  ].map((tab) => tab.textContent?.trim() ?? '')
}

function getActionsTrigger(host: HTMLElement): HTMLElement {
  const trigger = host.querySelector<HTMLElement>(
    '[data-slot="dropdown-menu-trigger"]'
  )
  assert.ok(trigger, 'expected the collapsed channel actions trigger')
  return trigger
}

// 每个用例在 renderChannelsPage 里显式指定登录用户；这里只保证用例之间
// 不带上一轮的认证状态。
afterEach(async () => {
  useAuthStore.getState().auth.reset()
  if (rendered) {
    await act(async () => rendered?.root.unmount())
    rendered.queryClient.clear()
    rendered.host.remove()
    rendered = null
  }
  document.body.replaceChildren()
})

afterAll(async () => {
  await resetSharedDomWindow()
})

describe('channel page header actions', () => {
  test('keeps the collapsed actions trigger out of the tab content area', async () => {
    const host = await renderChannelsPage({
      id: 1,
      username: 'root',
      role: ROLE.SUPER_ADMIN,
    })

    const header = host.querySelector('header')
    assert.ok(header, 'expected the page header to render')
    const heading = header.querySelector('h1')
    assert.ok(heading, 'expected the page title to render')
    const tabList = header.querySelector('[data-slot="tabs-list"]')
    assert.ok(tabList, 'expected the tab bar to render')
    const trigger = getActionsTrigger(host)

    // 工具栏属于页头：放进页签内容区会在窄屏留下一整条只有「⋯」的独占行。
    assert.equal(header.contains(trigger), true)
    assert.equal(trigger.closest('.hidden'), null)

    // 属于页头，而不是页签内容区。
    const panels = [...host.querySelectorAll('[data-slot="tabs-content"]')]
    assert.ok(panels.length > 0, 'expected the tab panels to render')
    assert.equal(panels.some((panel) => panel.contains(trigger)), false)

    // 页签与标题同处第一行，工具栏排在其后（第二行）。
    assert.equal(heading.parentElement?.contains(tabList), true)
    assert.ok(
      tabList.compareDocumentPosition(trigger) &
        Node.DOCUMENT_POSITION_FOLLOWING
    )
  })

  test('renders all five page tabs in one row beside the title', async () => {
    const host = await renderChannelsPage({
      id: 1,
      username: 'root',
      role: ROLE.SUPER_ADMIN,
    })

    // 定价面板内部的三个子页签上提到页面页签行，与「创建 / 渠道」并列。
    // 创建页签同时挂窄屏/宽屏两个文案，textContent 会把两者拼起来。
    assert.deepEqual(pageTabLabels(host), [
      'CreateCreate Channel',
      'Channels',
      'Pricing',
      'Tool prices',
      'Upstream sync',
    ])

    const tabTriggers = [
      ...host.querySelectorAll<HTMLButtonElement>(
        '[data-slot="tabs-trigger"]'
      ),
    ]
    const pricingTab = tabTriggers.find(
      (tab) => tab.textContent?.trim() === 'Pricing'
    )
    assert.ok(pricingTab, 'expected the pricing tab to render')
    assert.equal(
      pricingTab.getAttribute('aria-selected'),
      'false',
      'pricing is not the default tab'
    )

    await act(async () => pricingTab.click())
    assert.equal(pricingTab.getAttribute('aria-selected'), 'true')
  })

  test('keeps the tab row beside the page title', async () => {
    const host = await renderChannelsPage({
      id: 1,
      username: 'root',
      role: ROLE.SUPER_ADMIN,
    })

    const title = host.querySelector('h1')
    assert.ok(title, 'expected the page title to render')
    const tabList = host.querySelector('[data-slot="tabs-list"]')
    assert.ok(tabList, 'expected the page tab bar to render')

    // 页签与标题处于同一个行容器，窄屏下整行 wrap，不会掉到工具栏下面。
    assert.equal(title.parentElement?.contains(tabList), true)

    // 工具栏在页签之后（第二行），不再与页签抢标题那一格。
    const trigger = getActionsTrigger(host)
    assert.ok(
      tabList.compareDocumentPosition(trigger) &
        Node.DOCUMENT_POSITION_FOLLOWING
    )
  })

  test('hides the pricing tabs without the system settings permission', async () => {
    const host = await renderChannelsPage({
      id: 2,
      username: 'admin',
      role: ROLE.ADMIN,
      permissions: { admin_permissions: {} },
    })

    // 定价读写 /api/option，后端要 system.settings；没权限就不该看到入口，
    // 也不该为多出来的三个页签留下空格。
    assert.deepEqual(pageTabLabels(host), ['CreateCreate Channel', 'Channels'])
  })

  test('hides the list toolbar once the create tab takes over', async () => {
    const host = await renderChannelsPage({
      id: 1,
      username: 'root',
      role: ROLE.SUPER_ADMIN,
    })
    const trigger = getActionsTrigger(host)

    const createTab = host.querySelector<HTMLButtonElement>(
      'header [data-slot="tabs-trigger"]'
    )
    assert.ok(createTab, 'expected the create tab trigger to render')
    assert.equal(createTab.textContent?.includes('Create'), true)

    await act(async () => createTab.click())

    assert.notEqual(trigger.closest('.hidden'), null)
  })
})
