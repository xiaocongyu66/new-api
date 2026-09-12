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
import { after, describe, test } from 'node:test'

import { domWindow, resetSharedDomWindow } from '@/test-utils/happy-dom-env'

// KaTeX/dompurify consult `document.compatMode`; force standards mode and
// drop the own property again in `afterAll` so the shared document keeps no
// residue from this file.
domWindow.document.write('<!doctype html><html><body></body></html>')
Object.defineProperty(domWindow.document, 'compatMode', {
  configurable: true,
  value: 'CSS1Compat',
})

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { Response } = await import('../response')
const { loadKatex } = await import('../../ui/katex')

async function renderResponse(content: string) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await loadKatex()

  await act(async () => {
    root.render(<Response final>{content}</Response>)
  })

  return { container, root }
}

describe('Response math rendering', () => {
  after(async () => {
    Reflect.deleteProperty(domWindow.document, 'compatMode')
    await resetSharedDomWindow()
  })

  test('renders inline, block, and math fenced formulas with KaTeX', async () => {
    const rendered = await renderResponse(
      'Inline $x^2$\n\n$$\nE = mc^2\n$$\n\n```math\nf(x) = x^2\n```'
    )
    assert.ok(rendered.container.querySelectorAll('.katex-html').length >= 3)

    await act(async () => rendered.root.unmount())
    rendered.container.remove()
  })
})
