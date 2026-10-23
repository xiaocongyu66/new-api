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
import { ROLE } from '@/lib/roles'

installDomGlobals()

// 单测只锁操作层契约；渠道类型图标来自 @lobehub/icons，其模块图在 bun 下会因
// @lobehub/ui 缺少 Tag 导出而加载失败，这里换成空实现。
mock.module('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
  LOBE_ICON_KEYS: [],
}))

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const { useAuthStore } = await import('@/stores/auth-store')
const { ChannelsProvider } = await import('../../channels-provider')
const { ChannelMutateDrawer } = await import('../channel-mutate-drawer')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

type ApiMethod = (
  url: string,
  config?: unknown
) => Promise<{ data: unknown }>
type MockableApi = {
  get: ApiMethod
  post: ApiMethod
}
type RenderedDrawer = {
  host: HTMLDivElement
  queryClient: InstanceType<typeof QueryClient>
  root: ReturnType<typeof createRoot>
}

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPost = apiClient.post
let renderedDrawer: RenderedDrawer | null = null

function installApiFixtures() {
  apiClient.get = async (url) => {
    switch (url) {
      case '/api/user/2fa/status':
        return { data: { success: true, data: { enabled: false } } }
      case '/api/user/passkey':
        return { data: { success: true, data: { enabled: false } } }
      case '/api/group/':
        return { data: { success: true, data: ['default'] } }
      case '/api/channel/models':
        return { data: { success: true, data: [] } }
      case '/api/prefill_group':
        return { data: { success: true, data: [] } }
      default:
        throw new Error(`Unexpected GET ${url}`)
    }
  }
  apiClient.post = async (url) => {
    throw new Error(`Unexpected POST ${url}`)
  }
}

async function waitForCondition(
  condition: () => boolean,
  failureMessage: string
): Promise<void> {
  if (condition()) return

  await new Promise<void>((resolve, reject) => {
    const observer = new MutationObserver(() => {
      if (!condition()) return
      clearTimeout(timeoutId)
      observer.disconnect()
      resolve()
    })
    const timeoutId = setTimeout(() => {
      observer.disconnect()
      reject(new Error(`${failureMessage}: ${document.body.textContent}`))
    }, 1500)

    observer.observe(document, {
      attributes: true,
      childList: true,
      characterData: true,
      subtree: true,
    })
  })
}

async function renderDrawer(onOpenChange: (open: boolean) => void) {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  renderedDrawer = { host, queryClient, root }

  await act(async () => {
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'root',
      role: ROLE.SUPER_ADMIN,
    })
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <ChannelsProvider>
            <ChannelMutateDrawer open onOpenChange={onOpenChange} />
          </ChannelsProvider>
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
  await act(async () =>
    waitForCondition(
      () => findActionButton('Save') !== null && document.querySelector('form') !== null,
      'channel drawer did not finish rendering'
    )
  )
}

function findActionButton(text: string): HTMLButtonElement | null {
  return (
    [
      ...document.querySelectorAll<HTMLButtonElement>('button'),
    ].find(
      (candidate) =>
        candidate.textContent?.trim() === text &&
        // 弹层自带的右上角关闭按钮只有 sr-only 文案，不能当成操作层的 Close。
        candidate.querySelector('.sr-only') === null
    ) ?? null
  )
}

function requireActionButton(text: string): HTMLButtonElement {
  const button = findActionButton(text)
  assert.ok(button, `Expected a "${text}" action button`)
  return button
}

afterEach(async () => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  useAuthStore.getState().auth.reset()
  if (renderedDrawer) {
    await act(async () => renderedDrawer?.root.unmount())
    renderedDrawer.queryClient.clear()
    renderedDrawer.host.remove()
    renderedDrawer = null
  }
  document.body.replaceChildren()
})

afterAll(async () => {
  await resetSharedDomWindow()
})

describe('channel edit drawer actions', () => {
  test('renders exactly Save and Cancel in the title row, outside the scrolling editor form', async () => {
    installApiFixtures()
    await renderDrawer(() => undefined)

    const form = document.querySelector('form')
    assert.ok(form, 'expected the editor form to render')
    const saveButton = requireActionButton('Save')
    const cancelButton = requireActionButton('Cancel')

    // 两个按钮同处页头操作组：与标题同行、位于最右侧，不在滚动表单里。
    assert.equal(saveButton.parentElement, cancelButton.parentElement)
    const actionGroup = saveButton.parentElement
    assert.ok(actionGroup, 'expected the header action group to exist')
    assert.equal(form.contains(actionGroup), false)
    assert.equal(
      actionGroup.closest('[data-slot="sheet-header"]') !== null,
      true,
      'actions must sit in the drawer header row'
    )

    // 右上角自带的 × 关闭按钮已删除，底部按钮条带（带边框和 backdrop-blur 的遮板）也不再存在。
    assert.equal(
      [...document.querySelectorAll('[data-slot="sheet-content"] button')].some(
        (button) => button.textContent?.trim() === 'Close'
      ),
      false,
      'the built-in X close button must be gone'
    )
    assert.equal(
      document.querySelector(
        '[data-slot="sheet-content"] [data-slot="sheet-footer"]'
      ),
      null
    )

    // 保存按钮仍然提交编辑器表单，取消按钮不会触发表单提交。
    assert.equal(saveButton.getAttribute('type'), 'submit')
    assert.equal(saveButton.getAttribute('form'), form.id)
    assert.equal(cancelButton.closest('form'), null)
  })

  test('Cancel closes the drawer without saving', async () => {
    installApiFixtures()
    const openChanges: boolean[] = []
    await renderDrawer((open) => openChanges.push(open))

    const cancelButton = requireActionButton('Cancel')
    await act(async () => cancelButton.click())

    assert.deepEqual(openChanges, [false])
  })
})
