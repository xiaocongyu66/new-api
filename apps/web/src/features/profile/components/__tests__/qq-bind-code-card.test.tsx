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
import assert from 'node:assert/strict'
import { afterAll, beforeEach, describe, mock, test } from 'bun:test'

import { Window } from 'happy-dom'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

type BindStatus = {
  qq_checkin_enabled: boolean
  bound: boolean
  qq_username?: string
}

let bindStatus: BindStatus = { qq_checkin_enabled: true, bound: true }
let unbindResult: { success: boolean; message?: string } = { success: true }
const unbindCalls: number[] = []

mock.module('../../api', () => ({
  getQQBindStatus: async () => ({ success: true, data: bindStatus }),
  generateQQBindCode: async () => ({
    success: true,
    data: { code: '#AbCdEf', expired_at: 0, expires_in: 180 },
  }),
  unbindQQ: async () => {
    unbindCalls.push(Date.now())
    // 解绑成功后状态接口应当报告未绑定，卡片据此换回验证码生成入口
    if (unbindResult.success) bindStatus = { ...bindStatus, bound: false }
    return unbindResult
  },
}))

const toastCalls: { level: string; message: string }[] = []
mock.module('sonner', () => ({
  toast: {
    success: (message: string) => toastCalls.push({ level: 'success', message }),
    error: (message: string) => toastCalls.push({ level: 'error', message }),
  },
}))

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } = await import(
  '@tanstack/react-query'
)
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QQBindCodeCard } = await import('../qq-bind-code-card')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'QQ bind code': 'QQ bind code',
        'Bound QQ account': 'Bound QQ account',
        Unbind: 'Unbind',
        'Confirm Unbind': 'Confirm Unbind',
        'QQ account unbound': 'QQ account unbound',
        'Failed to unbind QQ account': 'Failed to unbind QQ account',
        'Generate code': 'Generate code',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function findButton(scope: ParentNode, label: string) {
  return [...scope.querySelectorAll('button')].find(
    (button) => button.textContent?.trim() === label
  )
}

// 卡片的绑定状态来自 useQuery，首次渲染只拿到 loading 骨架，断言前必须让查询
// 落地并把随之而来的重渲染冲干净。
async function flushQueries() {
  for (let attempt = 0; attempt < 10; attempt++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }
}

async function renderCard() {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <QQBindCodeCard show />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
  await flushQueries()

  return {
    container,
    async cleanup() {
      await act(async () => root.unmount())
      container.remove()
      queryClient.clear()
    },
  }
}

describe('QQ bind code card unbind', () => {
  beforeEach(() => {
    bindStatus = { qq_checkin_enabled: true, bound: true }
    unbindResult = { success: true }
    unbindCalls.length = 0
    toastCalls.length = 0
  })

  afterAll(() => {
    domWindow.close()
  })

  test('unbinds only after the confirmation is accepted', async () => {
    const { container, cleanup } = await renderCard()

    const unbindButton = findButton(container, 'Unbind')
    assert.ok(unbindButton, '已绑定状态必须给出解绑入口')

    await act(async () => {
      unbindButton.click()
    })

    // 弹窗渲染在 portal 里，因此在 document 范围内查找确认按钮。
    const confirmButton = findButton(document.body, 'Confirm Unbind')
    assert.ok(confirmButton, '解绑必须经过确认弹窗')
    assert.equal(unbindCalls.length, 0, '仅打开弹窗不应发出解绑请求')

    await act(async () => {
      confirmButton.click()
    })

    assert.equal(unbindCalls.length, 1)
    assert.deepEqual(toastCalls, [
      { level: 'success', message: 'QQ account unbound' },
    ])

    await cleanup()
  })

  test('keeps the bound state and reports the server message when unbind fails', async () => {
    unbindResult = { success: false, message: 'QQ 签到功能未启用' }
    const { container, cleanup } = await renderCard()

    await act(async () => {
      findButton(container, 'Unbind')?.click()
    })
    await act(async () => {
      findButton(document.body, 'Confirm Unbind')?.click()
    })

    assert.equal(unbindCalls.length, 1)
    assert.deepEqual(toastCalls, [
      { level: 'error', message: 'QQ 签到功能未启用' },
    ])
    assert.ok(
      findButton(container, 'Unbind'),
      '解绑失败后必须保留解绑入口，不能把卡片切成未绑定状态'
    )

    await cleanup()
  })

  test('stays visible for a bound user after QQ check-in is switched off', async () => {
    bindStatus = { qq_checkin_enabled: false, bound: true }
    const { container, cleanup } = await renderCard()

    assert.ok(
      findButton(container, 'Unbind'),
      '签到关闭后已绑定用户仍需能自助解绑'
    )

    await cleanup()
  })

  test('hides itself for an unbound user once QQ check-in is off', async () => {
    bindStatus = { qq_checkin_enabled: false, bound: false }
    const { container, cleanup } = await renderCard()

    assert.equal(container.textContent, '')

    await cleanup()
  })
})
