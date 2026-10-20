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
import { afterAll, afterEach, beforeEach, describe, mock, test } from 'bun:test'
import assert from 'node:assert/strict'

import {
  installDomGlobals,
  resetSharedDomWindow,
} from '@/test-utils/happy-dom-env'

// `bun test` 全进程共用一个 happy-dom window：先钉住 DOM 全局，再加载 React 与组件。
installDomGlobals()

// 页签内容区的重组件与页头排布无关，换成空壳，单测只锁页头契约。
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
        'Model Pricing': 'Model Pricing',
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

let sectionChanges: string[] = []

async function renderChannelsPage(): Promise<HTMLDivElement> {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  rendered = { host, queryClient, root }

  await act(async () =>
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <Channels onSectionChange={(section) => sectionChanges.push(section)} />
        </I18nextProvider>
      </QueryClientProvider>
    )
  )

  return host
}

function getActionsTrigger(host: HTMLElement): HTMLElement {
  const trigger = host.querySelector<HTMLElement>(
    '[data-slot="dropdown-menu-trigger"]'
  )
  assert.ok(trigger, 'expected the collapsed channel actions trigger')
  return trigger
}

// 新建页签对无权限用户禁用，只有超级管理员才走得到真正的页签切换。
beforeEach(() => {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'root',
    role: ROLE.SUPER_ADMIN,
  })
})

afterEach(async () => {
  sectionChanges = []
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
  test('keeps the collapsed actions trigger on the title row', async () => {
    const host = await renderChannelsPage()

    const header = host.querySelector('header')
    assert.ok(header, 'expected the page header to render')
    const heading = header.querySelector('h1')
    assert.ok(heading, 'expected the page title to render')
    const tabList = header.querySelector('[data-slot="tabs-list"]')
    assert.ok(tabList, 'expected the tab bar to render')
    const trigger = getActionsTrigger(host)

    // 与标题共用同一个行容器：窄屏下它留在标题行，而不是掉到页签下面自成一行。
    assert.equal(heading.parentElement?.contains(trigger), true)
    assert.equal(trigger.closest('.hidden'), null)

    // 属于页头，而不是页签内容区。
    const panels = [...host.querySelectorAll('[data-slot="tabs-content"]')]
    assert.ok(panels.length > 0, 'expected the tab panels to render')
    assert.equal(panels.some((panel) => panel.contains(trigger)), false)

    // 排在页签行之前，即工具栏在页签上方。
    assert.ok(
      trigger.compareDocumentPosition(tabList) &
        Node.DOCUMENT_POSITION_FOLLOWING
    )
  })

  test('renders the page section tabs and reports section changes', async () => {
    const host = await renderChannelsPage()
    const sectionTabs = [...host.querySelectorAll('[data-slot="tabs-trigger"]')]
    const labels = sectionTabs.map((tab) => tab.textContent?.trim())
    assert.ok(
      labels.includes('Channels') && labels.includes('Model Pricing'),
      'expected the page section tabs to render'
    )

    const pricingTab = sectionTabs.find(
      (tab) => tab.textContent?.trim() === 'Model Pricing'
    )
    assert.ok(pricingTab, 'expected the model pricing section tab to render')
    await act(async () => pricingTab.click())
    assert.deepEqual(sectionChanges, ['model-pricing'])
  })

  test('hides the list toolbar once the create tab takes over', async () => {
    const host = await renderChannelsPage()
    const trigger = getActionsTrigger(host)

    // 页面上还有一层「渠道 / 模型定价」的分页导航，创建页签在页头里，
    // 因此按页头作用域查找，不要抓整页第一个页签。
    const createTab = host.querySelector<HTMLButtonElement>(
      'header [data-slot="tabs-trigger"]'
    )
    assert.ok(createTab, 'expected the create tab trigger to render')
    assert.equal(createTab.textContent?.includes('Create'), true)

    await act(async () => createTab.click())

    assert.notEqual(trigger.closest('.hidden'), null)
  })
})
