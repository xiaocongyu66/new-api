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
const { ModelRatioVisualEditor } = await import('../model-ratio-visual-editor')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'Unset price models': 'Unset price models',
        'No models with unset prices': 'No models with unset prices',
      },
    },
  },
})

type EditorProps = React.ComponentProps<typeof ModelRatioVisualEditor>

const baseProps: EditorProps = {
  savedModelPrice: '{}',
  savedModelRatio: '{"alpha-model":1}',
  savedCacheRatio: '{}',
  savedCreateCacheRatio: '{}',
  savedCompletionRatio: '{}',
  savedImageRatio: '{}',
  savedAudioRatio: '{}',
  savedAudioCompletionRatio: '{}',
  savedBillingMode: '{}',
  savedBillingExpr: '{}',
  modelPrice: '{}',
  modelRatio: '{"alpha-model":1}',
  cacheRatio: '{}',
  createCacheRatio: '{}',
  completionRatio: '{}',
  imageRatio: '{}',
  audioRatio: '{}',
  audioCompletionRatio: '{}',
  billingMode: '{}',
  billingExpr: '{}',
  candidateModelNames: ['alpha-model', 'beta-model'],
  onChange: () => {},
  onSave: () => {},
  isSaving: false,
}

type RenderedEditor = {
  container: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

async function renderEditor(
  overrides: Partial<EditorProps> = {}
): Promise<RenderedEditor> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <ModelRatioVisualEditor {...baseProps} {...overrides} />
      </I18nextProvider>
    )
  })

  return { container, root }
}

async function unmountEditor(rendered: RenderedEditor) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

function findUnsetFilterButton(container: HTMLElement): HTMLButtonElement {
  const button = [
    ...container.querySelectorAll<HTMLButtonElement>('button'),
  ].find((candidate) => candidate.textContent?.includes('Unset price models'))
  assert.ok(button, 'expected the unset price filter button to render')
  return button
}

describe('model ratio visual editor', () => {
  afterAll(async () => {
    await resetSharedDomWindow()
  })

  test('shows only models without saved pricing while the filter is active', async () => {
    const rendered = await renderEditor()

    assert.equal(rendered.container.textContent?.includes('alpha-model'), true)
    assert.equal(rendered.container.textContent?.includes('beta-model'), true)

    const filterButton = findUnsetFilterButton(rendered.container)
    assert.equal(filterButton.getAttribute('aria-pressed'), 'false')

    await act(async () => filterButton.click())

    const activeFilterButton = findUnsetFilterButton(rendered.container)
    assert.equal(activeFilterButton.getAttribute('aria-pressed'), 'true')
    assert.equal(rendered.container.textContent?.includes('alpha-model'), false)
    assert.equal(rendered.container.textContent?.includes('beta-model'), true)

    await act(async () => activeFilterButton.click())
    assert.equal(rendered.container.textContent?.includes('alpha-model'), true)

    await unmountEditor(rendered)
  })

  test('explains the empty result when every model has pricing', async () => {
    const rendered = await renderEditor({
      candidateModelNames: ['alpha-model'],
    })

    await act(async () => findUnsetFilterButton(rendered.container).click())

    assert.equal(
      rendered.container.textContent?.includes('No models with unset prices'),
      true
    )

    await unmountEditor(rendered)
  })
})
