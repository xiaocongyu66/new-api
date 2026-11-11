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
along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
const { TieredPricingEditor } = await import('../tiered-pricing-editor')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {},
    },
  },
})

// A len-condition fixed-price ladder, the shape production tiered models
// store; the visual editor cannot represent it, so it must load as raw text.
const LEN_LADDER =
  'len <= 10000 ? tier("t1", fixed(0.0140))\n: len <= 100000 ? tier("t2", fixed(0.0240))\n: tier("t4", fixed(0.0800))'

type EditorProps = React.ComponentProps<typeof TieredPricingEditor>

function renderEditor(props: EditorProps) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  act(() => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <TieredPricingEditor {...props} />
      </I18nextProvider>
    )
  })
  return { container, root }
}

describe('TieredPricingEditor', () => {
  afterAll(async () => {
    await resetSharedDomWindow()
  })

  test('loads an unvisual expression into the raw editor without overwriting it', async () => {
    const changes: string[] = []
    const rendered = renderEditor({
      modelName: 'expr-model',
      billingExpr: LEN_LADDER,
      requestRuleExpr: '',
      onBillingExprChange: (next) => changes.push(next),
      onRequestRuleExprChange: () => {},
    })

    // The raw editor must carry the stored expression, not the empty
    // default `p * 0 + c * 0` the visual fallback generates.
    const textareas = [
      ...rendered.container.querySelectorAll('textarea'),
    ]
    const rawText = textareas.map((area) => area.value).join('\n')
    assert.ok(
      rawText.includes('len <= 10000'),
      'the raw expression editor must show the stored len ladder'
    )

    // The mount-time propagation must not replace the stored expression
    // with the default until the admin actually edits something.
    assert.ok(
      changes.every((value) => value.includes('len <=')),
      `unexpected default overwrite on mount: ${JSON.stringify(changes)}`
    )

    await act(async () => rendered.root.unmount())
    rendered.container.remove()
  })
})
