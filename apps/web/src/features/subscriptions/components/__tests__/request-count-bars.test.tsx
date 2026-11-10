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
import { afterAll, describe, test } from 'bun:test'
import assert from 'node:assert/strict'

import type React from 'react'

import { resetSharedDomWindow } from '@/test-utils/happy-dom-env'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')

let cleanup: () => void = () => {}
afterAll(() => cleanup())

async function renderWithI18n(element: React.ReactElement) {
  resetSharedDomWindow()
  const container = document.createElement('div')
  document.body.appendChild(container)
  const i18n = createInstance()
  await i18n.use(initReactI18next).init({
    lng: 'en',
    resources: {
      en: {
        translation: {
          'No request counters': 'No request counters',
        },
      },
    },
  })
  const root = createRoot(container)
  await act(async () => {
    root.render(<I18nextProvider i18n={i18n}>{element}</I18nextProvider>)
  })
  cleanup = () => {
    act(() => root.unmount())
    container.remove()
  }
  return container
}

describe('RequestCountBars', () => {
  test('renders one two-row entry per counter with used/total numbers', async () => {
    const { RequestCountBars } = await import('../request-count-bars')
    const container = await renderWithI18n(
      <RequestCountBars
        counters={[
          { id: 1, group_name: 'vip', model_name: 'gpt-4o', total: 2, used: 1 },
          { id: 2, group_name: 'vip', model_name: 'claude', total: 4, used: 4 },
        ]}
      />
    )
    const text = container.textContent || ''
    assert.ok(text.includes('vip · gpt-4o'), `missing label, got: ${text}`)
    assert.ok(text.includes('1/2'), `missing used/total, got: ${text}`)
    assert.ok(text.includes('4/4'), `missing exhausted counter, got: ${text}`)
    assert.ok(
      container.querySelector('[class*="text-red-500"]'),
      'exhausted counter should be red'
    )
  })

  test('shows the empty fallback when there are no counters', async () => {
    const { RequestCountBars } = await import('../request-count-bars')
    const container = await renderWithI18n(<RequestCountBars counters={[]} />)
    assert.match(container.textContent || '', /No request counters/)
  })

  test('guards against zero-total and missing counters without NaN', async () => {
    const { RequestCountBars } = await import('../request-count-bars')
    const container = await renderWithI18n(
      <RequestCountBars
        counters={[
          { id: 3, group_name: 'g', model_name: 'm', total: 0, used: 0 },
        ]}
      />
    )
    const text = container.textContent || ''
    assert.ok(text.includes('0/0'), `missing zero-total counter, got: ${text}`)
    assert.ok(!text.includes('NaN'), `NaN leaked into the UI: ${text}`)
  })
})
